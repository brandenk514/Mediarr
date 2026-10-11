package downloads

import (
	"errors"
	"fmt"
)

// ClientKind selects a concrete download client (the config-driven swap of
// #36/#37: same pipeline code, a different client by config).
type ClientKind string

const (
	// KindMock is the M1 mock (no external dependency; default).
	KindMock ClientKind = "mock"
	// KindQB is the real qBittorrent client (#36).
	KindQB ClientKind = "qbittorrent"
	// KindSAB is the real SABnzbd client (#37).
	KindSAB ClientKind = "sabnzbd"
)

// ClientOptions bundles everything NewClient needs to construct the selected
// client. It is deliberately plain (no config import) so the downloads package
// stays independent of the environment loader.
type ClientOptions struct {
	// Kind selects the implementation.
	Kind ClientKind
	// MockDir is the downloads directory (mock, and the default save/complete
	// directory for the real clients when they have none of their own).
	MockDir string
	// QB configures the qBittorrent client (used when Kind == KindQB).
	QB QBittorrentConfig
	// SAB configures the SABnzbd client (used when Kind == KindSAB).
	SAB SABnzbdConfig
}

// NewClient builds the download client selected by opts.Kind. An empty MockDir
// is an error for every kind: the pipeline must know where files land (or, for
// the real clients, what default save directory to fall back to). Unknown
// kinds are an error rather than a silent mock fallback — a misconfiguration
// must fail boot, not download into the wrong place.
func NewClient(opts ClientOptions) (Client, error) {
	if opts.MockDir == "" {
		return nil, errors.New("downloads: downloads directory is required")
	}
	switch opts.Kind {
	case "", KindMock:
		return NewMockClient("mock", opts.MockDir)
	case KindQB:
		if opts.QB.SavePath == "" {
			opts.QB.SavePath = opts.MockDir
		}
		c, err := NewQBittorrentClient(opts.QB)
		if err != nil {
			return nil, fmt.Errorf("qbittorrent client: %w", err)
		}
		return c, nil
	case KindSAB:
		if opts.SAB.CompleteDir == "" {
			opts.SAB.CompleteDir = opts.MockDir
		}
		c, err := NewSABnzbdClient(opts.SAB)
		if err != nil {
			return nil, fmt.Errorf("sabnzbd client: %w", err)
		}
		return c, nil
	default:
		return nil, fmt.Errorf("downloads: unknown client kind %q", opts.Kind)
	}
}
