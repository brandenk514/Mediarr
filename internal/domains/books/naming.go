// Package books naming (Readarr B5): the book naming template engine.
//
// This is the "genuinely-new backend piece" of B5: a working renderer for the
// Readarr book naming tag set — the brace-delimited author / book /
// release-year / medium / quality / media-info tokens. It is a pure function
// over a plain-data context (no DB, no I/O), so the import/rename pipeline can
// call it and tests can drive it directly.
//
// The token set and its semantics follow the archived Readarr FileNameBuilder
// (final release v0.4.18.2805) and the parity plan's Media-Management naming
// bullet (docs/readarr-parity-plan.md §2):
//
//	Author   {Author Name} {Author CleanName} {Author NameThe}
//	           {Author SortName} {Author NameFirstCharacter}
//	           {Author Disambiguation}
//	Book     {Book Title} {Book CleanTitle} {Book TitleThe}
//	           {Book TitleNoSub} {Book CleanTitleNoSub} {Book TitleTheNoSub}
//	           {Book Subtitle} {Book CleanSubtitle} {Book SubtitleThe}
//	           {Book Disambiguation} {Book Series} {Book SeriesPosition}
//	           {Book SeriesTitle} {Book Type}
//	Year     {Release Year} {Release YearFirst} {Edition Year}
//	Medium   {medium:NN} {PartNumber} {PartNumber:NN} {PartCount}
//	           {PartCount:NN} {Medium Format}
//	Quality  {Quality Full} {Quality Title} {Quality Proper}
//	File     {Original Title} {Original Filename} {Release Group}
//	Audio    {MediaInfo AudioCodec} {MediaInfo AudioChannels}
//	           {MediaInfo AudioBitRate} {MediaInfo AudioBitsPerSample}
//	           {MediaInfo AudioSampleRate}
//	Custom   {Custom Formats}
//
// Rendering rules (mirroring Readarr):
//   - the template is split on "/" and "\" into path components;
//   - each component renders its tokens, collapses repeated separators, trims
//     trailing separators, and is dropped when empty;
//   - a token that has no value for this book (e.g. {Author Disambiguation} on
//     an unambiguous author) renders to nothing — it does not leave a gap;
//   - an all-lowercase token name lowercases its value, an all-uppercase token
//     name uppercases it (e.g. {Book Title} is case-preserving, {book title}
//     lowercases);
//   - an internal separator in a multi-word token (e.g. {Book_Title}) replaces
//     the spaces in the value with that separator;
//   - illegal characters are cleaned per ReplaceIllegalCharacters /
//     ColonReplacement, spaces per SpaceHandling, and the whole name per
//     CaseHandling.
package books

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// NamingOptions is the book naming engine's options (the books-specific subset
// of the media-management book naming block). The service layer maps the
// settings document into this; the engine stays decoupled from settings types.
type NamingOptions struct {
	// RenameBooks, when false, makes RenderBookFileName return the original
	// filename (no template is applied).
	RenameBooks bool
	// ReplaceIllegalCharacters, when true, replaces filesystem-illegal
	// characters; when false they are stripped.
	ReplaceIllegalCharacters bool
	// ColonReplacement is the single character (or empty = remove) used for
	// ':' in titles, applied when ReplaceIllegalCharacters is true.
	ColonReplacement string
	// SpaceHandling: "space" (default), "period", "underscore", "dash".
	SpaceHandling string
	// CaseHandling: "default" (default), "uppercase", "lowercase".
	CaseHandling string
}

// DefaultNamingOptions is the engine's default options (rename on, replace
// illegal chars, no colon replacement, space + default case).
var DefaultNamingOptions = NamingOptions{
	RenameBooks:              true,
	ReplaceIllegalCharacters: true,
	ColonReplacement:         "",
	SpaceHandling:            "space",
	CaseHandling:             "default",
}

