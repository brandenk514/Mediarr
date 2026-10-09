package books

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlanImport_Layout pins the per-author import layout
// (root/<Author>/<Title>/<Title>.<ext>) and the path-safety / collision
// behaviour of PlanImport.
func TestPlanImport_Layout(t *testing.T) {
	tests := []struct {
		name      string
		root      string
		author    string
		title     string
		source    string
		wantRel   string // expected path relative to root
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "epub",
			root:    "media",
			author:  "Ursula K. Le Guin",
			title:   "The Dispossessed",
			source:  "/downloads/The Dispossessed.epub",
			wantRel: "Ursula K. Le Guin/The Dispossessed/The Dispossessed.epub",
		},
		{
			name:    "mobi",
			root:    "media",
			author:  "Robert Jordan",
			title:   "The Eye of the World",
			source:  "/downloads/The Eye of the World.mobi",
			wantRel: "Robert Jordan/The Eye of the World/The Eye of the World.mobi",
		},
		{
			name:    "azw3",
			root:    "media",
			author:  "Brandon Sanderson",
			title:   "Mistborn",
			source:  "/downloads/Mistborn.azw3",
			wantRel: "Brandon Sanderson/Mistborn/Mistborn.azw3",
		},
		{
			name:    "empty root rejected",
			root:    "",
			author:  "A",
			title:   "T",
			source:  "/downloads/t.epub",
			wantErr: true,
		},
		{
			// Path separators in a title are neutralised by cleanPathSegment,
			// so a "../../"-style name cannot escape the media root.
			name:    "embedded slashes neutralised",
			root:    "media",
			author:  "A",
			title:   "../../etc/passwd",
			source:  "/downloads/t.epub",
			wantRel: "A/....etcpasswd/....etcpasswd.epub",
		},
		{
			name:    "empty title falls back to 'title'",
			root:    "media",
			author:  "A",
			title:   "",
			source:  "/downloads/t.epub",
			wantRel: "A/title/title.epub",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PlanImport(tt.root, tt.author, tt.title, tt.source)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("PlanImport() error = nil, want error")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("PlanImport() error = %q, want substring %q", err, tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("PlanImport() error = %v", err)
			}
			want := filepath.Join(tt.root, filepath.FromSlash(tt.wantRel))
			if got.DestPath != want {
				t.Fatalf("DestPath = %q, want %q", got.DestPath, want)
			}
			if got.SourcePath != tt.source {
				t.Fatalf("SourcePath = %q, want %q", got.SourcePath, tt.source)
			}
		})
	}
}

// TestPlanImport_Collision verifies that importing over an existing file is a
// collision (never an overwrite) and that the path-only companion agrees with
// the full plan.
func TestPlanImport_Collision(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "Book.epub")
	if err := os.WriteFile(source, []byte("x"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	plan, err := PlanImport(root, "Author", "Book", source)
	if err != nil {
		t.Fatalf("PlanImport first: %v", err)
	}
	// Simulate an existing destination.
	if err := os.MkdirAll(filepath.Dir(plan.DestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan.DestPath, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = PlanImport(root, "Author", "Book", source)
	if !errors.Is(err, ErrCollision) {
		t.Fatalf("PlanImport over existing = %v, want ErrCollision", err)
	}

	// The path-only companion must compute the same destination.
	want, err := DestPath(root, "Author", "Book", source)
	if err != nil {
		t.Fatalf("DestPath: %v", err)
	}
	if want != plan.DestPath {
		t.Fatalf("DestPath = %q, want %q (match plan)", want, plan.DestPath)
	}
}

func TestCleanPathSegment(t *testing.T) {
	tests := []struct{ in, want string }{
		{"The Great Gatsby", "The Great Gatsby"},
		{"a/b\\c", "abc"},
		{"  lots   of   spaces  ", "lots of spaces"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := cleanPathSegment(tt.in); got != tt.want {
			t.Errorf("cleanPathSegment(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
