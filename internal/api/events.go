package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

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

// eventsUpgrader upgrades the authenticated handshake to a WebSocket.
//
// CheckOrigin is permissive: mediarr is a single-user local app, the UI is
// served from the same host, and the browser already enforces same-origin for
// the fetch that mints the bearer token. A hostile cross-origin page cannot
// obtain a token without the user's credentials, so origin-checking adds
// nothing here.
var eventsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4 * 1024,
	WriteBufferSize: 4 * 1024,
	CheckOrigin:     func(*http.Request) bool { return true },
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
// may also be passed as ?token= (the single-user local app trade-off noted in
// eventsUpgrader; the token is a server-minted secret, not a credential).
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

	conn, err := eventsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already sent the appropriate error response.
	}
	defer func() { _ = conn.Close() }() // teardown: handler exits either way

	sub, err := s.eventsDeps.Events.Subscribe(r.Context(), liveEventTypes...)
	if err != nil {
		writeCloseError(conn, "failed to subscribe to events: "+err.Error())
		return
	}

	// Greet the client so it can confirm the socket is live before the first
	// real event.
	_ = conn.WriteJSON(wsEvent{Type: "hello", At: time.Now().UTC()})

	// Read pump: the client sends no data, so this just detects close /
	// protocol errors (and answers pings). One reader + one writer is the
	// pattern gorilla/websocket allows concurrently.
	readErr := make(chan error, 1)
	go func() {
		for {
			if _, _, rerr := conn.ReadMessage(); rerr != nil {
				readErr <- rerr
				return
			}
		}
	}()

	// Write pump: stream bus events to the socket until the bus channel
	// closes or the write fails (client gone).
	writeErr := make(chan error, 1)
	go func() {
		for ev := range sub {
			var payload map[string]any
			_ = decodeEventPayload(ev, &payload)
			if err := conn.WriteJSON(wsEvent{Type: ev.Type, Payload: payload, At: time.Now().UTC()}); err != nil {
				writeErr <- err
				return
			}
		}
		writeErr <- nil
	}()

	select {
	case <-r.Context().Done():
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

// writeCloseError sends a 4000-series close and closes the socket, for the
// rare post-upgrade failure (e.g. the bus is down).
func writeCloseError(conn *websocket.Conn, msg string) {
	_ = conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseInternalServerErr, msg),
		time.Now().Add(time.Second),
	)
	_ = conn.Close()
}
