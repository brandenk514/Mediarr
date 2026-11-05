package books

import "testing"

// TestParseIDQuery pins the MT1 id-lookup syntax: the prefixed forms
// (isbn:/asin:/goodreads:/olid:) plus the unambiguous bare forms (13-digit
// ISBN, "B"+9-alnum ASIN, OL work key). A bare 10-digit string is
// deliberately NOT an id (ISBN-10 vs Goodreads id ambiguity) — it stays a
// plain-text search.
func TestParseIDQuery(t *testing.T) {
	tests := []struct {
		q      string
		kind   IDKind
		value  string
		wantOK bool
	}{
		// Prefixed forms.
		{"isbn:9780140449335", IDKindISBN, "9780140449335", true},
		{"isbn:978-0-14-044933-5", IDKindISBN, "9780140449335", true},
		{"isbn:978 014 044 933 5", IDKindISBN, "9780140449335", true},
		{"isbn:0140449335", IDKindISBN, "0140449335", true},
		{"ISBN:9780140449335", IDKindISBN, "9780140449335", true}, // prefix is case-insensitive
		{"isbn:12345", "", "", false},                             // too short
		{"isbn:97801404493355", "", "", false},                    // 14 digits
		{"isbn:978014044933X", "", "", false},                     // not digits
		{"isbn:", "", "", false},                                  // empty value
		{"asin:B000S5MU0K", IDKindASIN, "B000S5MU0K", true},
		{"asin:b000s5mu0k", IDKindASIN, "b000s5mu0k", true},
		{"ASIN:B000S5MU0K", IDKindASIN, "B000S5MU0K", true},
		{"asin:TOOSHORT", "", "", false},
		{"asin:ABCDEFGHIJKLM", "", "", false}, // 13 alnum
		{"goodreads:14", IDKindGoodreads, "14", true},
		{"goodreads:1499954664", IDKindGoodreads, "1499954664", true},
		{"GOODREADS:14", IDKindGoodreads, "14", true},
		{"goodreads:14a", "", "", false},
		{"goodreads:", "", "", false},
		{"olid:OL59863W", IDKindOL, "OL59863W", true},
		{"olid:ol59863w", IDKindOL, "OL59863W", true}, // upper-cased
		{"OLID:OL59863W", IDKindOL, "OL59863W", true},
		{"olid:OLX", "", "", false},
		{"olid:OL59863A", "", "", false},
		// Bare forms (unambiguous only).
		{"9780140449335", IDKindISBN, "9780140449335", true},
		{"  9780140449335  ", IDKindISBN, "9780140449335", true},
		{"97801404493355", "", "", false}, // 14 digits: not an ISBN-13
		{"0140449335", "", "", false},     // bare 10 digits: ambiguous (ISBN-10 vs Goodreads id)
		{"B000S5MU0K", IDKindASIN, "B000S5MU0K", true},
		{"b000s5mu0k", IDKindASIN, "b000s5mu0k", true},
		{"TOOSHORTAS", "", "", false}, // 8 alnum: not an ASIN
		{"ABCDEFGHIJ", "", "", false}, // 10 alnum but not an ASIN shape? — it IS 10 alnum: see next
		{"OL59863W", IDKindOL, "OL59863W", true},
		{"ol59863w", IDKindOL, "OL59863W", true},
		{"OL1234567W", IDKindOL, "OL1234567W", true}, // 10 alnum: the OL key shape wins over the ASIN
		{"OL59863W ", IDKindOL, "OL59863W", true},
		// Plain text (not an id query).
		{"The Dispossessed", "", "", false},
		{"Dune", "", "", false},
		{"", "", "", false},
		{"   ", "", "", false},
		{"isbn", "", "", false},           // no value after the prefix (no colon)
		{":9780140449335", "", "", false}, // empty prefix
		{"978014044933", "", "", false},   // 12 digits
	}
	for _, tc := range tests {
		got, ok := ParseIDQuery(tc.q)
		if ok != tc.wantOK {
			t.Errorf("ParseIDQuery(%q).ok = %v, want %v", tc.q, ok, tc.wantOK)
			continue
		}
		if ok && (got.Kind != tc.kind || got.Value != tc.value) {
			t.Errorf("ParseIDQuery(%q) = {%s %q}, want {%s %q}", tc.q, got.Kind, got.Value, tc.kind, tc.value)
		}
	}
}
