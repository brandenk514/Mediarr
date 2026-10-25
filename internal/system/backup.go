package system

import (
	"context"
	"encoding/json"
	"time"

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

// DownloadClientView is the masked read shape of the download-client config
// (the OpenAPI DownloadClientView). Secrets are presence booleans only.
type DownloadClientView struct {
	Kind           string `json:"kind"`
	QBBase         string `json:"qb_base"`
	QBUsername     string `json:"qb_username"`
	QBAPIKeySet    bool   `json:"qb_api_key_set"`
	QBPasswordSet  bool   `json:"qb_password_set"`
	SABBase        string `json:"sab_base"`
	SABAPIKeySet   bool   `json:"sab_api_key_set"`
	SABCompleteDir string `json:"sab_complete_dir"`
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
	DownloadClient DownloadClientView               `json:"download_client"`
	Libraries      map[string]BackupLibraryOverride `json:"libraries"`
	Sections       []BackupSection                  `json:"sections"`
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
// (the Postgres adapter) satisfies it directly.
type BackupStore interface {
	GetDownloadClient(ctx context.Context) (*settings.DownloadClient, error)
	PutDownloadClient(ctx context.Context, dc *settings.DownloadClient) error
	GetLibrary(ctx context.Context, k settings.LibraryKind) (*settings.LibraryOverride, error)
	PutLibrary(ctx context.Context, k settings.LibraryKind, o *settings.LibraryOverride) error
	GetSectionDocument(ctx context.Context, s settings.Section) ([]byte, error)
	PutSectionDocument(ctx context.Context, s settings.Section, doc []byte) error
}

// BackupService exports and imports the configuration backup document.
type BackupService struct {
	Store   BackupStore
	Version string

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

// toDCView masks a DownloadClient into its presence-only wire view.
func toDCView(dc *settings.DownloadClient) DownloadClientView {
	if dc == nil {
		dc = &settings.DownloadClient{}
	}
	return DownloadClientView{
		Kind:           dc.Kind,
		QBBase:         dc.QBBase,
		QBUsername:     dc.QBUsername,
		QBAPIKeySet:    dc.QBAPIKey != "",
		QBPasswordSet:  dc.QBPassword != "",
		SABBase:        dc.SABBase,
		SABAPIKeySet:   dc.SABAPIKey != "",
		SABCompleteDir: dc.SABCompleteDir,
	}
}

// Export builds the backup document. includeQueue / includeSeries attach the
// optional snapshots when a provider is available.
func (s *BackupService) Export(ctx context.Context, includeQueue, includeSeries bool) (*BackupDocument, error) {
	dc, err := s.Store.GetDownloadClient(ctx)
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
		Settings:   BackupSettings{DownloadClient: toDCView(dc), Libraries: libs, Sections: sections},
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

	// Download client (non-secret fields; secrets are not carried in the view).
	dc := &settings.DownloadClient{
		Kind:           d.Settings.DownloadClient.Kind,
		QBBase:         d.Settings.DownloadClient.QBBase,
		QBUsername:     d.Settings.DownloadClient.QBUsername,
		SABBase:        d.Settings.DownloadClient.SABBase,
		SABCompleteDir: d.Settings.DownloadClient.SABCompleteDir,
	}
	if err := s.Store.PutDownloadClient(ctx, dc); err != nil {
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
