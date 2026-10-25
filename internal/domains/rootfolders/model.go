// Package rootfolders is the root-folder registry domain: the on-disk
// locations where series/media live. The Add-New flow and Library Import
// consume the registry (PLAN P2). Like the other media domains it is a pure
// struct + repository interface: no DB, no HTTP, no I/O — the Postgres
// adapter implements Repo.
package rootfolders

import (
	"errors"
	"time"
)

// RootFolder is one registered media root: a path on disk that series (or
// other media) live under.
type RootFolder struct {
	ID        int64
	Path      string
	Name      string // optional label; defaults to ""
	CreatedAt time.Time
}

// Sentinel errors for the registry.
var (
	ErrNotFound  = errors.New("root folder not found")
	ErrDuplicate = errors.New("root folder path already exists")
	ErrEmptyPath = errors.New("root folder path is required")
)

// Repo is the persistence contract for the root-folder registry.
type Repo interface {
	// List returns all registered root folders, ordered by id.
	List() ([]RootFolder, error)
	// Get returns the root folder with the given id.
	Get(id int64) (*RootFolder, error)
	// Create stores a new root folder and returns its id.
	Create(rf RootFolder) (int64, error)
	// Update replaces the stored path/name for the given id.
	Update(rf RootFolder) error
	// Delete removes the root folder with the given id.
	Delete(id int64) error
}
