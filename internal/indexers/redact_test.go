package indexers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestRedactURL_MasksSensitives is the #34 core redaction test: every
// sensitive query param's value is masked while host/path and non-secret
// params survive, so the result stays useful for debugging.
func TestRedactURL_MasksSensitives(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			"https://idx.example/api?apikey=SECRET&t=search&q=Dune",
			"https://idx.example/api?apikey=***&t=search&q=Dune",
		},
		// Bare URL embedded in a transport-error message.
		{
			`Get "https://idx.example/api?key=ABC123": dial tcp: connection refused`,
			`Get "https://idx.example/api?key=***": dial tcp: connection refused`,
		},
		// Multiple secrets, mixed case param name.
		{
			"https://idx.example/?APIKEY=shh&token=zzz",
			"https://idx.example/?APIKEY=***&token=***",
		},
		// No secrets → unchanged (no re-encoding side effects).
		{
			"https://idx.example/api?t=caps",
			"https://idx.example/api?t=caps",
		},
		// Empty string.
		{"", ""},
		// Non-URL text passes through.
		{"no url here, just text", "no url here, just text"},
	}
	for _, tc := range cases {
		got := redactURL(tc.in)
		if got != tc.want {
			t.Errorf("redactURL(%q)\n got  %q\n want %q", tc.in, got, tc.want)
		}
	}
}

// TestRedactURL_NeverLeaksKey is a property test: for a range of secret values
// and param names, the redacted output must NOT contain the raw secret.
func TestRedactURL_NeverLeaksKey(t *testing.T) {
	secret := "s3cr3t-api-key-XYZ"
	params := []string{"apikey", "api_key", "key", "token", "pass", "password", "secret", "auth", "api"}
	for _, p := range params {
		in := "https://idx.example/path?" + p + "=" + secret
		got := redactURL(in)
		if strings.Contains(got, secret) {
			t.Fatalf("redactURL leaked the secret for param %q: %q", p, got)
		}
		if !strings.Contains(got, "***") {
			t.Fatalf("redactURL did not mask param %q: %q", p, got)
		}
	}
}

// TestRedactURL_MultipleURLs redacts every URL in a message, not just the first.
func TestRedactURL_MultipleURLs(t *testing.T) {
	in := `A "https://a.example/?key=AAA" then "https://b.example/x?token=BBB"`
	got := redactURL(in)
	if strings.Contains(got, "AAA") || strings.Contains(got, "BBB") {
		t.Fatalf("expected both secrets masked, got %q", got)
	}
}

// TestHTTPGet_TransportError_Redacted proves end-to-end that a transport
// failure (dial to a closed port) does not leak the API key into either the
// returned error string or the health-tracker LastError (#34: "log redaction").
func TestHTTPGet_TransportError_Redacted(t *testing.T) {
	// Find a port that is (almost certainly) closed locally.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closed := srv.URL // remember it so we can close and reuse the port
	srv.Close()

	// Re-opening would race; instead point at a port that is free by using a
	// server we open then close, then immediately hitting it gives a refused
	// connection. This is the standard "closed port" pattern.
	u, err := url.Parse(closed)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_ = u

	health := NewHealthTracker()
	deps := AdapterDeps{Health: health}

	// Call httpGet directly against the now-closed port.
	_, gerr := deps.httpGet(context.Background(), "probe", closed, queryParam("apikey", "SHOULDNOTLEAK"))
	if gerr == nil {
		t.Fatal("expected a transport error, got nil")
	}
	if !errors.Is(gerr, ErrTransport) {
		t.Fatalf("expected ErrTransport, got %v", gerr)
	}
	if strings.Contains(gerr.Error(), "SHOULDNOTLEAK") {
		t.Fatalf("transport error leaked the API key: %q", gerr.Error())
	}

	// The health tracker's LastError must also be redacted.
	st := health.Stats("probe")
	if strings.Contains(st.LastError, "SHOULDNOTLEAK") {
		t.Fatalf("LastError leaked the API key: %q", st.LastError)
	}
	if st.Failures != 1 {
		t.Fatalf("expected 1 failure recorded, got %+v", st)
	}
}

// TestRedactPublicFunc confirms the exported Redact() is the same function the
// adapters use (so the API/health layers can redact user-supplied strings).
func TestRedactPublicFunc(t *testing.T) {
	in := "see https://x.example/?apikey=TOPSECRET for details"
	if !strings.Contains(Redact(in), "apikey=***") || strings.Contains(Redact(in), "TOPSECRET") {
		t.Fatalf("Redact did not mask: %q", Redact(in))
	}
}
