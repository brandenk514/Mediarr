package books

import (
	"context"
	"time"
)

// SourceOpenLibrary is the provenance source tag for works resolved from
// the keyless Open Library adapter (MT3).
const SourceOpenLibrary = "openlibrary"

// DefaultMetadataTTL is how long a cached resolved-work row is considered
// fresh before the lookup re-fetches it from its source. Open Library is
// keyless with no SLA, so we do not re-fetch per request — but a week is
// short enough that stale book metadata (title/author/year) is not a
// user-facing concern. A configured TTL of zero/invalid falls back to this.
const DefaultMetadataTTL = 7 * 24 * time.Hour

// WorkMetadata is a resolved book work with provenance (MT3): the
// OpenLibrary work key, its title/author/year/cover, where it was resolved
// from (Source), under which source-native id (SourceID), and when the row
// was last fetched (FetchedAt, the TTL anchor).
type WorkMetadata struct {
	WorkKey   string // resolved OL work key (bare id, e.g. "OL8189755W")
	Source    string // provenance source, e.g. "openlibrary"
	SourceID  string // source-native id (the OL key for now; Readarr id at B6)
	Title     string
	Author    string
	Year      int
	CoverURL  string
	FetchedAt time.Time
}

// MetadataStore persists resolved works with provenance (MT3). It is the
// single source for "was this resolved from Open Library, when, under what
// key": the books lookup consults it before calling Open Library (a fresh
// row is served without a provider call; an expired or missing row triggers
// a re-fetch that upserts the row), and work resolutions upsert into it.
//
// Implemented by the Postgres adapter (book_work_metadata); tests use a
// fake. Nil-safe at the service: with no store the lookup is uncached and
// the refresh command answers not-configured.
type MetadataStore interface {
	// GetWork loads the stored metadata for a resolved OL work key. ok is
	// false when no row exists (err is nil in that case).
	GetWork(ctx context.Context, workKey string) (WorkMetadata, bool, error)
	// UpsertWork inserts a resolved work or refreshes the existing row for
	// its (Source, SourceID), re-anchoring FetchedAt on every call.
	UpsertWork(ctx context.Context, w WorkMetadata) error
}

// IsFresh reports whether a row fetched at fetchedAt is still within ttl of
// now — the MT3 TTL decision. Pure so it is unit-testable; the caller
// supplies a positive ttl (the service falls back to DefaultMetadataTTL).
func IsFresh(fetchedAt, now time.Time, ttl time.Duration) bool {
	return !now.After(fetchedAt.Add(ttl))
}
