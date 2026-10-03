package movies

import (
	"regexp"
	"strings"
)

// parse.go implements scene release-name parsing for movies.
//
// Per the plan, M1 uses a pragmatic token-table parser rather than a full
// grammar: split on delimiters, classify each token (year, height, codec,
// source, scene-group, episode markers), and treat the remaining leading
// tokens as the title. We expand from real-world failures, not upfront.
//
// Delimiters: spaces, dots, underscores are all treated as separators.
// Parenthesised blocks and bracketed groups are peeled off.

var (
	reHeight = regexp.MustCompile(`^([0-9]{2,4})p$`)
	reYear   = regexp.MustCompile(`^(1[89][0-9]{2}|20[0-9]{2})$`)
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

// ParseRelease parses a scene release name into structured fields.
//
// It is deliberately tolerant: it extracts what it can and leaves Title as
// the joined leading tokens it could not classify. It never errors — the
// caller decides whether a parse is good enough.
func ParseRelease(name string) ParsedRelease {
	out := ParsedRelease{Codec: CodecAny}
	name = strings.TrimSpace(name)

	// Peel off trailing bracketed group (often a scene name).
	if i := strings.LastIndex(name, "["); i >= 0 {
		name = strings.TrimSpace(name[:i])
	}

	rawTokens := tokenize(name)
	var titleTokens []string

	for _, tok := range rawTokens {
		norm := strings.ToLower(tok)
		norm = strings.ReplaceAll(norm, ".", "")
		norm = strings.ReplaceAll(norm, "_", "")
		norm = strings.ReplaceAll(norm, "-", "")

		// Height: 1080p / 2160p
		if m := reHeight.FindStringSubmatch(tok); m != nil {
			if h, err := atoiSafe(m[1]); err == nil {
				out.Height = h
				continue
			}
		}

		// Codec (check before year — "264" is a codec, not a year).
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

		// Audio codec tokens (DDP5.1 -> ddp5,1 ; AAC ; EAC3 ; DTS) — noise.
		if reAudio.MatchString(norm) {
			continue
		}

		// Source
		if s, ok := sourceTokens[norm]; ok {
			if out.Source == "" {
				out.Source = s
			}
			continue
		}

		// Episode markers (TV leakage) — treat as title noise, skip.
		if isSceneGroup(norm) {
			continue
		}

		// Year
		if m := reYear.FindStringSubmatch(norm); m != nil {
			if out.Year == 0 {
				out.Year = atoi(norm)
			}
			continue
		}

		// Known noise tokens.
		switch norm {
		case "the", "a", "an", "and", "x264", "x265", "proper", "repack",
			"internal", "limited", "remux", "ddp", "atmos", "truehd",
			"eac3", "aac", "ac3", "dts", "hifi", "hd", "uhd", "lossless",
			"hi10p", "10bit", "8bit", "hdr", "hdr10", "sdr", "screener",
			"amc", "hdr10+", "dv", "dovi", "dolby", "vision", "dual",
			"audio", "multi", "subbed", "dubbed", "complete", "season",
			"special", "edits", "extended", "theatrical", "h", "1", "dl":
			continue
		}

		// Otherwise: part of the title.
		titleTokens = append(titleTokens, tok)
	}

	out.Title = strings.Join(titleTokens, " ")
	out.QualityName = qualityNameForHeight(out.Height)
	return out
}

// tokenize splits on spaces/dots/underscores/hyphens and strips surrounding
// punctuation. "Inception.2010.1080p.BluRay.x265" -> [Inception, 2010, ...].
// Hyphen splits handle "WEB-DL" (web) and "x264-FLYt3R" (codec + scene grp).
func tokenize(name string) []string {
	// Normalise delimiters to spaces.
	repl := strings.NewReplacer(".", " ", "_", " ", "-", " ", "	", " ", "\n", " ")
	name = repl.Replace(name)
	// Strip parenthesised blocks (language/codec notes).
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

func atoi(s string) int {
	n, _ := atoiSafe(s)
	return n
}

// isSceneGroup reports whether a normalised token looks like a scene group
// tag (zero-for, one-oh leetspeak). Heuristic: 4+ chars with at least one
// digit and one letter, starting with a letter — these are almost always
// scene groups or episode codes, never movie titles.
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

func atoiSafe(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return n, errNotInt
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

var errNotInt = errType("not an integer")

type errType string

func (e errType) Error() string { return string(e) }
