package postgres_test

import (
	"context"
	"testing"

	tv "github.com/brandenk514/mediarr/internal/domains/tv"
)

// TestTVRepo_SeriesMetadataRoundTrip is the #157 integration test: the
// 0037 migration, SetSeriesMetadata, and the GetSeries metadata lift
// against a REAL Postgres (testcontainers). It covers both halves of the
// keyless contract at the persistence tier:
//   - a series with no metadata yet reads back with nil pointer fields,
//     "" string fields, nil/empty arrays, and nil Scores (never an error);
//   - persisted metadata lifts back onto the pointer fields exactly (the
//     enricher's write is what a later list read serves — no second
//     provider call needed).
func TestTVRepo_SeriesMetadataRoundTrip(t *testing.T) {
	repo := startTVPG(t)
	ctx := context.Background()

	id, err := repo.CreateSeries(ctx, tv.Series{
		Title: "Breaking Bad", Year: 2008, QualityProfile: "HD-1080p", Monitored: true,
	})
	if err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}

	// Keyless first: nothing enriched yet.
	fresh, err := repo.GetSeries(ctx, id)
	if err != nil {
		t.Fatalf("GetSeries (fresh): %v", err)
	}
	if fresh.Overview != nil || fresh.Status != nil || fresh.Network != nil ||
		fresh.OriginalLanguage != nil || fresh.Scores != nil {
		t.Fatalf("fresh series must read keyless-clean: %+v", fresh)
	}
	if fresh.FirstAirDate != "" || fresh.ImdbID != "" || fresh.TraktID != "" || fresh.TrailerURL != "" {
		t.Fatalf("fresh series string fields must be empty: %+v", fresh)
	}
	// The service layer (normalizeSeriesArrays) lifts nil arrays to [];
	// at the repo tier an empty TEXT[] may scan as nil or [] — both are
	// "empty", so assert emptiness, not nil-ness.
	if len(fresh.Genres) != 0 || len(fresh.Keywords) != 0 || len(fresh.ProductionCountries) != 0 ||
		len(fresh.Cast) != 0 || len(fresh.Crew) != 0 {
		t.Fatalf("fresh series arrays must be empty: %+v", fresh)
	}

	// Now the enricher's write: SetSeriesMetadata + SetSeriesArtwork, the
	// same pair GetSeries issues after a detail fetch.
	err = repo.SetSeriesMetadata(ctx, id, tv.SeriesMetadata{
		Genres:              []string{"Drama", "Crime"},
		Keywords:            []string{"drug kingpin"},
		OriginalLanguage:    "en",
		Status:              "Ended",
		Network:             "AMC",
		FirstAirDate:        "2008-01-20",
		ProductionCountries: []string{"United States", "Mexico"},
		TmdbPct:             89,
	})
	if err != nil {
		t.Fatalf("SetSeriesMetadata: %v", err)
	}
	if err := repo.SetSeriesArtwork(ctx, id, "/bb-poster.jpg", "/bb-backdrop.jpg"); err != nil {
		t.Fatalf("SetSeriesArtwork: %v", err)
	}

	got, err := repo.GetSeries(ctx, id)
	if err != nil {
		t.Fatalf("GetSeries (after enrich): %v", err)
	}
	if len(got.Genres) != 2 || got.Genres[0] != "Drama" || got.Genres[1] != "Crime" {
		t.Fatalf("Genres = %v", got.Genres)
	}
	if len(got.Keywords) != 1 || got.Keywords[0] != "drug kingpin" {
		t.Fatalf("Keywords = %v", got.Keywords)
	}
	if got.OriginalLanguage == nil || *got.OriginalLanguage != "en" {
		t.Fatalf("OriginalLanguage = %v", got.OriginalLanguage)
	}
	if got.Status == nil || *got.Status != "Ended" {
		t.Fatalf("Status = %v", got.Status)
	}
	if got.Network == nil || *got.Network != "AMC" {
		t.Fatalf("Network = %v", got.Network)
	}
	if got.FirstAirDate != "2008-01-20" {
		t.Fatalf("FirstAirDate = %q", got.FirstAirDate)
	}
	if len(got.ProductionCountries) != 2 || got.ProductionCountries[0] != "United States" {
		t.Fatalf("ProductionCountries = %v", got.ProductionCountries)
	}
	if got.Scores == nil || got.Scores.TmdbPct == nil || *got.Scores.TmdbPct != 89 {
		t.Fatalf("Scores = %+v", got.Scores)
	}
	if got.PosterURL != "/bb-poster.jpg" || got.BackdropURL != "/bb-backdrop.jpg" {
		t.Fatalf("artwork = %q / %q", got.PosterURL, got.BackdropURL)
	}
	// The detail-only fields are never written here — a list read must
	// still show them empty.
	if got.Overview != nil || got.TrailerURL != "" || len(got.Cast) != 0 || len(got.Crew) != 0 || got.ImdbID != "" {
		t.Fatalf("detail-only fields must stay empty: %+v", got)
	}

	// Idempotence: re-writing the same metadata is a no-op on read.
	if err := repo.SetSeriesMetadata(ctx, id, tv.SeriesMetadata{
		Genres: []string{"Drama", "Crime"}, Keywords: []string{"drug kingpin"},
		OriginalLanguage: "en", Status: "Ended", Network: "AMC",
		FirstAirDate: "2008-01-20", ProductionCountries: []string{"United States", "Mexico"},
		TmdbPct: 89,
	}); err != nil {
		t.Fatalf("SetSeriesMetadata (again): %v", err)
	}
	again, err := repo.GetSeries(ctx, id)
	if err != nil {
		t.Fatalf("GetSeries (again): %v", err)
	}
	if again.Scores == nil || *again.Scores.TmdbPct != 89 || again.Status == nil || *again.Status != "Ended" {
		t.Fatalf("re-read after rewrite = %+v", again.Scores)
	}

	// A series that never gets metadata stays keyless-clean forever.
	other, err := repo.CreateSeries(ctx, tv.Series{
		Title: "No Metadata Show", Year: 2020, QualityProfile: "HD-1080p", Monitored: false,
	})
	if err != nil {
		t.Fatalf("CreateSeries (other): %v", err)
	}
	od, err := repo.GetSeries(ctx, other)
	if err != nil {
		t.Fatalf("GetSeries (other): %v", err)
	}
	if od.Status != nil || od.Scores != nil || len(od.Genres) != 0 || od.FirstAirDate != "" {
		t.Fatalf("un-enriched series must stay keyless-clean: %+v", od)
	}
}
