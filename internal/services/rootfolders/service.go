// Package rootfolders is the service layer for the root-folder registry
// (PLAN P2): it validates and normalizes root-folder paths and enforces
// uniqueness, delegating persistence to the domain Repo.
package rootfolders

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"

	rfdom "github.com/brandenk514/mediarr/internal/domains/rootfolders"
)

// Service validates root-folder operations against the registry.
type Service struct {
	repo rfdom.Repo
}

// New builds a root-folder service over the given repository.
func New(repo rfdom.Repo) *Service {
	return &Service{repo: repo}
}

// List returns all registered root folders.
func (s *Service) List() ([]rfdom.RootFolder, error) {
	return s.repo.List()
}

// Get returns one root folder by id. sql.ErrNoRows maps to ErrNotFound.
func (s *Service) Get(id int64) (*rfdom.RootFolder, error) {
	rf, err := s.repo.Get(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, rfdom.ErrNotFound
		}
		return nil, err
	}
	return rf, nil
}

// Create registers a new root folder after path normalization and
// duplicate checking.
func (s *Service) Create(rf rfdom.RootFolder) (int64, error) {
	cleaned, err := normalize(rf.Path)
	if err != nil {
		return 0, err
	}
	if dup, err := s.findPath(cleaned); err != nil {
		return 0, err
	} else if dup != nil {
		return 0, rfdom.ErrDuplicate
	}
	rf.Path = cleaned
	return s.repo.Create(rf)
}

// Update changes a root folder's path/name. The new path is normalized and
// must not collide with another registered folder.
func (s *Service) Update(rf rfdom.RootFolder) (*rfdom.RootFolder, error) {
	existing, err := s.Get(rf.ID)
	if err != nil {
		return nil, err
	}
	if rf.Path != "" {
		cleaned, err := normalize(rf.Path)
		if err != nil {
			return nil, err
		}
		if dup, err := s.findPath(cleaned); err != nil {
			return nil, err
		} else if dup != nil && dup.ID != rf.ID {
			return nil, rfdom.ErrDuplicate
		}
		rf.Path = cleaned
	} else {
		rf.Path = existing.Path
	}
	if rf.Name == "" {
		rf.Name = existing.Name
	}
	// L3: omitted default profiles keep their stored values.
	if rf.DefaultQualityProfile == "" {
		rf.DefaultQualityProfile = existing.DefaultQualityProfile
	}
	if rf.DefaultMetadataProfile == "" {
		rf.DefaultMetadataProfile = existing.DefaultMetadataProfile
	}
	if err := s.repo.Update(rf); err != nil {
		return nil, err
	}
	updated, err := s.Get(rf.ID)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// Delete removes a root folder by id.
func (s *Service) Delete(id int64) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	return s.repo.Delete(id)
}

// normalize trims and cleans a root-folder path, requiring it to be
// non-empty.
func normalize(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", rfdom.ErrEmptyPath
	}
	return filepath.Clean(p), nil
}

// findPath returns the registered folder with the given clean path, or nil.
func (s *Service) findPath(clean string) (*rfdom.RootFolder, error) {
	all, err := s.repo.List()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Path == clean {
			return &all[i], nil
		}
	}
	return nil, nil
}