// BookNamingContext is the plain data a naming template can render from. The
// service fills it from author / title / edition / book file / quality / media
// info; the engine treats it as opaque input. Zero/empty fields mean "unknown"
// and render to nothing (or "Unknown" for the year tokens).
type BookNamingContext struct {
	// Author
	AuthorName           string
	AuthorSortName       string // "Last, First"; "" = unset
	AuthorDisambiguation string
	// Book / edition
	BookTitle          string // edition title
	BookSubtitle       string // "" = no subtitle
	BookDisambiguation string
	BookType           string // "Book" / "Audiobook" / ...; "" = unset
	// Series (the book's primary series)
	SeriesTitle    string
	SeriesPosition string // "1", "2.5", ...; "" = not in a series
	// Release dates (0 = unknown)
	ReleaseYear      int
	ReleaseYearFirst int // original / first edition year
	EditionYear      int
	// File
	PartNumber       int    // 1..PartCount; 0 = single part
	PartCount        int    // 1 = no part tokens render
	QualityTitle     string // e.g. "AZW3", "MP3 320"
	QualityProper    string // "" | "Proper" | "Repack"
	OriginalTitle    string // scene name, or the original filename
	OriginalFilename string // original filename without extension
	ReleaseGroup     string // "" = unknown (renders "Readarr")
	// Media info (audio)
	MediaInfoAudioCodec         string
	MediaInfoAudioChannels      string
	MediaInfoAudioBitRate       string
	MediaInfoAudioBitsPerSample string
	MediaInfoAudioSampleRate    string
	// Custom formats (names, space-joined by {Custom Formats})
	CustomFormats []string
}

// tokenHandler renders one token value for the context, reporting whether the
// token applies to this book (false = render nothing).
type tokenHandler func(BookNamingContext) (string, bool)

// tokenRe matches one brace-delimited token: an optional prefix, a one- or
// two-word name, an optional ":customFormat" (e.g. {PartNumber:02}), and an
// optional suffix.
var tokenRe = regexp.MustCompile(
	`(?i)\{(?P<prefix>[- ._[(]*)(?P<name>[a-z0-9]+(?:[- ._]+[a-z0-9]+)?)(?::(?P<customFormat>[a-z0-9]+))?(?P<suffix>[- ._)\]]*)\}`)

// separatorRe finds the internal separator between a two-word token's words.
var separatorRe = regexp.MustCompile(`[- ._]+`)

