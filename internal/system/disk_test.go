package system

import (
	"errors"
	"testing"
)

func TestBuildDiskDedupesAndPercents(t *testing.T) {
	fakeStat := func(path string) (total, used, free int64, err error) {
		return 1000, 400, 600, nil
	}
	// A real existing dir for the media root; downloads + /other are not real.
	media := t.TempDir()
	report := BuildDisk(media, "/data/downloads", []string{media, "/other"}, fakeStat)

	if len(report.Roots) != 3 {
		t.Fatalf("expected 3 roots (deduped), got %d: %+v", len(report.Roots), report.Roots)
	}
	if report.Roots[0].Path != media {
		t.Fatalf("first root=%q want %q", report.Roots[0].Path, media)
	}
	if !report.Roots[0].Exists {
		t.Fatalf("media root should exist")
	}
	if report.Roots[0].TotalBytes != 1000 || report.Roots[0].UsedBytes != 400 || report.Roots[0].FreeBytes != 600 {
		t.Fatalf("usage wrong: %+v", report.Roots[0])
	}
	if report.Roots[0].UsagePct != 0.4 {
		t.Fatalf("usage_pct=%v want 0.4", report.Roots[0].UsagePct)
	}
	// Downloads dir and /other are not real on this machine -> exists=false, zero usage.
	if report.Roots[1].Exists || report.Roots[2].Exists {
		t.Fatalf("non-existent roots should be marked !exists: %+v %+v", report.Roots[1], report.Roots[2])
	}
	if report.Roots[1].TotalBytes != 0 || report.Roots[2].TotalBytes != 0 {
		t.Fatalf("non-existent roots should have zero usage")
	}
	if report.MediaRoot != media || report.DownloadsDir != "/data/downloads" {
		t.Fatalf("config echo wrong: %+v", report)
	}
}

func TestBuildDiskEmptyPathsSkipped(t *testing.T) {
	report := BuildDisk("", "", nil, func(string) (int64, int64, int64, error) { return 1, 1, 1, nil })
	if len(report.Roots) != 0 {
		t.Fatalf("expected no roots for empty paths, got %+v", report.Roots)
	}
}

func TestBuildDiskStatErrorYieldsZeroUsage(t *testing.T) {
	// A path that exists but whose statfs fails -> exists=true, zero usage.
	existing := t.TempDir()
	report := BuildDisk(existing, "", nil, func(string) (int64, int64, int64, error) {
		return 0, 0, 0, errors.New("statfs failed")
	})
	if len(report.Roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(report.Roots))
	}
	if !report.Roots[0].Exists {
		t.Fatalf("root should exist")
	}
	if report.Roots[0].TotalBytes != 0 {
		t.Fatalf("usage should be zero on stat error, got %+v", report.Roots[0])
	}
	if report.Roots[0].UsagePct != 0 {
		t.Fatalf("usage_pct should be 0 on stat error")
	}
}

func TestBuildDiskDefaultStatIsSet(t *testing.T) {
	// With a nil Stat we use the real syscall implementation against a real dir.
	report := BuildDisk(t.TempDir(), "", nil, nil)
	if len(report.Roots) != 1 || !report.Roots[0].Exists {
		t.Fatalf("expected existing root with real stat, got %+v", report.Roots)
	}
	if report.Roots[0].TotalBytes <= 0 {
		t.Fatalf("real stat should report a positive total, got %+v", report.Roots[0])
	}
}
