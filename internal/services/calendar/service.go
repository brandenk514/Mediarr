// Package calendar is the P4 service layer. It owns the HTTP-facing behavior
// of the calendar: it pulls the episode rows from the store and hands them to
// the pure domain (domains/calendar) for the window/filter + iCal rendering.
//
// The service has no I/O of its own beyond the injected Repo; `now` is a
// function (default time.Now) so the future cutoff is deterministic in tests.
package calendar

import (
	"context"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/calendar"
)

// Service answers the calendar page and feed.
type Service struct {
	repo dom.Repo
	now  func() time.Time
}

// New builds a calendar service over the given repository.
func New(repo dom.Repo) *Service {
	return &Service{repo: repo, now: time.Now}
}

// Calendar returns the upcoming monitored air dates in [start, end].
func (s *Service) Calendar(ctx context.Context, start, end time.Time) ([]dom.Entry, error) {
	rows, err := s.repo.Episodes(ctx)
	if err != nil {
		return nil, err
	}
	return dom.Build(rows, start, end, s.now()), nil
}

// ICal renders the upcoming monitored air dates in [start, end] as an iCal
// document (VCALENDAR + a VEVENT per entry).
func (s *Service) ICal(ctx context.Context, start, end time.Time) (string, error) {
	rows, err := s.repo.Episodes(ctx)
	if err != nil {
		return "", err
	}
	now := s.now()
	return dom.RenderICal(dom.Build(rows, start, end, now), now), nil
}
