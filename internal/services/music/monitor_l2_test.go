package music

import (
	"context"
	"errors"
	"testing"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/music"
)

// strPtr wraps a string for the pointer fields of UpdateArtistInput.
func strPtr(s string) *string { return &s }

// tagsPtr wraps a tag list for the pointer field of UpdateArtistInput.
func tagsPtr(ts []string) *[]string { return &ts }

// TestAddAlbum_MonitorPolicy exercises the L2 monitor policy: when the
// caller does not request a monitored value, the artist's policy decides —
// all/missing (and empty) → on, none/existing → off, future → the release
// year is after the current year, first/last → the album is the
// chronologically first/last known album of the artist (the album being
// added included).
func TestAddAlbum_MonitorPolicy(t *testing.T) {
	year := time.Now().UTC().Year()
	cases := []struct {
		name          string
		policy        string
		existing      []int // years present on the artist before the add
		albumYear     int
		wantMonitored bool
	}{
		{"empty policy defaults to all", "", nil, 2020, true},
		{"all", dom.PolicyAll, nil, 2020, true},
		{"none", dom.PolicyNone, nil, 2020, false},
		{"missing", dom.PolicyMissing, nil, 2020, true},
		{"existing", dom.PolicyExisting, nil, 2020, false},
		{"future future year", dom.PolicyFuture, nil, year + 1, true},
		{"future current year", dom.PolicyFuture, nil, year, false},
		{"future past year", dom.PolicyFuture, nil, year - 5, false},
		{"first is the first", dom.PolicyFirst, []int{1977, 1980}, 1975, true},
		{"first is not the first", dom.PolicyFirst, []int{1977, 1980}, 1990, false},
		{"first unknown year", dom.PolicyFirst, []int{1977, 1980}, 0, false},
		{"last is the last", dom.PolicyLast, []int{1977, 1980}, 1990, true},
		{"last is not the last", dom.PolicyLast, []int{1977, 1980}, 1975, false},
		{"last unknown year", dom.PolicyLast, nil, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := setupService(t)
			ctx := context.Background()
			artistID, err := svc.AddArtist(ctx, AddArtistInput{Name: "Artist", Monitored: true, MonitorPolicy: tc.policy})
			if err != nil {
				t.Fatalf("add artist: %v", err)
			}
			for i, y := range tc.existing {
				if _, err := svc.AddAlbum(ctx, artistID, "Existing", y, "Lossless", boolPtr(false)); err != nil {
					t.Fatalf("add existing album %d: %v", i, err)
				}
			}
			albumID, err := svc.AddAlbum(ctx, artistID, "New", tc.albumYear, "Lossless", nil)
			if err != nil {
				t.Fatalf("add album: %v", err)
			}
			alb, err := repo.GetAlbum(ctx, albumID)
			if err != nil {
				t.Fatalf("get album: %v", err)
			}
			if alb.Monitored != tc.wantMonitored {
				t.Fatalf("monitored = %v, want %v (policy %q)", alb.Monitored, tc.wantMonitored, tc.policy)
			}
		})
	}
}

// TestAddAlbum_ExplicitMonitoredWins verifies that an explicitly requested
// monitored value beats the artist's monitor policy in both directions.
func TestAddAlbum_ExplicitMonitoredWins(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	idNone, err := svc.AddArtist(ctx, AddArtistInput{Name: "None Policy", Monitored: true, MonitorPolicy: dom.PolicyNone})
	if err != nil {
		t.Fatalf("add artist: %v", err)
	}
	onID, err := svc.AddAlbum(ctx, idNone, "Explicit On", 2020, "Lossless", boolPtr(true))
	if err != nil {
		t.Fatalf("add album: %v", err)
	}
	if a, _ := repo.GetAlbum(ctx, onID); !a.Monitored {
		t.Error("explicit true must win over the 'none' policy")
	}

	idAll, err := svc.AddArtist(ctx, AddArtistInput{Name: "All Policy", Monitored: true, MonitorPolicy: dom.PolicyAll})
	if err != nil {
		t.Fatalf("add artist: %v", err)
	}
	offID, err := svc.AddAlbum(ctx, idAll, "Explicit Off", 2020, "Lossless", boolPtr(false))
	if err != nil {
		t.Fatalf("add album: %v", err)
	}
	if a, _ := repo.GetAlbum(ctx, offID); a.Monitored {
		t.Error("explicit false must win over the 'all' policy")
	}
}

