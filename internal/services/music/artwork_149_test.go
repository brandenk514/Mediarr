package music

import (
	"context"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/music"
)

// Issue #149 Stage A (music): the artist's persisted Cover Art Archive cover
// (the L4 Cover field) surfaces as PosterURL on the detail and list
// documents; BackdropURL stays "" (music has no landscape art). Keyless or
// cover-less artists degrade to "".

func TestGetArtist_MapsCoverToPosterURL(t *testing.T) {
	repo := newMemRepo()
	repo.artists[1] = dom.Artist{ID: 1, Name: "Radiohead", Cover: "https://coverartarchive.org/release-group/a1/poster.jpg"}
	svc := New(Deps{Repo: repo})

	a, err := svc.GetArtist(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetArtist: %v", err)
	}
	if want := "https://coverartarchive.org/release-group/a1/poster.jpg"; a.PosterURL != want {
		t.Fatalf("PosterURL = %q, want %q", a.PosterURL, want)
	}
	if a.BackdropURL != "" {
		t.Fatalf("BackdropURL = %q, want \"\" (music has no backdrop)", a.BackdropURL)
	}
}

func TestListArtists_MapsCoverToPosterURL(t *testing.T) {
	repo := newMemRepo()
	repo.artists[1] = dom.Artist{ID: 1, Name: "Radiohead", Cover: "https://caa.example/r1.jpg"}
	repo.artists[2] = dom.Artist{ID: 2, Name: "Unknown"} // cover-less -> ""
	svc := New(Deps{Repo: repo})

	list, err := svc.ListArtists(context.Background())
	if err != nil {
		t.Fatalf("ListArtists: %v", err)
	}
	byID := map[int64]dom.Artist{}
	for _, a := range list {
		byID[a.ID] = a
	}
	if got := byID[1].PosterURL; got != "https://caa.example/r1.jpg" {
		t.Fatalf("artist 1 PosterURL = %q, want the persisted cover", got)
	}
	if got := byID[2].PosterURL; got != "" {
		t.Fatalf("cover-less artist PosterURL = %q, want \"\"", got)
	}
	if byID[1].BackdropURL != "" || byID[2].BackdropURL != "" {
		t.Fatal("music BackdropURL must be empty")
	}
}

func TestGetArtist_KeylessArtworkEmpty(t *testing.T) {
	repo := newMemRepo()
	repo.artists[1] = dom.Artist{ID: 1, Name: "No Art"} // no persisted cover: keyless degrades to ""
	svc := New(Deps{Repo: repo})

	a, err := svc.GetArtist(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetArtist: %v", err)
	}
	if a.PosterURL != "" || a.BackdropURL != "" {
		t.Fatalf("keyless artwork = %q/%q, want empty/empty", a.PosterURL, a.BackdropURL)
	}
}
