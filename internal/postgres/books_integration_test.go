package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	books "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// startBooksPG spins up a Postgres container, opens a pool, runs migrations,
// and returns a *BooksRepo (and the raw *sql.DB for schema-level assertions).
// Mirrors the music/auth integration-test harness.
func startBooksPG(t *testing.T) (*postgres.BooksRepo, *sql.DB) {
	t.Helper()
	ctx := context.Background()

	c, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("mediarr"),
		tcpostgres.WithUsername("mediarr"),
		tcpostgres.WithPassword("mediarr-test"),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := db.PingContext(ctx); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ping: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	st := postgres.NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return postgres.NewBooksRepo(db), db
}

// TestBooksRepo_AuthorTitleLifecycle exercises the author/title CRUD round-trip
// against real Postgres (the 0005_books schema + BooksRepo SQL).
func TestBooksRepo_AuthorTitleLifecycle(t *testing.T) {
	r, _ := startBooksPG(t)
	ctx := context.Background()

	authorID, err := r.CreateAuthor(ctx, books.Author{Name: "Ursula K. Le Guin", Monitored: true})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}
	if authorID == 0 {
		t.Fatal("expected non-zero author id")
	}

	a, err := r.GetAuthor(ctx, authorID)
	if err != nil {
		t.Fatalf("get author: %v", err)
	}
	if a.Name != "Ursula K. Le Guin" {
		t.Errorf("author = %q, want Ursula K. Le Guin", a.Name)
	}
	if !a.Monitored {
		t.Error("author should be monitored")
	}
	if a.AddedAt.IsZero() {
		t.Error("added_at should be set (defaults to now())")
	}

	// Unknown id → ErrAuthorNotFound.
	if _, err := r.GetAuthor(ctx, 9999); err != books.ErrAuthorNotFound {
		t.Errorf("get missing author: err = %v, want ErrAuthorNotFound", err)
	}

	// Duplicate author name must be rejected by the UNIQUE constraint.
	if _, err := r.CreateAuthor(ctx, books.Author{Name: "Ursula K. Le Guin"}); err == nil {
		t.Error("duplicate author name should fail")
	}

	all, err := r.ListAuthors(ctx)
	if err != nil {
		t.Fatalf("list authors: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("list authors len = %d, want 1", len(all))
	}

	// Titles: (author, name) identity within an author.
	titleID, err := r.CreateTitle(ctx, books.Title{
		AuthorID: authorID, Name: "The Dispossessed", Monitored: true,
	})
	if err != nil {
		t.Fatalf("create title: %v", err)
	}
	ti, err := r.GetTitle(ctx, titleID)
	if err != nil {
		t.Fatalf("get title: %v", err)
	}
	if ti.Name != "The Dispossessed" || ti.AuthorID != authorID {
		t.Errorf("title = %+v, want The Dispossessed", *ti)
	}

	// Duplicate (author, name) must be rejected.
	if _, err := r.CreateTitle(ctx, books.Title{AuthorID: authorID, Name: "The Dispossessed"}); err == nil {
		t.Error("duplicate (author,name) title should fail")
	}

	// A different title by the same author is fine.
	if _, err := r.CreateTitle(ctx, books.Title{AuthorID: authorID, Name: "The Left Hand of Darkness"}); err != nil {
		t.Fatalf("create second title: %v", err)
	}

	titles, err := r.ListTitles(ctx, authorID)
	if err != nil {
		t.Fatalf("list titles: %v", err)
	}
	if len(titles) != 2 {
		t.Errorf("titles len = %d, want 2", len(titles))
	}

	// Toggle title monitoring.
	if err := r.SetTitleMonitored(ctx, titleID, false); err != nil {
		t.Fatalf("set title monitored: %v", err)
	}
	ti, _ = r.GetTitle(ctx, titleID)
	if ti.Monitored {
		t.Error("title should be unmonitored after SetTitleMonitored(false)")
	}
}

