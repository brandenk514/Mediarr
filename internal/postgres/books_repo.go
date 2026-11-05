package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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
		SELECT id FROM book_authors WHERE name = $1`, a.Name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("books: create author: %w", err)
	}
	tags := nullSafeTags(a.Tags)
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO book_authors (name, monitored, monitor_policy, quality_profile,
		                             metadata_profile, root_folder, tags)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		a.Name, a.Monitored, a.MonitorPolicy, a.QualityProfile,
		a.MetadataProfile, a.RootFolder, tags,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("books: create author: %w", err)
	}
	return id, nil
}

// rowScanner is the surface scanAuthor needs; both *sql.Row and *sql.Rows
// satisfy it.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanAuthor scans one book_authors row (id, name, monitored, monitor_policy,
// quality_profile, metadata_profile, root_folder, tags, added_at) into an
// Author.
func scanAuthor(sc rowScanner) (books.Author, error) {
	var a books.Author
	var tags []string
	if err := sc.Scan(&a.ID, &a.Name, &a.Monitored, &a.MonitorPolicy,
		&a.QualityProfile, &a.MetadataProfile, &a.RootFolder, &tags, &a.AddedAt); err != nil {
		return a, err
	}
	a.Tags = nullSafeTags(tags)
	return a, nil
}

// GetAuthor loads an author by id.
func (r *BooksRepo) GetAuthor(ctx context.Context, id int64) (*books.Author, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, monitored, monitor_policy, quality_profile,
		       metadata_profile, root_folder, tags, added_at
		FROM book_authors WHERE id = $1`, id)
	a, err := scanAuthor(row)
	if err == sql.ErrNoRows {
		return nil, books.ErrAuthorNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("books: get author: %w", err)
	}
	return &a, nil
}

const authorColumns = `id, name, monitored, monitor_policy, quality_profile,
	                   metadata_profile, root_folder, tags, added_at`

// ListAuthors returns all authors, oldest first.
func (r *BooksRepo) ListAuthors(ctx context.Context) ([]books.Author, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+authorColumns+`
		FROM book_authors ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("books: list authors: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
	var out []books.Author
	for rows.Next() {
		a, err := scanAuthor(rows)
		if err != nil {
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

// UpdateAuthor writes the author's B2 editable fields in one row (Readarr B2
// edit modal, mirroring music's UpdateArtist). The caller passes the full
// merged author (current values plus the edits); the name is not writable.
func (r *BooksRepo) UpdateAuthor(ctx context.Context, a books.Author) error {
	tags := nullSafeTags(a.Tags)
	res, err := r.db.ExecContext(ctx, `
		UPDATE book_authors
		SET monitored = $2, monitor_policy = $3, quality_profile = $4,
		    metadata_profile = $5, root_folder = $6, tags = $7
		WHERE id = $1`,
		a.ID, a.Monitored, a.MonitorPolicy, a.QualityProfile,
		a.MetadataProfile, a.RootFolder, tags,
	)
	if err != nil {
		return fmt.Errorf("books: update author: %w", err)
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
		SELECT id FROM book_titles WHERE author_id = $1 AND name = $2`, t.AuthorID, t.Name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("books: create title: %w", err)
	}
	err = r.db.QueryRowContext(ctx, `
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
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
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

// CreateEdition inserts an edition and returns its id. The optional language
// / ISBN / publisher / year / pages are stored as NULL/” when empty so the
// (title, isbn) uniqueness constraint treats them as distinct.
func (r *BooksRepo) CreateEdition(ctx context.Context, e books.Edition) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO book_editions (title_id, format, language, publisher, isbn, year, pages, monitored)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, 0), NULLIF($7, 0), $8)
		RETURNING id`,
		e.TitleID, e.Format, e.Language, e.Publisher, e.ISBN, e.Year, e.Pages, e.Monitored,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("books: create edition: %w", err)
	}
	return id, nil
}

