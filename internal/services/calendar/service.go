// Package calendar is the P4/R4 service layer. It owns the HTTP-facing
// behavior of the calendar: it pulls the episode or movie rows from the
// store and hands them to the pure domain (domains/calendar) for the
// window/filter + iCal rendering.
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

// Calendar returns the calendar entries for kind in [start, end]. kind=tv
// (the default; an empty kind falls back to it) is the Sonarr P4 episode
// calendar, unchanged. kind=movie (Radarr R4) is the movie release-date
// calendar. kind=music (Lidarr L4) is the music release-date calendar
// (upcoming releases of monitored albums).
func (s *Service) Calendar(ctx context.Context, kind dom.Kind, start, end time.Time) ([]dom.Entry, error) {
	now := s.now()
	if kind == dom.KindMovie {
		rows, err := s.repo.MovieRows(ctx)
		if err != nil {
			return nil, err
		}
		return dom.BuildMovies(rows, start, end, now), nil
	}
	if kind == dom.KindMusic {
		rows, err := s.repo.MusicRows(ctx)
		if err != nil {
			return nil, err
		}
		return dom.BuildMusic(rows, start, end, now), nil
	}
	rows, err := s.repo.Episodes(ctx)
	if err != nil {
		return nil, err
	}
	return dom.Build(rows, start, end, now), nil
}

// ICal renders the calendar entries for kind in [start, end] as an iCal
// document (VCALENDAR + a VEVENT per entry).
func (s *Service) ICal(ctx context.Context, kind dom.Kind, start, end time.Time) (string, error) {
	now := s.now()
	if kind == dom.KindMovie {
		rows, err := s.repo.MovieRows(ctx)
		if err != nil {
			return "", err
		}
		return dom.RenderICal(dom.BuildMovies(rows, start, end, now), now), nil
	}
	if kind == dom.KindMusic {
		rows, err := s.repo.MusicRows(ctx)
		if err != nil {
			return "", err
		}
		return dom.RenderICal(dom.BuildMusic(rows, start, end, now), now), nil
	}
	rows, err := s.repo.Episodes(ctx)
	if err != nil {
		return "", err
	}
	return dom.RenderICal(dom.Build(rows, start, end, now), now), nil
}