// TestUpdateArtist_L2Edit covers the L2 Edit Artist use-case: omitted
// (nil) fields keep the current value, provided fields are applied, the L1
// monitored toggle fans out to the artist's albums and records history, and
// an unknown artist is not found.
func TestUpdateArtist_L2Edit(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	id, err := svc.AddArtist(ctx, AddArtistInput{Name: "Bob Marley", Monitored: false})
	if err != nil {
		t.Fatalf("add artist: %v", err)
	}
	album1, err := svc.AddAlbum(ctx, id, "Legend", 1977, "Lossless", boolPtr(false))
	if err != nil {
		t.Fatalf("add album 1: %v", err)
	}
	album2, err := svc.AddAlbum(ctx, id, "Uprising", 1980, "Lossless", boolPtr(false))
	if err != nil {
		t.Fatalf("add album 2: %v", err)
	}

	// Monitored omitted: only the provided fields are applied; the monitored
	// state and its albums are untouched (no fan-out).
	if _, err := svc.UpdateArtist(ctx, id, UpdateArtistInput{
		MonitorPolicy:    strPtr(dom.PolicyFuture),
		MonitorNewAlbums: strPtr("all_albums"),
		QualityProfile:   strPtr("320K"),
		Path:             strPtr("bob"),
		Tags:             tagsPtr([]string{"reggae", "classic"}),
	}); err != nil {
		t.Fatalf("update artist: %v", err)
	}
	a, err := repo.GetArtist(ctx, id)
	if err != nil {
		t.Fatalf("get artist: %v", err)
	}
	if a.Monitored {
		t.Error("monitored must stay false when omitted from the update")
	}
	if a.MonitorPolicy != dom.PolicyFuture || a.MonitorNewAlbums != "all_albums" {
		t.Errorf("monitor fields = %q/%q, want future/all_albums", a.MonitorPolicy, a.MonitorNewAlbums)
	}
	if a.QualityProfile != "320K" || a.Path != "bob" {
		t.Errorf("profile/path = %q/%q, want 320K/bob", a.QualityProfile, a.Path)
	}
	if len(a.Tags) != 2 || a.Tags[0] != "reggae" || a.Tags[1] != "classic" {
		t.Errorf("tags = %v, want [reggae classic]", a.Tags)
	}
	if al, _ := repo.GetAlbum(ctx, album1); al.Monitored {
		t.Error("no fan-out expected without a monitored change")
	}

	// L1-style toggle: only Monitored provided. Fans out to both albums,
	// records history, and must not clobber the fields set above.
	updated, err := svc.UpdateArtist(ctx, id, UpdateArtistInput{Monitored: boolPtr(true)})
	if err != nil {
		t.Fatalf("toggle monitored: %v", err)
	}
	if !updated.Monitored {
		t.Error("monitored must be true after the toggle")
	}
	if updated.MonitorPolicy != dom.PolicyFuture || updated.QualityProfile != "320K" ||
		updated.Path != "bob" || len(updated.Tags) != 2 {
		t.Errorf("toggle clobbered other fields: %+v", updated)
	}
	for _, albumID := range []int64{album1, album2} {
		if al, _ := repo.GetAlbum(ctx, albumID); !al.Monitored {
			t.Errorf("fan-out: album %d must be monitored", albumID)
		}
	}
	if !historyHas(t, repo, id, "monitored") {
		t.Error("expected a 'monitored' history entry for the toggle")
	}

	if _, err := svc.UpdateArtist(ctx, 999, UpdateArtistInput{Monitored: boolPtr(false)}); !errors.Is(err, dom.ErrArtistNotFound) {
		t.Errorf("unknown artist: want ErrArtistNotFound, got %v", err)
	}
}