// collapseSeparators / trailingSepRe / illegalCharRe are the filename-cleanup
// helpers (the Readarr FileNameCleanupRegex / TrimSeparatorsRegex equivalents).
// collapseSeparators replaces runs of the same separator char (one of
// "-._  ") with a single occurrence — the RE2-safe equivalent of
// `([- ._])\1+`.
func collapseSeparators(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prev := rune(-1)
	for _, r := range s {
		if isCollapseSep(r) && r == prev {
			continue // run of the same separator: drop
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}

func isCollapseSep(r rune) bool {
	return r == '-' || r == '_' || r == '.' || r == ' '
}

// trailingSepRe / illegalCharRe are the remaining filename-cleanup regexes.
// trailingSepRe mirrors Readarr's TrimSeparatorsRegex (one trailing separator).
var (
	trailingSepRe = regexp.MustCompile(`[- ._]$`)
	illegalCharRe = regexp.MustCompile(`[\\<>?"|]`)
)

// cleanTitle is the Readarr CleanTitle equivalent:
//
//	title = title.Replace("&", "and")
//	title = ScenifyReplaceChars.Replace(title, " ")   // [\/] → " "
//	title = ScenifyRemoveChars.Replace(title, "")
//
// ScenifyRemoveChars drops: (a) the punctuation set , < > / \ ; : ' " | ` ~ !
// ? @ $ % ^ * - _ = when surrounded by whitespace on both sides; (b) ' : ? ,
// when immediately followed by "s"/"m"+space, a space, or end-of-string; (c)
// all ( ) [ ] { } brackets. Dots are NOT removed ("K." is preserved).
//
// Go's RE2 has no lookbehind/lookahead, so the three rules are applied with a
// rune scan instead of the C# regex.
var cleanReplaceRe = regexp.MustCompile(`[/\\]`)

var cleanGroup1 = map[rune]bool{
	',': true, '<': true, '>': true, '/': true, '\\': true, ';': true,
	':': true, '\'': true, '"': true, '|': true, '`': true, '~': true,
	'!': true, '?': true, '@': true, '$': true, '%': true, '^': true,
	'*': true, '-': true, '_': true, '=': true,
}

var cleanGroup2 = map[rune]bool{
	'\'': true, ':': true, '?': true, ',': true,
}

func isCleanWS(r rune) bool {
	return r == ' ' || r == '	' || r == '\n' || r == '\r'
}

func cleanTitle(s string) string {
	s = strings.NewReplacer("&", "and").Replace(s)
	s = cleanReplaceRe.ReplaceAllString(s, " ")
	runes := []rune(s)
	n := len(runes)
	var out []rune
	for i, r := range runes {
		switch r {
		case '(', ')', '[', ']', '{', '}': // rule (c)
			continue
		}
		if cleanGroup1[r] {
			prevWS := i > 0 && isCleanWS(runes[i-1])
			nextWS := i+1 < n && isCleanWS(runes[i+1])
			if prevWS && nextWS { // rule (a)
				continue
			}
		}
		if cleanGroup2[r] {
			if i+1 >= n { // followed by end-of-string
				continue
			}
			if isCleanWS(runes[i+1]) { // followed by whitespace
				continue
			}
			if i+2 < n && (runes[i+1] == 's' || runes[i+1] == 'm') && isCleanWS(runes[i+2]) {
				// followed by "s " or "m " (possessive)
				continue
			}
		}
		out = append(out, r)
	}
	return string(out)
}

// titleThe moves a leading "The"/"An"/"A" article to the end (the Readarr
// TitleThe equivalent): "The Dispossessed (2016)" → "Dispossessed, The (2016)".
var titlePrefixRe = regexp.MustCompile(`(?i)^(The|An|A)\s+(.*?)(\s*\([^)]+\))*$`)

func titleThe(s string) string {
	trimmed := strings.TrimSpace(s)
	m := titlePrefixRe.FindStringSubmatch(trimmed)
	if m == nil {
		return s
	}
	return strings.TrimRight(m[2], " ") + ", " + m[1] + m[3]
}

// titleTheFirstChar returns the uppercased first character of the author's
// TitleThe name (the {Author NameFirstCharacter} token).
func authorFirstChar(name string) string {
	t := titleThe(name)
	t = strings.TrimLeft(t, " \t")
	if t == "" {
		return ""
	}
	r := []rune(t)[0]
	return string(unicode.ToUpper(r))
}

// padInt renders n zero-padded to width w (the ":NN" custom format, e.g.
// {PartNumber:02} → "01"). width 0 renders n plainly.
func padInt(n, width int) string {
	if width <= 0 {
		return strconv.Itoa(n)
	}
	return fmt.Sprintf("%0*d", width, n)
}

// isAllZeros reports whether s is non-empty and all '0' digits (a valid
// ":NN" zero-width custom format).
func isAllZeros(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != '0' {
			return false
		}
	}
	return true
}

// tokenValues builds the token→handler table for the context. Keys are stored
// lowercased so lookup is case-insensitive, mirroring Readarr's
// FileNameBuilderTokenEqualityComparer ({Book Title}, {BOOK TITLE} and
// {book title} all resolve to the same handler).
func tokenValues() map[string]tokenHandler {
	raw := map[string]tokenHandler{
		// Author
		"{Author Name}":               func(c BookNamingContext) (string, bool) { return c.AuthorName, c.AuthorName != "" },
		"{Author CleanName}":          func(c BookNamingContext) (string, bool) { v := cleanTitle(c.AuthorName); return v, v != "" },
		"{Author NameThe}":            func(c BookNamingContext) (string, bool) { v := titleThe(c.AuthorName); return v, c.AuthorName != "" },
		"{Author SortName}":           func(c BookNamingContext) (string, bool) { return c.AuthorSortName, c.AuthorSortName != "" },
		"{Author NameFirstCharacter}": func(c BookNamingContext) (string, bool) { v := authorFirstChar(c.AuthorName); return v, v != "" },
		"{Author Disambiguation}":     func(c BookNamingContext) (string, bool) { return c.AuthorDisambiguation, c.AuthorDisambiguation != "" },
		// Book
		"{Book Title}":      func(c BookNamingContext) (string, bool) { return c.BookTitle, c.BookTitle != "" },
		"{Book CleanTitle}": func(c BookNamingContext) (string, bool) { v := cleanTitle(c.BookTitle); return v, v != "" },
		"{Book TitleThe}":   func(c BookNamingContext) (string, bool) { v := titleThe(c.BookTitle); return v, c.BookTitle != "" },
		"{Book TitleNoSub}": func(c BookNamingContext) (string, bool) { v := titleNoSub(c); return v, v != "" },
		"{Book CleanTitleNoSub}": func(c BookNamingContext) (string, bool) {
			v := cleanTitle(titleNoSub(c))
			return v, titleNoSub(c) != ""
		},
		"{Book TitleTheNoSub}": func(c BookNamingContext) (string, bool) { v := titleThe(titleNoSub(c)); return v, titleNoSub(c) != "" },
		"{Book Subtitle}":      func(c BookNamingContext) (string, bool) { return c.BookSubtitle, c.BookSubtitle != "" },
		"{Book CleanSubtitle}": func(c BookNamingContext) (string, bool) {
			v := cleanTitle(c.BookSubtitle)
			return v, c.BookSubtitle != ""
		},
		"{Book SubtitleThe}": func(c BookNamingContext) (string, bool) {
			v := titleThe(c.BookSubtitle)
			return v, c.BookSubtitle != ""
		},
		"{Book Disambiguation}": func(c BookNamingContext) (string, bool) { return c.BookDisambiguation, c.BookDisambiguation != "" },
		"{Book Series}":         func(c BookNamingContext) (string, bool) { return c.SeriesTitle, c.SeriesTitle != "" },
		"{Book SeriesPosition}": func(c BookNamingContext) (string, bool) { return c.SeriesPosition, c.SeriesPosition != "" },
		"{Book SeriesTitle}":    func(c BookNamingContext) (string, bool) { return seriesTitle(c), c.SeriesTitle != "" },
		"{Book Type}":           func(c BookNamingContext) (string, bool) { return c.BookType, c.BookType != "" },
		// Year
		"{Release Year}":      func(c BookNamingContext) (string, bool) { return yearOrUnknown(c.ReleaseYear), true },
		"{Release YearFirst}": func(c BookNamingContext) (string, bool) { return yearOrUnknown(c.ReleaseYearFirst), true },
		"{Edition Year}":      func(c BookNamingContext) (string, bool) { return yearOrUnknown(c.EditionYear), true },
		// Medium / parts
		"{PartNumber}": func(c BookNamingContext) (string, bool) { return partValue(c, c.PartNumber, 0), c.PartCount > 1 },
		"{PartCount}":  func(c BookNamingContext) (string, bool) { return partValue(c, c.PartCount, 0), c.PartCount > 1 },
		"{medium}":     func(c BookNamingContext) (string, bool) { return partValue(c, c.PartNumber, 0), c.PartCount > 1 },
		"{Medium Format}": func(c BookNamingContext) (string, bool) {
			if c.PartCount <= 1 {
				return "", false
			}
			return "CD " + partValue(c, c.PartNumber, 0), true
		},
		// Quality
		"{Quality Full}":   func(c BookNamingContext) (string, bool) { return c.QualityTitle, c.QualityTitle != "" },
		"{Quality Title}":  func(c BookNamingContext) (string, bool) { return c.QualityTitle, c.QualityTitle != "" },
		"{Quality Proper}": func(c BookNamingContext) (string, bool) { return c.QualityProper, c.QualityProper != "" },
		// File
		"{Original Title}":    func(c BookNamingContext) (string, bool) { return originalTitle(c), originalTitle(c) != "" },
		"{Original Filename}": func(c BookNamingContext) (string, bool) { return c.OriginalFilename, c.OriginalFilename != "" },
		"{Release Group}":     func(c BookNamingContext) (string, bool) { return releaseGroup(c), true },
		// Media info (audio)
		"{MediaInfo AudioCodec}": func(c BookNamingContext) (string, bool) { return c.MediaInfoAudioCodec, c.MediaInfoAudioCodec != "" },
		"{MediaInfo AudioChannels}": func(c BookNamingContext) (string, bool) {
			return c.MediaInfoAudioChannels, c.MediaInfoAudioChannels != ""
		},
		"{MediaInfo AudioBitRate}": func(c BookNamingContext) (string, bool) {
			return c.MediaInfoAudioBitRate, c.MediaInfoAudioBitRate != ""
		},
		"{MediaInfo AudioBitsPerSample}": func(c BookNamingContext) (string, bool) {
			return c.MediaInfoAudioBitsPerSample, c.MediaInfoAudioBitsPerSample != ""
		},
		"{MediaInfo AudioSampleRate}": func(c BookNamingContext) (string, bool) {
			return c.MediaInfoAudioSampleRate, c.MediaInfoAudioSampleRate != ""
		},
		// Custom formats
		"{Custom Formats}": func(c BookNamingContext) (string, bool) {
			v := strings.Join(c.CustomFormats, " ")
			return v, v != ""
		},
	}
	m := make(map[string]tokenHandler, len(raw))
	for k, v := range raw {
		m[strings.ToLower(k)] = v
	}
	return m
}

// titleNoSub splits the title at the first colon and returns the part before it
// (the {Book *NoSub} tokens); the full title when there is no colon.
func titleNoSub(c BookNamingContext) string {
	if i := strings.Index(c.BookTitle, ":"); i >= 0 {
		return strings.TrimSpace(c.BookTitle[:i])
	}
	return c.BookTitle
}

// seriesTitle renders "SeriesTitle #Position" (the {Book SeriesTitle} token).
func seriesTitle(c BookNamingContext) string {
	if c.SeriesPosition != "" {
		return c.SeriesTitle + " #" + c.SeriesPosition
	}
	return c.SeriesTitle
}

func yearOrUnknown(y int) string {
	if y <= 0 {
		return "Unknown"
	}
	return strconv.Itoa(y)
}

// partValue renders a part number, applying the token's ":NN" custom format
// when present (customFormat "" → plain, "0" → 1 digit, "02" → 2 digits).
func partValue(c BookNamingContext, n int, width int) string {
	return padInt(n, width)
}

func originalTitle(c BookNamingContext) string {
	if c.OriginalTitle != "" {
		return c.OriginalTitle
	}
	return c.OriginalFilename
}

func releaseGroup(c BookNamingContext) string {
	if c.ReleaseGroup != "" {
		return c.ReleaseGroup
	}
	return "Readarr"
}

// renderToken renders one parsed token to its (uncleaned) value, applying the
// token-name case rule, the ":NN" part padding, and the internal-separator
// rule. It returns "" when the token has no value for the context.
func renderToken(m map[string]tokenHandler, g tokenGroups, c BookNamingContext) string {
	// Canonical key: the token's words joined by a single space (so
	// {Book_Title} and {Book Title} resolve to the same handler). The key is
	// lowercased for a case-insensitive lookup (Readarr's comparer).
	words := separatorRe.Split(g.name, -1)
	canonical := strings.ToLower("{" + strings.Join(words, " ") + "}")

	h, ok := m[canonical]
	if !ok {
		return ""
	}
	v, present := h(c)
	if !present {
		return ""
	}
	value := strings.TrimSpace(v)
	if value == "" {
		return ""
	}

	// ":NN" custom format zero-pads the part/medium number tokens: the
	// custom format is a run of zeros whose length is the minimum digit
	// count ({medium:0} → "1", {medium:00} → "01").
	if isAllZeros(g.custom) {
		if n, perr := strconv.Atoi(value); perr == nil {
			value = padInt(n, len(g.custom))
		}
	}

	// Token-name case rule: all-lowercase → lowercase value, all-uppercase →
	// uppercase value, mixed → as-is.
	if allCase(g.name, true) {
		value = strings.ToLower(value)
	} else if allCase(g.name, false) {
		value = strings.ToUpper(value)
	}

	// Internal separator: {Book_Title} → "Book Title" value with spaces
	// replaced by "_".
	if g.sep != "" {
		value = strings.ReplaceAll(value, " ", g.sep)
	}

	// Wrap the value in the token's prefix/suffix (e.g. "[{Author Name}]"
	// keeps the brackets around the rendered name).
	return g.prefix + value + g.suffix
}

// allCase reports whether every letter in s is lowercase (lower=true) or
// uppercase (lower=false).
func allCase(s string, lower bool) bool {
	anyLetter := false
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		anyLetter = true
		if lower && !unicode.IsLower(r) {
			return false
		}
		if !lower && !unicode.IsUpper(r) {
			return false
		}
	}
	return anyLetter
}

