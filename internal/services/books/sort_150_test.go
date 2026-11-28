package books

import (
	"context"
	"errors"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/paging"
)

// Issue #150: the authors list endpoint validates ?sort= server-side
// (unknown field/direction → paging.ErrInvalidSort, the API maps it to
// 400) and orders the full set before paging. The books allowlist is
// title/added (the persisted author fields).

func TestListAuthorsQuery_UnknownSortRejected(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	for _, sort := range []string{"publisher-asc", "title-sideways"} {
		_, err := svc.ListAuthorsQuery(context.Background(), paging.Query{Sort: sort})
		if !errors.Is(err, paging.ErrInvalidSort) {
			t.Errorf("sort %q: err = %v, want paging.ErrInvalidSort", sort, err)
		}
	}
}

func TestListAuthorsQuery_SortsByTitle(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	if _, err := repo.CreateAuthor(context.Background(), dom.Author{Name: "Ursula K. Le Guin"}); err != nil {
		t.Fatalf("CreateAuthor: %v", err)
	}
	if _, err := repo.CreateAuthor(context.Background(), dom.Author{Name: "N.K. Jemisin"}); err != nil {
		t.Fatalf("CreateAuthor: %v", err)
	}

	page, err := svc.ListAuthorsQuery(context.Background(), paging.Query{Sort: "title-asc"})
	if err != nil {
		t.Fatalf("ListAuthorsQuery: %v", err)
	}
	var got []string
	for _, it := range page.Items {
		got = append(got, it.Name)
	}
	if len(got) != 2 || got[0] != "N.K. Jemisin" || got[1] != "Ursula K. Le Guin" {
		t.Fatalf("title-asc = %v, want [N.K. Jemisin, Ursula K. Le Guin]", got)
	}
}
