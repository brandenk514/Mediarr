package books

import (
	"context"
	"testing"

	"github.com/brandenk514/mediarr/internal/paging"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
)

// Issue #149 Stage A (books): the author's poster is the OpenLibrary
// covers.openlibrary.org cover of the first title's first ISBN'd edition
// (documented keyless scheme); BackdropURL stays "" (books have no
// landscape art). Authors without ISBN'd editions degrade to "".

func TestGetAuthor_PosterFromFirstEditionISBN(t *testing.T) {
	repo := newMemRepo()
	repo.authors[1] = dom.Author{ID: 1, Name: "Ursula K. Le Guin"}
	aid, err := repo.CreateTitle(context.Background(), dom.Title{AuthorID: 1, Name: "The Dispossessed"})
	if err != nil {
		t.Fatalf("CreateTitle: %v", err)
	}
	if _, err := repo.CreateEdition(context.Background(), dom.Edition{TitleID: aid, Format: "epub", ISBN: "9780441478125"}); err != nil {
		t.Fatalf("CreateEdition: %v", err)
	}
	svc := New(Deps{Repo: repo})

	a, err := svc.GetAuthor(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetAuthor: %v", err)
	}
	if want := "https://covers.openlibrary.org/b/isbn/9780441478125-L.jpg"; a.PosterURL != want {
		t.Fatalf("PosterURL = %q, want %q", a.PosterURL, want)
	}
	if a.BackdropURL != "" {
		t.Fatalf("BackdropURL = %q, want \"\" (books have no backdrop)", a.BackdropURL)
	}
}

func TestListAuthors_PosterFromFirstEditionISBN(t *testing.T) {
	repo := newMemRepo()
	repo.authors[1] = dom.Author{ID: 1, Name: "Ursula K. Le Guin"}
	repo.authors[2] = dom.Author{ID: 2, Name: "Unknown Author"} // no titles -> ""
	t1, err := repo.CreateTitle(context.Background(), dom.Title{AuthorID: 1, Name: "The Left Hand of Darkness"})
	if err != nil {
		t.Fatalf("CreateTitle: %v", err)
	}
	if _, err := repo.CreateEdition(context.Background(), dom.Edition{TitleID: t1, Format: "epub", ISBN: "9780441478125"}); err != nil {
		t.Fatalf("CreateEdition: %v", err)
	}
	svc := New(Deps{Repo: repo})

	list, err := svc.ListAuthors(context.Background())
	if err != nil {
		t.Fatalf("ListAuthors: %v", err)
	}
	byID := map[int64]dom.Author{}
	for _, a := range list {
		byID[a.ID] = a
	}
	if want := "https://covers.openlibrary.org/b/isbn/9780441478125-L.jpg"; byID[1].PosterURL != want {
		t.Fatalf("author 1 PosterURL = %q, want %q", byID[1].PosterURL, want)
	}
	if got := byID[2].PosterURL; got != "" {
		t.Fatalf("title-less author PosterURL = %q, want \"\"", got)
	}
	if byID[1].BackdropURL != "" || byID[2].BackdropURL != "" {
		t.Fatal("books BackdropURL must be empty")
	}
}

func TestGetAuthor_KeylessArtworkEmpty(t *testing.T) {
	repo := newMemRepo()
	repo.authors[1] = dom.Author{ID: 1, Name: "No Art Author"}
	// Keyless R3 for books (the documented keyless covers scheme): a
	// title with no ISBN'd edition has no cover source — the field
	// degrades to "" on the wire, never an error.
	if _, err := repo.CreateTitle(context.Background(), dom.Title{AuthorID: 1, Name: "Some Book"}); err != nil {
		t.Fatalf("CreateTitle: %v", err)
	}
	svc := New(Deps{Repo: repo})

	a, err := svc.GetAuthor(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetAuthor: %v", err)
	}
	if a.PosterURL != "" || a.BackdropURL != "" {
		t.Fatalf("keyless artwork = %q/%q, want empty/empty", a.PosterURL, a.BackdropURL)
	}
}

func TestListBooks_PosterFromFirstEditionISBN(t *testing.T) {
	repo := newMemRepo()
	repo.authors[1] = dom.Author{ID: 1, Name: "Ursula K. Le Guin"}
	t1, err := repo.CreateTitle(context.Background(), dom.Title{AuthorID: 1, Name: "The Dispossessed"})
	if err != nil {
		t.Fatalf("CreateTitle: %v", err)
	}
	if _, err := repo.CreateTitle(context.Background(), dom.Title{AuthorID: 1, Name: "Artless Title"}); err != nil { // no ISBN -> ""
		t.Fatalf("CreateTitle: %v", err)
	}
	if _, err := repo.CreateEdition(context.Background(), dom.Edition{TitleID: t1, Format: "epub", ISBN: "9780441478125"}); err != nil {
		t.Fatalf("CreateEdition: %v", err)
	}
	svc := New(Deps{Repo: repo})

	books, err := svc.ListBooks(context.Background())
	if err != nil {
		t.Fatalf("ListBooks: %v", err)
	}
	byName := map[string]dom.Book{}
	for _, b := range books {
		byName[b.Name] = b
	}
	if want := "https://covers.openlibrary.org/b/isbn/9780441478125-L.jpg"; byName["The Dispossessed"].PosterURL != want {
		t.Fatalf("The Dispossessed PosterURL = %q, want %q", byName["The Dispossessed"].PosterURL, want)
	}
	if got := byName["Artless Title"].PosterURL; got != "" {
		t.Fatalf("artless book PosterURL = %q, want \"\"", got)
	}
}

// TestListAuthorsQuery_PosterFromFirstEditionISBN pins the #149 list-poster
// behavior on the surface the GET /api/v1/books/authors endpoint actually
// calls (found by the run-5 live :8080 verification: the wire list served ""
// while the detail served the cover).
func TestListAuthorsQuery_PosterFromFirstEditionISBN(t *testing.T) {
	repo := newMemRepo()
	repo.authors[1] = dom.Author{ID: 1, Name: "Ursula K. Le Guin"}
	t1, err := repo.CreateTitle(context.Background(), dom.Title{AuthorID: 1, Name: "The Dispossessed"})
	if err != nil {
		t.Fatalf("CreateTitle: %v", err)
	}
	if _, err := repo.CreateEdition(context.Background(), dom.Edition{TitleID: t1, Format: "epub", ISBN: "9780441478125"}); err != nil {
		t.Fatalf("CreateEdition: %v", err)
	}
	svc := New(Deps{Repo: repo})

	page, err := svc.ListAuthorsQuery(context.Background(), paging.Query{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatalf("ListAuthorsQuery: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("authors = %d, want 1", len(page.Items))
	}
	if got := page.Items[0].PosterURL; got != "https://covers.openlibrary.org/b/isbn/9780441478125-L.jpg" {
		t.Fatalf("list PosterURL = %q, want the OpenLibrary ISBN cover", got)
	}
}
