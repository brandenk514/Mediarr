package postgres

import (
	"context"
	"database/sql"
	"fmt"

	books "github.com/brandenk514/mediarr/internal/domains/books"
)

// BookMetadataStore is the Postgres-backed books.MetadataStore (MT3): the
// resolved-work provenance rows in book_work_metadata.
type BookMetadataStore struct {
	db *sql.DB
}

// NewBookMetadataStore builds the MT3 resolved-work store over the given pool.
func NewBookMetadataStore(db *sql.DB) *BookMetadataStore {
	return &BookMetadataStore{db: db}
}

var _ books.MetadataStore = (*BookMetadataStore)(nil)

// GetWork loads the stored metadata for a resolved OL work key. When more
// than one source row resolves to the same work key (the B6 Readarr rows
// and the OpenLibrary row coexist), the newest-fetched wins; the source tie
// break keeps the choice deterministic. The row's cached editions (MT2)
// come back in EditionsJSON (nil for a row that predates the column or was
// written without a details fetch).
func (r *BookMetadataStore) GetWork(ctx context.Context, workKey string) (books.WorkMetadata, bool, error) {
	var w books.WorkMetadata
	var editions []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT source, source_id, work_key, title, author, year, cover_url, fetched_at, editions
		FROM book_work_metadata
		WHERE work_key = $1
		ORDER BY fetched_at DESC, source
		LIMIT 1`, workKey).Scan(
		&w.Source, &w.SourceID, &w.WorkKey, &w.Title, &w.Author,
		&w.Year, &w.CoverURL, &w.FetchedAt, &editions,
	)
	if err == sql.ErrNoRows {
		return books.WorkMetadata{}, false, nil
	}
	if err != nil {
		return books.WorkMetadata{}, false, fmt.Errorf("books: get work metadata: %w", err)
	}
	if editions != nil {
		w.EditionsJSON = editions
	}
	return w, true, nil
}

// upsertEditionsJSON renders a row's editions for the JSONB column: the
// serialized list, or '[]' when the row carries none (a plain search
// resolution) — the column is NOT NULL.
func upsertEditionsJSON(w books.WorkMetadata) string {
	if len(w.EditionsJSON) > 0 {
		return string(w.EditionsJSON)
	}
	return "[]"
}

// UpsertWork inserts a resolved work or refreshes the existing row for its
// (source, source_id); fetched_at is re-anchored to the supplied FetchedAt
// (the caller sets it to now on every fetch) and the cached editions (MT2)
// are written with the row, so a work-key refresh refreshes the editions
// too.
func (r *BookMetadataStore) UpsertWork(ctx context.Context, w books.WorkMetadata) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO book_work_metadata
		       (source, source_id, work_key, title, author, year, cover_url, fetched_at, editions)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (source, source_id) DO UPDATE SET
		    work_key   = excluded.work_key,
		    title      = excluded.title,
		    author     = excluded.author,
		    year       = excluded.year,
		    cover_url  = excluded.cover_url,
		    fetched_at = excluded.fetched_at,
		    editions   = excluded.editions`,
		w.Source, w.SourceID, w.WorkKey, w.Title, w.Author,
		w.Year, w.CoverURL, w.FetchedAt, upsertEditionsJSON(w),
	)
	if err != nil {
		return fmt.Errorf("books: upsert work metadata: %w", err)
	}
	return nil
}
