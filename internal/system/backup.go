package system

import (
	"context"
	"encoding/json"
	"time"

	"github.com/brandenk514/mediarr/internal/downloadclients"
	"github.com/brandenk514/mediarr/internal/settings"
)

// ---- P6 System > Backup: config export/import (self-contained JSON document) ----
//
// The backup document is the app's configuration as a self-contained JSON
// blob: the download-client config (masked, per PLAN §8 — secrets are never
// carried, only presence), the four per-library overrides, and every generic
// settings section's raw stored document. Optional queue / series snapshots
// can be attached when requested. Import writes the config back and is
// idempotent (re-importing the same document converges to the same state).
//
// The service is pure: it programs against the settings Store seam (the same
// interface the Postgres settings adapter implements) plus injectable
// snapshot/import providers, so the export/import round-trip is unit-testable
// without a live database.

// BackupDownloadClient is a saved download client in a backup document (the
// OpenAPI BackupDownloadClient). Masked: the API key is a presence flag only
// and is never carried in the document — a restore recreates each client with
// an empty key, which the user re-enters (and the test endpoint re-validates)
// after import, like the legacy backup's behavior for the single client.
type BackupDownloadClient struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Enabled     bool   `json:"enabled"`
	Position    int    `json:"position"`
	BaseURL     string `json:"base_url"`
	Username    string `json:"username"`
	APIKeySet   bool   `json:"api_key_set"`
	CompleteDir string `json:"complete_dir"`
}

// BackupLibraryOverride mirrors settings.LibraryOverride in the backup wire
// shape (kept local so the backup document is self-describing).
type BackupLibraryOverride struct {
	Path           string `json:"path"`
	QualityProfile string `json:"quality_profile"`
	ChecksumVerify *bool  `json:"checksum_verify,omitempty"`
}

// BackupSection is one settings section document in a backup (the raw section
// JSON, encoded as a JSON string).
type BackupSection struct {
	Section  string `json:"section"`
	Document string `json:"document"`
}

// BackupSettings is the settings block of a backup document.
type BackupSettings struct {
	DownloadClients []BackupDownloadClient           `json:"download_clients"`
	Libraries       map[string]BackupLibraryOverride `json:"libraries"`
	Sections        []BackupSection                  `json:"sections"`
}

// BackupDocument is a self-contained configuration backup (OpenAPI
// BackupDocument). Queue / Series are optional snapshots, present only when
// requested at export.
type BackupDocument struct {
	Version    string          `json:"version"`
	ExportedAt time.Time       `json:"exported_at"`
	Settings   BackupSettings  `json:"settings"`
	Queue      json.RawMessage `json:"queue,omitempty"`
	Series     json.RawMessage `json:"series,omitempty"`
}

// BackupImportResult is the report for a backup import (OpenAPI
// BackupImportResult).
type BackupImportResult struct {
	Imported         bool   `json:"imported"`
	SettingsImported int    `json:"settings_imported"`
	QueueImported    int    `json:"queue_imported"`
	SeriesImported   int    `json:"series_imported"`
	Message          string `json:"message,omitempty"`
}

// BackupStore is the persistence surface the backup service programs against.
// It is the subset of settings.Store the backup needs, so a settings.Store
// (the Postgres adapter) satisfies it directly. Download clients live in
// their own table (Prowlarr PR5) and are reached through the BackupService's
// DCRepo field instead.
type BackupStore interface {
	GetLibrary(ctx context.Context, k settings.LibraryKind) (*settings.LibraryOverride, error)
	PutLibrary(ctx context.Context, k settings.LibraryKind, o *settings.LibraryOverride) error
	GetSectionDocument(ctx context.Context, s settings.Section) ([]byte, error)
	PutSectionDocument(ctx context.Context, s settings.Section, doc []byte) error
}

// BackupService exports and imports the configuration backup document.
type BackupService struct {
	Store   BackupStore
	Version string

	// DCRepo is the saved download-client store (Prowlarr PR5). Export masks
	// each client into BackupDownloadClient; import replaces the stored list
	// (keys are never carried, so imported clients come back with no key). A
	// nil DCRepo exports an empty list and import skips the clients.
	DCRepo downloadclients.Repo

	// Optional snapshot providers for the queue / series attachments. A nil
	// provider means "not available" — the export omits that snapshot.
	QueueSnap  func(ctx context.Context) (json.RawMessage, error)
	SeriesSnap func(ctx context.Context) (json.RawMessage, error)

	// Optional import sinks for the queue / series snapshots. A nil sink means
	// the snapshot is carried by the document but not written (counted as 0).
	QueueImport  func(ctx context.Context, raw json.RawMessage) (int, error)
	SeriesImport func(ctx context.Context, raw json.RawMessage) (int, error)

	// Now is the export timestamp source (injectable for tests).
	Now func() time.Time
}

func (s *BackupService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// backupClients masks every saved download client into the backup wire shape.
func (s *BackupService) backupClients(ctx context.Context) ([]BackupDownloadClient, error) {
	if s.DCRepo == nil {
		return []BackupDownloadClient{}, nil
	}
	recs, err := s.DCRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]BackupDownloadClient, 0, len(recs))
	for _, rec := range recs {
		out = append(out, BackupDownloadClient{
			Name:        rec.Name,
			Kind:        string(rec.Kind),
			Enabled:     rec.Enabled,
			Position:    rec.Position,
			BaseURL:     rec.BaseURL,
			Username:    rec.Username,
			APIKeySet:   rec.APIKey != "",
			CompleteDir: rec.CompleteDir,
		})
	}
	return out, nil
}

