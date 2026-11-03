// Postgres target for the Prowlarr migration (Prowlarr PR6): it implements
// ProwlarrMigrationTarget against the existing Mediarr stores — the tag
// settings section, the indexer store (vault-aware: re-encrypts the
// plaintext API key and the definition contract's secret config fields with
// the deployment's AES-256-GCM vault before they are written), the
// download-client store (upserts per client, since Prowlarr runs several),
// and the indexer-proxy store. All store lookups/writes go through the
// injected repos so unit tests run without Postgres.

package system

import (
	"context"
	"encoding/json"

	"github.com/brandenk514/mediarr/internal/downloadclients"
	"github.com/brandenk514/mediarr/internal/indexers"
	"github.com/brandenk514/mediarr/internal/settings"
)

// ProwlarrIndexerStore is the indexer-store seam the Prowlarr target needs:
// a by-name lookup plus a full-definition create. The Postgres adapter in
// cmd/mediarr/main.go wraps *postgres.IndexersRepo, whose Create
// re-encrypts def.APIKey and the contract's secret config fields at rest
// (internal/secrets) — the credential RE-ENCRYPTION step of the import.
type ProwlarrIndexerStore interface {
	// FindIndexer returns the id of an existing indexer with this exact name.
	FindIndexer(ctx context.Context, name string) (int64, bool)
	// CreateIndexer inserts the indexer. def.APIKey and any secret
	// config-field values are PLAINTEXT here; the store encrypts them at
	// rest. It must not create a second indexer for an existing name.
	CreateIndexer(ctx context.Context, def indexers.Definition) (int64, error)
}

// ProwlarrProxyStore is the indexer-proxy-store seam: a by-name lookup plus
// a create. The Postgres adapter wraps *postgres.IndexerProxiesRepo, whose
// Create encrypts the password at rest.
type ProwlarrProxyStore interface {
	// FindProxy returns the id of an existing proxy with this exact name.
	FindProxy(ctx context.Context, name string) (int64, bool)
	// CreateProxy inserts the proxy (p.Password is PLAINTEXT here; the
	// store encrypts it at rest).
	CreateProxy(ctx context.Context, p indexers.Proxy) (int64, error)
}

// ProwlarrPostgresTarget is the real ProwlarrMigrationTarget: it writes the
// imported indexers, download clients, tags, and proxies into the existing
// Mediarr stores.
type ProwlarrPostgresTarget struct {
	Settings SettingsStore
	Indexers ProwlarrIndexerStore
	Proxies  ProwlarrProxyStore
	DCRepo   downloadclients.Repo
}

// NewProwlarrPostgresTarget builds the Postgres target for the Prowlarr
// migrator. The caller sets DCRepo after construction (matching the
// Sonarr/Radarr target wiring in main.go).
func NewProwlarrPostgresTarget(
	st SettingsStore,
	ix ProwlarrIndexerStore,
	proxies ProwlarrProxyStore,
) *ProwlarrPostgresTarget {
	return &ProwlarrPostgresTarget{Settings: st, Indexers: ix, Proxies: proxies}
}

// EnsureTag implements ProwlarrMigrationTarget. Tags live in the settings
// store's tags section (the same section the Tags page manages), and Prowlarr
// tag ids are preserved so the indexers' and proxies' tag references
// resolve.
func (t *ProwlarrPostgresTarget) EnsureTag(ctx context.Context, tag ProwlarrTag) (int64, TargetAction, error) {
	raw, err := t.Settings.GetSectionDocument(ctx, settings.SectionTags)
	if err != nil {
		return 0, TargetAction(""), err
	}
	var doc settings.TagsSettings
	if raw == nil {
		doc = settings.DefaultTags()
	} else if err := json.Unmarshal(raw, &doc); err != nil {
		doc = settings.TagsSettings{Tags: []settings.TagInfo{}}
	}
	var existing *settings.TagInfo
	for i := range doc.Tags {
		if doc.Tags[i].ID == tag.ID {
			existing = &doc.Tags[i]
			break
		}
	}
	if existing != nil {
		if existing.Label == tag.Label {
			return int64(tag.ID), ActionExists, nil
		}
		existing.Label = tag.Label
	} else {
		doc.Tags = append(doc.Tags, settings.TagInfo{ID: tag.ID, Label: tag.Label})
	}
	updated, err := json.Marshal(doc)
	if err != nil {
		return 0, TargetAction(""), err
	}
	if err := t.Settings.PutSectionDocument(ctx, settings.SectionTags, updated); err != nil {
		return 0, TargetAction(""), err
	}
	return int64(tag.ID), ActionCreated, nil
}