// cleanFileName applies the illegal-character and colon cleanup to a rendered
// value (the Readarr CleanFileName equivalent).
func cleanFileName(s string, o NamingOptions) string {
	if o.ReplaceIllegalCharacters {
		if o.ColonReplacement != "" {
			s = strings.ReplaceAll(s, ":", o.ColonReplacement)
		} else {
			s = strings.ReplaceAll(s, ":", "")
		}
		repl := strings.NewReplacer(
			"\\", "+", "/", "+", "<", "", ">", "", "?", "!",
			"*", "-", "|", "", "\"", "",
		)
		s = repl.Replace(s)
	} else {
		s = strings.ReplaceAll(s, ":", "")
		s = illegalCharRe.ReplaceAllString(s, "")
	}
	return strings.Trim(s, " ")
}

// cleanFolderName collapses repeated separators and trims (the Readarr
// CleanFolderName equivalent).
func cleanFolderName(s string) string {
	s = collapseSeparators(s)
	return strings.Trim(s, " .")
}

// tokenGroups are the parsed parts of one matched token.
type tokenGroups struct {
	prefix string
	name   string
	sep    string
	custom string
	suffix string
}

// parseToken extracts the token's groups from a full brace match.
func parseToken(tok string) (tokenGroups, bool) {
	sm := tokenRe.FindStringSubmatch(tok)
	if sm == nil {
		return tokenGroups{}, false
	}
	names := tokenRe.SubexpNames()
	g := tokenGroups{}
	for i, n := range names {
		if i == 0 || sm[i] == "" {
			continue
		}
		switch n {
		case "prefix":
			g.prefix = sm[i]
		case "name":
			g.name = sm[i]
		case "customFormat":
			g.custom = sm[i]
		case "suffix":
			g.suffix = sm[i]
		}
	}
	g.sep = internalSeparator(g.name)
	return g, true
}

