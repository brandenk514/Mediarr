package tv

import (
	"context"
	"errors"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/tv"
	"github.com/brandenk514/mediarr/internal/paging"
)

// Issue #150: the series list endpoint validates ?sort= server-side
// (unknown field/direction → paging.ErrInvalidSort, the API maps it to
// 400) and orders the full set before paging. Until the #157 TV metadata
// enrichment lands, the sort allowlist is title/year/added.

func seedSeries(t *testing.T, repo *memRepo, title string, year int) {
	t.Helper()
	id, err := repo.CreateSeries(context.Background(), dom.Series{Title: title, Year: year})
	if err != nil {
		t.Fatalf("CreateSeries %s: %v", title, err)
	}
	_ = id
}

func TestListSeriesQuery_UnknownSortRejected(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	for _, sort := range []string{"genre-asc", "title-sideways"} {
		_, err := svc.ListSeriesQuery(context.Background(), paging.Query{Sort: sort})
		if !errors.Is(err, paging.ErrInvalidSort) {
			t.Errorf("sort %q: err = %v, want paging.ErrInvalidSort", sort, err)
		}
	}
}

func TestListSeriesQuery_SortsByTitle(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	seedSeries(t, repo, "Breaking Bad", 2008)
	seedSeries(t, repo, "Arrow", 2012)
	seedSeries(t, repo, "The Wire", 2002)

	page, err := svc.ListSeriesQuery(context.Background(), paging.Query{Sort: "title-asc"})
	if err != nil {
		t.Fatalf("ListSeriesQuery: %v", err)
	}
	var got []string
	for _, it := range page.Items {
		got = append(got, it.Title)
	}
	// Article-skipping, case-insensitive title order (the existing movie
	// title-sort convention): Arrow, Breaking Bad, The Wire.
	if len(got) != 3 || got[0] != "Arrow" || got[1] != "Breaking Bad" || got[2] != "The Wire" {
		t.Fatalf("title-asc = %v, want [Arrow, Breaking Bad, The Wire]", got)
	}
}
