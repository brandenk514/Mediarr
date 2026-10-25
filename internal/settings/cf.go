package settings

import (
	"regexp"
	"strconv"
	"strings"
)

// CFCandidate is the release description the custom-format scorer judges.
// It mirrors the CustomFormatScoreRequest body (the settings endpoint maps
// the request straight onto this struct).
type CFCandidate struct {
	Title   string
	Quality string
	Source  string
	Kind    string
	SizeMB  int64
	Tags    []string
}

// CFMatch is the per-format contribution to a scoring result.
type CFMatch struct {
	Name  string `json:"name"`
	Score int    `json:"score"`
}

// ScoreCF runs the custom-format engine over a candidate: a format scores
// when it is enabled and EVERY rule matches (Sonarr's all-rules-match
// semantics); its score is then added to the total (negative scores
// penalise). Unmatched or disabled formats contribute nothing. Returns the
// total and the per-format breakdown (matched formats only, in input order).
func ScoreCF(formats []CustomFormat, c CFCandidate) (int, []CFMatch) {
	total := 0
	matches := []CFMatch{}
	for _, f := range formats {
		if !f.Enabled || len(f.Rules) == 0 {
			continue
		}
		if matchAllRules(f.Rules, c) {
			total += f.Score
			matches = append(matches, CFMatch{Name: f.Name, Score: f.Score})
		}
	}
	return total, matches
}

func matchAllRules(rules []CFRule, c CFCandidate) bool {
	for _, r := range rules {
		if !matchRule(r, c) {
			return false
		}
	}
	return true
}

// fieldValue extracts the string form of a candidate field; ok is false when
// the field is empty (the "exists" operator uses this).
func fieldValue(field string, c CFCandidate) (value string, ok bool) {
	switch field {
	case "title":
		return c.Title, c.Title != ""
	case "quality":
		return c.Quality, c.Quality != ""
	case "source":
		return c.Source, c.Source != ""
	case "kind":
		return c.Kind, c.Kind != ""
	case "size_mb":
		if c.SizeMB <= 0 {
			return "", false
		}
		return strconv.FormatInt(c.SizeMB, 10), true
	case "tag":
		if len(c.Tags) == 0 {
			return "", false
		}
		return strings.Join(c.Tags, " "), true
	default:
		return "", false
	}
}

// fieldValueAny reports whether any single tag satisfies the string check —
// "tag" rules apply to individual tags, not the joined blob (a "contains
// 4k" rule should match a release tagged ["1080p4k"] per-tag, and "equals"
// must compare against a whole tag).
func tagValueMatches(r CFRule, c CFCandidate) bool {
	for _, t := range c.Tags {
		if stringTagCheck(r, t) {
			return true
		}
	}
	return false
}

func stringTagCheck(r CFRule, t string) bool {
	f := strings.ToLower(t)
	v := strings.ToLower(r.Value)
	switch r.Operator {
	case "contains":
		return strings.Contains(f, v)
	case "not_contains":
		return true // per-tag; a format's not_contains is decided on the joined set below
	case "equals":
		return f == v
	case "regex":
		re, err := regexp.Compile(r.Value)
		if err != nil {
			return false
		}
		return re.MatchString(t)
	case "exists":
		return t != ""
	}
	return false
}

func matchRule(r CFRule, c CFCandidate) bool {
	if r.Field == "tag" {
		switch r.Operator {
		case "contains", "equals", "regex":
			return tagValueMatches(r, c)
		case "not_contains":
			// Matches when NO tag contains the value.
			for _, t := range c.Tags {
				if strings.Contains(strings.ToLower(t), strings.ToLower(r.Value)) {
					return false
				}
			}
			return true
		case "exists":
			return len(c.Tags) > 0
		default:
			return false // numeric operators have no meaning on tags
		}
	}
	value, ok := fieldValue(r.Field, c)
	if r.Operator == "exists" {
		return ok
	}
	if !ok {
		return false
	}
	switch r.Operator {
	case "contains":
		return strings.Contains(strings.ToLower(value), strings.ToLower(r.Value))
	case "not_contains":
		return !strings.Contains(strings.ToLower(value), strings.ToLower(r.Value))
	case "equals":
		return strings.EqualFold(value, r.Value)
	case "regex":
		re, err := regexp.Compile(r.Value)
		if err != nil {
			return false
		}
		return re.MatchString(value)
	case "gt", "lt":
		num, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return false
		}
		lim, err := strconv.ParseFloat(r.Value, 64)
		if err != nil {
			return false // non-numeric bound never matches
		}
		if r.Operator == "gt" {
			return num > lim
		}
		return num < lim
	}
	return false
}
