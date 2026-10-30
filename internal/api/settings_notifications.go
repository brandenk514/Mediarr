package api

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/brandenk514/mediarr/internal/notify"
)

// NotificationsDeps bundles the saved notification-sink store (Prowlarr PR4
// Notifications section). Repo is the vault-aware sink repository
// (postgres.NotificationsRepo implements notify.SinkRepo); it is the only
// layer that sees token ciphertext. HTTPClient overrides the delivery
// client for the test endpoint (nil = notify.BuildSink's 10s default).
type NotificationsDeps struct {
	Repo       notify.SinkRepo
	HTTPClient *http.Client
}

// SetNotifications registers the notification-sink endpoints. Call after
// NewServer; nil deps make the endpoints 501.
func (s *Server) SetNotifications(deps *NotificationsDeps) {
	s.notificationsDeps = deps
	s.register("GET /api/v1/settings/notifications", s.requireAuth(s.ListNotifications))
	s.register("POST /api/v1/settings/notifications", s.requireAuth(s.CreateNotification))
	s.register("PUT /api/v1/settings/notifications/{id}", s.requireAuth(s.UpdateNotification))
	s.register("DELETE /api/v1/settings/notifications/{id}", s.requireAuth(s.DeleteNotification))
	s.register("POST /api/v1/settings/notifications/{id}/test", s.requireAuth(s.TestNotification))
}

// notificationSinkJSON is the wire shape of a saved sink: token is always
// empty (masked) and token_set reports its presence.
type notificationSinkJSON struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Enabled   bool      `json:"enabled"`
	URL       string    `json:"url"`
	SectionID string    `json:"section_id"`
	Token     string    `json:"token"`
	TokenSet  bool      `json:"token_set"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
}

func sinkToJSON(rec notify.SinkRecord) notificationSinkJSON {
	return notificationSinkJSON{
		ID:        rec.ID,
		Name:      rec.Name,
		Kind:      string(rec.Kind),
		Enabled:   rec.Enabled,
		URL:       rec.URL,
		SectionID: rec.SectionID,
		Token:     "",
		TokenSet:  rec.Token != "",
		UserAgent: rec.UserAgent,
		CreatedAt: rec.CreatedAt,
	}
}

// notificationSinkCreate is the body for POST /api/v1/settings/notifications.
// Enabled is nil = true (enabled). Token is plaintext in the request but
// never echoed back.
type notificationSinkCreate struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Enabled   *bool  `json:"enabled"`
	URL       string `json:"url"`
	SectionID string `json:"section_id"`
	Token     string `json:"token"`
	UserAgent string `json:"user_agent"`
}

// notificationSinkUpdate is the body for PUT /api/v1/settings/notifications/{id}.
// An empty token keeps the stored token (masked-edit round-trip); an
// omitted enabled keeps the stored value.
type notificationSinkUpdate struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Enabled   *bool  `json:"enabled"`
	URL       string `json:"url"`
	SectionID string `json:"section_id"`
	Token     string `json:"token"`
	UserAgent string `json:"user_agent"`
}

// validateSinkConstruction runs the sink through notify.BuildSink so a 400
// carries the exact sink-construction message ("webhook requires a url",
// `unknown notification kind "x"`, ...).
func validateSinkConstruction(kind, url string) error {
	_, err := notify.BuildSink(notify.SinkSpec{Kind: notify.SinkKind(kind), URL: url})
	return err
}

// ListNotifications returns every saved sink in creation order, masked.
func (s *Server) ListNotifications(w http.ResponseWriter, r *http.Request) {
	if s.notificationsDeps == nil {
		writeError(w, http.StatusNotImplemented, "notification sinks are not available")
		return
	}
	recs, err := s.notificationsDeps.Repo.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list notification sinks")
		return
	}
	out := make([]notificationSinkJSON, 0, len(recs))
	for _, rec := range recs {
		out = append(out, sinkToJSON(rec))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateNotification stores a new sink and returns it masked (201). An
// unknown kind or missing url is a 400 carrying the sink-construction
// message.
func (s *Server) CreateNotification(w http.ResponseWriter, r *http.Request) {
	if s.notificationsDeps == nil {
		writeError(w, http.StatusNotImplemented, "notification sinks are not available")
		return
	}
	var in notificationSinkCreate
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := validateSinkConstruction(in.Kind, in.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	rec := notify.SinkRecord{
		Name:      in.Name,
		Kind:      notify.SinkKind(in.Kind),
		Enabled:   enabled,
		URL:       in.URL,
		SectionID: in.SectionID,
		Token:     in.Token,
		UserAgent: in.UserAgent,
	}
	id, createdAt, err := s.notificationsDeps.Repo.Create(r.Context(), rec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create notification sink")
		return
	}
	rec.ID = id
	rec.CreatedAt = createdAt
	writeJSON(w, http.StatusCreated, sinkToJSON(rec))
}

// UpdateNotification fully replaces a sink's fields (200, masked). An empty
// token keeps the stored token; an unknown id is a 404.
func (s *Server) UpdateNotification(w http.ResponseWriter, r *http.Request) {
	if s.notificationsDeps == nil {
		writeError(w, http.StatusNotImplemented, "notification sinks are not available")
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in notificationSinkUpdate
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := validateSinkConstruction(in.Kind, in.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	existing, err := s.notificationsDeps.Repo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "unknown notification sink")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read notification sink")
		return
	}
	token := in.Token
	if token == "" {
		token = existing.Token // masked-edit round-trip: keep the stored secret
	}
	enabled := existing.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	rec := notify.SinkRecord{
		ID:        id,
		Name:      in.Name,
		Kind:      notify.SinkKind(in.Kind),
		Enabled:   enabled,
		URL:       in.URL,
		SectionID: in.SectionID,
		Token:     token,
		UserAgent: in.UserAgent,
		CreatedAt: existing.CreatedAt,
	}
	if err := s.notificationsDeps.Repo.Update(r.Context(), rec); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "unknown notification sink")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update notification sink")
		return
	}
	writeJSON(w, http.StatusOK, sinkToJSON(rec))
}

// DeleteNotification removes a sink (204). An unknown id is a 404.
func (s *Server) DeleteNotification(w http.ResponseWriter, r *http.Request) {
	if s.notificationsDeps == nil {
		writeError(w, http.StatusNotImplemented, "notification sinks are not available")
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.notificationsDeps.Repo.Get(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "unknown notification sink")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read notification sink")
		return
	}
	if err := s.notificationsDeps.Repo.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete notification sink")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// TestNotification builds the sink from the stored (vault-decrypted) config
// and delivers a test event to it. A config error is a 400; a delivery
// failure (unreachable or non-2xx target) is a 502; success is 200 with
// {ok: true}. An unknown id is a 404.
func (s *Server) TestNotification(w http.ResponseWriter, r *http.Request) {
	if s.notificationsDeps == nil {
		writeError(w, http.StatusNotImplemented, "notification sinks are not available")
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	rec, err := s.notificationsDeps.Repo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "unknown notification sink")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read notification sink")
		return
	}
	sink, err := notify.BuildSink(notify.SinkSpec{
		Kind:       rec.Kind,
		URL:        rec.URL,
		SectionID:  rec.SectionID,
		Token:      rec.Token,
		UserAgent:  rec.UserAgent,
		HTTPClient: s.notificationsDeps.HTTPClient,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := sink.Deliver(r.Context(), notify.Event{Type: "test"}); err != nil {
		writeError(w, http.StatusBadGateway, "failed to deliver test notification: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
