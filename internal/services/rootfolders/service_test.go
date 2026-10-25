package rootfolders

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	rfdom "github.com/brandenk514/mediarr/internal/domains/rootfolders"
)

// fakeRepo is an in-memory rootfolders.Repo for service unit tests.
type fakeRepo struct {
	folders map[int64]rfdom.RootFolder
	nextID  int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{folders: map[int64]rfdom.RootFolder{}, nextID: 1}
}

func (f *fakeRepo) List() ([]rfdom.RootFolder, error) {
	out := make([]rfdom.RootFolder, 0, len(f.folders))
	for _, rf := range f.folders {
		out = append(out, rf)
	}
	return out, nil
}

func (f *fakeRepo) Get(id int64) (*rfdom.RootFolder, error) {
	rf, ok := f.folders[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return &rf, nil
}

func (f *fakeRepo) Create(rf rfdom.RootFolder) (int64, error) {
	for _, existing := range f.folders {
		if existing.Path == rf.Path {
			return 0, errors.New("duplicate key value violates unique constraint")
		}
	}
	id := f.nextID
	f.nextID++
	rf.ID = id
	if rf.CreatedAt.IsZero() {
		rf.CreatedAt = time.Now().UTC()
	}
	f.folders[id] = rf
	return id, nil
}

func (f *fakeRepo) Update(rf rfdom.RootFolder) error {
	if _, ok := f.folders[rf.ID]; !ok {
		return errors.New("no row")
	}
	f.folders[rf.ID] = rf
	return nil
}

func (f *fakeRepo) Delete(id int64) error {
	if _, ok := f.folders[id]; !ok {
		return errors.New("no row")
	}
	delete(f.folders, id)
	return nil
}

func TestCreateNormalizeAndDuplicate(t *testing.T) {
	svc := New(newFakeRepo())

	id, err := svc.Create(rfdom.RootFolder{Path: "/mnt/media/"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rf, err := svc.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rf.Path != "/mnt/media" {
		t.Errorf("path = %q, want cleaned /mnt/media", rf.Path)
	}

	if _, err := svc.Create(rfdom.RootFolder{Path: "/mnt/media"}); !errors.Is(err, rfdom.ErrDuplicate) {
		t.Errorf("duplicate create err = %v, want ErrDuplicate", err)
	}
}

func TestCreateEmptyPath(t *testing.T) {
	svc := New(newFakeRepo())
	if _, err := svc.Create(rfdom.RootFolder{Path: "   "}); !errors.Is(err, rfdom.ErrEmptyPath) {
		t.Errorf("empty path err = %v, want ErrEmptyPath", err)
	}
}

func TestUpdatePathAndName(t *testing.T) {
	svc := New(newFakeRepo())
	id, _ := svc.Create(rfdom.RootFolder{Path: "/a", Name: "Old"})

	updated, err := svc.Update(rfdom.RootFolder{ID: id, Path: "/b/nested/"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Path != "/b/nested" || updated.Name != "Old" {
		t.Errorf("updated = %+v, want path /b/nested + preserved name", *updated)
	}

	// Renaming to another registered folder's path is a duplicate.
	id2, _ := svc.Create(rfdom.RootFolder{Path: "/c"})
	if _, err := svc.Update(rfdom.RootFolder{ID: id, Path: "/c"}); !errors.Is(err, rfdom.ErrDuplicate) {
		t.Errorf("collision update err = %v, want ErrDuplicate", err)
	}

	// Unknown id → ErrNotFound.
	if _, err := svc.Update(rfdom.RootFolder{ID: 99}); !errors.Is(err, rfdom.ErrNotFound) {
		t.Errorf("update missing err = %v, want ErrNotFound", err)
	}
	_ = id2
}

func TestDelete(t *testing.T) {
	svc := New(newFakeRepo())
	id, _ := svc.Create(rfdom.RootFolder{Path: "/x"})
	if err := svc.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Get(id); !errors.Is(err, rfdom.ErrNotFound) {
		t.Errorf("get after delete err = %v, want ErrNotFound", err)
	}
	if err := svc.Delete(99); !errors.Is(err, rfdom.ErrNotFound) {
		t.Errorf("delete missing err = %v, want ErrNotFound", err)
	}
}

func TestList(t *testing.T) {
	svc := New(newFakeRepo())
	_, _ = svc.Create(rfdom.RootFolder{Path: "/m1"})
	_, _ = svc.Create(rfdom.RootFolder{Path: "/m2"})
	all, err := svc.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("list len = %d, want 2", len(all))
	}
}
