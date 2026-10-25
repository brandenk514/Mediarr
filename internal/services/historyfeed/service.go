// Package historyfeed is the service layer for the global Activity >
// History feed (P3): it normalizes the feed filter and programs the domain
// Repo, which applies the filters in SQL.
package historyfeed

import (
	"context"

	dom "github.com/brandenk514/mediarr/internal/domains/historyfeed"
)

// Service programs the feed repository.
type Service struct {
	repo dom.Repo
}

// New builds a history-feed service over the given repository.
func New(repo dom.Repo) *Service { return &Service{repo: repo} }

// List returns the feed events matching f (since inclusive, event
// case-insensitive, limit clamped to [1, MaxLimit]), newest first.
func (s *Service) List(ctx context.Context, f dom.Filter) ([]dom.Item, error) {
	f.Normalize()
	return s.repo.List(ctx, f)
}
