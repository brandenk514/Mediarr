package books

import "testing"

// --- MT4 profile filter matrix (pure domain tests) ---

// TestProfileFilters_ZeroPassesEverything: a zero ProfileFilters (the
// seeded "Default" profile) passes every tier-1/tier-2 fact and every
// edition — no filter set, no opinion.
func TestProfileFilters_ZeroPassesEverything(t *testing.T) {
	z := &ProfileFilters{}
	if !z.PassTier1(Tier1Facts{}) {
		t.Error("zero filter should pass empty tier-1 facts")
	}
	if !z.PassTier2(Tier2Facts{}) {
		t.Error("zero filter should pass empty tier-2 facts")
	}
	if !z.PassEdition(WorkEdition{}) {
		t.Error("zero filter should pass an empty edition")
	}
	if z.Active() || z.Tier1Active() || z.Tier2Active() {
		t.Error("zero filter is not active (neither tier)")
	}
}

// TestProfileFilters_TierActivity — which flags switch on which tier:
// tier 1 = languages / release date / popularity proxy; tier 2 = ISBN/ASIN
// / broken parts / secondary series.
func TestProfileFilters_TierActivity(t *testing.T) {
	cases := []struct {
		name string
		ff   *ProfileFilters
		t1   bool
		t2   bool
	}{
		{"none", &ProfileFilters{}, false, false},
		{"min popularity 1", &ProfileFilters{MinimumPopularity: 1}, true, false},
		{"min popularity 0 (off)", &ProfileFilters{MinimumPopularity: 0}, false, false},
		{"skip missing release date", &ProfileFilters{SkipMissingReleaseDate: true}, true, false},
		{"allowed languages", &ProfileFilters{AllowedLanguages: []string{"eng"}}, true, false},
		{"skip no isbn", &ProfileFilters{SkipNoISBNASIN: true}, false, true},
		{"skip broken parts", &ProfileFilters{SkipBrokenParts: true}, false, true},
		{"skip secondary series", &ProfileFilters{SkipSecondarySeries: true}, false, true},
		{"all", &ProfileFilters{
			MinimumPopularity: 2, SkipMissingReleaseDate: true,
			SkipNoISBNASIN: true, SkipBrokenParts: true, SkipSecondarySeries: true,
			AllowedLanguages: []string{"eng"},
		}, true, true},
	}
	for _, tc := range cases {
		if got, want := tc.ff.Tier1Active(), tc.t1; got != want {
			t.Errorf("%s: Tier1Active = %v, want %v", tc.name, got, want)
		}
		if got, want := tc.ff.Tier2Active(), tc.t2; got != want {
			t.Errorf("%s: Tier2Active = %v, want %v", tc.name, got, want)
		}
		if got, want := tc.ff.Active(), tc.t1 || tc.t2; got != want {
			t.Errorf("%s: Active = %v, want %v", tc.name, got, want)
		}
	}
}

// TestProfileFilters_AllowedLanguages — the OR rule and its edges: empty
// allowlist passes everything; a hit passes when ANY declared code is in
// the list (case-insensitive, exact code — no language-family expansion);
// a hit with NO language data fails a non-empty list.
func TestProfileFilters_AllowedLanguages(t *testing.T) {
	cases := []struct {
		name      string
		allowlist []string
		codes     []string
		want      bool
	}{
		{"empty allowlist: nothing filtered", nil, nil, true},
		{"exact match", []string{"eng", "fra"}, []string{"eng"}, true},
		{"case-insensitive", []string{"eng", "fra"}, []string{"ENG"}, true},
		{"partial overlap (any)", []string{"eng", "fra"}, []string{"deu", "eng"}, true},
		{"none in list", []string{"eng", "fra"}, []string{"deu", "spa"}, false},
		{"unknown (no data) fails a non-empty list", []string{"eng"}, nil, false},
		{"no family expansion: en does not match eng", []string{"en"}, []string{"eng"}, false},
	}
	for _, tc := range cases {
		fl := &ProfileFilters{AllowedLanguages: tc.allowlist}
		if got := fl.languageAllowed(tc.codes); got != tc.want {
			t.Errorf("%s: languageAllowed(%v) = %v, want %v", tc.name, tc.codes, got, tc.want)
		}
	}
}

