package books

// WorkEdition is one OpenLibrary edition of a work (MT2): the edition the
// add flow offers when a work is selected, pre-filled from the provider
// instead of manual data entry. It is pure data (no I/O) — the provider
// adapter maps Open Library's edition documents onto it, the API serializes
// it, and the provenance store caches it (as WorkMetadata.EditionsJSON).
type WorkEdition struct {
	// Key is the bare OpenLibrary edition id (e.g. "OL47044678M").
	Key string
	// Title is the edition's title (often the work title plus a subtitle);
	// "" when the edition document has none.
	Title string
	// ISBN is the edition's preferred ISBN — the first ISBN-13, else the
	// first ISBN-10; "" when the edition has none.
	ISBN string
	// Formats is the edition's file formats (e.g. ["EPUB", "PDF"]);
	// empty when unknown.
	Formats []string
	// Languages is the edition's ISO 639-1 language codes (e.g. ["eng"]);
	// empty when unknown.
	Languages []string
	// Publishers is the edition's publisher names; empty when unknown.
	Publishers []string
	// PublishDate is Open Library's raw publish-date string (it is not a
	// full date — e.g. "2017-06", "2017", or "xxxx" for unknown); "" when
	// absent.
	PublishDate string
	// Year is the parsed publication year (the leading year of PublishDate);
	// 0 when unknown.
	Year int
	// Pages is the edition's page count; 0 when unknown.
	Pages int
}

// WorkDetails is the full Open Library detail of one work (MT2): the
// work-level title/authors/cover plus the work's editions. The editions
// endpoint (GET /api/v1/search/books/works/{workKey}/editions) returns it,
// and the provenance store caches it alongside the MT3 work row (the same
// TTL/refresh path — a refresh for the work key refreshes the editions too).
type WorkDetails struct {
	// WorkKey is the bare OpenLibrary work id (e.g. "OL59863W").
	WorkKey string
	// Title is the work's title; "" when unknown.
	Title string
	// Authors is the work's author names (empty when unknown).
	Authors []string
	// CoverURL is the work's cover image URL (the documented
	// covers.openlibrary.org scheme); "" when the work has no cover.
	CoverURL string
	// FirstPublishYear is the work's first publish year; 0 when unknown.
	FirstPublishYear int
	// Editions is the work's editions (capped by the adapter); nil/empty
	// when the work has no editions.
	Editions []WorkEdition
}