// importClients replaces the stored download-client list with the document's
// (delete-all, then re-create in document order). Unknown kinds are skipped
// (forward/back-compat, like unknown sections); keys are never carried, so
// recreated clients start with an empty key.
func (s *BackupService) importClients(ctx context.Context, in []BackupDownloadClient) error {
	if s.DCRepo == nil || len(in) == 0 {
		return nil
	}
	existing, err := s.DCRepo.List(ctx)
	if err != nil {
		return err
	}
	for _, rec := range existing {
		if err := s.DCRepo.Delete(ctx, rec.ID); err != nil {
			return err
		}
	}
	position := 0
	for _, b := range in {
		if !downloadclients.Kind(b.Kind).Valid() {
			continue
		}
		position++
		if b.Position > 0 {
			position = b.Position
		}
		if _, _, err := s.DCRepo.Create(ctx, downloadclients.Record{
			Name:        b.Name,
			Kind:        downloadclients.Kind(b.Kind),
			Enabled:     b.Enabled,
			Position:    position,
			BaseURL:     b.BaseURL,
			Username:    b.Username,
			CompleteDir: b.CompleteDir,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Export builds the backup document. includeQueue / includeSeries attach the
// optional snapshots when a provider is available.
func (s *BackupService) Export(ctx context.Context, includeQueue, includeSeries bool) (*BackupDocument, error) {
	clients, err := s.backupClients(ctx)
	if err != nil {
		return nil, err
	}

	libs := make(map[string]BackupLibraryOverride, len(settings.LibraryKinds))
	for _, k := range settings.LibraryKinds {
		o, err := s.Store.GetLibrary(ctx, k)
		if err != nil {
			return nil, err
		}
		if o == nil {
			o = &settings.LibraryOverride{}
		}
		libs[k.String()] = BackupLibraryOverride{
			Path:           o.Path,
			QualityProfile: o.QualityProfile,
			ChecksumVerify: o.ChecksumVerify,
		}
	}

	sections := make([]BackupSection, 0, len(settings.AllSections))
	for _, sec := range settings.AllSections {
		raw, err := s.Store.GetSectionDocument(ctx, sec)
		if err != nil {
			return nil, err
		}
		doc := string(raw)
		if len(raw) == 0 {
			// A never-saved section exports its defaults so the document is
			// self-contained on a fresh import.
			d, err := sec.DecodeDocument(nil)
			if err != nil {
				return nil, err
			}
			b, err := sec.EncodeDocument(d)
			if err != nil {
				return nil, err
			}
			doc = string(b)
		}
		sections = append(sections, BackupSection{Section: string(sec), Document: doc})
	}

	out := &BackupDocument{
		Version:    s.Version,
		ExportedAt: s.now(),
		Settings:   BackupSettings{DownloadClients: clients, Libraries: libs, Sections: sections},
	}
	if includeQueue && s.QueueSnap != nil {
		q, err := s.QueueSnap(ctx)
		if err != nil {
			return nil, err
		}
		out.Queue = q
	}
	if includeSeries && s.SeriesSnap != nil {
		sr, err := s.SeriesSnap(ctx)
		if err != nil {
			return nil, err
		}
		out.Series = sr
	}
	return out, nil
}

// Import writes a backup document back into the store. It validates every
// section document before writing (a malformed document is a 400, reported by
// the API layer), then writes the download client, libraries and sections.
// Idempotent: writing the same document twice converges to the same state.
func (s *BackupService) Import(ctx context.Context, d *BackupDocument, includeQueue, includeSeries bool) (*BackupImportResult, error) {
	if d == nil {
		return nil, ErrInvalidBackup
	}

	// Download clients (Prowlarr PR5: the plural list, masked — keys are
	// never carried, imported clients come back with an empty key).
	if err := s.importClients(ctx, d.Settings.DownloadClients); err != nil {
		return nil, err
	}

	// Per-library overrides (only the four known kinds).
	for kind, o := range d.Settings.Libraries {
		k := settings.LibraryKind(kind)
		if !k.IsValid() {
			continue
		}
		lo := &settings.LibraryOverride{
			Path:           o.Path,
			QualityProfile: o.QualityProfile,
			ChecksumVerify: o.ChecksumVerify,
		}
		if err := s.Store.PutLibrary(ctx, k, lo); err != nil {
			return nil, err
		}
	}

	// Generic sections: validate, then write.
	settingsImported := 0
	for _, bsec := range d.Settings.Sections {
		sec, ok := settings.ParseSection(bsec.Section)
		if !ok {
			// Skip unknown slugs (forward/back-compat) rather than failing.
			continue
		}
		doc, err := sec.DecodeDocument([]byte(bsec.Document))
		if err != nil {
			return nil, ErrInvalidBackup
		}
		if msg := sec.ValidateDocument(doc); msg != "" {
			return nil, ErrInvalidBackup
		}
		if err := s.Store.PutSectionDocument(ctx, sec, []byte(bsec.Document)); err != nil {
			return nil, err
		}
		settingsImported++
	}

	res := &BackupImportResult{Imported: true, SettingsImported: settingsImported, Message: "backup imported"}
	if includeQueue && len(d.Queue) > 0 && s.QueueImport != nil {
		n, err := s.QueueImport(ctx, d.Queue)
		if err != nil {
			return nil, err
		}
		res.QueueImported = n
	}
	if includeSeries && len(d.Series) > 0 && s.SeriesImport != nil {
		n, err := s.SeriesImport(ctx, d.Series)
		if err != nil {
			return nil, err
		}
		res.SeriesImported = n
	}
	return res, nil
}

// ErrInvalidBackup reports a backup document that could not be imported
// (malformed or invalid section document). The API layer maps it to a 400.
var ErrInvalidBackup = errInvalidBackup{}

type errInvalidBackup struct{}

func (errInvalidBackup) Error() string { return "system: invalid backup document" }