// TestBooksRepo_EditionLifecycle exercises edition CRUD + the (title, isbn)
// identity, including the NULL-ISBN fallback (two editions of the same title
// with no ISBN are both allowed to coexist).
func TestBooksRepo_EditionLifecycle(t *testing.T) {
	r, _ := startBooksPG(t)
	ctx := context.Background()

	authorID, _ := r.CreateAuthor(ctx, books.Author{Name: "Frank Herbert"})
	titleID, _ := r.CreateTitle(ctx, books.Title{AuthorID: authorID, Name: "Dune"})

	// A fully-specified edition.
	eid, err := r.CreateEdition(ctx, books.Edition{
		TitleID: titleID, Format: "epub",
		Publisher: "Ace", ISBN: "9780441172719", Year: 1990, Pages: 896,
		Monitored: true,
	})
	if err != nil {
		t.Fatalf("create edition: %v", err)
	}
	e, err := r.GetEdition(ctx, eid)
	if err != nil {
		t.Fatalf("get edition: %v", err)
	}
	if e.Format != "epub" || e.Publisher != "Ace" || e.ISBN != "9780441172719" ||
		e.Year != 1990 || e.Pages != 896 {
		t.Errorf("edition = %+v, want epub/Ace/978.../1990/896", *e)
	}

	// Unknown id → ErrEditionNotFound.
	if _, err := r.GetEdition(ctx, 9999); err != books.ErrEditionNotFound {
		t.Errorf("get missing edition: err = %v, want ErrEditionNotFound", err)
	}

	// Same (title, isbn) must be rejected (regardless of format).
	if _, err := r.CreateEdition(ctx, books.Edition{TitleID: titleID, Format: "mobi", ISBN: "9780441172719"}); err == nil {
		t.Error("duplicate (title,isbn) edition should fail")
	}

	// A no-ISBN edition is allowed alongside them: Postgres treats NULLs as
	// distinct in the (title, isbn) UNIQUE constraint, so editions without an
	// ISBN are not collapsed onto one row.
	if _, err := r.CreateEdition(ctx, books.Edition{TitleID: titleID, Format: "azw3"}); err != nil {
		t.Fatalf("create no-isbn azw3 edition: %v", err)
	}

	editions, err := r.ListEditions(ctx, titleID)
	if err != nil {
		t.Fatalf("list editions: %v", err)
	}
	if len(editions) != 2 {
		t.Errorf("editions len = %d, want 2 (epub + azw3)", len(editions))
	}

	// Toggle a single edition's monitoring.
	if err := r.SetEditionMonitored(ctx, eid, false); err != nil {
		t.Fatalf("set edition monitored: %v", err)
	}
	e2, _ := r.GetEdition(ctx, eid)
	if e2.Monitored {
		t.Error("edition should be unmonitored after SetEditionMonitored(false)")
	}
}

// TestBooksRepo_CascadeDeletion verifies that deleting an author cascades to
// their titles and editions (the ON DELETE CASCADE constraints), so there are
// no orphans left behind.
func TestBooksRepo_CascadeDeletion(t *testing.T) {
	r, db := startBooksPG(t)
	ctx := context.Background()

	authorID, _ := r.CreateAuthor(ctx, books.Author{Name: "Ted Chiang"})
	titleID, _ := r.CreateTitle(ctx, books.Title{AuthorID: authorID, Name: "Stories of Your Life"})
	editionID, _ := r.CreateEdition(ctx, books.Edition{TitleID: titleID, Format: "epub", ISBN: "9781250156869"})

	// Delete the author by id; titles and editions must cascade.
	if _, err := db.ExecContext(ctx, `DELETE FROM book_authors WHERE id = $1`, authorID); err != nil {
		t.Fatalf("delete author: %v", err)
	}

	if _, err := r.GetTitle(ctx, titleID); err != books.ErrTitleNotFound {
		t.Errorf("title after author delete: err = %v, want ErrTitleNotFound", err)
	}
	if _, err := r.GetEdition(ctx, editionID); err != books.ErrEditionNotFound {
		t.Errorf("edition after author delete: err = %v, want ErrEditionNotFound", err)
	}
	// ListAuthors is now empty.
	all, err := r.ListAuthors(ctx)
	if err != nil {
		t.Fatalf("list authors: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("authors len = %d, want 0 after cascade delete", len(all))
	}
}
