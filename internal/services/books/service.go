// Package books (services) is the use-case layer for the books domain. It
// orchestrates the full download pipeline — add author → add title/edition →
// want (at title or edition granularity) → search → e-book format match → pick
// best → download → import → satisfy — against the pure domain (domains/books)
// and the swappable adapter interfaces (indexers, downloads).
//
// It depends on interfaces, never on Postgres/Redis concretely, so the pipeline
// is testable end-to-end with fakes and a real Postgres repository alike (the
// same pattern as services/music, services/tv, and services/movies).
package books

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/books"
	"github.com/brandenk514/mediarr/internal/downloads"
	"github.com/brandenk514/mediarr/internal/indexers"
)

// ErrNoMatch is returned when no indexer release satisfies a wanted title's
// format profile.
var ErrNoMatch = fmt.Errorf("books: no release matched the format profile")

// ErrNoWanted is returned when an author has no pending wanted titles/editions
// to service (nothing to do).
var ErrNoWanted = fmt.Errorf("books: no pending wanted titles or editions")

// ErrAuthorNotFound, ErrTitleNotFound, and ErrEditionNotFound are re-exported
// from the pure domain so API/transport layers can map them to 404 without
// importing the domain directly.
var (
	ErrAuthorNotFound  = dom.ErrAuthorNotFound
	ErrTitleNotFound   = dom.ErrTitleNotFound
	ErrEditionNotFound = dom.ErrEditionNotFound
)

// Deps bundles the Service's collaborators. Everything is an interface (or the
// pure domain), so the service is testable with a fake indexer, a mock download
// client, and either a real Postgres repo or an in-memory fake.
type Deps struct {
	// Repo persists authors, titles, editions, wanted, queue, and history.
	Repo dom.Repo
	// Indexers are searched (fan-out) for candidate releases.
	Indexers []indexers.Searcher
	// Client is the download client that fetches the chosen release.
	Client downloads.Client
	// MediaRoot is where imported files are placed (e.g. /media/books).
	MediaRoot string
	// DefaultProfile is used when a title has no profile set.
	DefaultProfile string
}

// Service implements the books use-cases.
type Service struct {
	deps Deps
}

// New builds a Service.
func New(deps Deps) *Service {
	if deps.DefaultProfile == "" {
		deps.DefaultProfile = "Best"
	}
	return &Service{deps: deps}
}

// AddAuthor creates an author (if new) and records the add in history. The
// monitored flag sets the author-level monitor toggle (PLAN §4): a monitored
// author has its titles wanted by default; an unmonitored author is added
// quietly. Returns the author id.
func (s *Service) AddAuthor(ctx context.Context, name string, monitored bool) (int64, error) {
	if strings.TrimSpace(name) == "" {
		return 0, fmt.Errorf("books: author name is required")
	}
	a := dom.Author{
		Name:      strings.TrimSpace(name),
		Monitored: monitored,
	}
	id, err := s.deps.Repo.CreateAuthor(ctx, a)
	if err != nil {
		return 0, err
	}
	s.record(ctx, id, "added", "author added to library")
	return id, nil
}

// AddTitle creates a known title within an author (if new) and, when the title
// is monitored, ensures a pending whole-title wanted row for it. Returns the
// title id.
func (s *Service) AddTitle(ctx context.Context, authorID int64, name string, monitored bool) (int64, error) {
	if strings.TrimSpace(name) == "" {
		return 0, fmt.Errorf("books: title name is required")
	}
	title := dom.Title{
		AuthorID:  authorID,
		Name:      strings.TrimSpace(name),
		Monitored: monitored,
	}
	id, err := s.deps.Repo.CreateTitle(ctx, title)
	if err != nil {
		return 0, err
	}
	if monitored {
		if err := s.deps.Repo.EnsureWantedTitle(ctx, id); err != nil {
			return 0, err
		}
	}
	s.record(ctx, authorID, "title-added", name)
	return id, nil
}