// EnsureIndexer implements ProwlarrMigrationTarget: a name-keyed idempotent
// import into the indexer store. The resolved ref carries the PLAINTEXT
// credential; the store's Create re-encrypts it (and the contract's secret
// config fields) with the vault before it is persisted.
func (t *ProwlarrPostgresTarget) EnsureIndexer(ctx context.Context, ref ProwlarrIndexerRef) (int64, TargetAction, error) {
	if id, ok := t.Indexers.FindIndexer(ctx, ref.Name); ok {
		return id, ActionExists, nil
	}
	id, err := t.Indexers.CreateIndexer(ctx, indexers.Definition{
		Name:           ref.Name,
		Kind:           ref.Kind,
		BaseURL:        ref.BaseURL,
		APIKey:         ref.APIKey, // PLAINTEXT: the repo encrypts it at rest
		DefinitionName: ref.DefinitionName,
		ConfigFields:   ref.ConfigFields,
		Enabled:        ref.Enable,
		Priority:       ref.Priority,
		Privacy:        ref.Privacy,
		SyncProfile:    ref.SyncLevel,
		Redirect:       ref.Redirect,
		Tags:           ref.Tags,
	})
	if err != nil {
		return 0, TargetAction(""), err
	}
	return id, ActionCreated, nil
}

// EnsureDownloadClient implements ProwlarrMigrationTarget: Prowlarr runs
// several clients, so each resolved client is upserted into the single
// download-client record by name (the shared upsert path used by the
// Sonarr/Radarr migrators).
func (t *ProwlarrPostgresTarget) EnsureDownloadClient(ctx context.Context, ref ProwlarrDCRef) (TargetAction, error) {
	rec := &downloadclients.Record{
		Name:        ref.Name,
		Kind:        ref.Kind,
		Enabled:     ref.Enabled,
		Position:    ref.Position,
		BaseURL:     ref.BaseURL,
		Username:    ref.Username,
		APIKey:      ref.APIKey, // PLAINTEXT: the repo encrypts it at rest
		CompleteDir: ref.CompleteDir,
	}
	return upsertDownloadClient(ctx, t.DCRepo, rec)
}

// EnsureIndexerProxy implements ProwlarrMigrationTarget: a name-keyed
// idempotent import of a FlareSolverr endpoint into the proxy store. The
// ref carries the PLAINTEXT password; the store's Create encrypts it at
// rest.
func (t *ProwlarrPostgresTarget) EnsureIndexerProxy(ctx context.Context, ref ProwlarrProxyRef) (int64, TargetAction, error) {
	if id, ok := t.Proxies.FindProxy(ctx, ref.Name); ok {
		return id, ActionExists, nil
	}
	id, err := t.Proxies.CreateProxy(ctx, indexers.Proxy{
		Name:     ref.Name,
		URL:      ref.URL,
		Username: ref.Username,
		Password: ref.Password, // PLAINTEXT: the repo encrypts it at rest
		Tags:     ref.Tags,
	})
	if err != nil {
		return 0, TargetAction(""), err
	}
	return id, ActionCreated, nil
}

var _ ProwlarrMigrationTarget = (*ProwlarrPostgresTarget)(nil)
