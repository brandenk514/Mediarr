package tv

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// parse.go implements scene release-name parsing for TV.
//
// Per the plan (PLAN §14), M2 uses a pragmatic token-table parser rather than
// a full grammar: peel off the scene group in brackets, split on delimiters,
// classify each token (season/episode, multi-episode, multi-part, year, height,
// codec, source), and treat the remaining leading tokens as the title. We expand
// from real-world failures, not upfront completeness.
//
// Unlike the movie parser — which is deliberately tolerant and never errors —
// the TV parser returns a *typed error* when a name has no season/episode
// marker (PLAN §14 acceptance: "ambiguous/garbage names return a typed error,
// not a silent guess"). We can't infer a TV episode from a bare title, so
// guessing would be worse than refusing.

var (
	// A bare season-pack token: S01 / Season 1 (often followed by COMPLETE).
	reSeasonOnly = regexp.MustCompile(`(?i)^S\s?(\d{1,2})$`)
	// A lone year (used for the year/title fallback path).
	reYear = regexp.MustCompile(`^(1[89][0-9]{2}|20[0-9]{2})$`)
	// Height: 1080p / 2160p.
	reHeight = regexp.MustCompile(`^([0-9]{2,4})p$`)
	// Audio codecs appear as bare tokens: "DDP5.1" -> "DDP5","1"; "AAC".
	reAudio = regexp.MustCompile(`^(ddp|aac|ac3|eac3|dts|dtsx|flac|opus|atmos|truehd|mlp|lpcm)([0-9]{0,3})?$`)
)

// codecTokens maps normalised token text to a canonical codec.
var codecTokens = map[string]string{
	"x264": CodecH264, "264": CodecH264, "h264": CodecH264, "avc": CodecH264,
	"x265": CodecH265, "265": CodecH265, "h265": CodecH265, "hevc": CodecH265,
	"av1":   CodecAV1,
	"mpeg2": CodecMPEG2, "mpeg-2": CodecMPEG2,
	"xvid": "xvid", "divx": "divx",
}

// sourceTokens maps normalised token text to a canonical source.
var sourceTokens = map[string]string{
	"bd": "blu-ray", "blu-ray": "blu-ray", "blu": "blu-ray", "bluray": "blu-ray",
	"brrip": "blu-ray", "bdrip": "blu-ray",
	"web": "web", "webrip": "web", "webdl": "web", "web-dl": "web",
	"hdtv": "hdtv", "dvdrip": "dvd", "dvd": "dvd",
}

// ErrNoEpisodeMarker is returned when a release name contains no season/episode
// information. Callers should treat this as "not a parseable TV episode" rather
// than guessing. It is the typed error required by PLAN §14.
var ErrNoEpisodeMarker = errors.New("tv: parse: no season/episode marker found")