// AddEdition creates a known edition within a title (if new) and, when the
// edition is monitored, ensures a pending edition-level wanted row for it.
// Returns the edition id.
func (s *Service) AddEdition(ctx context.Context, titleID int64, format, isbn, publisher string, year, pages int, monitored bool) (int64, error) {
	if err := dom.ValidateFormat(format); err != nil {
		return 0, err
	}
	e := dom.Edition{
		TitleID:   titleID,
		Format:    strings.ToLower(strings.TrimSpace(format)),
		ISBN:      isbn,
		Publisher: publisher,
		Year:      year,
		Pages:     pages,
		Monitored: monitored,
	}
	id, err := s.deps.Repo.CreateEdition(ctx, e)
	if err != nil {
		return 0, err
	}
	if monitored {
		if err := s.deps.Repo.EnsureWantedEdition(ctx, titleID, id); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// ListAuthors returns all authors (used by GET /api/v1/books).
func (s *Service) ListAuthors(ctx context.Context) ([]dom.Author, error) {
	return s.deps.Repo.ListAuthors(ctx)
}

// GetAuthor loads an author (used by GET /api/v1/books/{id}).
func (s *Service) GetAuthor(ctx context.Context, id int64) (*dom.Author, error) {
	return s.deps.Repo.GetAuthor(ctx, id)
}

// ListTitles returns an author's known titles (used by
// GET /api/v1/books/{id}/titles).
func (s *Service) ListTitles(ctx context.Context, authorID int64) ([]dom.Title, error) {
	return s.deps.Repo.ListTitles(ctx, authorID)
}

// GetTitle loads a title (used by GET /api/v1/books/titles/{id}).
func (s *Service) GetTitle(ctx context.Context, id int64) (*dom.Title, error) {
	return s.deps.Repo.GetTitle(ctx, id)
}

// ListEditions returns a title's known editions (used by
// GET /api/v1/books/titles/{id}/editions).
func (s *Service) ListEditions(ctx context.Context, titleID int64) ([]dom.Edition, error) {
	return s.deps.Repo.ListEditions(ctx, titleID)
}

// GetEdition loads an edition (used by GET /api/v1/books/editions/{id}).
func (s *Service) GetEdition(ctx context.Context, id int64) (*dom.Edition, error) {
	return s.deps.Repo.GetEdition(ctx, id)
}

// ListWanted returns a title's wanted entries (title-level and edition-level)
// with their status (used by GET /api/v1/books/titles/{id}/wanted).
func (s *Service) ListWanted(ctx context.Context, titleID int64) ([]dom.Wanted, error) {
	return s.deps.Repo.ListWanted(ctx, titleID)
}

// SetAuthorMonitored toggles the author-level monitor (PLAN §4) and propagates
// it to every known title of the author, ensuring a pending whole-title wanted
// row for each on toggle-on. The wanted rows are the pipeline's work queue.
func (s *Service) SetAuthorMonitored(ctx context.Context, authorID int64, monitored bool) error {
	if err := s.deps.Repo.SetAuthorMonitored(ctx, authorID, monitored); err != nil {
		return err
	}
	titles, err := s.deps.Repo.ListTitles(ctx, authorID)
	if err != nil {
		return err
	}
	for _, t := range titles {
		if err := s.SetTitleMonitored(ctx, t.ID, monitored); err != nil {
			return err
		}
	}
	event := "monitored"
	if !monitored {
		event = "unmonitored"
	}
	s.record(ctx, authorID, event, "author-level toggle")
	return nil
}

// SetTitleMonitored toggles a single title's monitoring; when turning on, it
// ensures a pending whole-title wanted row (so a re-monitored title is wanted
// again).
func (s *Service) SetTitleMonitored(ctx context.Context, titleID int64, monitored bool) error {
	if err := s.deps.Repo.SetTitleMonitored(ctx, titleID, monitored); err != nil {
		return err
	}
	if monitored {
		return s.deps.Repo.EnsureWantedTitle(ctx, titleID)
	}
	return nil
}

// SetEditionMonitored toggles a single edition's monitoring; when turning on,
// it ensures a pending edition-level wanted row.
func (s *Service) SetEditionMonitored(ctx context.Context, titleID, editionID int64, monitored bool) error {
	if err := s.deps.Repo.SetEditionMonitored(ctx, editionID, monitored); err != nil {
		return err
	}
	if !monitored {
		return nil
	}
	return s.deps.Repo.EnsureWantedEdition(ctx, titleID, editionID)
}

// RunPipeline drives the full pipeline for one author: it services every
// pending wanted title/edition by searching indexers, matching the best e-book
// release for the title's format profile, downloading it, importing it under
// the media root, and marking the wanted entries satisfied.
//
// A whole-title release satisfies the title-level wanted and every edition-
// level wanted within that title (the file delivers the title in the requested
// format). The pipeline is idempotent: wanted entries already satisfied are
// skipped, and an author with no pending wanted entries returns ErrNoWanted
// without searching.
func (s *Service) RunPipeline(ctx context.Context, authorID int64) (int, error) {
	author, err := s.deps.Repo.GetAuthor(ctx, authorID)
	if err != nil {
		return 0, err
	}

	// Collect the pending wanted per title.
	titles, err := s.deps.Repo.ListTitles(ctx, authorID)
	if err != nil {
		return 0, err
	}
	type titleWork struct {
		title   dom.Title
		pending []dom.Wanted
	}
	var work []titleWork
	for _, t := range titles {
		wants, err := s.deps.Repo.ListWanted(ctx, t.ID)
		if err != nil {
			return 0, err
		}
		var pending []dom.Wanted
		for _, w := range wants {
			if w.Status == dom.WantedPending {
				pending = append(pending, w)
			}
		}
		if len(pending) > 0 {
			work = append(work, titleWork{title: t, pending: pending})
		}
	}
	if len(work) == 0 {
		return 0, ErrNoWanted
	}

	if s.deps.Client == nil {
		s.record(ctx, authorID, "no-client", "no download client configured")
		return 0, fmt.Errorf("books: no download client configured")
	}

	// 1. Search (once for the author).
	results, err := s.searchAll(ctx, author.Name)
	if err != nil {
		s.record(ctx, authorID, "search", "search failed: "+err.Error())
		return 0, err
	}
	s.record(ctx, authorID, "searched", fmt.Sprintf("%d candidate release(s)", len(results)))

	// 2. Service each title that has pending wanted entries.
	imported := 0
	for _, w := range work {
		profile := s.deps.DefaultProfile
		cands := s.buildCandidates(author, w.title, profile, results)
		best, ok := dom.BestForTitle(cands, author.Name, w.title.Name)
		if !ok {
			s.record(ctx, authorID, "no-match",
				fmt.Sprintf("no release satisfied %q (%s)", w.title.Name, profile))
			continue
		}
		n, err := s.serviceTitle(ctx, author, w.title, best, w.pending)
		if err != nil {
			s.record(ctx, authorID, "import-failed", err.Error())
			continue
		}
		imported += n
	}
	if imported == 0 {
		return 0, ErrNoMatch
	}
	return imported, nil
}

// searchAll fans out to every indexer concurrently (via the shared M5 Fanout),
// merges the results, and dedupes by release title (first indexer wins). The
// domain pickers re-score and re-order the candidates, so the merged order
// does not affect which release is chosen; the Fanout adds concurrency, a
// per-indexer timeout, and (when the indexers are wrapped in a CacheSearcher)
// a Redis result cache — see PLAN §5 and #30.
func (s *Service) searchAll(ctx context.Context, term string) ([]indexers.SearchResult, error) {
	q := indexers.SearchQuery{Term: term}
	f := indexers.NewFanout(s.deps.Indexers...)
	return f.Search(ctx, q)
}

// buildCandidates parses and format-matches every indexer result against the
// title's profile, dropping releases that do not parse or satisfy it.
func (s *Service) buildCandidates(author *dom.Author, title dom.Title, profile string, results []indexers.SearchResult) []dom.CandidateRelease {
	var cands []dom.CandidateRelease
	for _, r := range results {
		c, ok := dom.BuildCandidate(r.Title, r.Indexer, profile)
		if !ok {
			continue
		}
		cands = append(cands, c)
	}
	return cands
}

// serviceTitle downloads the best release once, imports it under the media
// root, and satisfies every pending wanted entry the release delivers (the
// title level plus any edition-level wanted within the title). Returns the
// number of wanted entries satisfied.
func (s *Service) serviceTitle(ctx context.Context, author *dom.Author, title dom.Title, rel dom.CandidateRelease, pending []dom.Wanted) (int, error) {
	// Download once.
	queueID, err := s.sendToClient(ctx, author, title, rel)
	if err != nil {
		return 0, err
	}

	// Import the e-book file.
	if err := s.importTitle(ctx, author, title, rel, queueID); err != nil {
		return 0, err
	}

	// Satisfy every pending wanted the release delivers.
	satisfied := 0
	for _, w := range pending {
		if err := s.deps.Repo.MarkWantedSatisfied(ctx, w.TitleID, w.EditionID, rel.Title); err != nil {
			s.record(ctx, author.ID, "satisfy-failed", err.Error())
			continue
		}
		satisfied++
	}
	s.record(ctx, author.ID, "imported",
		fmt.Sprintf("imported %q satisfying %d wanted", rel.Title, satisfied))
	return satisfied, nil
}

// sendToClient queues the release, sends it to the download client, and records
// the queue entry. Returns the queue entry id.
func (s *Service) sendToClient(ctx context.Context, author *dom.Author, title dom.Title, rel dom.CandidateRelease) (int64, error) {
	qe := dom.QueueEntry{
		AuthorID:       author.ID,
		TitleID:        title.ID,
		ReleaseTitle:   rel.Title,
		Indexer:        rel.Indexer,
		DownloadClient: s.deps.Client.Name(),
		State:          dom.QueueQueued,
		Progress:       0,
	}
	id, err := s.deps.Repo.CreateQueue(ctx, qe)
	if err != nil {
		return 0, err
	}
	qe.ID = id
	qe.State = dom.QueueDownloading
	_ = s.deps.Repo.UpdateQueue(ctx, qe)

	if err := s.deps.Client.Add(ctx, downloads.Release{
		Title:    rel.Title,
		FileName: fileNameFor(title, rel),
	}); err != nil {
		qe.State = dom.QueueFailed
		_ = s.deps.Repo.UpdateQueue(ctx, qe)
		return 0, err
	}
	return id, nil
}

// importTitle polls the client until the release is complete and imports the
// e-book file under the media root, marking the queue complete.
func (s *Service) importTitle(ctx context.Context, author *dom.Author, title dom.Title, rel dom.CandidateRelease, queueID int64) error {
	file, err := s.waitComplete(ctx, rel.Title)
	if err != nil {
		return err
	}
	if file == "" {
		return fmt.Errorf("books: download did not produce a file for %q", rel.Title)
	}

	plan, err := dom.PlanImport(s.deps.MediaRoot, author.Name, title.Name, file)
	if err != nil {
		if !errors.Is(err, dom.ErrCollision) {
			return err
		}
		// The file already exists; reuse its destination in place.
		dest, derr := dom.DestPath(s.deps.MediaRoot, author.Name, title.Name, file)
		if derr != nil {
			return derr
		}
		plan = &dom.Import{DestPath: dest}
	}

	// Copy the file into place unless it is already there.
	if _, statErr := os.Stat(plan.DestPath); os.IsNotExist(statErr) {
		if err := copyFile(plan.SourcePath, plan.DestPath); err != nil {
			return err
		}
	}

	// Remove the source file now that it has been imported.
	_ = os.Remove(file)

	qe := dom.QueueEntry{ID: queueID, State: dom.QueueComplete, Progress: 100}
	if err := s.deps.Repo.UpdateQueue(ctx, qe); err != nil {
		return err
	}
	return nil
}

// waitComplete polls the download client until the release reports complete
// (bounded by a deadline) and returns the file path.
func (s *Service) waitComplete(ctx context.Context, title string) (string, error) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		st, err := s.deps.Client.Status(ctx, title)
		if err != nil {
			return "", err
		}
		if st.Complete && st.File != "" {
			return st.File, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("books: timed out waiting for download of %q", title)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// record appends a history entry, ignoring errors (history is best-effort).
func (s *Service) record(ctx context.Context, authorID int64, event, detail string) {
	_ = s.deps.Repo.AddHistory(ctx, dom.HistoryEntry{
		AuthorID: authorID, Event: event, Detail: detail,
	})
}

// fileNameFor derives a safe download-client file name from a title and a
// parsed release: a cleaned title name plus the e-book extension inferred from
// the release's format.
func fileNameFor(title dom.Title, rel dom.CandidateRelease) string {
	name := title.Name
	if name == "" {
		name = rel.Title
	}
	if name == "" {
		name = "book"
	}
	name = strings.ReplaceAll(name, ".", " ")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.Join(strings.Fields(name), " ")
	return strings.ToLower(name) + extForFormat(rel.Format.Format)
}

func extForFormat(format string) string {
	switch format {
	case dom.FormatEPUB:
		return ".epub"
	case dom.FormatMOBI:
		return ".mobi"
	case dom.FormatAZW3:
		return ".azw3"
	default:
		return ".epub"
	}
}

// copyFile copies src to dst, creating dst's directory as needed.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
