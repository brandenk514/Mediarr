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
	res, err := r.db.ExecContext(ctx, `
		UPDATE book_editions SET monitored = $2 WHERE id = $1`, id, monitored)
	if err != nil {
		return fmt.Errorf("books: set edition monitored: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return books.ErrEditionNotFound
	}
	return nil
}
