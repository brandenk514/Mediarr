package postgres_test

import (
	"testing"

	rfdom "github.com/brandenk514/mediarr/internal/domains/rootfolders"
	"github.com/brandenk514/mediarr/internal/settings"
)

// TestRootFoldersRepo_Calibre (Readarr B5): the per-root Calibre block
// round-trips through the JSONB column — a configured root stores the block
// (password included; the API masks it on read), and a root without one
// decodes to nil.
func TestRootFoldersRepo_Calibre(t *testing.T) {
	rf := startRFPG(t)

	c := &settings.CalibreSettings{
		Enabled: true, Host: "calibre.example", Port: 8080,
		Username: "admin", Password: "the-secret", PasswordSet: true,
		Library: "Library", OutputFormat: "EPUB", Monitor: true,
		DefaultTags: []int{1, 2},
	}
	id, err := rf.Create(rfdom.RootFolder{Path: "/data/books", Name: "Books", Calibre: c})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := rf.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Calibre == nil {
		t.Fatal("calibre after create = nil, want the stored block")
	}
	if got.Calibre.Host != "calibre.example" || got.Calibre.Password != "the-secret" ||
		got.Calibre.OutputFormat != "EPUB" || !got.Calibre.Monitor ||
		len(got.Calibre.DefaultTags) != 2 || !got.Calibre.HardlinksDisabled() {
		t.Fatalf("calibre after create = %+v, want the stored block", got.Calibre)
	}

	// Update replaces the block.
	updated := &settings.CalibreSettings{
		Enabled: true, Host: "calibre.example", Port: 8081,
		Username: "admin", Password: "the-secret", Library: "Library",
	}
	if err := rf.Update(rfdom.RootFolder{
		ID: id, Path: "/data/books", Name: "Books", Calibre: updated,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = rf.Get(id)
	if got.Calibre == nil || got.Calibre.Port != 8081 || got.Calibre.OutputFormat != "" {
		t.Fatalf("calibre after update = %+v, want the replaced block", got.Calibre)
	}

	// A root without a Calibre block decodes to nil (the "{}" default).
	plainID, err := rf.Create(rfdom.RootFolder{Path: "/data/plain"})
	if err != nil {
		t.Fatalf("create plain: %v", err)
	}
	plain, err := rf.Get(plainID)
	if err != nil {
		t.Fatalf("get plain: %v", err)
	}
	if plain.Calibre != nil {
		t.Fatalf("plain root calibre = %+v, want nil", plain.Calibre)
	}
	if plain.HardlinksDisabled() {
		t.Fatal("nil calibre must not disable hard links")
	}
}
