// Package blocklist is the service layer for the blocklist (P3 Activity >
// Blocklist): it validates entries and delegates persistence to the domain
// Repo, applying the in-memory filter so a fake or partial repository
// still lists correctly.
package blocklist

import (
	"context"
	"strings"

	dom "github.com/brandenk514/mediarr/internal/domains/blocklist"
)

// Service validates blocklist operations and programs the Repo.
type Service struct {
	repo dom.Repo
}

// New builds a blocklist service over the given repository.
func New(repo dom.Repo) *Service { return &Service{repo: repo} }

// List returns the blocked releases matching f, newest first.
func (s *Service) List(ctx context.Context, f dom.Filter) ([]dom.Entry, error) {
	items, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, err
	}
	return f.Apply(items), nil
}

// Add records a blocked release after validating the kind and title.
func (s *Service) Add(ctx context.Context, e dom.Entry) (int64, error) {
	if !e.Kind.Valid() {
		return 0, dom.ErrInvalidKind
	}
	if strings.TrimSpace(e.ReleaseTitle) == "" {
		return 0, dom.ErrEmptyTitle
	}
	e.ReleaseTitle = strings.TrimSpace(e.ReleaseTitle)
	e.Indexer = strings.TrimSpace(e.Indexer)
	e.Reason = strings.TrimSpace(e.Reason)
	return s.repo.Add(ctx, e)
}

// Unblock removes a blocked release by id, reporting whether a row was
// deleted (false = unknown id, surfaced as a 404 by the API layer).
func (s *Service) Unblock(ctx context.Context, id int64) (bool, error) {
	if id <= 0 {
		return false, dom.ErrInvalidID
	}
	return s.repo.Unblock(ctx, id)
}
