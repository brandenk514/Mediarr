package api

// P7 (sonarr-parity-plan §4): the multi-kind shell serves Movies, Music, and
// Books through the same API patterns as TV. Wave 1 added the movie release
// endpoint (TestGetMovieWanted, lists_test.go) and the movie add-flow E2E
// (addflow_e2e_test.go). These integration tests cover the music and books
// library endpoints the P7 UI pages drive (list → add → detail children)
// with real services on in-memory repos — no live external calls.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	booksdom "github.com/brandenk514/mediarr/internal/domains/books"
	musicdom "github.com/brandenk514/mediarr/internal/domains/music"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/providers"
	bookssvc "github.com/brandenk514/mediarr/internal/services/books"
	musicsvc "github.com/brandenk514/mediarr/internal/services/music"
)

// kindsServer is a fully-wired, authenticated server with the music and books
// endpoints on in-memory repos.
type kindsServer struct {
	s     *Server
	token string
}

// newKindsServer builds the fixture; the optional metadata adapter (L4) is
// wired into the music service when provided (nil adapter = enrichment is
// skipped with a warning, which the L4 tests exercise).
func newKindsServer(t *testing.T, music *kindsMusicRepo, books *kindsBooksRepo, metadata ...*providers.MusicBrainz) *kindsServer {
	t.Helper()
	store := newFakeStore()
	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	hash, err := auth.HashPassword("a-very-long-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser(context.Background(), "admin", "", "admin", hash); err != nil {
		t.Fatal(err)
	}
	rec := postJSON(t, s.Handler(), "/api/v1/auth/login", loginRequest{
		Username: "admin", Password: "a-very-long-password",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	var lr loginResponse
	if err := json.NewDecoder(rec.Body).Decode(&lr); err != nil {
		t.Fatal(err)
	}
	musicDeps := musicsvc.Deps{Repo: music, DefaultProfile: "FLAC"}
	if len(metadata) > 0 {
		musicDeps.Metadata = metadata[0]
	}
	musicSvc := musicsvc.New(musicDeps)
	s.SetMusic(&MusicDeps{Svc: musicSvc})
	booksSvc := bookssvc.New(bookssvc.Deps{Repo: books, DefaultProfile: "Best"})
	s.SetBooks(&BooksDeps{Svc: booksSvc})
	return &kindsServer{s: s, token: lr.Token}
}

// ---- In-memory music repo (embeds the no-op stub, overrides the CRUD the
// tests exercise). ----

type kindsMusicRepo struct {
	stubMusicRepo
	artists map[int64]musicdom.Artist
	albums  map[int64]musicdom.Album
	tracks  map[int64][]musicdom.Track
	nextID  int64
}

func newKindsMusicRepo() *kindsMusicRepo {
	return &kindsMusicRepo{
		artists: map[int64]musicdom.Artist{},
		albums:  map[int64]musicdom.Album{},
		tracks:  map[int64][]musicdom.Track{},
		nextID:  1,
	}
}

func (r *kindsMusicRepo) CreateArtist(_ context.Context, a musicdom.Artist) (int64, error) {
	id := r.nextID
	r.nextID++
	a.ID = id
	if a.AddedAt.IsZero() {
		a.AddedAt = time.Now().UTC()
	}
	r.artists[id] = a
	return id, nil
}

func (r *kindsMusicRepo) GetArtist(_ context.Context, id int64) (*musicdom.Artist, error) {
	a, ok := r.artists[id]
	if !ok {
		return nil, musicdom.ErrArtistNotFound
	}
	return &a, nil
}

func (r *kindsMusicRepo) ListArtists(_ context.Context) ([]musicdom.Artist, error) {
	out := make([]musicdom.Artist, 0, len(r.artists))
	for _, a := range r.artists {
		out = append(out, a)
	}
	return out, nil
}

func (r *kindsMusicRepo) SetArtistMonitored(_ context.Context, id int64, monitored bool) error {
	if a, ok := r.artists[id]; ok {
		a.Monitored = monitored
		r.artists[id] = a
	}
	return nil
}

// UpdateArtist mirrors the real repo's L2 edit semantics: the L1/L2 editable
// fields are written on the stored artist; unknown artist → ErrArtistNotFound.
func (r *kindsMusicRepo) UpdateArtist(_ context.Context, a musicdom.Artist) error {
	cur, ok := r.artists[a.ID]
	if !ok {
		return musicdom.ErrArtistNotFound
	}
	cur.Monitored = a.Monitored
	cur.Status = a.Status
	cur.QualityProfile = a.QualityProfile
	cur.Path = a.Path
	cur.MonitorPolicy = a.MonitorPolicy
	cur.MonitorNewAlbums = a.MonitorNewAlbums
	cur.Tags = a.Tags
	// L4 enrichment fields (mirrors MusicRepo.UpdateArtist).
	cur.Rating = a.Rating
	cur.Genre = a.Genre
	cur.Links = a.Links
	cur.Cover = a.Cover
	r.artists[a.ID] = cur
	return nil
}

func (r *kindsMusicRepo) CreateAlbum(_ context.Context, a musicdom.Album) (int64, error) {
	id := r.nextID
	r.nextID++
	a.ID = id
	if a.AddedAt.IsZero() {
		a.AddedAt = time.Now().UTC()
	}
	r.albums[id] = a
	return id, nil
}

func (r *kindsMusicRepo) GetAlbum(_ context.Context, id int64) (*musicdom.Album, error) {
	a, ok := r.albums[id]
	if !ok {
		return nil, musicdom.ErrAlbumNotFound
	}
	return &a, nil
}

func (r *kindsMusicRepo) ListAlbums(_ context.Context, artistID int64) ([]musicdom.Album, error) {
	var out []musicdom.Album
	for _, a := range r.albums {
		if a.ArtistID == artistID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *kindsMusicRepo) SetAlbumMonitored(_ context.Context, id int64, monitored bool) error {
	if a, ok := r.albums[id]; ok {
		a.Monitored = monitored
		r.albums[id] = a
	}
	return nil
}

// L4 (refresh flow): metadata enrichment and track replacement through the
// real handlers.
func (r *kindsMusicRepo) UpdateAlbum(_ context.Context, a musicdom.Album) error {
	cur, ok := r.albums[a.ID]
	if !ok {
		return musicdom.ErrAlbumNotFound
	}
	cur.MBID = a.MBID
	cur.Year = a.Year
	cur.AlbumType = a.AlbumType
	cur.ReleaseDate = a.ReleaseDate
	cur.Duration = a.Duration
	cur.Cover = a.Cover
	cur.Labels = a.Labels
	cur.Genres = a.Genres
	r.albums[a.ID] = cur
	return nil
}
func (r *kindsMusicRepo) DeleteTracks(_ context.Context, albumID int64) error {
	delete(r.tracks, albumID)
	return nil
}

// SetTrackStatus (L4 manual import): mark a track's file state.
func (r *kindsMusicRepo) SetTrackStatus(_ context.Context, albumID int64, disc, number int, status string) (bool, error) {
	for i, t := range r.tracks[albumID] {
		if t.Disc == disc && t.Number == number {
			t.Status = status
			r.tracks[albumID][i] = t
			return true, nil
		}
	}
	return false, nil
}

// No-op lifecycle methods the services call on the add paths.
func (r *kindsMusicRepo) AddHistory(_ context.Context, _ musicdom.HistoryEntry) error { return nil }
func (r *kindsMusicRepo) EnsureWantedAlbum(_ context.Context, _ int64) error {
	return nil
}
func (r *kindsMusicRepo) EnsureWantedTrack(_ context.Context, _, _ int64) error {
	return nil
}
func (r *kindsMusicRepo) GetWanted(_ context.Context, _, _ int64) (*musicdom.Wanted, error) {
	return nil, nil
}
func (r *kindsMusicRepo) ListWanted(_ context.Context, _ int64) ([]musicdom.Wanted, error) {
	return nil, nil
}
func (r *kindsMusicRepo) ListHistory(_ context.Context, _ int64, _ int) ([]musicdom.HistoryEntry, error) {
	return nil, nil
}

// CreateTrack and ListTracks override the stub's panics so the L1 endpoint
// tests can exercise the track/file footer counts through the real handlers.
func (r *kindsMusicRepo) CreateTrack(_ context.Context, t musicdom.Track) (int64, error) {
	t.ID = r.nextID
	r.nextID++
	if t.AddedAt.IsZero() {
		t.AddedAt = time.Now().UTC()
	}
	r.tracks[t.AlbumID] = append(r.tracks[t.AlbumID], t)
	return t.ID, nil
}
func (r *kindsMusicRepo) ListTracks(_ context.Context, albumID int64) ([]musicdom.Track, error) {
	out := make([]musicdom.Track, len(r.tracks[albumID]))
	copy(out, r.tracks[albumID])
	return out, nil
}

// LibrarySummary computes the footer counts from the in-memory state with the
// same semantics as the SQL: "files" = tracks with a non-empty status (an
// imported file).
func (r *kindsMusicRepo) LibrarySummary(_ context.Context) (musicdom.LibrarySummary, error) {
	var s musicdom.LibrarySummary
	s.Artists = int64(len(r.artists))
	for _, a := range r.artists {
		switch a.Status {
		case musicdom.StatusInactive:
			s.Inactive++
		case musicdom.StatusContinuing:
			s.Continuing++
		}
		if a.Monitored {
			s.Monitored++
		} else {
			s.Unmonitored++
		}
	}
	for _, ts := range r.tracks {
		for _, t := range ts {
			s.Tracks++
			if t.Status != "" {
				s.Files++
				s.TotalFileSize += t.Size
			}
		}
	}
	return s, nil
}

// ---- In-memory books repo (same pattern). ----

type kindsBooksRepo struct {
	stubBooksRepo
	authors  map[int64]booksdom.Author
	titles   map[int64]booksdom.Title
	editions map[int64]booksdom.Edition
	nextID   int64
	// Readarr B1.
	bookFiles     map[int64]booksdom.BookFile
	nextBookFile  int64
	series        map[int64]booksdom.Series
	nextSeries    int64
	seriesMembers map[kindsMemberKey]struct{}
	history       []booksdom.HistoryEntry
	nextHistory   int64
}

type kindsMemberKey struct {
	series int64
	title  int64
}

func newKindsBooksRepo() *kindsBooksRepo {
	return &kindsBooksRepo{
		authors:       map[int64]booksdom.Author{},
		titles:        map[int64]booksdom.Title{},
		editions:      map[int64]booksdom.Edition{},
		bookFiles:     map[int64]booksdom.BookFile{},
		series:        map[int64]booksdom.Series{},
		seriesMembers: map[kindsMemberKey]struct{}{},
		nextID:        1,
		nextBookFile:  1,
		nextSeries:    1,
		nextHistory:   1,
	}
}

func (r *kindsBooksRepo) CreateAuthor(_ context.Context, a booksdom.Author) (int64, error) {
	id := r.nextID
	r.nextID++
	a.ID = id
	if a.AddedAt.IsZero() {
		a.AddedAt = time.Now().UTC()
	}
	r.authors[id] = a
	return id, nil
}

func (r *kindsBooksRepo) GetAuthor(_ context.Context, id int64) (*booksdom.Author, error) {
	a, ok := r.authors[id]
	if !ok {
		return nil, booksdom.ErrAuthorNotFound
	}
	return &a, nil
}

func (r *kindsBooksRepo) ListAuthors(_ context.Context) ([]booksdom.Author, error) {
	out := make([]booksdom.Author, 0, len(r.authors))
	for _, a := range r.authors {
		out = append(out, a)
	}
	return out, nil
}

func (r *kindsBooksRepo) SetAuthorMonitored(_ context.Context, id int64, monitored bool) error {
	if a, ok := r.authors[id]; ok {
		a.Monitored = monitored
		r.authors[id] = a
	}
	return nil
}

func (r *kindsBooksRepo) UpdateAuthor(_ context.Context, a booksdom.Author) error {
	if _, ok := r.authors[a.ID]; !ok {
		return booksdom.ErrAuthorNotFound
	}
	a.ID = r.authors[a.ID].ID
	a.AddedAt = r.authors[a.ID].AddedAt
	r.authors[a.ID] = a
	return nil
}

func (r *kindsBooksRepo) CreateTitle(_ context.Context, t booksdom.Title) (int64, error) {
	id := r.nextID
	r.nextID++
	t.ID = id
	if t.AddedAt.IsZero() {
		t.AddedAt = time.Now().UTC()
	}
	r.titles[id] = t
	return id, nil
}

func (r *kindsBooksRepo) GetTitle(_ context.Context, id int64) (*booksdom.Title, error) {
	t, ok := r.titles[id]
	if !ok {
		return nil, booksdom.ErrTitleNotFound
	}
	return &t, nil
}

func (r *kindsBooksRepo) ListTitles(_ context.Context, authorID int64) ([]booksdom.Title, error) {
	var out []booksdom.Title
	for _, t := range r.titles {
		if t.AuthorID == authorID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *kindsBooksRepo) SetTitleMonitored(_ context.Context, id int64, monitored bool) error {
	if t, ok := r.titles[id]; ok {
		t.Monitored = monitored
		r.titles[id] = t
	}
	return nil
}

func (r *kindsBooksRepo) CreateEdition(_ context.Context, e booksdom.Edition) (int64, error) {
	id := r.nextID
	r.nextID++
	e.ID = id
	if e.AddedAt.IsZero() {
		e.AddedAt = time.Now().UTC()
	}
	r.editions[id] = e
	return id, nil
}

func (r *kindsBooksRepo) ListEditions(_ context.Context, titleID int64) ([]booksdom.Edition, error) {
	var out []booksdom.Edition
	for _, e := range r.editions {
		if e.TitleID == titleID {
			out = append(out, e)
		}
	}
	return out, nil
}
func (r *kindsBooksRepo) GetEdition(_ context.Context, id int64) (*booksdom.Edition, error) {
	e, ok := r.editions[id]
	if !ok {
		return nil, booksdom.ErrEditionNotFound
	}
	return &e, nil
}

func (r *kindsBooksRepo) SetEditionMonitored(_ context.Context, id int64, monitored bool) error {
	if e, ok := r.editions[id]; ok {
		e.Monitored = monitored
		r.editions[id] = e
	}
	return nil
}

// No-op lifecycle methods the services call on the add paths.
func (r *kindsBooksRepo) AddHistory(_ context.Context, e booksdom.HistoryEntry) error {
	e.ID = r.nextHistory
	r.nextHistory++
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	r.history = append(r.history, e)
	return nil
}
func (r *kindsBooksRepo) ListHistory(_ context.Context, authorID int64, limit int) ([]booksdom.HistoryEntry, error) {
	var out []booksdom.HistoryEntry
	for _, e := range r.history {
		if e.AuthorID == authorID {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}
func (r *kindsBooksRepo) EnsureWantedTitle(_ context.Context, _ int64) error { return nil }
func (r *kindsBooksRepo) EnsureWantedEdition(_ context.Context, _, _ int64) error {
	return nil
}
func (r *kindsBooksRepo) GetWanted(_ context.Context, _, _ int64) (*booksdom.Wanted, error) {
	return nil, nil
}
func (r *kindsBooksRepo) ListWanted(_ context.Context, _ int64) ([]booksdom.Wanted, error) {
	return nil, nil
}

// Readarr B4: the bookshelves rollup pair for a title, mirroring the postgres
// repo's correlated subqueries — HasFile (any imported file) and Format (the
// newest imported file's format, else the newest edition's format, else "").
func (r *kindsBooksRepo) rollupFor(titleID int64) (bool, string) {
	var hasFile, picked bool
	var format string
	var at time.Time
	var id int64
	for _, f := range r.bookFiles {
		if f.TitleID != titleID {
			continue
		}
		hasFile = true
		if !picked || f.AddedAt.After(at) || (f.AddedAt.Equal(at) && f.ID > id) {
			picked, format, at, id = true, f.Format, f.AddedAt, f.ID
		}
	}
	if hasFile {
		return hasFile, format
	}
	for _, e := range r.editions {
		if e.TitleID != titleID {
			continue
		}
		if !picked || e.AddedAt.After(at) || (e.AddedAt.Equal(at) && e.ID > id) {
			picked, format, at, id = true, e.Format, e.AddedAt, e.ID
		}
	}
	return false, format
}

// Readarr B1 methods.
func (r *kindsBooksRepo) CreateBookFile(_ context.Context, f booksdom.BookFile) (int64, error) {
	for _, v := range r.bookFiles {
		if v.TitleID == f.TitleID && v.Path == f.Path {
			f.ID, f.AddedAt = v.ID, v.AddedAt
			r.bookFiles[f.ID] = f
			return f.ID, nil
		}
	}
	f.ID = r.nextBookFile
	r.nextBookFile++
	f.AddedAt = time.Now().UTC()
	r.bookFiles[f.ID] = f
	return f.ID, nil
}
func (r *kindsBooksRepo) ListBookFilesByTitle(_ context.Context, titleID int64) ([]booksdom.BookFile, error) {
	var out []booksdom.BookFile
	for _, f := range r.bookFiles {
		if f.TitleID == titleID {
			out = append(out, f)
		}
	}
	return out, nil
}
func (r *kindsBooksRepo) ListBookFilesByEdition(_ context.Context, editionID int64) ([]booksdom.BookFile, error) {
	var out []booksdom.BookFile
	for _, f := range r.bookFiles {
		if f.EditionID == editionID {
			out = append(out, f)
		}
	}
	return out, nil
}
func (r *kindsBooksRepo) ListBooks(_ context.Context) ([]booksdom.Book, error) {
	var out []booksdom.Book
	for _, t := range r.titles {
		if a, ok := r.authors[t.AuthorID]; ok {
			hasFile, format := r.rollupFor(t.ID)
			out = append(out, booksdom.Book{Title: t, AuthorName: a.Name, HasFile: hasFile, Format: format})
		}
	}
	return out, nil
}
func (r *kindsBooksRepo) CreateSeries(_ context.Context, s booksdom.Series) (int64, error) {
	for _, v := range r.series {
		if v.Name == s.Name {
			return v.ID, nil
		}
	}
	s.ID = r.nextSeries
	r.nextSeries++
	s.AddedAt = time.Now().UTC()
	r.series[s.ID] = s
	return s.ID, nil
}
func (r *kindsBooksRepo) GetSeries(_ context.Context, id int64) (*booksdom.Series, error) {
	s, ok := r.series[id]
	if !ok {
		return nil, booksdom.ErrSeriesNotFound
	}
	return &s, nil
}
func (r *kindsBooksRepo) ListSeries(_ context.Context) ([]booksdom.Series, error) {
	var out []booksdom.Series
	for _, s := range r.series {
		out = append(out, s)
	}
	return out, nil
}
func (r *kindsBooksRepo) ListSeriesBooks(_ context.Context, seriesID int64) ([]booksdom.SeriesBook, error) {
	if _, err := r.GetSeries(context.Background(), seriesID); err != nil {
		return nil, err
	}
	var out []booksdom.SeriesBook
	for k := range r.seriesMembers {
		if k.series != seriesID {
			continue
		}
		t, ok := r.titles[k.title]
		if !ok {
			continue
		}
		if a, ok := r.authors[t.AuthorID]; ok {
			hasFile, format := r.rollupFor(t.ID)
			out = append(out, booksdom.SeriesBook{Title: t, AuthorName: a.Name, HasFile: hasFile, Format: format})
		}
	}
	return out, nil
}
func (r *kindsBooksRepo) ListTitleSeries(_ context.Context, titleID int64) ([]booksdom.Series, error) {
	var out []booksdom.Series
	for id, s := range r.series {
		if _, ok := r.seriesMembers[kindsMemberKey{series: id, title: titleID}]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}
func (r *kindsBooksRepo) ListAuthorSeries(_ context.Context, authorID int64) ([]booksdom.AuthorSeries, error) {
	if _, ok := r.authors[authorID]; !ok {
		return nil, booksdom.ErrAuthorNotFound
	}
	var out []booksdom.AuthorSeries
	for id, s := range r.series {
		var titles []string
		for k := range r.seriesMembers {
			if k.series != id {
				continue
			}
			if t, ok := r.titles[k.title]; ok && t.AuthorID == authorID {
				titles = append(titles, t.Name)
			}
		}
		if len(titles) > 0 {
			out = append(out, booksdom.AuthorSeries{Series: s, Titles: titles})
		}
	}
	return out, nil
}
func (r *kindsBooksRepo) LinkTitleToSeries(_ context.Context, titleID, seriesID int64) error {
	if _, err := r.GetTitle(context.Background(), titleID); err != nil {
		return err
	}
	if _, err := r.GetSeries(context.Background(), seriesID); err != nil {
		return err
	}
	r.seriesMembers[kindsMemberKey{series: seriesID, title: titleID}] = struct{}{}
	return nil
}

func (f *kindsServer) post(t *testing.T, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.token)
	rec := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(rec, req)
	return rec
}

// ---- Music library flow: add artist → add album (lossless profile) → list. ----

func TestP7_MusicLibraryFlow(t *testing.T) {
	music, books := newKindsMusicRepo(), newKindsBooksRepo()
	f := newKindsServer(t, music, books)
	h := f.s.Handler()

	// Add an artist (the add-artist page's core path).
	rec := f.post(t, "/api/v1/music/artists", map[string]any{
		"name": "Radiohead", "mbid": "mbid-1",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add artist status = %d body=%s", rec.Code, rec.Body.String())
	}
	var artistID struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&artistID); err != nil {
		t.Fatal(err)
	}

	// The artists list is the P1-style paginated envelope.
	rec = getJSON(t, h, "/api/v1/music/artists", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list artists status = %d", rec.Code)
	}
	var page struct {
		Items []struct {
			Name string `json:"Name"`
		} `json:"Items"`
		Total int `json:"Total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].Name != "Radiohead" {
		t.Fatalf("page = %+v", page)
	}

	// Add an album with the lossless quality profile (the P7 add-album form).
	rec = f.post(t, "/api/v1/music/artists/1/albums", map[string]any{
		"name": "In Rainbows", "year": 2007, "quality_profile": "FLAC",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add album status = %d body=%s", rec.Code, rec.Body.String())
	}

	// The artist's album list carries the lossless profile.
	rec = getJSON(t, h, "/api/v1/music/artists/1/albums", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list albums status = %d", rec.Code)
	}
	var albums []struct {
		Name           string `json:"Name"`
		QualityProfile string `json:"QualityProfile"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&albums); err != nil {
		t.Fatal(err)
	}
	if len(albums) != 1 || albums[0].Name != "In Rainbows" || albums[0].QualityProfile != "FLAC" {
		t.Fatalf("albums = %+v", albums)
	}

	// Artist-level monitor toggle (the list header toggle).
	rec = putJSON(t, h, "/api/v1/music/artists/1", f.token, map[string]bool{"monitored": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("set artist monitored status = %d", rec.Code)
	}
	if music.artists[artistID.ID].Monitored {
		t.Fatal("artist should be unmonitored after the toggle")
	}

	// Unknown artist is a 404.
	rec = getJSON(t, h, "/api/v1/music/artists/999", f.token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown artist status = %d", rec.Code)
	}

	// Unauthenticated requests are rejected.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/music/artists", nil)
	unauth := httptest.NewRecorder()
	h.ServeHTTP(unauth, req)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d", unauth.Code)
	}
	_ = books // books covered below; shared server keeps both route sets registered
}

// ---- Books library flow: add author → add title → add edition → list. ----

func TestP7_BooksLibraryFlow(t *testing.T) {
	music, books := newKindsMusicRepo(), newKindsBooksRepo()
	f := newKindsServer(t, music, books)
	h := f.s.Handler()

	rec := f.post(t, "/api/v1/books/authors", map[string]any{"name": "Ursula K. Le Guin"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add author status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = getJSON(t, h, "/api/v1/books/authors", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list authors status = %d", rec.Code)
	}
	var page struct {
		Items []struct {
			Name string `json:"Name"`
		} `json:"Items"`
		Total int `json:"Total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].Name != "Ursula K. Le Guin" {
		t.Fatalf("page = %+v", page)
	}

	rec = f.post(t, "/api/v1/books/authors/1/titles", map[string]any{"name": "The Dispossessed"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add title status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = getJSON(t, h, "/api/v1/books/authors/1/titles", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list titles status = %d", rec.Code)
	}
	var titles []struct {
		Name string `json:"Name"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&titles); err != nil {
		t.Fatal(err)
	}
	if len(titles) != 1 || titles[0].Name != "The Dispossessed" {
		t.Fatalf("titles = %+v", titles)
	}

	rec = f.post(t, "/api/v1/books/titles/2/editions", map[string]any{
		"format": "epub", "isbn": "978-0060722820", "year": 2010, "pages": 320,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add edition status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = getJSON(t, h, "/api/v1/books/titles/2/editions", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list editions status = %d", rec.Code)
	}
	var editions []struct {
		Format string `json:"Format"`
		ISBN   string `json:"ISBN"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&editions); err != nil {
		t.Fatal(err)
	}
	if len(editions) != 1 || editions[0].Format != "epub" || editions[0].ISBN != "978-0060722820" {
		t.Fatalf("editions = %+v", editions)
	}

	// Unknown title is a 404.
	rec = getJSON(t, h, "/api/v1/books/titles/999", f.token)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown title status = %d", rec.Code)
	}
}

// TestP7_BooksB1ReadSurface exercises the Readarr B1 additions: edition
// language round-trip, per-edition monitored toggle, the flat Books index,
// book files, series, and per-author history.
func TestP7_BooksB1ReadSurface(t *testing.T) {
	music, books := newKindsMusicRepo(), newKindsBooksRepo()
	f := newKindsServer(t, music, books)
	h := f.s.Handler()

	rec := f.post(t, "/api/v1/books/authors", map[string]any{"name": "Ursula K. Le Guin"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add author status = %d", rec.Code)
	}
	rec = f.post(t, "/api/v1/books/authors/1/titles", map[string]any{"name": "The Dispossessed"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add title status = %d", rec.Code)
	}
	// Title id is 2; the edition gets id 3.
	rec = f.post(t, "/api/v1/books/titles/2/editions", map[string]any{
		"format": "epub", "language": "en", "isbn": "978-0060722820", "pages": 320,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add edition status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Edition list carries the language.
	rec = getJSON(t, h, "/api/v1/books/titles/2/editions", f.token)
	var editions []struct {
		Format   string `json:"Format"`
		Language string `json:"Language"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&editions); err != nil {
		t.Fatal(err)
	}
	if len(editions) != 1 || editions[0].Language != "en" {
		t.Fatalf("editions = %+v", editions)
	}

	// Per-edition monitored toggle.
	rec = putJSON(t, h, "/api/v1/books/editions/3", f.token, map[string]any{"monitored": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle edition status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = getJSON(t, h, "/api/v1/books/titles/2/editions", f.token)
	if err := json.NewDecoder(rec.Body).Decode(&editions); err != nil {
		t.Fatal(err)
	}

	// Flat Books index: every book across all authors, with author names.
	rec = getJSON(t, h, "/api/v1/books", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list books status = %d", rec.Code)
	}
	var booksIdx []struct {
		ID         int64  `json:"ID"`
		Name       string `json:"Name"`
		AuthorName string `json:"AuthorName"`
		HasFile    bool   `json:"HasFile"`
		Format     string `json:"Format"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&booksIdx); err != nil {
		t.Fatal(err)
	}
	if len(booksIdx) != 1 || booksIdx[0].Name != "The Dispossessed" ||
		booksIdx[0].AuthorName != "Ursula K. Le Guin" {
		t.Fatalf("books index = %+v", booksIdx)
	}
	// Readarr B4 rollup pair: the title has an epub edition but no imported
	// file, so HasFile=false and the resolvable Format falls back to the
	// edition's format.
	if booksIdx[0].HasFile || booksIdx[0].Format != "epub" {
		t.Fatalf("books index rollup = %+v (want HasFile=false, Format=epub)", booksIdx[0])
	}

	// Series lifecycle: add (idempotent), list, get, link a title, list
	// membership from both directions, unknown series is a 404.
	rec = f.post(t, "/api/v1/books/series", map[string]any{"name": "The Dispossessed Saga"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add series status = %d", rec.Code)
	}
	var added struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&added); err != nil {
		t.Fatal(err)
	}
	seriesID := added.ID
	rec = f.post(t, "/api/v1/books/series", map[string]any{"name": "The Dispossessed Saga"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("idempotent add series status = %d", rec.Code)
	}
	rec = getJSON(t, h, "/api/v1/books/series", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list series status = %d", rec.Code)
	}
	var series []struct {
		ID   int64  `json:"ID"`
		Name string `json:"Name"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&series); err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("series = %+v", series)
	}
	if rec = getJSON(t, h, fmt.Sprintf("/api/v1/books/series/%d", seriesID), f.token); rec.Code != http.StatusOK {
		t.Fatalf("get series status = %d", rec.Code)
	}
	if rec = getJSON(t, h, "/api/v1/books/series/999999", f.token); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown series status = %d", rec.Code)
	}

	rec = f.post(t, "/api/v1/books/titles/2/series", map[string]any{"series_id": seriesID})
	if rec.Code != http.StatusOK {
		t.Fatalf("link title status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = f.post(t, "/api/v1/books/titles/2/series", map[string]any{"series_id": seriesID})
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotent link status = %d", rec.Code)
	}
	rec = getJSON(t, h, "/api/v1/books/titles/2/series", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("title series status = %d", rec.Code)
	}
	if err := json.NewDecoder(rec.Body).Decode(&series); err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Name != "The Dispossessed Saga" {
		t.Fatalf("title series = %+v", series)
	}
	rec = getJSON(t, h, fmt.Sprintf("/api/v1/books/series/%d/books", seriesID), f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("series books status = %d", rec.Code)
	}
	var seriesBooks []struct {
		Name       string `json:"Name"`
		AuthorName string `json:"AuthorName"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&seriesBooks); err != nil {
		t.Fatal(err)
	}
	if len(seriesBooks) != 1 || seriesBooks[0].AuthorName != "Ursula K. Le Guin" {
		t.Fatalf("series books = %+v", seriesBooks)
	}
	rec = getJSON(t, h, "/api/v1/books/authors/1/series", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("author series status = %d", rec.Code)
	}

	// Book files: empty for a freshly added (never imported) title.
	rec = getJSON(t, h, "/api/v1/books/titles/2/files", f.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("book files status = %d", rec.Code)
	}
	var files []struct {
		Path string `json:"Path"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %+v", files)
	}
	if rec = getJSON(t, h, "/api/v1/books/titles/999/files", f.token); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown title files status = %d", rec.Code)
	}

	// Per-author history: 200 (may be non-empty) for a known author, 404 for
	// an unknown one.
	if rec = getJSON(t, h, "/api/v1/books/authors/1/history", f.token); rec.Code != http.StatusOK {
		t.Fatalf("author history status = %d", rec.Code)
	}
	if rec = getJSON(t, h, "/api/v1/books/authors/999/history", f.token); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown author history status = %d", rec.Code)
	}
}
