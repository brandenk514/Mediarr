// Package downloads provides the download-client layer. In v1 it defines the
// narrow client interface the movie pipeline programs against plus a mock
// client for the M1 spike and end-to-end tests. Real adapters (qBittorrent,
// SABnzbd) arrive in M5.
package downloads

import (
	"context"
	"os"
	"path/filepath"
)

// Release is the thing a download client fetches.
type Release struct {
	// Title is the scene release name being downloaded.
	Title string
	// FileName is the name the client will produce on completion.
	FileName string
	// PathDir is where the client writes completed files (its "downloads" dir).
	// Empty means the client's default directory.
	PathDir string
}

// Status is the observed state of a download.
type Status struct {
	// Complete is true once the file exists at the expected location.
	Complete bool
	// Progress is 0-100.
	Progress int
	// File, when complete, is the absolute path to the downloaded file.
	File string
}

// Client is the narrow interface the movie pipeline needs to drive a download
// client: send a release, then poll until it completes. The consumer defines
// this interface; the mock and, later, real adapters implement it.
type Client interface {
	Name() string
	// Add sends a release to the client.
	Add(ctx context.Context, r Release) error
	// Status reports the state of a previously-added release. When complete it
	// returns the path to the downloaded file.
	Status(ctx context.Context, title string) (Status, error)
}

// MockClient simulates a download client. Add immediately "downloads" the
// release by writing a placeholder file, and Status then reports it complete
// with the real file path — giving the end-to-end pipeline a real file on disk
// to import, with no network I/O.
type MockClient struct {
	name  string
	base  string            // default downloads directory
	files map[string]string // release title -> absolute file path
}

// NewMockClient builds a MockClient that writes into dir (created if needed).
func NewMockClient(name, dir string) (*MockClient, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &MockClient{name: name, base: dir, files: map[string]string{}}, nil
}

// Name implements Client.
func (m *MockClient) Name() string { return m.name }

// Add implements Client. It materialises the file immediately and records it.
func (m *MockClient) Add(ctx context.Context, r Release) error {
	dir := r.PathDir
	if dir == "" {
		dir = m.base
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f := filepath.Join(dir, r.FileName)
	// A minimal placeholder payload; enough for the import step to move it.
	if err := os.WriteFile(f, []byte("mediarr-mock-download: "+r.Title+"\n"), 0o644); err != nil {
		return err
	}
	m.files[r.Title] = f
	return nil
}

// Status implements Client.
func (m *MockClient) Status(ctx context.Context, title string) (Status, error) {
	if f, ok := m.files[title]; ok {
		if st, err := os.Stat(f); err == nil && !st.IsDir() {
			return Status{Complete: true, Progress: 100, File: f}, nil
		}
	}
	return Status{Complete: false, Progress: 0}, nil
}
