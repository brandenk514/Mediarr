package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/brandenk514/mediarr/internal/auth"
)

// AuthDeps bundles what the auth endpoints need.
type AuthDeps struct {
	Repo auth.UserStore
}

// authenticateUser verifies username/password and returns the user.
func (d *AuthDeps) authenticateUser(ctx context.Context, username, password string) (*auth.UserRow, error) {
	u, err := d.Repo.GetUserByUsername(ctx, username)
	if errors.Is(err, auth.ErrUserNotFound) {
		// Single-user v1 first boot: create the initial admin.
		return d.firstBootCreate(ctx, username, password)
	}
	if err != nil {
		return nil, err
	}
	if !u.Enabled {
		return nil, auth.ErrInvalidCredentials
	}
	if !auth.VerifyPassword(password, u.PasswordHash) {
		return nil, auth.ErrInvalidCredentials
	}
	_ = d.Repo.TouchLastLogin(ctx, u.ID)
	return u, nil
}

// firstBootCreate creates the initial admin user and returns it.
func (d *AuthDeps) firstBootCreate(ctx context.Context, username, password string) (*auth.UserRow, error) {
	n, err := d.Repo.CountUsers(ctx)
	if err != nil {
		return nil, err
	}
	if n != 0 {
		return nil, auth.ErrInvalidCredentials
	}
	if !validUsername(username) || len(password) < 10 {
		return nil, auth.ErrInvalidCredentials
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	if _, err := d.Repo.CreateUser(ctx, username, "", "admin", hash); err != nil {
		return nil, err
	}
	return d.Repo.GetUserByUsername(ctx, username)
}

func validUsername(s string) bool {
	if len(s) < 3 || len(s) > 32 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// ---- handlers ----

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token  string `json:"token"`
	User   string `json:"user"`
	Role   string `json:"role"`
	Prefix string `json:"token_prefix"`
}

// Login handles POST /api/v1/auth/login. Returns a raw API token (shown once).
func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	u, err := s.authDeps.authenticateUser(r.Context(), req.Username, req.Password)
	if err != nil {
		// Uniform error for all auth failures (no user-existence oracle).
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	tp, err := auth.NewToken("ma_")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	if _, err := s.authDeps.Repo.CreateToken(r.Context(), u.ID, "ui-session", tp, nil); err != nil {
		writeError(w, http.StatusInternalServerError, "token storage failed")
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{
		Token:  tp.Raw,
		User:   u.Username,
		Role:   u.Role,
		Prefix: tp.Prefix,
	})
}

// currentUser resolves the bearer token to a user, or writes an auth error.
func (s *Server) currentUser(w http.ResponseWriter, r *http.Request) (*auth.UserRow, bool) {
	raw := bearerToken(r)
	if raw == "" {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return nil, false
	}
	u, _, err := s.authDeps.Repo.TokenUser(r.Context(), raw)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return nil, false
	}
	return u, true
}

// requireAuth is middleware for API routes.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.currentUser(w, r); !ok {
			return
		}
		next(w, r)
	}
}

// Me handles GET /api/v1/auth/me.
func (s *Server) Me(w http.ResponseWriter, r *http.Request) {
	u, _ := s.currentUser(w, r)
	writeJSON(w, http.StatusOK, map[string]string{
		"username": u.Username,
		"role":     u.Role,
	})
}

// ---- helpers ----

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	return ""
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

var _ = context.Background