// TestProfileFilters_SkipMissingReleaseDate.
func TestProfileFilters_SkipMissingReleaseDate(t *testing.T) {
	fl := &ProfileFilters{SkipMissingReleaseDate: true}
	if !fl.PassTier1(Tier1Facts{ReleaseYear: 1974}) {
		t.Error("year 1974 should pass")
	}
	if fl.PassTier1(Tier1Facts{ReleaseYear: 0}) {
		t.Error("year 0 (missing) should be dropped")
	}
	// Off: a missing year passes.
	if !(&ProfileFilters{}).PassTier1(Tier1Facts{ReleaseYear: 0}) {
		t.Error("skip off: missing year passes")
	}
}

// TestProfileFilters_MinimumPopularityProxy — the indexed-edition count
// boundary: a hit passes a minimum N when its edition count is >= N;
// N-1 and an UNKNOWN (0) count fail any positive minimum; a zero minimum
// passes everything.
func TestProfileFilters_MinimumPopularityProxy(t *testing.T) {
	cases := []struct {
		min  int
		ed   int
		want bool
	}{
		{0, 0, true}, // min off: unknown count passes
		{0, 1, true},
		{1, 0, false}, // unknown count fails a positive minimum
		{1, 1, true},  // boundary: == N passes
		{1, 2, true},
		{2, 1, false}, // N-1 fails
		{3, 3, true},
		{3, 2, false},
	}
	for _, tc := range cases {
		fl := &ProfileFilters{MinimumPopularity: tc.min}
		if got := fl.PassTier1(Tier1Facts{EditionCount: tc.ed}); got != tc.want {
			t.Errorf("min=%d editions=%d: pass=%v, want %v", tc.min, tc.ed, got, tc.want)
		}
	}
}

// TestProfileFilters_Tier2ISBN — skip-no-ISBN/ASIN at work level (at least
// one edition must carry an ISBN or an ASIN; an ASIN-only edition still
// counts as identified).
func TestProfileFilters_Tier2ISBN(t *testing.T) {
	fl := &ProfileFilters{SkipNoISBNASIN: true}
	if !fl.PassTier2(Tier2Facts{HasISBNOrASIN: true}) {
		t.Error("a work with an ISBN/ASIN edition passes")
	}
	if fl.PassTier2(Tier2Facts{}) {
		t.Error("a work with no ISBN/ASIN edition is dropped")
	}
	if !(&ProfileFilters{}).PassTier2(Tier2Facts{}) {
		t.Error("skip off: no-ISBN work passes")
	}
	// Tier2FactsFromEditions: an ASIN-only edition marks the work.
	if !Tier2FactsFromEditions([]WorkEdition{{ISBN: ""}, {ASIN: "B000S5MU0K"}}).HasISBNOrASIN {
		t.Error("an ASIN-only edition marks the work identified")
	}
	if Tier2FactsFromEditions([]WorkEdition{{Languages: []string{"eng"}}}).HasISBNOrASIN {
		t.Error("no identifiers: work unmarked")
	}
}

// TestProfileFilters_Tier2BrokenParts — the broken-parts rule: a numbered
// membership in a series the document indexes with < 2 distinct volume
// labels = broken; no numbered membership = pass; the documented
// degradation (no series data) = pass.
func TestProfileFilters_Tier2BrokenParts(t *testing.T) {
	fl := &ProfileFilters{SkipBrokenParts: true}
	cases := []struct {
		name string
		refs []SeriesRef
		want bool
	}{
		{"no series data (degrade): pass", nil, true},
		{"no numbered membership (secondary only): pass", []SeriesRef{{Title: "Series X"}}, true},
		{"one volume of a series: broken", []SeriesRef{{Title: "Series X", Volume: "1"}}, false},
		{"two distinct volumes: ok", []SeriesRef{{Title: "Series X", Volume: "1"}, {Title: "Series X", Volume: "2"}}, true},
		{"same volume twice: still broken", []SeriesRef{{Title: "Series X", Volume: "1"}, {Title: "Series X", Volume: "1"}}, false},
		{"two series, one broken: broken", []SeriesRef{
			{Title: "Series X", Volume: "1"}, {Title: "Series X", Volume: "2"},
			{Title: "Series Y", Volume: "7"},
		}, false},
		{"two series, both complete: ok", []SeriesRef{
			{Title: "Series X", Volume: "1"}, {Title: "Series X", Volume: "2"},
			{Title: "Series Y", Volume: "7"}, {Title: "Series Y", Volume: "8"},
		}, true},
	}
	for _, tc := range cases {
		if got := fl.PassTier2(Tier2Facts{Series: tc.refs}); got != tc.want {
			t.Errorf("%s: pass=%v, want %v", tc.name, got, tc.want)
		}
	}
	// Skip off: a broken series passes.
	if !(&ProfileFilters{}).PassTier2(Tier2Facts{Series: []SeriesRef{{Title: "Series X", Volume: "1"}}}) {
		t.Error("skip off: broken parts pass")
	}
}

