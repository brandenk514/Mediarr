package books

import (
	"context"
	"testing"

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