// ParseRelease parses a scene release name into structured TV fields.
//
// It succeeds when it can extract at least a season and either episode(s) or a
// season-pack marker. It returns ErrNoEpisodeMarker (wrapped with context) when
// the name has no episode information — the caller decides how to handle a
// non-episode release. It never silently guesses an episode number.
func ParseRelease(name string) (ParsedRelease, error) {
	out := ParsedRelease{Codec: CodecAny}
	orig := strings.TrimSpace(name)

	// Peel off a trailing bracketed scene group (e.g. "[FLYt3R]").
	name = orig
	if i := strings.LastIndex(name, "["); i >= 0 {
		name = strings.TrimSpace(name[:i])
	}

	rawTokens := tokenize(name)
	// Expand dashed/underscored multi-episode ranges at the token level: a
	// complete "SxxEyy" token immediately followed by a bare 1-2 digit token is
	// treated as a range (S01E01-04 -> S01E01 S01E02 S01E03 S01E04). A height
	// like "1080p" carries a letter and a bare "1080" is 4 digits, so neither is
	// mistaken for a range end. This runs before the classification loop.
	rawTokens = expandRangeTokens(rawTokens)

	var titleTokens []string
	seenMarker := false

	for i := 0; i < len(rawTokens); i++ {
		tok := rawTokens[i]
		norm := strings.ToLower(tok)
		norm = strings.ReplaceAll(norm, ".", "")
		norm = strings.ReplaceAll(norm, "_", "")
		norm = strings.ReplaceAll(norm, "-", "")

		// Season/episode marker — the load-bearing token. Once we see it the
		// series title is complete: every later token is release metadata
		// (quality, codec, source, part, scene group, episode title) and never
		// contributes to the title.
		if se := parseSE(tok); se != nil {
			if out.Season == 0 {
				out.Season = se.season
			}
			out.Episodes = append(out.Episodes, se.eps...)
			seenMarker = true
			continue
		}
		if n, ok := parseSeasonOnly(tok, norm); ok {
			if out.Season == 0 {
				out.Season = n
			}
			out.SeasonPack = true
			seenMarker = true
			continue
		}

		if seenMarker {
			// Post-marker: extract quality/codec/source/part, drop the rest.
			if part, skip, ok := consumePart(rawTokens, i, norm); ok {
				out.Part = part
				i += skip
				continue
			}
			if m := reHeight.FindStringSubmatch(tok); m != nil {
				if h, err := atoiSafe(m[1]); err == nil {
					out.Height = h
				}
				continue
			}
			if c, ok := codecTokens[norm]; ok {
				if out.Codec == CodecAny {
					out.Codec = c
				}
				continue
			}
			if norm == "1080" || norm == "720" || norm == "1440" || norm == "2160" {
				out.Height = atoi(norm)
				continue
			}
			if reAudio.MatchString(norm) {
				continue
			}
			if s, ok := sourceTokens[norm]; ok {
				if out.Source == "" {
					out.Source = s
				}
				continue
			}
			switch norm {
			case "complete", "full", "all", "finale", "special":
				if out.Season != 0 {
					out.SeasonPack = true
				}
			}
			// Everything else post-marker (scene groups, episode titles, stray
			// tags) is deliberately dropped — it is not the series title.
			continue
		}

		// --- pre-marker: this token may be part of the series title ---
		if m := reYear.FindStringSubmatch(norm); m != nil {
			if out.Year == 0 {
				out.Year = atoi(norm)
			}
			continue
		}
		if isPreMarkerNoise(norm) {
			continue
		}
		if isSceneGroup(norm) {
			continue
		}
		titleTokens = append(titleTokens, tok)
	}

	// If we found explicit episodes, the season-pack flag is redundant.
	if len(out.Episodes) > 0 {
		out.SeasonPack = false
	}

	if !seenMarker {
		return out, fmt.Errorf("%w: %q", ErrNoEpisodeMarker, orig)
	}

	out.Episodes = dedupeSortedInts(out.Episodes)
	out.Title = strings.Join(titleTokens, " ")
	out.QualityName = qualityNameForHeight(out.Height)
	return out, nil
}

// seSpec is the parsed result of a season/episode token.
type seSpec struct {
	season int
	eps    []int
}

// parseSE classifies a token that looks like SxxEyy (with multi-episode tail).
// Returns nil when the token is not a season/episode marker.
func parseSE(tok string) *seSpec {
	s := strings.ToLower(strings.ReplaceAll(tok, " ", ""))
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "-", "")
	if !strings.HasPrefix(s, "s") {
		return nil
	}
	s = s[1:] // drop the leading 's'
	if len(s) < 2 {
		return nil
	}
	// The season is the run of digits after 's'.
	season := 0
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		season = season*10 + int(s[i]-'0')
		i++
	}
	if i == 0 || season == 0 {
		return nil
	}
	// Expect an 'e' next.
	if i >= len(s) || s[i] != 'e' {
		return nil
	}
	i++
	if i >= len(s) || s[i] < '0' || s[i] > '9' {
		return nil
	}
	ep := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		ep = ep*10 + int(s[i]-'0')
		i++
	}
	spec := &seSpec{season: season, eps: []int{ep}}
	// Optional tail: explicit additional episodes (S01E01E02 -> [1, 2]).
	for i < len(s) && s[i] == 'e' {
		i++
		if i >= len(s) || s[i] < '0' || s[i] > '9' {
			break
		}
		n := 0
		nd := false
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			n = n*10 + int(s[i]-'0')
			nd = true
			i++
		}
		if nd {
			spec.eps = append(spec.eps, n)
		}
	}
	return spec
}

// parseSeasonOnly classifies a bare season token (S01 / Season 1).
func parseSeasonOnly(tok, norm string) (int, bool) {
	if m := reSeasonOnly.FindStringSubmatch(tok); m != nil {
		return atoi(m[1]), true
	}
	// "Season 1" arrives as two tokens; the "season" keyword is consumed by
	// the noise switch and the bare "1" would be lost, so we only handle the
	// single-token S01 form here.
	return 0, false
}

