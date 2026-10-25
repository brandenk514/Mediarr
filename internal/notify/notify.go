// Package notify is the Connect (notification) dispatch layer (P5 wave 2).
// It builds a Sink for one saved notification (webhook, Plex, Jellyfin, or
// Emby) and delivers events to it. Sinks are thin over an injected
// *http.Client so the whole layer is testable against httptest servers.
//
// Secret contract: a notification's token is stored vault-encrypted in the
// section document (masked on read, blank on write keeps it). BuildSink takes
// the ALREADY-OPENED token (the settings repo opens vault secrets on read) —
// this package never touches the vault, keeping it dependency-free.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Event is the notification payload. Type drives the sink's request shape
// ("test" is the Connect section's test button; real event types land when
// the event stream pipes into dispatch).
type Event struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// Sink delivers one event to one notification target.
type Sink interface {
	Deliver(ctx context.Context, ev Event) error
}

// SinkKind re-declares the notification vocabulary so this package does not
// import the settings domain (the API layer validates kinds against the
// section's enum; BuildSink accepts the same four strings).
type SinkKind string

const (
	KindWebhook  SinkKind = "webhook"
	KindPlex     SinkKind = "plex"
	KindJellyfin SinkKind = "jellyfin"
	KindEmby     SinkKind = "emby"
)

// SinkSpec is what BuildSink needs from a saved notification.
type SinkSpec struct {
	Kind       SinkKind
	URL        string
	SectionID  string
	Token      string
	UserAgent  string
	HTTPClient *http.Client
}

// BuildSink constructs the sink for the spec. A missing URL (or unknown
// kind) is an error the API maps to 400.
func BuildSink(spec SinkSpec) (Sink, error) {
	httpc := spec.HTTPClient
	if httpc == nil {
		httpc = &http.Client{Timeout: 10 * time.Second}
	}
	ua := spec.UserAgent
	if ua == "" {
		ua = "mediarr-notify/1.0"
	}
	switch spec.Kind {
	case KindWebhook:
		if spec.URL == "" {
			return nil, fmt.Errorf("webhook requires a url")
		}
		return &webhookSink{base: spec.URL, token: spec.Token, httpc: httpc, ua: ua}, nil
	case KindPlex:
		if spec.URL == "" {
			return nil, fmt.Errorf("plex requires a base url")
		}
		return &plexSink{base: spec.URL, sectionID: spec.SectionID, token: spec.Token, httpc: httpc, ua: ua}, nil
	case KindJellyfin:
		if spec.URL == "" {
			return nil, fmt.Errorf("jellyfin requires a base url")
		}
		return &jellyfinSink{base: spec.URL, token: spec.Token, httpc: httpc, ua: ua}, nil
	case KindEmby:
		if spec.URL == "" {
			return nil, fmt.Errorf("emby requires a base url")
		}
		return &embySink{base: spec.URL, token: spec.Token, httpc: httpc, ua: ua}, nil
	default:
		return nil, fmt.Errorf("unknown notification kind %q", spec.Kind)
	}
}

// postJSON is the shared transport: POST a JSON body to the sink URL with an
// optional auth header; non-2xx responses are an error carrying the status.
func postJSON(ctx context.Context, httpc *http.Client, ua, url, authHeader, authValue string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", ua)
	if authHeader != "" {
		req.Header.Set(authHeader, authValue)
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return fmt.Errorf("deliver: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("deliver: target returned %d", resp.StatusCode)
	}
	return nil
}

// webhookSink POSTs the raw event JSON to an arbitrary URL (the generic
// Connect target: ntfy, gotify, home-assistant webhooks, ...).
type webhookSink struct {
	base  string
	token string
	httpc *http.Client
	ua    string
}

func (s *webhookSink) Deliver(ctx context.Context, ev Event) error {
	hdr, val := "", ""
	if s.token != "" {
		hdr, val = "Authorization", "Bearer "+s.token
	}
	return postJSON(ctx, s.httpc, s.ua, s.base, hdr, val, ev)
}

// plexSink triggers a library refresh via the Plex DVR API
// (POST /library/sections/{id}/refresh when a section is pinned; the root
// refresh otherwise).
type plexSink struct {
	base      string
	sectionID string
	token     string
	httpc     *http.Client
	ua        string
}

func (s *plexSink) Deliver(ctx context.Context, ev Event) error {
	url := s.base
	if s.sectionID != "" {
		url = s.base + "/library/sections/" + s.sectionID + "/refresh"
	}
	return postJSON(ctx, s.httpc, s.ua, url, "X-Plex-Token", s.token, map[string]string{"event": ev.Type})
}

// jellyfinSink asks Jellyfin to refresh its library (POST /RefreshLibrary
// with the API-key header).
type jellyfinSink struct {
	base  string
	token string
	httpc *http.Client
	ua    string
}

func (s *jellyfinSink) Deliver(ctx context.Context, ev Event) error {
	return postJSON(ctx, s.httpc, s.ua, s.base+"/RefreshLibrary", "X-Emby-Token", s.token, map[string]string{"event": ev.Type})
}

// embySink is the Emby equivalent (same API family, different UA).
type embySink struct {
	base  string
	token string
	httpc *http.Client
	ua    string
}

func (s *embySink) Deliver(ctx context.Context, ev Event) error {
	return postJSON(ctx, s.httpc, s.ua, s.base+"/RefreshLibrary", "X-Emby-Token", s.token, map[string]string{"event": ev.Type})
}
