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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	booksdom "github.com/brandenk514/mediarr/internal/domains/books"
	musicdom "github.com/brandenk514/mediarr/internal/domains/music"
	"github.com/brandenk514/mediarr/internal/health"
	bookssvc "github.com/brandenk514/mediarr/internal/services/books"
	musicsvc "github.com/brandenk514/mediarr/internal/services/music"
)

// kindsServer is a fully-wired, authenticated server with the music and books
// endpoints on in-memory repos.
type kindsServer struct {
	s     *Server
	token string
}

func newKindsServer(t *testing.T, music *kindsMusicRepo, books *kindsBooksRepo) *kindsServer {
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
	musicSvc := musicsvc.New(musicsvc.Deps{Repo: music, DefaultProfile: "FLAC"})
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
}

func newKindsBooksRepo() *kindsBooksRepo {
	return &kindsBooksRepo{
		authors:  map[int64]booksdom.Author{},
		titles:   map[int64]booksdom.Title{},
		editions: map[int64]booksdom.Edition{},
		nextID:   1,
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

func (r *kindsBooksRepo) SetEditionMonitored(_ context.Context, id int64, monitored bool) error {
	if e, ok := r.editions[id]; ok {
		e.Monitored = monitored
		r.editions[id] = e
	}
	return nil
}

// No-op lifecycle methods the services call on the add paths.
func (r *kindsBooksRepo) AddHistory(_ context.Context, _ booksdom.HistoryEntry) error {
	return nil
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