// isPreMarkerNoise reports whether a pre-marker token is a known release tag or
// article that should be dropped from the series title. Articles like "the" are
// deliberately NOT noise — they are part of real series titles ("The Wire").
// Only pure release tags and bare numeric markers are dropped here.
func isPreMarkerNoise(norm string) bool {
	switch norm {
	case "proper", "repack", "internal", "limited", "remux", "sample",
		"screener", "s01", "s02", "s03", "s04", "s05":
		return true
	}
	return false
}

// consumePart recognises a "Part N" (optionally "of M") multi-part marker among
// the post-marker tokens. It returns the part number, how many extra tokens were
// consumed past the part token (1 for "of M", 0 otherwise), and ok. A bare digit
// adjacent to "part" is not treated as a part marker to avoid swallowing
// unrelated numbers.
func consumePart(tokens []string, i int, norm string) (part, skip int, ok bool) {
	if norm != "part" {
		return 0, 0, false
	}
	// "part 1" -> next token must be a bare number.
	if i+1 < len(tokens) && isBareNumber(strings.ToLower(tokens[i+1])) {
		n := atoi(strings.ToLower(tokens[i+1]))
		skip = 1
		// "part 1 of 2" -> consume "of 2" as well.
		if i+3 < len(tokens) && strings.ToLower(tokens[i+2]) == "of" &&
			isBareNumber(strings.ToLower(tokens[i+3])) {
			skip = 3
		}
		return n, skip, true
	}
	return 0, 0, false
}

// expandRangeTokens expands a "SxxEyy <n>" token pair into the explicit episode
// list it denotes (S01E01 04 -> S01E01 S01E02 S01E03 S01E04). The second token
// must be a bare 1-2 digit integer (a height like "1080p" carries a letter and a
// bare "1080" is 4 digits, so neither is treated as a range end). Tokens other
// than the expanded episodes are left untouched.
func expandRangeTokens(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	for i := 0; i < len(tokens); i++ {
		out = append(out, tokens[i])
		se := parseSE(tokens[i])
		if se == nil || len(tokens) <= i+1 {
			continue
		}
		next := strings.ToLower(strings.Trim(tokens[i+1], " "))
		if !isBareNumber(next) || len(next) > 2 {
			continue
		}
		start, end := se.eps[0], atoi(next)
		if end < start {
			end = start
		}
		for e := start + 1; e <= end; e++ {
			out = append(out, fmt.Sprintf("S%02dE%02d", se.season, e))
		}
		i++ // consume the range-end token
	}
	return out
}

// isBareNumber reports whether s is only ASCII digits.
func isBareNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func rangeInts(start, end int) []int {
	if end < start {
		end = start
	}
	out := make([]int, 0, end-start+1)
	for i := start; i <= end; i++ {
		out = append(out, i)
	}
	return out
}

func dedupeSortedInts(in []int) []int {
	if len(in) == 0 {
		return in
	}
	seen := make(map[int]bool, len(in))
	out := make([]int, 0, len(in))
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Ints(out)
	return out
}

// tokenize splits on spaces/dots/underscores and strips surrounding
// punctuation. "Breaking.Bad.S01E01" -> [Breaking, Bad, S01E01]. Brackets and
// parens are treated as separators.
func tokenize(name string) []string {
	repl := strings.NewReplacer(".", " ", "_", " ", "-", " ", "\t", " ", "\n", " ")
	name = repl.Replace(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '(', ')', '[', ']', '{', '}':
			return ' '
		}
		return r
	}, name)
	var out []string
	for _, tok := range strings.Fields(name) {
		tok = strings.Trim(tok, "'\"|")
		if tok != "" {
			out = append(out, tok)
		}
	}
	return out
}

// isSceneGroup reports whether a normalised token looks like a scene group tag
// (zero-for, one-oh leetspeak). Heuristic: 4+ chars with at least one digit and
// one letter, starting with a letter — these are almost always scene groups or
// release tags, never series titles.
func isSceneGroup(norm string) bool {
	if len(norm) < 4 {
		return false
	}
	hasDigit, hasLetter := false, false
	for i := 0; i < len(norm); i++ {
		c := norm[i]
		switch {
		case c >= '0' && c <= '9':
			hasDigit = true
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			hasLetter = true
		default:
			return false
		}
	}
	return hasDigit && hasLetter
}
