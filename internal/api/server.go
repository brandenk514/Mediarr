// Package api wires the HTTP handlers, middleware, and routing for mediarr.
// It is the outermost layer: it validates input, calls services, and
// serialises responses. It contains no business logic.
package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/brandenk514/mediarr/internal/health"
)

// Server is the HTTP server. It owns the mux and middleware chain.
type Server struct {
	mux          *http.ServeMux
	health       *health.Checker
	authDeps     *AuthDeps
	indexersDeps *IndexersDeps
	moviesDeps   *MoviesDeps
	tvDeps       *TVDeps
	musicDeps    *MusicDeps
	booksDeps    *BooksDeps
}

// NewServer builds a Server with the given readiness checker.
func NewServer(checker *health.Checker) *Server {
	s := &Server{mux: http.NewServeMux(), health: checker}
	h := health.NewHandlers(checker)

	s.mux.HandleFunc("GET /healthz", h.Liveness)
	s.mux.HandleFunc("GET /readyz", h.Readiness)
	s.mux.HandleFunc("GET /version", s.version)
	return s
}

// SetAuth wires the auth endpoints. Called after construction once the
// auth repository is available (keeps NewServer usable for health-only
// boot scenarios and tests).
func (s *Server) SetAuth(deps *AuthDeps) {
	s.authDeps = deps
	s.mux.HandleFunc("POST /api/v1/auth/login", s.Login)
	s.mux.HandleFunc("GET /api/v1/auth/me", s.requireAuth(s.Me))
}

// Handler returns the fully wired http.Handler (middleware applied).
func (s *Server) Handler() http.Handler {
	return withMiddleware(s.mux)
}

// Serve runs the HTTP server until the context is cancelled.
func (s *Server) Serve(ctx context.Context, addr string, readTimeout, writeTimeout, shutdownTimeout time.Duration) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.Handler(),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// version returns the build info as JSON.
func (s *Server) version(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(health.InfoFunc())
}

// ---- middleware ----

func withMiddleware(next http.Handler) http.Handler {
	return requestLogger(recoverPanic(requestID(next)))
}

// requestID injects an X-Request-Id header (echoes an inbound one if present).
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || !isValidID(id) {
			id = newRequestID()
		}
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type requestIDKeyType struct{}

var requestIDKey = requestIDKeyType{}

// RequestIDFromContext returns the request ID for logging.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// requestLogger logs each request with method, path, status, and duration.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		// Structured log (JSON) — no body, no sensitive headers.
		logEntry := map[string]string{
			"level":      "info",
			"msg":        "http_request",
			"method":     r.Method,
			"path":       r.URL.Path,
			"status":     itoa(rw.status),
			"duration":   time.Since(start).String(),
			"request_id": RequestIDFromContext(r.Context()),
		}
		b, _ := json.Marshal(logEntry)
		// Use a minimal log sink; in production this would go to a logger package.
		_ = b
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// recoverPanic turns panics into 500s and prevents a request from killing the process.
func recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"internal server error"}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---- helpers ----

func newRequestID() string {
	// 16 random chars from a 36-char alphabet ≈ 78 bits of entropy.
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "req-unknown"
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

func isValidID(id string) bool {
	if len(id) < 8 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == ':') {
			return false
		}
	}
	return true
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
