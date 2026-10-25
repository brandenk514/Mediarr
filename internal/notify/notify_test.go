package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildSink_UnknownKind(t *testing.T) {
	if _, err := BuildSink(SinkSpec{Kind: SinkKind("carrier-pigeon")}); err == nil {
		t.Fatal("unknown kind must be an error")
	}
}

func TestBuildSink_WebhookRequiresURL(t *testing.T) {
	if _, err := BuildSink(SinkSpec{Kind: KindWebhook}); err == nil {
		t.Fatal("webhook without url must be an error")
	}
}

func TestWebhookSink_DeliversWithToken(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	sink, err := BuildSink(SinkSpec{Kind: KindWebhook, URL: srv.URL, Token: "tok", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Deliver(context.Background(), Event{Type: "test", Title: "hi"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if !strings.Contains(gotBody, `"type":"test"`) || !strings.Contains(gotBody, `"title":"hi"`) {
		t.Fatalf("body = %q", gotBody)
	}
}

func TestWebhookSink_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	sink, err := BuildSink(SinkSpec{Kind: KindWebhook, URL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Deliver(context.Background(), Event{Type: "test"}); err == nil {
		t.Fatal("non-2xx must be an error")
	}
}

func TestPlexSink_UsesTokenHeaderAndSectionPath(t *testing.T) {
	var gotPath, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Plex-Token")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	sink, err := BuildSink(SinkSpec{
		Kind: KindPlex, URL: strings.TrimSuffix(srv.URL, "/"), SectionID: "42",
		Token: "plex-tok", HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Deliver(context.Background(), Event{Type: "test"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if gotPath != "/library/sections/42/refresh" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotToken != "plex-tok" {
		t.Fatalf("token = %q", gotToken)
	}
}

func TestJellyfinSink(t *testing.T) {
	var gotPath, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Emby-Token")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	sink, err := BuildSink(SinkSpec{Kind: KindJellyfin, URL: srv.URL, Token: "jf-tok", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Deliver(context.Background(), Event{Type: "test"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if gotPath != "/RefreshLibrary" || gotToken != "jf-tok" {
		t.Fatalf("path=%q token=%q", gotPath, gotToken)
	}
}

func TestEmbySink(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	sink, err := BuildSink(SinkSpec{Kind: KindEmby, URL: srv.URL, Token: "e", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Deliver(context.Background(), Event{Type: "test"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if gotPath != "/RefreshLibrary" {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestSink_TransportError(t *testing.T) {
	// A client pointed at a dead port must surface a transport error, not
	// a panic or a silent success.
	sink, err := BuildSink(SinkSpec{Kind: KindWebhook, URL: "http://127.0.0.1:1/hook"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Deliver(context.Background(), Event{Type: "test"}); err == nil {
		t.Fatal("dead endpoint must be an error")
	}
}
