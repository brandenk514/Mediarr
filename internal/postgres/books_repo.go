package postgres

import (
	"context"
	"database/sql"
	"fmt"

	books "github.com/brandenk514/mediarr/internal/domains/books"
)

// BooksRepo is the Postgres-backed implementation of books.Repo.
type BooksRepo struct {
	db *sql.DB
}

// NewBooksRepo builds a books repository over the given pool.
func NewBooksRepo(db *sql.DB) *BooksRepo { return &BooksRepo{db: db} }

var _ books.Repo = (*BooksRepo)(nil)

// --- Authors ---

// CreateAuthor inserts an author and returns its id.
func (r *BooksRepo) CreateAuthor(ctx context.Context, a books.Author) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO book_authors (name, monitored)
		VALUES ($1, $2)
		RETURNING id`,
		a.Name, a.Monitored,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("books: create author: %w", err)
	}
	return id, nil
}

// GetAuthor loads an author by id.
func (r *BooksRepo) GetAuthor(ctx context.Context, id int64) (*books.Author, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, monitored, added_at
		FROM book_authors WHERE id = $1`, id)
	a := &books.Author{}
	err := row.Scan(&a.ID, &a.Name, &a.Monitored, &a.AddedAt)
	if err == sql.ErrNoRows {
		return nil, books.ErrAuthorNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("books: get author: %w", err)
	}
	return a, nil
}