// TestProfileFilters_Tier2SecondarySeries — secondary-only membership
// (series references, none numbered) is dropped; a numbered membership
// anywhere passes; no series data passes (the documented degradation).
func TestProfileFilters_Tier2SecondarySeries(t *testing.T) {
	fl := &ProfileFilters{SkipSecondarySeries: true}
	cases := []struct {
		name string
		refs []SeriesRef
		want bool
	}{
		{"no series data (degrade): pass", nil, true},
		{"secondary only: dropped", []SeriesRef{{Title: "Series X"}}, false},
		{"secondary + numbered: pass", []SeriesRef{{Title: "Series X"}, {Title: "Series X", Volume: "3"}}, true},
		{"numbered only: pass", []SeriesRef{{Title: "Series X", Volume: "3"}}, true},
		{"unlabeled series entry ignored", []SeriesRef{{Title: ""}}, true},
	}
	for _, tc := range cases {
		if got := fl.PassTier2(Tier2Facts{Series: tc.refs}); got != tc.want {
			t.Errorf("%s: pass=%v, want %v", tc.name, got, tc.want)
		}
	}
	if !(&ProfileFilters{}).PassTier2(Tier2Facts{Series: []SeriesRef{{Title: "Series X"}}}) {
		t.Error("skip off: secondary-only passes")
	}
}

// TestProfileFilters_Tier2FromEditions — the series/identifier facts
// flatten across the work document's editions.
func TestProfileFilters_Tier2FromEditions(t *testing.T) {
	eds := []WorkEdition{
		{ISBN: "9780060559155", Series: []SeriesRef{{Title: "A", Volume: "1"}}},
		{Series: []SeriesRef{{Title: "A", Volume: "2"}, {Title: "Anthology"}}},
	}
	tf := Tier2FactsFromEditions(eds)
	if !tf.HasISBNOrASIN {
		t.Error("an edition with an ISBN marks the work")
	}
	if len(tf.Series) != 3 {
		t.Fatalf("series refs = %d, want 3: %+v", len(tf.Series), tf.Series)
	}
}

// TestProfileFilters_PassEdition — the per-edition subset (the editions
// endpoint): allowed-languages, skip-missing-release-date, skip-no-
// ISBN/ASIN. The work-level filters (popularity, series) deliberately do
// NOT apply per edition.
func TestProfileFilters_PassEdition(t *testing.T) {
	fl := &ProfileFilters{
		SkipMissingReleaseDate: true,
		SkipNoISBNASIN:         true,
		AllowedLanguages:       []string{"eng"},
	}
	cases := []struct {
		name string
		ed   WorkEdition
		want bool
	}{
		{"full edition passes", WorkEdition{ISBN: "9780060559155", Year: 1974, Languages: []string{"eng"}}, true},
		{"ASIN-only edition passes the ISBN filter", WorkEdition{ASIN: "B000S5MU0K", Year: 1974, Languages: []string{"eng"}}, true},
		{"no identifiers: dropped", WorkEdition{Year: 1974, Languages: []string{"eng"}}, false},
		{"missing year: dropped", WorkEdition{ISBN: "1", Languages: []string{"eng"}}, false},
		{"language out of list: dropped", WorkEdition{ISBN: "1", Year: 1974, Languages: []string{"fra"}}, false},
		{"no language data: dropped", WorkEdition{ISBN: "1", Year: 1974}, false},
	}
	for _, tc := range cases {
		if got := fl.PassEdition(tc.ed); got != tc.want {
			t.Errorf("%s: pass=%v, want %v", tc.name, got, tc.want)
		}
	}
	// Work-level filters do NOT apply per edition.
	wl := &ProfileFilters{
		MinimumPopularity:   99,
		SkipBrokenParts:     true,
		SkipSecondarySeries: true,
	}
	if !wl.PassEdition(WorkEdition{}) {
		t.Error("work-level filters must not drop an edition")
	}
}
