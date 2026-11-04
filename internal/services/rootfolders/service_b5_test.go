package rootfolders

import (
	"testing"

	rfdom "github.com/brandenk514/mediarr/internal/domains/rootfolders"
	"github.com/brandenk514/mediarr/internal/settings"
)

// B5: the per-root Calibre merge rules on Update — the omitted block keeps
// the stored one, a blank password keeps the stored key (the masked
// round-trip), and a new password replaces it.
func TestUpdateCalibreMerge(t *testing.T) {
	svc := New(newFakeRepo())
	stored := &settings.CalibreSettings{
		Enabled: true, Host: "calibre.example", Port: 8080,
		Username: "u", Password: "stored-key", PasswordSet: true,
	}
	id, err := svc.Create(rfdom.RootFolder{Path: "/books", Calibre: stored})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// 1. An omitted block keeps the stored one.
	updated, err := svc.Update(rfdom.RootFolder{ID: id, Path: "/books", Name: "Books"})
	if err != nil {
		t.Fatalf("update (omitted calibre): %v", err)
	}
	if updated.Calibre == nil || updated.Calibre.Host != "calibre.example" || updated.Calibre.Password != "stored-key" {
		t.Fatalf("omitted calibre = %+v, want the stored block", updated.Calibre)
	}

	// 2. A block with a blank password keeps the stored key (masked
	//    round-trip) while the other fields are replaced.
	replaced := &settings.CalibreSettings{
		Enabled: true, Host: "calibre.example", Port: 8081,
		Username: "u", OutputFormat: "EPUB",
	}
	updated, err = svc.Update(rfdom.RootFolder{ID: id, Calibre: replaced})
	if err != nil {
		t.Fatalf("update (blank password): %v", err)
	}
	if updated.Calibre.Password != "stored-key" || !updated.Calibre.PasswordSet {
		t.Fatalf("blank password = %+v, want the stored key kept + set", updated.Calibre)
	}
	if updated.Calibre.Port != 8081 || updated.Calibre.OutputFormat != "EPUB" {
		t.Fatalf("replacement fields = %+v, want port 8081 + EPUB", updated.Calibre)
	}

	// 3. A new password replaces the stored one.
	withNew := *replaced
	withNew.Password = "new-key"
	updated, err = svc.Update(rfdom.RootFolder{ID: id, Calibre: &withNew})
	if err != nil {
		t.Fatalf("update (new password): %v", err)
	}
	if updated.Calibre.Password != "new-key" {
		t.Fatalf("new password = %q, want new-key", updated.Calibre.Password)
	}

	// 4. The hard-links rule: enabled Calibre disables hard links; an
	//    explicit disabled block does not (and keeps the stored key).
	if !updated.HardlinksDisabled() {
		t.Fatal("enabled calibre must disable hard links")
	}
	disabled := &settings.CalibreSettings{Enabled: false, Host: "calibre.example", Port: 8081, Username: "u"}
	updated, err = svc.Update(rfdom.RootFolder{ID: id, Calibre: disabled})
	if err != nil {
		t.Fatalf("update (disabled block): %v", err)
	}
	if updated.HardlinksDisabled() {
		t.Fatal("disabled calibre must not disable hard links")
	}
	if updated.Calibre.Password != "new-key" {
		t.Fatalf("disabled block = %+v, want the stored key kept", updated.Calibre)
	}
}

// B5: Create passes the Calibre block through untouched (a fresh root has
// nothing to merge against).
func TestCreateCalibrePassthrough(t *testing.T) {
	svc := New(newFakeRepo())
	c := &settings.CalibreSettings{Enabled: true, Host: "h", Port: 1, Username: "u", Password: "p"}
	id, err := svc.Create(rfdom.RootFolder{Path: "/b", Calibre: c})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rf, err := svc.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rf.Calibre == nil || rf.Calibre.Password != "p" || !rf.HardlinksDisabled() {
		t.Fatalf("created calibre = %+v, want the stored block + hard links disabled", rf.Calibre)
	}

	// A root without a Calibre block stays nil (not a zero value).
	id, err = svc.Create(rfdom.RootFolder{Path: "/no"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rf, err = svc.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rf.Calibre != nil {
		t.Fatalf("no-calibre root = %+v, want nil", rf.Calibre)
	}
	if rf.HardlinksDisabled() {
		t.Fatal("nil calibre must not disable hard links")
	}
}
