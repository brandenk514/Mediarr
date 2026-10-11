package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/health"
	"github.com/brandenk514/mediarr/internal/store"
)

// fakeBus is an in-memory store.Events for the WebSocket bridge test: Publish
// fans an event out to every subscriber of its type; Subscribe returns a
// buffered channel that never closes (the test is short-lived and tears down
// by dropping the connection, not the bus).
type fakeBus struct {
	mu   sync.Mutex
	subs map[string][]chan store.Event
}

func newFakeBus() *fakeBus { return &fakeBus{subs: map[string][]chan store.Event{}} }

func (b *fakeBus) Publish(ctx context.Context, ev store.Event) error {
	b.mu.Lock()
	for _, ch := range b.subs[ev.Type] {
		select {
		case ch <- ev:
		default: // drop if full; the test only publishes after the sub is ready.
		}
	}
	b.mu.Unlock()
	return nil
}

func (b *fakeBus) Subscribe(ctx context.Context, types ...string) (<-chan store.Event, error) {
	out := make(chan store.Event, 16)
	b.mu.Lock()
	for _, t := range types {
		b.subs[t] = append(b.subs[t], out)
	}
	b.mu.Unlock()
	return out, nil
}

func (b *fakeBus) Ping(ctx context.Context) error { return nil }

var _ store.Events = (*fakeBus)(nil)

// newEventsTestServer builds an authenticated server with the WebSocket bridge
// wired to a fake user store (one live token) and an in-memory event bus.
func newEventsTestServer(t *testing.T) (*Server, string, *fakeBus) {
	t.Helper()

	bus := newFakeBus()
	userStore := newFakeStore()
	u, _ := userStore.CreateUser(context.Background(), "admin", "", "admin", "hash")
	pair, err := auth.NewToken("ma_live_")
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	_, _ = userStore.CreateToken(context.Background(), u, "test", pair, nil)

	s := NewServer(health.NewChecker(nil, nil))
	s.SetAuth(&AuthDeps{Repo: userStore})
	s.SetEvents(&EventsDeps{Repo: userStore, Events: bus})
	return s, pair.Raw, bus
}

// wsDial connects a gorilla client to the events endpoint with the given
// bearer token ("" = no Authorization header).
func wsDial(t *testing.T, url, token string) *websocket.Conn {
	t.Helper()
	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, _, err := dialer.Dial(toWS(url), header)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	return conn
}

// toWS converts an http(s) test URL to a ws(s) URL for the gorilla dialer.
func toWS(u string) string {
	if strings.HasPrefix(u, "https://") {
		return "wss://" + strings.TrimPrefix(u, "https://")
	}
	return "ws://" + strings.TrimPrefix(u, "http://")
}

// readFrame reads one JSON frame from the socket, bounded by a timeout so a
// regression that never writes can't hang the test.
func readFrame(t *testing.T, conn *websocket.Conn) wsEvent {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var ev wsEvent
	if err := conn.ReadJSON(&ev); err != nil {
		t.Fatalf("read frame: %v", err)
	}
	return ev
}

// TestEvents_RequiresAuth is the #40 "auth required" contract for the live
// channel: a handshake with no token or a bad token is rejected before the
// socket is opened. The token may arrive as a ?token= query param (browsers
// can't set the Authorization header on a WebSocket handshake).
func TestEvents_RequiresAuth(t *testing.T) {
	s, token, _ := newEventsTestServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	for _, tc := range []struct {
		name  string
		token string // header bearer token; "" = no Authorization header
		query string // ?token= value
	}{
		{"no token", "", ""},
		{"bad token", "ma_invalid_token_value", ""},
		{"bad query token", "", "ma_invalid_token_value"},
	} {
		dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
		hdr := http.Header{}
		if tc.token != "" {
			hdr.Set("Authorization", "Bearer "+tc.token)
		}
		url := toWS(srv.URL + "/api/v1/events")
		if tc.query != "" {
			url += "?token=" + tc.query
		}
		_, resp, err := dialer.Dial(url, hdr)
		if err == nil {
			t.Fatalf("%s: expected handshake to fail, but it succeeded", tc.name)
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			code := 0
			if resp != nil {
				code = resp.StatusCode
			}
			t.Fatalf("%s: got status %d, want 401 (err %v)", tc.name, code, err)
		}
	}

	// A valid token in the query string upgrades cleanly (browser path).
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, resp, err := dialer.Dial(toWS(srv.URL+"/api/v1/events?token="+token), http.Header{})
	if err != nil {
		t.Fatalf("query-token dial failed: %v (resp %v)", err, resp)
	}
	defer conn.Close()
	hello := readFrame(t, conn)
	if hello.Type != "hello" {
		t.Fatalf("first frame = %q, want hello", hello.Type)
	}
}

// TestEvents_StreamIs the #40 "WebSocket subscription for live queue/history
// updates" contract: after an authenticated handshake the client gets a hello
// frame, then one frame per published queue.updated / history.appended event,
// with the payload surfaced as JSON.
func TestEvents_Stream(t *testing.T) {
	s, token, bus := newEventsTestServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	conn := wsDial(t, srv.URL+"/api/v1/events", token)
	defer conn.Close()

	// 1. hello frame.
	hello := readFrame(t, conn)
	if hello.Type != "hello" {
		t.Fatalf("first frame = %q, want hello", hello.Type)
	}

	// 2. queue.updated arrives with its payload.
	qe := map[string]any{
		"movie_id":      float64(1),
		"release_title": "Inception.2010.1080p.WEB.x264",
		"indexer":       "nyaa",
		"state":         "downloading",
		"progress":      float64(42),
	}
	payload, _ := json.Marshal(qe)
	if err := bus.Publish(context.Background(), store.Event{Type: "queue.updated", Payload: payload}); err != nil {
		t.Fatalf("publish queue.updated: %v", err)
	}
	ev := readFrame(t, conn)
	if ev.Type != "queue.updated" {
		t.Fatalf("second frame type = %q, want queue.updated", ev.Type)
	}
	if ev.Payload["state"] != "downloading" || ev.Payload["indexer"] != "nyaa" {
		t.Errorf("queue.updated payload = %v", ev.Payload)
	}

	// 3. history.appended arrives.
	if err := bus.Publish(context.Background(), store.Event{
		Type:    "history.appended",
		Payload: []byte(`{"movie_id":1,"event":"imported","detail":"to /media/movies/Inception (2010)/Inception.mkv"}`),
	}); err != nil {
		t.Fatalf("publish history.appended: %v", err)
	}
	ev = readFrame(t, conn)
	if ev.Type != "history.appended" {
		t.Fatalf("third frame type = %q, want history.appended", ev.Type)
	}
	if ev.Payload["event"] != "imported" {
		t.Errorf("history.appended payload = %v", ev.Payload)
	}
}
