package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
)

// fakeUserStore is an in-memory auth.UserStore for API tests.
type fakeUserStore struct {
	users    map[string]*auth.UserRow
	usersPW  map[string]string
	tokens   map[string]auth.TokenPair // raw -> pair
	tokenRaw map[string]int64          // raw -> userID
	errs     *fakeErrs
}

type fakeErrs struct {
	createUser error
	getUser    error
}

func newFakeStore() *fakeUserStore {
	return &fakeUserStore{
		users:    map[string]*auth.UserRow{},
		usersPW:  map[string]string{},
		tokens:   map[string]auth.TokenPair{},
		tokenRaw: map[string]int64{},
	}
}

func (f *fakeUserStore) CountUsers(ctx context.Context) (int, error) {
	return len(f.users), nil
}

func (f *fakeUserStore) CreateUser(ctx context.Context, username, email, role, passwordHash string) (int64, error) {
	if f.errs != nil && f.errs.createUser != nil {
		return 0, f.errs.createUser
	}
	if _, ok := f.users[username]; ok {
		return 0, auth.ErrUserExists
	}
	id := int64(len(f.users) + 1)
	f.users[username] = &auth.UserRow{
		ID: id, Username: username, Role: role, Enabled: true,
		PasswordHash: passwordHash,
	}
	f.usersPW[username] = passwordHash
	return id, nil
}

func (f *fakeUserStore) GetUserByUsername(ctx context.Context, username string) (*auth.UserRow, error) {
	if f.errs != nil && f.errs.getUser != nil {
		return nil, f.errs.getUser
	}
	u, ok := f.users[username]
	if !ok {
		return nil, auth.ErrUserNotFound
	}
	return u, nil
}

func (f *fakeUserStore) TouchLastLogin(ctx context.Context, userID int64) error {
	return nil
}

func (f *fakeUserStore) CreateToken(ctx context.Context, userID int64, name string, tp auth.TokenPair, expiresAt *time.Time) (int64, error) {
	f.tokens[tp.Raw] = tp
	f.tokenRaw[tp.Raw] = userID
	return int64(len(f.tokens)), nil
}

func (f *fakeUserStore) TokenUser(ctx context.Context, rawToken string) (*auth.UserRow, int64, error) {
	if _, ok := f.tokens[rawToken]; !ok {
		return nil, 0, auth.ErrInvalidToken
	}
	userID := f.tokenRaw[rawToken]
	for _, u := range f.users {
		if u.ID == userID {
			return u, userID, nil
		}
	}
	return nil, 0, auth.ErrInvalidToken
}

func newAuthTestServer(store auth.UserStore) *Server {
	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: store})
	return s
}

func postJSON(t *testing.T, h http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// seedUser pre-creates a user with a known password and returns the username.
func seedUser(t *testing.T, store *fakeUserStore, username, password string) string {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateUser(context.Background(), username, "", "admin", hash); err != nil {
		t.Fatal(err)
	}
	return username
}

func TestLogin_FirstBoot_CreatesUser(t *testing.T) {
	store := newFakeStore()
	s := newAuthTestServer(store)

	rec := postJSON(t, s.Handler(), "/api/v1/auth/login", loginRequest{
		Username: "admin", Password: "initial-strong-passw0rd",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var lr loginResponse
	if err := json.NewDecoder(rec.Body).Decode(&lr); err != nil {
		t.Fatal(err)
	}
	if lr.Token == "" {
		t.Error("expected a token")
	}
	if lr.User != "admin" {
		t.Errorf("user = %q, want admin", lr.User)
	}
	if _, ok := store.users["admin"]; !ok {
		t.Error("user should have been created on first boot")
	}
}

func TestLogin_FirstBoot_RejectsWeakPassword(t *testing.T) {
	store := newFakeStore()
	s := newAuthTestServer(store)

	rec := postJSON(t, s.Handler(), "/api/v1/auth/login", loginRequest{
		Username: "admin", Password: "short",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for weak first-boot password", rec.Code)
	}
	if len(store.users) != 0 {
		t.Error("no user should be created for a weak password")
	}
}

func TestLogin_FirstBoot_RejectsBadUsername(t *testing.T) {
	store := newFakeStore()
	s := newAuthTestServer(store)

	rec := postJSON(t, s.Handler(), "/api/v1/auth/login", loginRequest{
		Username: "a b!", Password: "initial-strong-passw0rd",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for bad username", rec.Code)
	}
}

func TestLogin_ExistingUser_CorrectPassword(t *testing.T) {
	store := newFakeStore()
	seedUser(t, store, "admin", "known-pass-12345")
	s := newAuthTestServer(store)

	rec := postJSON(t, s.Handler(), "/api/v1/auth/login", loginRequest{
		Username: "admin", Password: "known-pass-12345",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLogin_ExistingUser_WrongPassword(t *testing.T) {
	store := newFakeStore()
	seedUser(t, store, "admin", "known-pass-12345")
	s := newAuthTestServer(store)

	rec := postJSON(t, s.Handler(), "/api/v1/auth/login", loginRequest{
		Username: "admin", Password: "wrong-pass-99999",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLogin_UnknownUser_401(t *testing.T) {
	store := newFakeStore()
	seedUser(t, store, "admin", "known-pass-12345") // ensure a user exists
	s := newAuthTestServer(store)

	rec := postJSON(t, s.Handler(), "/api/v1/auth/login", loginRequest{
		Username: "ghost", Password: "whatever-12345",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLogin_MissingFields_400(t *testing.T) {
	store := newFakeStore()
	s := newAuthTestServer(store)

	rec := postJSON(t, s.Handler(), "/api/v1/auth/login", loginRequest{Username: "admin"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMe_ValidToken(t *testing.T) {
	store := newFakeStore()
	seedUser(t, store, "admin", "known-pass-12345")
	s := newAuthTestServer(store)

	// Log in to get a token.
	rec := postJSON(t, s.Handler(), "/api/v1/auth/login", loginRequest{
		Username: "admin", Password: "known-pass-12345",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	var lr loginResponse
	_ = json.NewDecoder(rec.Body).Decode(&lr)

	// Use the token on /me.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+lr.Token)
	rec2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("/me status = %d, want 200", rec2.Code)
	}
	var me map[string]string
	_ = json.NewDecoder(rec2.Body).Decode(&me)
	if me["username"] != "admin" {
		t.Errorf("me.username = %q, want admin", me["username"])
	}
}

func TestMe_MissingToken_401(t *testing.T) {
	store := newFakeStore()
	s := newAuthTestServer(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMe_GarbageToken_401(t *testing.T) {
	store := newFakeStore()
	s := newAuthTestServer(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLogin_BearerToken_Format(t *testing.T) {
	store := newFakeStore()
	s := newAuthTestServer(store)

	rec := postJSON(t, s.Handler(), "/api/v1/auth/login", loginRequest{
		Username: "admin", Password: "initial-strong-passw0rd",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var lr loginResponse
	_ = json.NewDecoder(rec.Body).Decode(&lr)
	if !auth.ValidateTokenFormat(lr.Token) {
		t.Errorf("issued token %q should pass ValidateTokenFormat", lr.Token)
	}
}