// GetEdition loads an edition by id.
func (r *BooksRepo) GetEdition(ctx context.Context, id int64) (*books.Edition, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, title_id, format, COALESCE(language, ''),
		       COALESCE(publisher, ''), COALESCE(isbn, ''),
		       COALESCE(year, 0), COALESCE(pages, 0),
		       monitored, added_at
		FROM book_editions WHERE id = $1`, id)
	e := &books.Edition{}
	err := row.Scan(&e.ID, &e.TitleID, &e.Format, &e.Language, &e.Publisher, &e.ISBN,
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
		SELECT id, title_id, format, COALESCE(language, ''),
		       COALESCE(publisher, ''), COALESCE(isbn, ''),
		       COALESCE(year, 0), COALESCE(pages, 0),
		       monitored, added_at
		FROM book_editions WHERE title_id = $1
		ORDER BY id`, titleID)
	if err != nil {
		return nil, fmt.Errorf("books: list editions: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
	var out []books.Edition
	for rows.Next() {
		var e books.Edition
		if err := rows.Scan(&e.ID, &e.TitleID, &e.Format, &e.Language, &e.Publisher, &e.ISBN,
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
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
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
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
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
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
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

// --- Book files (Readarr B1) ---

// CreateBookFile records an imported e-book file. (title_id, path) is the
// file's identity: a re-import of the same file is a no-op (the existing id is
// returned), so the pipeline can call it unconditionally after each import.
func (r *BooksRepo) CreateBookFile(ctx context.Context, f books.BookFile) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO book_files (title_id, edition_id, path, format, size, pages, language)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (title_id, path) DO UPDATE SET
			edition_id = EXCLUDED.edition_id,
			format     = EXCLUDED.format,
			size       = EXCLUDED.size,
			pages      = EXCLUDED.pages,
			language   = EXCLUDED.language
		RETURNING id`,
		f.TitleID, f.EditionID, f.Path, f.Format, f.Size, f.Pages, f.Language,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("books: create book file: %w", err)
	}
	return id, nil
}

// ListBookFilesByTitle returns a title's imported files, oldest first.
func (r *BooksRepo) ListBookFilesByTitle(ctx context.Context, titleID int64) ([]books.BookFile, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, title_id, edition_id, path, format, size, pages, language, added_at
		FROM book_files WHERE title_id = $1
		ORDER BY added_at, id`, titleID)
	if err != nil {
		return nil, fmt.Errorf("books: list book files: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
	var out []books.BookFile
	for rows.Next() {
		var f books.BookFile
		if err := rows.Scan(&f.ID, &f.TitleID, &f.EditionID, &f.Path, &f.Format,
			&f.Size, &f.Pages, &f.Language, &f.AddedAt); err != nil {
			return nil, fmt.Errorf("books: scan book file: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListBookFilesByEdition returns the files tied to one edition, oldest first.
func (r *BooksRepo) ListBookFilesByEdition(ctx context.Context, editionID int64) ([]books.BookFile, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, title_id, edition_id, path, format, size, pages, language, added_at
		FROM book_files WHERE edition_id = $1
		ORDER BY added_at, id`, editionID)
	if err != nil {
		return nil, fmt.Errorf("books: list book files by edition: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
	var out []books.BookFile
	for rows.Next() {
		var f books.BookFile
		if err := rows.Scan(&f.ID, &f.TitleID, &f.EditionID, &f.Path, &f.Format,
			&f.Size, &f.Pages, &f.Language, &f.AddedAt); err != nil {
			return nil, fmt.Errorf("books: scan book file: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// --- Flat Books index (Readarr B1) ---

// ListBooks returns every book across all authors, joined with its author's
// name, sorted by author name then title name (the flat Books index).
//
// Readarr B4: each row also carries the bookshelves rollup pair — HasFile
// (a correlated EXISTS over book_files; false = "missing") and Format (the
// newest imported file's format, else the newest edition's format, else "" —
// the book's resolvable quality). Both are computed in the query so the UI
// never has to fan out per-title file/edition requests.
func (r *BooksRepo) ListBooks(ctx context.Context) ([]books.Book, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT t.id, t.author_id, t.name, t.monitored, t.added_at, a.name,
		       EXISTS (SELECT 1 FROM book_files f WHERE f.title_id = t.id),
		       COALESCE(
		           (SELECT f.format FROM book_files f
		             WHERE f.title_id = t.id
		             ORDER BY f.added_at DESC, f.id DESC LIMIT 1),
		           (SELECT e.format FROM book_editions e
		             WHERE e.title_id = t.id
		             ORDER BY e.added_at DESC, e.id DESC LIMIT 1),
		           '')
		FROM book_titles t
		JOIN book_authors a ON a.id = t.author_id
		ORDER BY a.name, t.name, t.id`)
	if err != nil {
		return nil, fmt.Errorf("books: list books: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
	var out []books.Book
	for rows.Next() {
		var b books.Book
		if err := rows.Scan(&b.ID, &b.AuthorID, &b.Name, &b.Monitored, &b.AddedAt,
			&b.AuthorName, &b.HasFile, &b.Format); err != nil {
			return nil, fmt.Errorf("books: scan book: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// --- Series (Readarr B1) ---

// CreateSeries inserts a series by name and returns its id. Creating the same
// name again is idempotent: the existing id is returned (M6 #42 pattern).
func (r *BooksRepo) CreateSeries(ctx context.Context, s books.Series) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO book_series (name) VALUES ($1)
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`, s.Name).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("books: create series: %w", err)
	}
	return id, nil
}

// GetSeries loads a series by id.
func (r *BooksRepo) GetSeries(ctx context.Context, id int64) (*books.Series, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, added_at FROM book_series WHERE id = $1`, id)
	s := &books.Series{}
	if err := row.Scan(&s.ID, &s.Name, &s.AddedAt); err == sql.ErrNoRows {
		return nil, books.ErrSeriesNotFound
	} else if err != nil {
		return nil, fmt.Errorf("books: get series: %w", err)
	}
	return s, nil
}

// ListSeries returns all series, alphabetically.
func (r *BooksRepo) ListSeries(ctx context.Context) ([]books.Series, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, added_at FROM book_series ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("books: list series: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
	var out []books.Series
	for rows.Next() {
		var s books.Series
		if err := rows.Scan(&s.ID, &s.Name, &s.AddedAt); err != nil {
			return nil, fmt.Errorf("books: scan series: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListSeriesBooks returns the books (titles + author names) in a series,
// sorted by author name then title name. An unknown series is
// ErrSeriesNotFound. Readarr B4: each member also carries the bookshelves
// rollup pair (HasFile / Format, the same correlated subqueries as ListBooks)
// so the series page can show every member's missing state and quality
// without per-title file/edition requests.
func (r *BooksRepo) ListSeriesBooks(ctx context.Context, seriesID int64) ([]books.SeriesBook, error) {
	if _, err := r.GetSeries(ctx, seriesID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT t.id, t.author_id, t.name, t.monitored, t.added_at, a.name,
		       EXISTS (SELECT 1 FROM book_files f WHERE f.title_id = t.id),
		       COALESCE(
		           (SELECT f.format FROM book_files f
		             WHERE f.title_id = t.id
		             ORDER BY f.added_at DESC, f.id DESC LIMIT 1),
		           (SELECT e.format FROM book_editions e
		             WHERE e.title_id = t.id
		             ORDER BY e.added_at DESC, e.id DESC LIMIT 1),
		           '')
		FROM book_series_members m
		JOIN book_titles t ON t.id = m.title_id
		JOIN book_authors a ON a.id = t.author_id
		WHERE m.series_id = $1
		ORDER BY a.name, t.name, t.id`, seriesID)
	if err != nil {
		return nil, fmt.Errorf("books: list series books: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
	var out []books.SeriesBook
	for rows.Next() {
		var b books.SeriesBook
		if err := rows.Scan(&b.ID, &b.AuthorID, &b.Name, &b.Monitored, &b.AddedAt,
			&b.AuthorName, &b.HasFile, &b.Format); err != nil {
			return nil, fmt.Errorf("books: scan series book: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListTitleSeries returns the series a book (title) belongs to, alphabetically.
func (r *BooksRepo) ListTitleSeries(ctx context.Context, titleID int64) ([]books.Series, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.id, s.name, s.added_at
		FROM book_series s
		JOIN book_series_members m ON m.series_id = s.id
		WHERE m.title_id = $1
		ORDER BY s.name, s.id`, titleID)
	if err != nil {
		return nil, fmt.Errorf("books: list title series: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
	var out []books.Series
	for rows.Next() {
		var s books.Series
		if err := rows.Scan(&s.ID, &s.Name, &s.AddedAt); err != nil {
			return nil, fmt.Errorf("books: scan title series: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListAuthorSeries returns the series the author's books belong to, with the
// names of the member titles by this author (the author detail's series
// links). A series whose only member books are by other authors is omitted.
func (r *BooksRepo) ListAuthorSeries(ctx context.Context, authorID int64) ([]books.AuthorSeries, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.id, s.name, s.added_at, t.name
		FROM book_series s
		JOIN book_series_members m ON m.series_id = s.id
		JOIN book_titles t ON t.id = m.title_id AND t.author_id = $1
		ORDER BY s.name, s.id, t.name, t.id`, authorID)
	if err != nil {
		return nil, fmt.Errorf("books: list author series: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop above
	bySeries := map[int64]*books.AuthorSeries{}
	var order []int64
	for rows.Next() {
		var id int64
		var name, title string
		var addedAt time.Time
		if err := rows.Scan(&id, &name, &addedAt, &title); err != nil {
			return nil, fmt.Errorf("books: scan author series: %w", err)
		}
		as, ok := bySeries[id]
		if !ok {
			as = &books.AuthorSeries{Series: books.Series{ID: id, Name: name, AddedAt: addedAt}, Titles: []string{}}
			bySeries[id] = as
			order = append(order, id)
		}
		as.Titles = append(as.Titles, title)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("books: list author series: %w", err)
	}
	out := make([]books.AuthorSeries, 0, len(order))
	for _, id := range order {
		out = append(out, *bySeries[id])
	}
	return out, nil
}

// --- Readarr B6 (drop-in migration) identity lookups ---
//
// The migration's Postgres target needs create-vs-exists distinctions the
// upsert-style Create* methods cannot express, so each entity has a Find*
// companion keyed on the books domain's stable identity.

// FindAuthor returns the id of the author named name, or (0, false, nil)
// when no author has that name.
func (r *BooksRepo) FindAuthor(ctx context.Context, name string) (int64, bool, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `SELECT id FROM book_authors WHERE name = $1`, name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("books: find author: %w", err)
	}
	return id, true, nil
}

// FindTitle returns the id of the title named name under authorID, or
// (0, false, nil) when that author has no such title.
func (r *BooksRepo) FindTitle(ctx context.Context, authorID int64, name string) (int64, bool, error) {
	var id int64
	err := r.db.QueryRowContext(ctx,
		`SELECT id FROM book_titles WHERE author_id = $1 AND name = $2`, authorID, name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("books: find title: %w", err)
	}
	return id, true, nil
}

// FindEdition returns the id of the (format, isbn) edition under titleID, or
// (0, false, nil) when no edition matches. An empty isbn matches the
// edition with no ISBN (the domain's fallback identity).
func (r *BooksRepo) FindEdition(ctx context.Context, titleID int64, format, isbn string) (int64, bool, error) {
	var id int64
	err := r.db.QueryRowContext(ctx,
		`SELECT id FROM book_editions
		WHERE title_id = $1 AND format = $2
		  AND ($3 = '' AND (isbn IS NULL OR isbn = '') OR isbn = $3)`, titleID, format, isbn).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("books: find edition: %w", err)
	}
	return id, true, nil
}

// FindBookFile returns the id of the file at path under titleID, or
// (0, false, nil) when no file matches.
func (r *BooksRepo) FindBookFile(ctx context.Context, titleID int64, path string) (int64, bool, error) {
	var id int64
	err := r.db.QueryRowContext(ctx,
		`SELECT id FROM book_files WHERE title_id = $1 AND path = $2`, titleID, path).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("books: find book file: %w", err)
	}
	return id, true, nil
}

// FindSeries returns the id of the series named name, or (0, false, nil)
// when no series has that name.
func (r *BooksRepo) FindSeries(ctx context.Context, name string) (int64, bool, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `SELECT id FROM book_series WHERE name = $1`, name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("books: find series: %w", err)
	}
	return id, true, nil
}

// IsSeriesMember reports whether titleID is already a member of seriesID.
func (r *BooksRepo) IsSeriesMember(ctx context.Context, seriesID, titleID int64) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM book_series_members WHERE series_id = $1 AND title_id = $2`,
		seriesID, titleID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("books: is series member: %w", err)
	}
	return n > 0, nil
}

// LinkTitleToSeries links a book to a series. Both entities must exist
// (ErrTitleNotFound / ErrSeriesNotFound); linking twice is a no-op.
func (r *BooksRepo) LinkTitleToSeries(ctx context.Context, titleID, seriesID int64) error {
	if _, err := r.GetTitle(ctx, titleID); err != nil {
		return err
	}
	if _, err := r.GetSeries(ctx, seriesID); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO book_series_members (series_id, title_id)
		VALUES ($1, $2)
		ON CONFLICT (series_id, title_id) DO NOTHING`, seriesID, titleID)
	if err != nil {
		return fmt.Errorf("books: link title to series: %w", err)
	}
	return nil
}