// ListAuthors returns all authors, oldest first.
func (r *BooksRepo) ListAuthors(ctx context.Context) ([]books.Author, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, monitored, added_at
		FROM book_authors ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("books: list authors: %w", err)
	}
	defer rows.Close()
	var out []books.Author
	for rows.Next() {
		var a books.Author
		if err := rows.Scan(&a.ID, &a.Name, &a.Monitored, &a.AddedAt); err != nil {
			return nil, fmt.Errorf("books: scan author: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetAuthorMonitored toggles an author's monitored flag.
func (r *BooksRepo) SetAuthorMonitored(ctx context.Context, id int64, monitored bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE book_authors SET monitored = $2 WHERE id = $1`, id, monitored)
	if err != nil {
		return fmt.Errorf("books: set author monitored: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return books.ErrAuthorNotFound
	}
	return nil
}

// --- Titles ---

// CreateTitle inserts a title and returns its id.
func (r *BooksRepo) CreateTitle(ctx context.Context, t books.Title) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO book_titles (author_id, name, monitored)
		VALUES ($1, $2, $3)
		RETURNING id`,
		t.AuthorID, t.Name, t.Monitored,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("books: create title: %w", err)
	}
	return id, nil
}

// GetTitle loads a title by id.
func (r *BooksRepo) GetTitle(ctx context.Context, id int64) (*books.Title, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, author_id, name, monitored, added_at
		FROM book_titles WHERE id = $1`, id)
	t := &books.Title{}
	err := row.Scan(&t.ID, &t.AuthorID, &t.Name, &t.Monitored, &t.AddedAt)
	if err == sql.ErrNoRows {
		return nil, books.ErrTitleNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("books: get title: %w", err)
	}
	return t, nil
}

// ListTitles returns all titles for an author, oldest first.
func (r *BooksRepo) ListTitles(ctx context.Context, authorID int64) ([]books.Title, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, author_id, name, monitored, added_at
		FROM book_titles WHERE author_id = $1
		ORDER BY id`, authorID)
	if err != nil {
		return nil, fmt.Errorf("books: list titles: %w", err)
	}
	defer rows.Close()
	var out []books.Title
	for rows.Next() {
		var t books.Title
		if err := rows.Scan(&t.ID, &t.AuthorID, &t.Name, &t.Monitored, &t.AddedAt); err != nil {
			return nil, fmt.Errorf("books: scan title: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetTitleMonitored toggles a single title's monitored flag.
func (r *BooksRepo) SetTitleMonitored(ctx context.Context, id int64, monitored bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE book_titles SET monitored = $2 WHERE id = $1`, id, monitored)
	if err != nil {
		return fmt.Errorf("books: set title monitored: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return books.ErrTitleNotFound
	}
	return nil
}

// --- Editions ---

// CreateEdition inserts an edition and returns its id. The optional ISBN /
// publisher / year / pages are stored as NULL when empty so the (title, isbn)
// uniqueness constraint treats them as distinct.
func (r *BooksRepo) CreateEdition(ctx context.Context, e books.Edition) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO book_editions (title_id, format, publisher, isbn, year, pages, monitored)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, 0), NULLIF($6, 0), $7)
		RETURNING id`,
		e.TitleID, e.Format, e.Publisher, e.ISBN, e.Year, e.Pages, e.Monitored,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("books: create edition: %w", err)
	}
	return id, nil
}

// GetEdition loads an edition by id.
func (r *BooksRepo) GetEdition(ctx context.Context, id int64) (*books.Edition, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, title_id, format,
		       COALESCE(publisher, ''), COALESCE(isbn, ''),
		       COALESCE(year, 0), COALESCE(pages, 0),
		       monitored, added_at
		FROM book_editions WHERE id = $1`, id)
	e := &books.Edition{}
	err := row.Scan(&e.ID, &e.TitleID, &e.Format, &e.Publisher, &e.ISBN,
		&e.Year, &e.Pages, &e.Monitored, &e.AddedAt)
	if err == sql.ErrNoRows {
		return nil, books.ErrEditionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("books: get edition: %w", err)
	}
	return e, nil
}

// ListEditions returns all editions for a title, oldest first.
func (r *BooksRepo) ListEditions(ctx context.Context, titleID int64) ([]books.Edition, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, title_id, format,
		       COALESCE(publisher, ''), COALESCE(isbn, ''),
		       COALESCE(year, 0), COALESCE(pages, 0),
		       monitored, added_at
		FROM book_editions WHERE title_id = $1
		ORDER BY id`, titleID)
	if err != nil {
		return nil, fmt.Errorf("books: list editions: %w", err)
	}
	defer rows.Close()
	var out []books.Edition
	for rows.Next() {
		var e books.Edition
		if err := rows.Scan(&e.ID, &e.TitleID, &e.Format, &e.Publisher, &e.ISBN,
			&e.Year, &e.Pages, &e.Monitored, &e.AddedAt); err != nil {
			return nil, fmt.Errorf("books: scan edition: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetEditionMonitored toggles a single edition's monitored flag.
func (r *BooksRepo) SetEditionMonitored(ctx context.Context, id int64, monitored bool) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE book_editions SET monitored = $2 WHERE id = $1`, id, monitored)
	if err != nil {
		return fmt.Errorf("books: set edition monitored: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return books.ErrEditionNotFound
	}
	return nil
}

// --- Wanted (title or edition granularity) ---

// EnsureWantedTitle inserts a pending whole-title wanted row (edition_id = 0)
// if one does not already exist. Idempotent.
func (r *BooksRepo) EnsureWantedTitle(ctx context.Context, titleID int64) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO book_wanted (title_id, edition_id, status)
		VALUES ($1, 0, 'pending')
		ON CONFLICT (title_id, edition_id) DO NOTHING`, titleID)
	if err != nil {
		return fmt.Errorf("books: ensure wanted title: %w", err)
	}
	return nil
}

// EnsureWantedEdition inserts a pending edition-level wanted row if one does
// not already exist. Idempotent.
func (r *BooksRepo) EnsureWantedEdition(ctx context.Context, titleID, editionID int64) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO book_wanted (title_id, edition_id, status)
		VALUES ($1, $2, 'pending')
		ON CONFLICT (title_id, edition_id) DO NOTHING`, titleID, editionID)
	if err != nil {
		return fmt.Errorf("books: ensure wanted edition: %w", err)
	}
	return nil
}

// GetWanted loads a single wanted row by (title, edition) — edition 0 is the
// whole-title request.
func (r *BooksRepo) GetWanted(ctx context.Context, titleID, editionID int64) (*books.Wanted, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT title_id, edition_id, status, release_title, created_at, satisfied_at
		FROM book_wanted WHERE title_id = $1 AND edition_id = $2`,
		titleID, editionID)
	w := &books.Wanted{}
	var status string
	err := row.Scan(&w.TitleID, &w.EditionID, &status, &w.ReleaseTitle,
		&w.CreatedAt, &w.SatisfiedAt)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("books: get wanted: %w", err)
	}
	w.Status = books.WantedStatus(status)
	return w, nil
}

// ListWanted returns all wanted rows for a title (title-level first, then
// edition-level), ordered by edition id.
func (r *BooksRepo) ListWanted(ctx context.Context, titleID int64) ([]books.Wanted, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT title_id, edition_id, status, release_title, created_at, satisfied_at
		FROM book_wanted WHERE title_id = $1
		ORDER BY edition_id`, titleID)
	if err != nil {
		return nil, fmt.Errorf("books: list wanted: %w", err)
	}
	defer rows.Close()
	var out []books.Wanted
	for rows.Next() {
		var w books.Wanted
		var status string
		if err := rows.Scan(&w.TitleID, &w.EditionID, &status, &w.ReleaseTitle,
			&w.CreatedAt, &w.SatisfiedAt); err != nil {
			return nil, fmt.Errorf("books: scan wanted: %w", err)
		}
		w.Status = books.WantedStatus(status)
		out = append(out, w)
	}
	return out, rows.Err()
}

// MarkWantedSatisfied flips a wanted row to satisfied and records the release
// that satisfied it.
func (r *BooksRepo) MarkWantedSatisfied(ctx context.Context, titleID, editionID int64, releaseTitle string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE book_wanted
		SET status = 'satisfied', release_title = $3, satisfied_at = now()
		WHERE title_id = $1 AND edition_id = $2`,
		titleID, editionID, releaseTitle)
	if err != nil {
		return fmt.Errorf("books: satisfy wanted: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("books: no wanted row for title %d edition %d", titleID, editionID)
	}
	return nil
}

// --- Queue ---

// CreateQueue inserts a queue entry and returns its id.
func (r *BooksRepo) CreateQueue(ctx context.Context, e books.QueueEntry) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO book_queue
			(author_id, title_id, release_title, indexer, download_client, state, progress)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		e.AuthorID, e.TitleID, e.ReleaseTitle, e.Indexer,
		e.DownloadClient, e.State, e.Progress,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("books: create queue: %w", err)
	}
	return id, nil
}