// internalSeparator returns the separator run between a two-word token's words
// ("_" for {Book_Title}, "" for a single word or a plain-space token).
func internalSeparator(name string) string {
	for i := 0; i < len(name); i++ {
		if name[i] == '_' || name[i] == '-' || name[i] == '.' {
			// Consume the whole separator run.
			j := i
			for j < len(name) && (name[j] == '_' || name[j] == '-' || name[j] == ' ' || name[j] == '.') {
				j++
			}
			return name[i:j]
		}
	}
	return ""
}

// renderComponent renders one path component (between separators) by replacing
// its tokens, then cleans the result.
func renderComponent(o NamingOptions, comp string, c BookNamingContext) string {
	m := tokenValues()
	out := tokenRe.ReplaceAllStringFunc(comp, func(tok string) string {
		g, ok := parseToken(tok)
		if !ok {
			return tok
		}
		return cleanFileName(renderToken(m, g, c), o)
	})
	out = collapseSeparators(out)
	out = trailingSepRe.ReplaceAllString(out, "")
	return strings.TrimSpace(out)
}

// RenderBookFileName renders the book's standard naming template to a
// (relative) file path without extension. When RenameBooks is false it returns
// the original filename (so the caller keeps the release name). The result
// excludes the file extension; the caller appends it.
func RenderBookFileName(o NamingOptions, template string, c BookNamingContext) (string, error) {
	if !o.RenameBooks {
		return originalTitle(c), nil
	}
	if strings.TrimSpace(template) == "" {
		return "", fmt.Errorf("books: naming: template is empty")
	}
	components := splitPathTemplate(template)
	var parts []string
	for _, comp := range components {
		r := renderComponent(o, comp, c)
		if r != "" {
			parts = append(parts, r)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("books: naming: template %q rendered to nothing", template)
	}
	return Finalise(strings.Join(parts, "/"), o), nil
}

// RenderAuthorFolder renders the author folder format template to a (relative)
// folder path. Only author tokens are meaningful here; book/file tokens render
// to nothing.
func RenderAuthorFolder(o NamingOptions, template string, c BookNamingContext) (string, error) {
	if strings.TrimSpace(template) == "" {
		return "", fmt.Errorf("books: naming: author folder template is empty")
	}
	components := splitPathTemplate(template)
	var parts []string
	for _, comp := range components {
		r := renderComponent(o, comp, c)
		r = cleanFolderName(r)
		if r != "" {
			parts = append(parts, r)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("books: naming: author folder template %q rendered to nothing", template)
	}
	return strings.Join(parts, "/"), nil
}

// splitPathTemplate splits a template on "/" and "\" (Readarr's component
// split), dropping empty pieces.
func splitPathTemplate(t string) []string {
	// Normalise backslashes, then split.
	t = strings.ReplaceAll(t, `\`, "/")
	var parts []string
	for _, p := range strings.Split(t, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// applySpaceHandling replaces spaces per the configured space handling.
func applySpaceHandling(s string, handling string) string {
	switch handling {
	case "period":
		return strings.ReplaceAll(s, " ", ".")
	case "underscore":
		return strings.ReplaceAll(s, " ", "_")
	case "dash":
		return strings.ReplaceAll(s, " ", "-")
	default: // "space" or unknown
		return s
	}
}

// applyCaseHandling applies the configured global case handling.
func applyCaseHandling(s string, handling string) string {
	switch handling {
	case "uppercase":
		return strings.ToUpper(s)
	case "lowercase":
		return strings.ToLower(s)
	default:
		return s
	}
}

// Finalise applies the space/case handling to a fully rendered name (the
// caller invokes it after RenderBookFileName/RenderAuthorFolder).
func Finalise(name string, o NamingOptions) string {
	name = applySpaceHandling(name, o.SpaceHandling)
	name = applyCaseHandling(name, o.CaseHandling)
	return name
}
