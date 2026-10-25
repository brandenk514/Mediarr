package settings

import "testing"

func cfRules(ops ...string) []CFRule {
	rules := make([]CFRule, len(ops))
	for i, op := range ops {
		rules[i] = CFRule{Field: "title", Operator: op, Value: "s01e01"}
	}
	return rules
}

func TestScoreCF_BasicScoring(t *testing.T) {
	c := CFCandidate{Title: "Show.S01E01.1080p.WEB.x264", Quality: "1080p"}
	formats := []CustomFormat{
		{ID: 1, Name: "1080p", Enabled: true, Score: 100, Rules: []CFRule{{Field: "quality", Operator: "equals", Value: "1080p"}}},
		{ID: 2, Name: "WEB source", Enabled: true, Score: 50, Rules: []CFRule{{Field: "title", Operator: "contains", Value: "WEB"}}},
		{ID: 3, Name: "Disabled", Enabled: false, Score: 500, Rules: []CFRule{{Field: "title", Operator: "contains", Value: "WEB"}}},
	}
	total, matches := ScoreCF(formats, c)
	if total != 150 {
		t.Fatalf("total = %d, want 150", total)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2 (disabled format must not score)", len(matches))
	}
	if matches[0].Name != "1080p" || matches[0].Score != 100 {
		t.Fatalf("first match = %+v", matches[0])
	}
}

func TestScoreCF_AllRulesMustMatch(t *testing.T) {
	c := CFCandidate{Title: "S01E01", Tags: []string{"4k", "hdr"}}
	formats := []CustomFormat{
		{
			ID: 1, Name: "needs both", Enabled: true, Score: 10,
			Rules: []CFRule{
				{Field: "title", Operator: "contains", Value: "s01e01"},
				{Field: "tag", Operator: "contains", Value: "hdr"},
				{Field: "tag", Operator: "contains", Value: "dolby"}, // absent
			},
		},
	}
	total, matches := ScoreCF(formats, c)
	if total != 0 || len(matches) != 0 {
		t.Fatalf("got total=%d matches=%v, want 0 (one rule unmatched)", total, matches)
	}
}

func TestScoreCF_NegativeScorePenalises(t *testing.T) {
	c := CFFallbackCandidate(t)
	total, _ := ScoreCF([]CustomFormat{
		{ID: 1, Name: "bonus", Enabled: true, Score: 100, Rules: []CFRule{{Field: "title", Operator: "exists", Value: ""}}},
		{ID: 2, Name: "penalty", Enabled: true, Score: -30, Rules: []CFRule{{Field: "quality", Operator: "contains", Value: "web"}}},
	}, c)
	if total != 70 {
		t.Fatalf("total = %d, want 70", total)
	}
}

func TestScoreCF_TagOperators(t *testing.T) {
	c := CFCandidate{Title: "x", Tags: []string{"4k", "1080p"}}
	cases := []struct {
		name string
		rule CFRule
		want bool
	}{
		{"contains hit", CFRule{Field: "tag", Operator: "contains", Value: "4K"}, true},
		{"contains miss", CFRule{Field: "tag", Operator: "contains", Value: "hdr"}, false},
		{"equals whole tag", CFRule{Field: "tag", Operator: "equals", Value: "1080p"}, true},
		{"equals partial must miss", CFRule{Field: "tag", Operator: "equals", Value: "1080"}, false},
		{"not_contains hit", CFRule{Field: "tag", Operator: "not_contains", Value: "hdr"}, true},
		{"not_contains miss", CFRule{Field: "tag", Operator: "not_contains", Value: "4k"}, false},
		{"exists with tags", CFRule{Field: "tag", Operator: "exists", Value: ""}, true},
		{"regex", CFRule{Field: "tag", Operator: "regex", Value: `^4\w$`}, true},
	}
	for _, tc := range cases {
		if got := matchRule(tc.rule, c); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
	// exists with no tags
	if matchRule(CFRule{Field: "tag", Operator: "exists", Value: ""}, CFCandidate{}) {
		t.Error("exists must be false with no tags")
	}
}

func TestScoreCF_SizeAndNumeric(t *testing.T) {
	c := CFCandidate{Title: "x", SizeMB: 50000}
	cases := []struct {
		name string
		rule CFRule
		want bool
	}{
		{"gt hit", CFRule{Field: "size_mb", Operator: "gt", Value: "40000"}, true},
		{"gt miss", CFRule{Field: "size_mb", Operator: "gt", Value: "60000"}, false},
		{"lt hit", CFRule{Field: "size_mb", Operator: "lt", Value: "60000"}, true},
		{"lt miss", CFRule{Field: "size_mb", Operator: "lt", Value: "40000"}, false},
		{"gt non-numeric bound", CFRule{Field: "size_mb", Operator: "gt", Value: "big"}, false},
		{"gt zero bound", CFRule{Field: "size_mb", Operator: "gt", Value: "0"}, true},
	}
	for _, tc := range cases {
		if got := matchRule(tc.rule, c); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestScoreCF_ZeroSizeNoNumericMatch(t *testing.T) {
	// A release with no known size (0) never matches a numeric size rule.
	c := CFCandidate{Title: "x"}
	if matchRule(CFRule{Field: "size_mb", Operator: "gt", Value: "1000"}, c) {
		t.Error("zero size must not match gt")
	}
	if matchRule(CFRule{Field: "size_mb", Operator: "exists", Value: ""}, c) {
		t.Error("zero size must not be 'exists'")
	}
}

func TestScoreCF_StringOperators(t *testing.T) {
	c := CFCandidate{Title: "Show.S01E01", Quality: "1080p", Source: "web", Kind: "tv"}
	cases := []struct {
		name string
		rule CFRule
		want bool
	}{
		{"contains ci", CFRule{Field: "title", Operator: "contains", Value: "s01e01"}, true},
		{"not_contains", CFRule{Field: "title", Operator: "not_contains", Value: "s02"}, true},
		{"equals ci", CFRule{Field: "quality", Operator: "equals", Value: "1080P"}, true},
		{"exists hit", CFRule{Field: "source", Operator: "exists", Value: ""}, true},
		{"kind exists", CFRule{Field: "kind", Operator: "exists", Value: ""}, true},
		{"regex hit", CFRule{Field: "title", Operator: "regex", Value: `S\d+E\d+`}, true},
		{"regex bad", CFRule{Field: "title", Operator: "regex", Value: `([`}, false},
	}
	for _, tc := range cases {
		if got := matchRule(tc.rule, c); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// CFFallbackCandidate is a small helper so the negative-score test reads
// like the others.
func CFFallbackCandidate(t *testing.T) CFCandidate {
	t.Helper()
	return CFCandidate{Title: "Show.S01E01.1080p.WEB", Quality: "WEB", Source: "web"}
}

var _ = cfRules // cfRules is retained for readability in later rule-table tests