// UpdateQueue updates a queue entry's state/progress.
func (r *BooksRepo) UpdateQueue(ctx context.Context, e books.QueueEntry) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE book_queue
		SET state = $2, progress = $3, updated_at = now()
		WHERE id = $1`, e.ID, e.State, e.Progress)
	if err != nil {
		return fmt.Errorf("books: update queue: %w", err)
	}
	return nil
}

// GetQueue loads a queue entry by id.
func (r *BooksRepo) GetQueue(ctx context.Context, id int64) (*books.QueueEntry, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, author_id, title_id, release_title, indexer, download_client,
		       state, progress, created_at, updated_at
		FROM book_queue WHERE id = $1`, id)
	e := &books.QueueEntry{}
	var state string
	err := row.Scan(&e.ID, &e.AuthorID, &e.TitleID, &e.ReleaseTitle,
		&e.Indexer, &e.DownloadClient, &state, &e.Progress, &e.CreatedAt, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("books: get queue: %w", err)
	}
	e.State = books.QueueState(state)
	return e, nil
}

// ListQueue returns all queue entries for an author, ordered by id.
func (r *BooksRepo) ListQueue(ctx context.Context, authorID int64) ([]books.QueueEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, author_id, title_id, release_title, indexer, download_client,
		       state, progress, created_at, updated_at
		FROM book_queue WHERE author_id = $1 ORDER BY id`, authorID)
	if err != nil {
		return nil, fmt.Errorf("books: list queue: %w", err)
	}
	defer rows.Close()
	var out []books.QueueEntry
	for rows.Next() {
		var e books.QueueEntry
		var state string
		if err := rows.Scan(&e.ID, &e.AuthorID, &e.TitleID, &e.ReleaseTitle,
			&e.Indexer, &e.DownloadClient, &state, &e.Progress, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("books: scan queue: %w", err)
		}
		e.State = books.QueueState(state)
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- History ---

// AddHistory appends a history event for an author.
func (r *BooksRepo) AddHistory(ctx context.Context, e books.HistoryEntry) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO book_history (author_id, event, detail)
		VALUES ($1, $2, $3)`, e.AuthorID, e.Event, e.Detail)
	if err != nil {
		return fmt.Errorf("books: add history: %w", err)
	}
	return nil
}

// ListHistory returns the most recent history events for an author (newest
// first).
func (r *BooksRepo) ListHistory(ctx context.Context, authorID int64, limit int) ([]books.HistoryEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, author_id, event, detail, at
		FROM book_history WHERE author_id = $1
		ORDER BY at DESC, id DESC LIMIT $2`, authorID, limit)
	if err != nil {
		return nil, fmt.Errorf("books: list history: %w", err)
	}
	defer rows.Close()
	var out []books.HistoryEntry
	for rows.Next() {
		var e books.HistoryEntry
		if err := rows.Scan(&e.ID, &e.AuthorID, &e.Event, &e.Detail, &e.At); err != nil {
			return nil, fmt.Errorf("books: scan history: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
