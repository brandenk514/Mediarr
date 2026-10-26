package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	moviesdom "github.com/brandenk514/mediarr/internal/domains/movies"
	tvdom "github.com/brandenk514/mediarr/internal/domains/tv"
	"github.com/brandenk514/mediarr/internal/notify"
	"github.com/brandenk514/mediarr/internal/providers"
	moviesvc "github.com/brandenk514/mediarr/internal/services/movies"
	tvsvc "github.com/brandenk514/mediarr/internal/services/tv"
	"github.com/brandenk514/mediarr/internal/settings"
)

// SettingsP5Deps bundles what the P5 wave-2/wave-3 endpoints need on top of
// the generic section store: the metadata-provider adapters for the
// P5.7 fallback chain, the library adders import lists drive, and an
// outbound HTTP client for list pulls (tests point it at stubs).
type SettingsP5Deps struct {
	Store settings.Store

	// Metadata adapters. Keyless adapters are always usable; keyed ones
	// built with an empty key answer providers.ErrNotConfigured and the
	// fallback chain skips past them (P5.7).
	TVDB        *providers.TVDB
	TMDB        *providers.TMDB
	TVmaze      *providers.TVmaze
	Wikidata    *providers.Wikidata
	MusicBrainz *providers.MusicBrainz
	OpenLibrary *providers.OpenLibrary

	// TV / Movies are the import-list adders (nil = that kind's items are
	// reported skipped, not added).
	TV     TVImporter
	Movies MoviesImporter

	// HTTP is the outbound client for list pulls / notification sinks
	// (nil = default).
	HTTP *http.Client
}

// TVImporter is the slice of the tv service import lists use.
type TVImporter interface {
	AddSeries(ctx context.Context, req tvsvc.AddSeriesRequest) (int64, error)
	ListSeries(ctx context.Context) ([]tvdom.Series, error)
}

// MoviesImporter is the slice of the movies service import lists use.
type MoviesImporter interface {
	AddMovie(ctx context.Context, req moviesvc.AddMovieRequest) (int64, error)
	ListMovies(ctx context.Context) ([]moviesdom.Movie, error)
}

// SetSettingsP5 registers the P5 wave-2/3 endpoints. Call after NewServer.
func (s *Server) SetSettingsP5(deps *SettingsP5Deps) {
	s.settingsP5Deps = deps
	s.register("POST /api/v1/settings/custom-formats/score", s.requireAuth(s.ScoreCustomFormats))
	s.register("POST /api/v1/settings/connect/test", s.requireAuth(s.TestConnectNotification))
	s.register("POST /api/v1/settings/metadata-source/search", s.requireAuth(s.SearchMetadataSource))
	s.register("POST /api/v1/settings/import-lists/import", s.requireAuth(s.RunImportListImport))
}

