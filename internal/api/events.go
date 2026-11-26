package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/brandenk514/mediarr/internal/auth"
	"github.com/brandenk514/mediarr/internal/store"
)

// EventsDeps bundles what the WebSocket bridge (#40: "WebSocket subscription
// for live queue/history updates") needs: the auth store (to authenticate the
// handshake) and the pub/sub bus (to subscribe and fan out live events).
type EventsDeps struct {
	Repo   auth.UserStore
	Events store.Events
}

// SetEvents wires GET /api/v1/events (#40). Called after construction,
// mirroring the other Set* hooks, so NewServer stays usable for health-only
// boot and tests.
func (s *Server) SetEvents(deps *EventsDeps) {
	s.eventsDeps = deps
	s.mux.HandleFunc("GET /api/v1/events", s.Events)
}

// wsEvent is one on-the-wire frame. The first frame is a "hello" handshake
// frame (payload null) so the client knows the socket is live before the first
// real event arrives.
type wsEvent struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload,omitempty"`
	At      time.Time      `json:"at"`
}

// liveEventTypes are the bus topics the dashboard cares about. A queue.state
// change or a history append is exactly what re-draws the queue and history
// cards.
var liveEventTypes = []string{"queue.updated", "history.appended"}

// Events handles GET /api/v1/events: a live WebSocket push of queue and
// history events. The handshake is authenticated with the same bearer token as
// the REST API (no token / invalid token → 401 before upgrade). Browsers
// cannot set the Authorization header on a WebSocket handshake, so the token
// may also be passed as ?token= (the token is a server-minted secret, not a
// credential).
// Once connected, the server subscribes to the bus and streams every matching
// event as a JSON frame. The connection is torn down when the client closes it
// or the request context is cancelled.
func (s *Server) Events(w http.ResponseWriter, r *http.Request) {
	raw := bearerToken(r)
	if raw == "" {
		raw = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if raw == "" {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}
	if _, _, err := s.eventsDeps.Repo.TokenUser(r.Context(), raw); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	ctx := r.Context()
	// InsecureSkipVerify (no origin check): mediarr is a single-user local
	// app, the UI is served from the same host, and the browser already
	// enforces same-origin for the fetch that mints the bearer token. A
	// hostile cross-origin page cannot obtain a token without the user's
	// credentials, so origin-checking adds nothing here.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return // Accept already sent the appropriate error response.
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }() // teardown: handler exits either way

	sub, err := s.eventsDeps.Events.Subscribe(ctx, liveEventTypes...)
	if err != nil {
		writeCloseError(ctx, conn, "failed to subscribe to events: "+err.Error())
		return
	}

	// Greet the client so it can confirm the socket is live before the first
	// real event.
	_ = writeEvent(ctx, conn, wsEvent{Type: "hello", At: time.Now().UTC()})

	// Read pump: the client sends no data, so this just detects close /
	// protocol errors; coder/websocket answers pings automatically and a
	// data message would trip its StatusPolicyViolation close. One reader +
	// one writer is the pattern the library allows.
	readErr := make(chan error, 1)
	go func() {
		// CloseRead returns a context cancelled when the peer closes.
		<-conn.CloseRead(ctx).Done()
		readErr <- nil
	}()

	// Write pump: stream bus events to the socket until the bus channel
	// closes or the write fails (client gone).
	writeErr := make(chan error, 1)
	go func() {
		for ev := range sub {
			var payload map[string]any
			_ = decodeEventPayload(ev, &payload)
			if err := writeEvent(ctx, conn, wsEvent{Type: ev.Type, Payload: payload, At: time.Now().UTC()}); err != nil {
				writeErr <- err
				return
			}
		}
		writeErr <- nil
	}()

	select {
	case <-ctx.Done():
	case rerr := <-readErr:
		_ = rerr // connection teardown: no further handling needed
	case werr := <-writeErr:
		_ = werr
	}
}

// decodeEventPayload turns a bus event's raw JSON payload into a map for the
// wire frame. A malformed payload degrades to a nil payload (the event type is
// still delivered) rather than dropping the frame.
func decodeEventPayload(ev store.Event, out *map[string]any) error {
	if len(ev.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(ev.Payload, out)
}

// writeEvent marshals a wire frame and writes it to the socket as a text
// message.
func writeEvent(ctx context.Context, conn *websocket.Conn, ev wsEvent) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, b)
}

// writeCloseError sends a 1011 (internal error) close for the rare post-
// upgrade failure (e.g. the bus is down). coder/websocket's Close sends the
// close frame and waits for the peer's, then the deferred normal-closure
// close in Events sees an already-closed connection and its ignored error
// keeps teardown exactly-once.
func writeCloseError(ctx context.Context, conn *websocket.Conn, msg string) {
	_ = conn.Close(websocket.StatusInternalError, msg)
}
