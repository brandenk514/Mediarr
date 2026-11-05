package books

import (
	"regexp"
	"strings"
)

// IDKind classifies the provider id an explicit id-lookup query names (MT1).
// The books lookup (GET /api/v1/search/books/lookup) routes a recognized id
// query to the matching resolver instead of a plain-text search.
type IDKind string

const (
	// IDKindISBN is an ISBN-10 or ISBN-13 (the id: syntax `isbn:`).
	IDKindISBN IDKind = "isbn"
	// IDKindASIN is an Amazon Standard Identification Number (`asin:`).
	IDKindASIN IDKind = "asin"
	// IDKindGoodreads is a Goodreads book id (`goodreads:`); it resolves via
	// Goodreads to an ISBN, then to the OpenLibrary work.
	IDKindGoodreads IDKind = "goodreads"
	// IDKindOL is an OpenLibrary work key (`olid:` or bare "OL…W" — the MT3
	// cached path).
	IDKindOL IDKind = "olid"
)

// IDQuery is a recognized id-lookup query: the id kind it names and the
// normalized id value (hyphens stripped from ISBNs, OL keys upper-cased).
type IDQuery struct {
	Kind  IDKind
	Value string
}

// id-lookup shape checks (MT1). The bare forms are deliberately conservative:
// only the unambiguous shapes are id queries, so a bare 10-digit string is
// NOT an id (it is ambiguous between an ISBN-10 and a numeric Goodreads id —
// the prefix disambiguates) and anything else stays a plain-text search.
var (
	idISBN13Re   = regexp.MustCompile(`^\d{13}$`)
	idISBNRe     = regexp.MustCompile(`^\d{10}$|^\d{13}$`)
	idASINRe     = regexp.MustCompile(`^[A-Za-z0-9]{10}$`)    // explicit `asin:` prefix: any 10 alnum
	idBareASINRe = regexp.MustCompile(`^[Bb][0-9A-Za-z]{9}$`) // bare: "B"+9 alnum only (unambiguous)
	idGRRe       = regexp.MustCompile(`^\d+$`)
	idOLRe       = regexp.MustCompile(`^ol\d+w$`)
)

// ParseIDQuery recognizes the MT1 id-lookup syntax in a lookup query:
//
//   - prefixed (case-insensitive prefix): `isbn:<10|13 digits>` (hyphens and
//     spaces tolerated, stripped), `asin:<10 alnum>`, `goodreads:<digits>`,
//     `olid:<OL…W>` (upper/lower case);
//   - bare: a 13-digit string (ISBN-13), a "B"+9-alnum string (an Amazon
//     ASIN), or an OL work key ("OL…W", upper or lower case).
//
// A bare 10-digit string is deliberately NOT an id — it is ambiguous between
// an ISBN-10 and a numeric Goodreads id — so it stays a plain-text search;
// the `isbn:` / `goodreads:` prefixes disambiguate. ok is false for anything
// unrecognized (the caller treats it as a plain-text query).
func ParseIDQuery(q string) (IDQuery, bool) {
	s := strings.TrimSpace(q)
	if s == "" {
		return IDQuery{}, false
	}
	// Prefixed forms (the documented, unambiguous scheme).
	if i := strings.IndexByte(s, ':'); i > 0 {
		prefix, rest := strings.ToLower(s[:i]), strings.TrimSpace(s[i+1:])
		switch prefix {
		case "isbn":
			v := stripISBNSeparators(rest)
			if idISBNRe.MatchString(v) {
				return IDQuery{Kind: IDKindISBN, Value: v}, true
			}
		case "asin":
			if idASINRe.MatchString(rest) {
				return IDQuery{Kind: IDKindASIN, Value: rest}, true
			}
		case "goodreads":
			if idGRRe.MatchString(rest) {
				return IDQuery{Kind: IDKindGoodreads, Value: rest}, true
			}
		case "olid":
			if idOLRe.MatchString(strings.ToLower(rest)) {
				return IDQuery{Kind: IDKindOL, Value: strings.ToUpper(rest)}, true
			}
		}
		return IDQuery{}, false
	}
	// Bare forms: unambiguous shapes only (order matters — an OL key like
	// "OL1234567W" is also 10 alnum, so the OL shape wins over the ASIN).
	if idISBN13Re.MatchString(s) {
		return IDQuery{Kind: IDKindISBN, Value: s}, true
	}
	if idOLRe.MatchString(strings.ToLower(s)) {
		return IDQuery{Kind: IDKindOL, Value: strings.ToUpper(s)}, true
	}
	if idBareASINRe.MatchString(s) {
		return IDQuery{Kind: IDKindASIN, Value: s}, true
	}
	return IDQuery{}, false
}

// stripISBNSeparators drops the hyphens and spaces an ISBN may carry
// (Open Library's own normalize_isbn does the same before indexing).
func stripISBNSeparators(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r != '-' && r != ' ' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
