package tv

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/tv"
)

// --- P2 parity tests: new series fields, series update, episode search,
// series history, and library scan/import.

func TestAddSeriesP2Fields(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	// Defaults: series_type "standard", empty tags.
	id, err := svc.AddSeries(ctx, AddSeriesRequest{
		Title: "Daily Show", Year: "2010", QualityProfile: "HD-1080p", Monitored: true,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	s, _ := repo.GetSeries(ctx, id)
	if s.SeriesType != "standard" {
		t.Errorf("series type = %q, want default standard", s.SeriesType)
	}
	if s.SeasonFolder {
		t.Error("season folder should default to false")
	}
	if s.Tags == nil || len(s.Tags) != 0 {
		t.Errorf("tags = %v, want empty non-nil slice", s.Tags)
	}

	// Explicit values round-trip.
	id2, err := svc.AddSeries(ctx, AddSeriesRequest{
		Title: "One Shot", Year: "2015", QualityProfile: "SD", Monitored: false,
		SeriesType: "oneshot", SeasonFolder: true, Tags: []string{"retro", "sci-fi"},
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	s2, _ := repo.GetSeries(ctx, id2)
	if s2.SeriesType != "oneshot" || !s2.SeasonFolder {
		t.Errorf("series = %+v, want oneshot + season folder", *s2)
	}
	if len(s2.Tags) != 2 || s2.Tags[0] != "retro" || s2.Tags[1] != "sci-fi" {
		t.Errorf("tags = %v, want [retro sci-fi]", s2.Tags)
	}
	if s2.QualityProfile != "SD" {
		t.Errorf("profile = %q, want SD (per-series quality profile)", s2.QualityProfile)
	}
}

func TestUpdateSeries(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	id, _ := svc.AddSeries(ctx, AddSeriesRequest{
		Title: "Severance", Year: "2022", QualityProfile: "HD-1080p", Monitored: true,
	})

	// Partial update: quality profile + tags.
	profile := "UHD-2160p"
	tags := []string{"drama"}
	updated, err := svc.UpdateSeries(ctx, id, dom.SeriesPatch{
		QualityProfile: &profile,
		Tags:           &tags,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.QualityProfile != "UHD-2160p" {
		t.Errorf("profile = %q, want UHD-2160p", updated.QualityProfile)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "drama" {
		t.Errorf("tags = %v, want [drama]", updated.Tags)
	}
	// Untouched fields survive the partial update.
	if !updated.Monitored || updated.SeriesType != "standard" {
		t.Errorf("untouched fields changed: %+v", *updated)
	}

	// A monitored change propagates to episodes (like the PUT toggle).
	_, _ = svc.AddEpisode(ctx, id, 1, 1, "Pilot", nil, true)
	mon := false
	if _, err := svc.UpdateSeries(ctx, id, dom.SeriesPatch{Monitored: &mon}); err != nil {
		t.Fatalf("update monitored: %v", err)
	}
	ep, _ := repo.GetEpisode(ctx, id, 1, 1)
	if ep.Monitored {
		t.Error("episode should be unmonitored after series-level unmonitor")
	}

	// Unknown series → ErrSeriesNotFound.
	if _, err := svc.UpdateSeries(ctx, 999, dom.SeriesPatch{}); err != dom.ErrSeriesNotFound {
		t.Errorf("update missing err = %v, want ErrSeriesNotFound", err)
	}
}

func TestUpdateSeriesRecorded(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	id, _ := svc.AddSeries(ctx, AddSeriesRequest{Title: "Fringe", Year: "2008", Monitored: true})
	st := "daily"
	if _, err := svc.UpdateSeries(ctx, id, dom.SeriesPatch{SeriesType: &st}); err != nil {
		t.Fatalf("update: %v", err)
	}
	hist, _ := repo.ListHistory(ctx, id, 0)
	found := false
	for _, h := range hist {
		if h.Event == "series-updated" && h.Detail == "series_type=daily" {
			found = true
		}
	}
	if !found {
		t.Error("expected a 'series-updated' history entry with series_type=daily detail")
	}
}

func TestSearchMissing(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	id, _ := svc.AddSeries(ctx, AddSeriesRequest{Title: "Fringe", Year: "2008", Monitored: true})
	// S01E01 monitored, S01E02 unmonitored.
	if _, err := svc.AddEpisode(ctx, id, 1, 1, "Pilot", nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddEpisode(ctx, id, 1, 2, "The Box", nil, false); err != nil {
		t.Fatal(err)
	}

	// First run: S01E01 (monitored, no wanted row yet from AddEpisode? AddEpisode
	// with monitored=true already ensured one — count stays 1) → searched.
	n, err := svc.SearchMissing(ctx, id)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if n != 1 {
		t.Errorf("searched = %d, want 1 (S01E01 only; S01E02 unmonitored)", n)
	}

	// Satisfy S01E01, then a re-run finds nothing missing.
	if err := repo.MarkWantedSatisfied(ctx, id, 1, 1, "Fringe.S01E01.1080p.WEB.x264", "HDTV-1080p"); err != nil {
		t.Fatal(err)
	}
	n, err = svc.SearchMissing(ctx, id)
	if err != nil {
		t.Fatalf("search 2: %v", err)
	}
	if n != 0 {
		t.Errorf("searched = %d, want 0 (S01E01 satisfied, S01E02 unmonitored)", n)
	}

	// Unknown series → ErrSeriesNotFound.
	if _, err := svc.SearchMissing(ctx, 999); err != dom.ErrSeriesNotFound {
		t.Errorf("search missing err = %v, want ErrSeriesNotFound", err)
	}
}

func TestListHistory(t *testing.T) {
	svc, repo := setupService(t)
	ctx := context.Background()

	id, _ := svc.AddSeries(ctx, AddSeriesRequest{Title: "Westworld", Year: "2016", Monitored: true})
	_ = repo.AddHistory(ctx, dom.HistoryEntry{SeriesID: id, Event: "download", Detail: "x"})

	all, err := svc.ListHistory(ctx, id, 0)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("history len = %d, want 2 (added + download)", len(all))
	}
	if all[0].Event != "download" {
		t.Errorf("newest-first: [0] = %q, want download", all[0].Event)
	}

	// Limit clamps.
	limited, err := svc.ListHistory(ctx, id, 1)
	if err != nil {
		t.Fatalf("history limit: %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("limited len = %d, want 1", len(limited))
	}

	if _, err := svc.ListHistory(ctx, 999, 0); err != dom.ErrSeriesNotFound {
		t.Errorf("history missing err = %v, want ErrSeriesNotFound", err)
	}
}

func TestScanLibrary(t *testing.T) {
	svc, _ := setupService(t)
	ctx := context.Background()

	seeded, _ := svc.AddSeries(ctx, AddSeriesRequest{Title: "Severance", Year: "2022", Monitored: true})

	root := t.TempDir()
	// Known series: "Severance (2022)/Season 01/..." with two video files.
	seasonDir := filepath.Join(root, "Severance (2022)", "Season 01")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seasonDir, "a.mkv"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seasonDir, "b.mp4"), []byte("01234567890123"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Unknown series with one video file.
	unknownDir := filepath.Join(root, "Unknown.Show", "Season 1")
	if err := os.MkdirAll(unknownDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unknownDir, "c.avi"), []byte("xx"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Same title, wrong year → must NOT match.
	mismatchDir := filepath.Join(root, "Severance (2023)")
	if err := os.MkdirAll(mismatchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stray file at the root is not a candidate.
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := svc.ScanLibrary(ctx, root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if report.Scanned != 3 {
		t.Fatalf("scanned = %d, want 3", report.Scanned)
	}
	if len(report.Candidates) != 3 {
		t.Fatalf("candidates = %d, want 3", len(report.Candidates))
	}

	byTitleYear := map[string]dom.ImportCandidate{}
	for _, c := range report.Candidates {
		byTitleYear[c.Title+"|"+itoa(c.Year)] = c
	}

	known, ok := byTitleYear["Severance|2022"]
	if !ok {
		t.Fatal("candidate 'Severance (2022)' missing")
	}
	if !known.Matched || known.SeriesID != seeded {
		t.Errorf("known candidate: matched=%v id=%d, want matched to %d", known.Matched, known.SeriesID, seeded)
	}
	if known.Seasons != 1 || known.Files != 2 || known.Size != 24 {
		t.Errorf("known candidate = %+v, want 1 season, 2 files, 24 bytes", known)
	}

	unknown, ok := byTitleYear["Unknown.Show|0"]
	if !ok {
		t.Fatal("candidate 'Unknown.Show' missing")
	}
	if unknown.Matched {
		t.Error("unknown candidate should not match")
	}
	if unknown.Files != 1 || unknown.Seasons != 1 {
		t.Errorf("unknown candidate = %+v, want 1 file, 1 season", unknown)
	}

	mismatch, ok := byTitleYear["Severance|2023"]
	if !ok {
		t.Fatal("candidate 'Severance (2023)' missing")
	}
	if mismatch.Matched {
		t.Error("year-mismatched folder must not match the 2022 series")
	}
	_ = mismatch

	// Nonexistent root → error.
	if _, err := svc.ScanLibrary(ctx, filepath.Join(root, "nope")); err == nil {
		t.Error("expected error scanning a nonexistent root")
	}
	// Empty root → error.
	if _, err := svc.ScanLibrary(ctx, "  "); err == nil {
		t.Error("expected error for empty root")
	}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