// p5HTTP is the outbound client for sinks and list pulls.
func (s *Server) p5HTTP() *http.Client {
	if s.settingsP5Deps != nil && s.settingsP5Deps.HTTP != nil {
		return s.settingsP5Deps.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// readP5Body decodes a JSON request body (capped at 1 MiB); the error is a
// 400 message.
func readP5Body(r *http.Request, out any) error {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("request body too large")
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("invalid request body: %v", err)
	}
	return nil
}

// kindSearcher adapts a kind-specific provider search method to the generic
// providers.Searcher for the P5.7 fallback chain. A nil fn (adapter not
// configured) answers providers.ErrNotConfigured, which the chain records
// and falls through.
type kindSearcher struct {
	name string
	fn   func(context.Context, string) ([]providers.Result, error)
}

func (k kindSearcher) Name() string { return k.name }

func (k kindSearcher) Search(ctx context.Context, q string) ([]providers.Result, error) {
	if k.fn == nil {
		return nil, providers.ErrNotConfigured
	}
	return k.fn(ctx, q)
}

// containsKind is the local kind vocabulary check (movie/tv/music/book).
func containsKind(v string, kinds ...string) bool {
	for _, k := range kinds {
		if v == k {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// custom-format scoring
// ---------------------------------------------------------------------------

// CustomFormatScoreRequest is the body of POST /api/v1/settings/custom-formats/score.
type CustomFormatScoreRequest struct {
	Title   string   `json:"title"`
	Quality string   `json:"quality"`
	Source  string   `json:"source"`
	Kind    string   `json:"kind"`
	SizeMB  int64    `json:"size_mb"`
	Tags    []string `json:"tags"`
}

// customFormatScoreResult is the response.
type customFormatScoreResult struct {
	Score   int                `json:"score"`
	Matches []settings.CFMatch `json:"matches"`
}

// ScoreCustomFormats runs the custom-format scorer over the saved formats
// from the custom-formats section.
func (s *Server) ScoreCustomFormats(w http.ResponseWriter, r *http.Request) {
	if s.settingsP5Deps == nil {
		writeError(w, http.StatusNotImplemented, "custom format scoring is not available")
		return
	}
	var req CustomFormatScoreRequest
	if err := readP5Body(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, err := s.settingsP5Deps.Store.GetSectionDocument(r.Context(), settings.SectionCustomFormats)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read settings section")
		return
	}
	doc, err := settings.SectionCustomFormats.DecodeDocument(raw)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stored settings section is corrupt")
		return
	}
	formats := doc.(*settings.CustomFormatsSettings).Formats
	total, matches := settings.ScoreCF(formats, settings.CFCandidate{
		Title: req.Title, Quality: req.Quality, Source: req.Source,
		Kind: req.Kind, SizeMB: req.SizeMB, Tags: req.Tags,
	})
	writeJSON(w, http.StatusOK, customFormatScoreResult{Score: total, Matches: matches})
}

// ---------------------------------------------------------------------------
// connect test
// ---------------------------------------------------------------------------

// ConnectTestRequest is the body of POST /api/v1/settings/connect/test.
type ConnectTestRequest struct {
	ID      int                          `json:"id"`
	Kind    string                       `json:"kind"`
	Options settings.NotificationOptions `json:"options"`
	Event   string                       `json:"event"`
}

// connectTestResult is the response (the endpoint itself always answers 200;
// delivery failure is reported in ok/message).
type connectTestResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// TestConnectNotification builds a sink from a saved notification (id > 0)
// or an inline kind+options (id == 0) and fires a test event through it.
func (s *Server) TestConnectNotification(w http.ResponseWriter, r *http.Request) {
	if s.settingsP5Deps == nil {
		writeError(w, http.StatusNotImplemented, "connect is not available")
		return
	}
	var req ConnectTestRequest
	if err := readP5Body(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	event := req.Event
	if event == "" {
		event = "test"
	}

	kind, options, ok := s.connectSinkSpec(r, req)
	if !ok {
		writeError(w, http.StatusBadRequest, "connect test requires a saved notification id or an inline kind")
		return
	}
	sink, err := notify.BuildSink(notify.SinkSpec{
		Kind:       notify.SinkKind(kind),
		URL:        options.URL,
		SectionID:  options.SectionID,
		Token:      options.Token,
		HTTPClient: s.p5HTTP(),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := sink.Deliver(r.Context(), notify.Event{Type: event, Title: "Mediarr test event"}); err != nil {
		writeJSON(w, http.StatusOK, connectTestResult{OK: false, Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, connectTestResult{OK: true, Message: "test event delivered"})
}

// connectSinkSpec resolves the sink spec from the request: a saved
// notification (the stored document carries the plaintext token — masking
// happens only at the section read boundary) or the inline definition.
func (s *Server) connectSinkSpec(r *http.Request, req ConnectTestRequest) (string, settings.NotificationOptions, bool) {
	if req.ID == 0 {
		if req.Kind == "" {
			return "", settings.NotificationOptions{}, false
		}
		if !containsKind(req.Kind, settings.NotificationKinds...) {
			return "", settings.NotificationOptions{}, false
		}
		return req.Kind, req.Options, true
	}
	raw, err := s.settingsP5Deps.Store.GetSectionDocument(r.Context(), settings.SectionConnect)
	if err != nil {
		return "", settings.NotificationOptions{}, false
	}
	doc, err := settings.SectionConnect.DecodeDocument(raw)
	if err != nil {
		return "", settings.NotificationOptions{}, false
	}
	for _, n := range doc.(*settings.ConnectSettings).Notifications {
		if n.ID == req.ID {
			return n.Kind, n.Options, true
		}
	}
	return "", settings.NotificationOptions{}, false
}

// ---------------------------------------------------------------------------
// metadata-source fallback search (P5.7)
// ---------------------------------------------------------------------------

// MetadataSourceSearchRequest is the body of
// POST /api/v1/settings/metadata-source/search.
type MetadataSourceSearchRequest struct {
	Kind  string `json:"kind"`
	Query string `json:"query"`
}

// providerAttempt mirrors the spec's ProviderAttempt.
type providerAttempt struct {
	Source    string `json:"source"`
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error"`
}

// metadataSourceSearchResult is the response (200 or 503).
type metadataSourceSearchResult struct {
	Kind     string            `json:"kind"`
	Source   string            `json:"source"`
	Results  []searchResult    `json:"results"`
	Attempts []providerAttempt `json:"attempts"`
}

// SearchMetadataSource runs a kind's ordered source list (the P5.7 fallback
// chain). 200 = the first answering source's results; 503 = every source
// failed (attempt log still returned).
func (s *Server) SearchMetadataSource(w http.ResponseWriter, r *http.Request) {
	if s.settingsP5Deps == nil {
		writeError(w, http.StatusNotImplemented, "metadata source search is not available")
		return
	}
	var req MetadataSourceSearchRequest
	if err := readP5Body(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !containsKind(req.Kind, "movie", "tv", "music", "book") || strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, "kind must be movie, tv, music or book and query must be non-empty")
		return
	}
	d := s.settingsP5Deps

	raw, err := d.Store.GetSectionDocument(r.Context(), settings.SectionMetadataSource)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read settings section")
		return
	}
	doc, err := settings.SectionMetadataSource.DecodeDocument(raw)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stored settings section is corrupt")
		return
	}
	list := doc.(*settings.MetadataSourceSettings).Sources[req.Kind]
	if len(list) == 0 {
		list = settings.DefaultMetadataSource().Sources[req.Kind]
	}

	// A nil adapter is an unconfigured source: the chain logs it and
	// falls through to the next one. kindSearcher bridges every adapter
	// (including the kind-specific TMDB/OpenLibrary methods) onto the
	// generic Searcher and keeps a nil adapter from becoming a typed-nil
	// interface that would panic on call.
	chain := providers.NewChain()
	for _, name := range list {
		var fn func(context.Context, string) ([]providers.Result, error)
		switch name {
		case "tvdb":
			if d.TVDB != nil {
				fn = d.TVDB.Search
			}
		case "tmdb":
			// TMDB exposes kind-specific searches.
			if d.TMDB != nil {
				if req.Kind == "tv" {
					fn = d.TMDB.SearchTV
				} else {
					fn = d.TMDB.SearchMovies
				}
			}
		case "tvmaze":
			if d.TVmaze != nil {
				fn = d.TVmaze.Search
			}
		case "wikidata":
			if d.Wikidata != nil {
				fn = d.Wikidata.Search
			}
		case "musicbrainz":
			if d.MusicBrainz != nil {
				fn = d.MusicBrainz.Search
			}
		case "openlibrary":
			if d.OpenLibrary != nil {
				if req.Kind == "music" {
					fn = d.OpenLibrary.SearchAuthors
				} else {
					fn = func(ctx context.Context, q string) ([]providers.Result, error) {
						return d.OpenLibrary.SearchWorks(ctx, "", q)
					}
				}
			}
		default:
			writeError(w, http.StatusBadRequest, "unknown provider in source list: "+name)
			return
		}
		chain.Sources = append(chain.Sources, providers.Source{
			Name: name, Searcher: kindSearcher{name: name, fn: fn},
		})
	}

	source, results, attempts, err := chain.Search(r.Context(), strings.TrimSpace(req.Query))
	attemptLog := make([]providerAttempt, 0, len(attempts))
	for _, a := range attempts {
		attemptLog = append(attemptLog, providerAttempt{
			Source: a.Source, OK: a.OK, LatencyMS: a.LatencyMS, Error: a.Error,
		})
	}
	out := metadataSourceSearchResult{
		Kind:     req.Kind,
		Source:   source,
		Results:  toWire(results),
		Attempts: attemptLog,
	}
	if out.Results == nil {
		out.Results = []searchResult{}
	}
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, out)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------------------
// import lists (wave 3)
// ---------------------------------------------------------------------------

// ImportRunRequest is the body of POST /api/v1/settings/import-lists/import.
type ImportRunRequest struct {
	ID     int  `json:"id"`
	DryRun bool `json:"dry_run"`
}

// importRunItem mirrors the spec's ImportRunItem.
type importRunItem struct {
	ExternalID string `json:"external_id"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Message    string `json:"message"`
}

// importRunResult mirrors the spec's ImportRunResult.
type importRunResult struct {
	DryRun bool            `json:"dry_run"`
	Total  int             `json:"total"`
	Items  []importRunItem `json:"items"`
}

// RunImportListImport pulls one saved import list (Trakt list, or another
// Sonarr instance via its v3 API — the drop-in bridge) and, unless dry_run,
// adds the items to the matching library. Items already present are reported
// exists, not duplicated.
func (s *Server) RunImportListImport(w http.ResponseWriter, r *http.Request) {
	if s.settingsP5Deps == nil {
		writeError(w, http.StatusNotImplemented, "import lists are not available")
		return
	}
	var req ImportRunRequest
	if err := readP5Body(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ID < 1 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return
	}
	d := s.settingsP5Deps
	raw, err := d.Store.GetSectionDocument(r.Context(), settings.SectionImportLists)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read settings section")
		return
	}
	doc, err := settings.SectionImportLists.DecodeDocument(raw)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stored settings section is corrupt")
		return
	}
	ils := doc.(*settings.ImportListsSettings)
	var list *settings.ImportList
	for i := range ils.Lists {
		if ils.Lists[i].ID == req.ID {
			list = &ils.Lists[i]
			break
		}
	}
	if list == nil {
		writeError(w, http.StatusNotFound, "import list not found")
		return
	}
	if !list.Enabled {
		writeError(w, http.StatusBadRequest, "import list is disabled")
		return
	}

	entries, err := fetchImportList(r.Context(), *list, s.p5HTTP())
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch import list: "+err.Error())
		return
	}

	items := make([]importRunItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, s.applyImportEntry(r, e, req.DryRun))
	}
	writeJSON(w, http.StatusOK, importRunResult{DryRun: req.DryRun, Total: len(items), Items: items})
}

// applyImportEntry adds (or reports) one fetched list item.
func (s *Server) applyImportEntry(r *http.Request, e importListEntry, dryRun bool) importRunItem {
	item := importRunItem{ExternalID: e.ExternalID, Title: e.Title, Kind: e.Kind}
	d := s.settingsP5Deps
	ctx := r.Context()
	switch e.Kind {
	case "tv":
		if d.TV == nil {
			item.Status, item.Message = "skipped", "no tv library configured"
			return item
		}
		exists, err := seriesTitleIn(d.TV, ctx, e.Title)
		if err != nil {
			item.Status, item.Message = "error", err.Error()
			return item
		}
		if exists {
			item.Status, item.Message = "exists", "already in tv library"
			return item
		}
		if dryRun {
			item.Status, item.Message = "skipped", "dry run: would add to tv"
			return item
		}
		if _, err := d.TV.AddSeries(ctx, tvsvc.AddSeriesRequest{Title: e.Title, Year: e.Year, Monitored: true}); err != nil {
			item.Status, item.Message = "error", err.Error()
			return item
		}
		item.Status, item.Message = "imported", "added to tv"
		return item
	case "movie":
		if d.Movies == nil {
			item.Status, item.Message = "skipped", "no movie library configured"
			return item
		}
		exists, err := movieTitleIn(d.Movies, ctx, e.Title)
		if err != nil {
			item.Status, item.Message = "error", err.Error()
			return item
		}
		if exists {
			item.Status, item.Message = "exists", "already in movie library"
			return item
		}
		if dryRun {
			item.Status, item.Message = "skipped", "dry run: would add to movies"
			return item
		}
		if _, err := d.Movies.AddMovie(ctx, moviesvc.AddMovieRequest{Title: e.Title, Year: e.Year}); err != nil {
			item.Status, item.Message = "error", err.Error()
			return item
		}
		item.Status, item.Message = "imported", "added to movies"
		return item
	default:
		item.Status, item.Message = "skipped", "unsupported list item kind "+strconv.Quote(e.Kind)
		return item
	}
}

// importListEntry is one fetched list item, normalized.
type importListEntry struct {
	ExternalID string
	Title      string
	Year       string
	Kind       string // "tv" | "movie"
}

// seriesTitleIn reports whether a tv series with the (case-insensitive)
// title is already in the library.
func seriesTitleIn(tv TVImporter, ctx context.Context, title string) (bool, error) {
	series, err := tv.ListSeries(ctx)
	if err != nil {
		return false, err
	}
	for _, s := range series {
		if strings.EqualFold(s.Title, title) {
			return true, nil
		}
	}
	return false, nil
}

// movieTitleIn reports whether a movie with the (case-insensitive) title is
// already in the library.
func movieTitleIn(m MoviesImporter, ctx context.Context, title string) (bool, error) {
	movies, err := m.ListMovies(ctx)
	if err != nil {
		return false, err
	}
	for _, m := range movies {
		if strings.EqualFold(m.Title, title) {
			return true, nil
		}
	}
	return false, nil
}

// fetchImportList pulls the list per its kind and normalizes the items.
func fetchImportList(ctx context.Context, l settings.ImportList, httpc *http.Client) ([]importListEntry, error) {
	if l.Kind == "trakt" {
		return fetchTraktList(ctx, l, httpc)
	}
	return fetchSonarrList(ctx, l, httpc)
}

// traktAPIBase is the Trakt API host (tests override it with a stub).
var traktAPIBase = "https://api.trakt.tv"

// fetchTraktList pulls a Trakt list (https://trakt.tv/users/<u>/lists/<s>)
// through the Trakt API (the list's api_key is the Trakt client id).
func fetchTraktList(ctx context.Context, l settings.ImportList, httpc *http.Client) ([]importListEntry, error) {
	user, slug, ok := parseTraktListURL(l.URL)
	if !ok {
		return nil, fmt.Errorf("trakt url must be https://trakt.tv/users/<user>/lists/<slug>")
	}
	u := traktAPIBase + "/users/" + user + "/lists/" + slug + "/items?extended=full"
	var raw []struct {
		Type string `json:"type"`
		ID   int    `json:"id"`
		Show *struct {
			Title string `json:"title"`
			Year  int    `json:"year"`
		} `json:"show"`
		Movie *struct {
			Title string `json:"title"`
			Year  int    `json:"year"`
		} `json:"movie"`
	}
	if err := httpGetJSON(ctx, httpc, u, l.APIKey, "trakt-api-key", &raw); err != nil {
		return nil, err
	}
	out := make([]importListEntry, 0, len(raw))
	for _, it := range raw {
		var e importListEntry
		switch it.Type {
		case "show":
			if it.Show == nil {
				continue
			}
			e = importListEntry{Kind: "tv", Title: it.Show.Title, Year: yearString(it.Show.Year), ExternalID: strconv.Itoa(it.ID)}
		case "movie":
			if it.Movie == nil {
				continue
			}
			e = importListEntry{Kind: "movie", Title: it.Movie.Title, Year: yearString(it.Movie.Year), ExternalID: strconv.Itoa(it.ID)}
		default:
			continue
		}
		if e.Title != "" {
			out = append(out, e)
		}
	}
	return out, nil
}

// fetchSonarrList pulls the series from another Sonarr instance (v3 API) —
// the drop-in bridge.
func fetchSonarrList(ctx context.Context, l settings.ImportList, httpc *http.Client) ([]importListEntry, error) {
	base := strings.TrimRight(l.URL, "/")
	var raw []struct {
		Title    string `json:"title"`
		Year     int    `json:"year"`
		SeriesID int    `json:"seriesId"`
	}
	if err := httpGetJSON(ctx, httpc, base+"/api/v3/series", l.APIKey, "X-Api-Key", &raw); err != nil {
		return nil, err
	}
	out := make([]importListEntry, 0, len(raw))
	for _, s := range raw {
		if s.Title == "" {
			continue
		}
		out = append(out, importListEntry{
			Kind: "tv", Title: s.Title, Year: yearString(s.Year), ExternalID: strconv.Itoa(s.SeriesID),
		})
	}
	return out, nil
}

// parseTraktListURL extracts user and slug from a Trakt list URL.
func parseTraktListURL(raw string) (string, string, bool) {
	const prefix = "trakt.tv/users/"
	i := strings.Index(raw, prefix)
	if i < 0 {
		return "", "", false
	}
	rest := raw[i+len(prefix):]
	parts := strings.Split(rest, "/")
	if len(parts) < 3 || parts[0] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[0], parts[2], true
}

// httpGetJSON does a GET with an auth header and decodes the JSON body.
func httpGetJSON(ctx context.Context, httpc *http.Client, url, token, header string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set(header, token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("target returned %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out); err != nil {
		return err
	}
	return nil
}

func yearString(y int) string {
	if y < 1000 {
		return ""
	}
	return strconv.Itoa(y)
}
