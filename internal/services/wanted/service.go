// Package wanted is the service layer for the global Wanted view (P3):
// tab classification + server-side paging for the Missing / Cutoff Unmet
// tables, the Search All fan-out, batch unmonitoring, and the phase-1
// manual import. It programs the domain Repo; the cutoff default lives in
// the domain (P5 replaces it with full profile-driven cutoff config).
package wanted

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tvdom "github.com/brandenk514/mediarr/internal/domains/tv"
	dom "github.com/brandenk514/mediarr/internal/domains/wanted"
	"github.com/brandenk514/mediarr/internal/paging"
)

// Service programs the wanted repository.
type Service struct {
	repo dom.Repo
}

// New builds a wanted service over the given repository.
func New(repo dom.Repo) *Service { return &Service{repo: repo} }

// cutoffs resolves every series' effective cutoff item from its quality
// profile (unknown profiles resolve to nil = "not evaluable").
func (s *Service) cutoffs(ctx context.Context) (map[int64]*tvdom.ProfileItem, error) {
	profiles, err := s.repo.SeriesProfiles(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]*tvdom.ProfileItem, len(profiles))
	for id, name := range profiles {
		p, ok := tvdom.ProfileByName(name)
		if !ok {
			continue
		}
		out[id] = dom.CutoffItem(p)
	}
	return out, nil
}

// List returns one server-side page of the tab's wanted episodes.
func (s *Service) List(ctx context.Context, tab dom.Tab, q string, page, pageSize int) (paging.Page[dom.Item], error) {
	tab = tab.Normalize()
	if !tab.Valid() {
		return paging.Page[dom.Item]{Items: []dom.Item{}}, dom.ErrUnknownTab
	}
	rows, err := s.repo.EpisodeRows(ctx)
	if err != nil {
		return paging.Page[dom.Item]{}, err
	}
	cutoffs, err := s.cutoffs(ctx)
	if err != nil {
		return paging.Page[dom.Item]{}, err
	}
	items := dom.Filter{Tab: tab, Q: q}.Apply(rows, cutoffs)

	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = paging.DefaultPageSize
	}
	if pageSize > paging.MaxPageSize {
		pageSize = paging.MaxPageSize
	}
	total := len(items)
	start := (page - 1) * pageSize
	if start >= total {
		return paging.Page[dom.Item]{Items: []dom.Item{}, Total: total, Page: page, PageSize: pageSize}, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return paging.Page[dom.Item]{Items: items[start:end], Total: total, Page: page, PageSize: pageSize}, nil
}

// SearchAll fans out a search for the tab's wanted episodes: it ensures a
// pending wanted row for each episode (re-requesting the search for
// cutoff-unmet episodes) and records one search-requested event per
// affected series. It returns how many episodes are now wanted for the
// search.
func (s *Service) SearchAll(ctx context.Context, tab dom.Tab) (int, error) {
	tab = tab.Normalize()
	if !tab.Valid() {
		return 0, dom.ErrUnknownTab
	}
	rows, err := s.repo.EpisodeRows(ctx)
	if err != nil {
		return 0, err
	}
	cutoffs, err := s.cutoffs(ctx)
	if err != nil {
		return 0, err
	}
	items := dom.Filter{Tab: tab}.Apply(rows, cutoffs)

	count := 0
	perSeries := make(map[int64]int)
	for _, it := range items {
		if err := s.repo.EnsureWantedPending(ctx, it.SeriesID, it.Season, it.Episode); err != nil {
			return 0, err
		}
		count++
		perSeries[it.SeriesID]++
	}
	for id, n := range perSeries {
		detail := "Searched 1 wanted episode"
		if n > 1 {
			detail = fmt.Sprintf("Searched %d wanted episodes", n)
		}
		if err := s.repo.AddHistory(ctx, "tv", id, "search-requested", detail); err != nil {
			return 0, err
		}
	}
	return count, nil
}

// Unmonitor turns monitoring off for each ref, counting how many existed
// (unknown episodes are skipped).
func (s *Service) Unmonitor(ctx context.Context, refs []dom.EpisodeRef) (int, error) {
	count := 0
	for _, r := range refs {
		ok, err := s.repo.SetEpisodeMonitoredOff(ctx, r.SeriesID, r.Season, r.Episode)
		if err != nil {
			return 0, err
		}
		if ok {
			count++
		}
	}
	return count, nil
}

// ManualImport records a manual import of a wanted episode from a local
// file (phase 1, TV-only): the file must exist on the server, the series
// and episode must exist, and the episode's wanted row is satisfied (the
// quality is unknown for a manual import, so the episode drops out of the
// Cutoff Unmet tab). It records a manual-import history event.
func (s *Service) ManualImport(ctx context.Context, req dom.ManualImport) (dom.ManualImportReport, error) {
	empty := dom.ManualImportReport{}
	if !strings.EqualFold(strings.TrimSpace(req.Kind), "tv") {
		return empty, dom.ErrUnsupportedKind
	}
	if req.Season <= 0 || req.Episode <= 0 {
		return empty, dom.ErrInvalidEpisode
	}
	req.FilePath = strings.TrimSpace(req.FilePath)
	if req.FilePath == "" {
		return empty, dom.ErrEmptyPath
	}

	series, err := s.repo.GetSeries(ctx, req.SeriesID)
	if err != nil {
		return empty, err
	}
	if series == nil {
		return empty, dom.ErrSeriesNotFound
	}
	ep, err := s.repo.GetEpisode(ctx, req.SeriesID, req.Season, req.Episode)
	if err != nil {
		return empty, err
	}
	if ep == nil {
		return empty, dom.ErrEpisodeNotFound
	}
	if _, err := os.Stat(req.FilePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return empty, dom.ErrFileNotFound
		}
		return empty, err
	}

	release := strings.TrimSpace(req.ReleaseTitle)
	if release == "" {
		release = filepath.Base(req.FilePath)
	}
	if err := s.repo.SatisfyWanted(ctx, req.SeriesID, req.Season, req.Episode, release, "Manual Import"); err != nil {
		return empty, err
	}
	detail := "Imported " + req.FilePath
	if release != filepath.Base(req.FilePath) {
		detail += " (release: " + release + ")"
	}
	if err := s.repo.AddHistory(ctx, "tv", req.SeriesID, "manual-import", detail); err != nil {
		return empty, err
	}
	return dom.ManualImportReport{Imported: true, Message: detail}, nil
}
