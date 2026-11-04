package postgres

import (
	"database/sql"
	"encoding/json"
	"fmt"

	rfdom "github.com/brandenk514/mediarr/internal/domains/rootfolders"
	"github.com/brandenk514/mediarr/internal/settings"
	"github.com/brandenk514/mediarr/internal/system"
)

// RootFoldersRepo is the Postgres-backed implementation of
// rootfolders.Repo (migration 0008; the per-root Calibre block is migration
// 0030).
type RootFoldersRepo struct {
	db *sql.DB
}

// NewRootFoldersRepo builds a root-folder repository over the given pool.
func NewRootFoldersRepo(db *sql.DB) *RootFoldersRepo {
	return &RootFoldersRepo{db: db}
}

var _ rfdom.Repo = (*RootFoldersRepo)(nil)

const rootFolderColumns = `id, path, name, created_at, default_quality_profile, default_metadata_profile, calibre`

// encodeCalibreJSON marshals a per-root Calibre config for the JSONB column
// (a nil config is the "{}" = not-configured default).
func encodeCalibreJSON(c *settings.CalibreSettings) (string, error) {
	if c == nil {
		return "{}", nil
	}
	//nolint:gosec // G117: the per-root Calibre password is persisted in the local app DB (the settings-DB credential surface) — the API masks it on read and keeps it on write, mirroring the import-list API keys
	raw, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("rootfolders: marshal calibre: %w", err)
	}
	return string(raw), nil
}

// decodeCalibreJSON parses the stored JSONB column into a per-root Calibre
// config; the "{}" default (and legacy blank rows) decode to nil.
func decodeCalibreJSON(s string) (*settings.CalibreSettings, error) {
	if s == "" || s == "{}" {
		return nil, nil
	}
	var c settings.CalibreSettings
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return nil, fmt.Errorf("rootfolders: unmarshal calibre: %w", err)
	}
	return &c, nil
}

// scanRootFolder reads one row into a RootFolder.
func scanRootFolder(row interface {
	Scan(...any) error
}) (*rfdom.RootFolder, error) {
	var rf rfdom.RootFolder
	var calibreJSON string
	err := row.Scan(&rf.ID, &rf.Path, &rf.Name, &rf.CreatedAt,
		&rf.DefaultQualityProfile, &rf.DefaultMetadataProfile, &calibreJSON)
	if err != nil {
		return nil, err
	}
	if rf.Calibre, err = decodeCalibreJSON(calibreJSON); err != nil {
		return nil, err
	}
	return &rf, nil
}

// List returns all registered root folders, ordered by id.
func (r *RootFoldersRepo) List() ([]rfdom.RootFolder, error) {
	rows, err := r.db.Query(`SELECT ` + rootFolderColumns + ` FROM root_folders ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("rootfolders: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []rfdom.RootFolder
	for rows.Next() {
		rf, err := scanRootFolder(rows)
		if err != nil {
			return nil, fmt.Errorf("rootfolders: scan row: %w", err)
		}
		// R2: report free space for the add flow's root-folder dropdown;
		// null when the path can't be stat'ed (e.g. a configured-but-absent
		// root in a dev container).
		if free, ok := system.FreeSpaceFor(rf.Path); ok {
			rf.FreeSpace = &free
		}
		out = append(out, *rf)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rootfolders: list rows: %w", err)
	}
	return out, nil
}

// Get returns the root folder with the given id, or sql.ErrNoRows.
func (r *RootFoldersRepo) Get(id int64) (*rfdom.RootFolder, error) {
	row := r.db.QueryRow(
		`SELECT `+rootFolderColumns+` FROM root_folders WHERE id = $1`, id)
	rf, err := scanRootFolder(row)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("rootfolders: get %d: %w", id, err)
	}
	return rf, nil
}

// Create inserts a new root folder and returns its id.
func (r *RootFoldersRepo) Create(rf rfdom.RootFolder) (int64, error) {
	calibre, err := encodeCalibreJSON(rf.Calibre)
	if err != nil {
		return 0, err
	}
	var id int64
	err = r.db.QueryRow(`
		INSERT INTO root_folders (path, name, default_quality_profile, default_metadata_profile, calibre)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`, rf.Path, rf.Name,
		rf.DefaultQualityProfile, rf.DefaultMetadataProfile, calibre).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("rootfolders: create %q: %w", rf.Path, err)
	}
	return id, nil
}

// Update replaces a root folder's path/name/default profiles/calibre by id;
// errors if no row matches. (The service resolves omitted — empty — fields
// to the stored values before calling this.)
func (r *RootFoldersRepo) Update(rf rfdom.RootFolder) error {
	calibre, err := encodeCalibreJSON(rf.Calibre)
	if err != nil {
		return err
	}
	res, err := r.db.Exec(
		`UPDATE root_folders
		 SET path = $2, name = $3,
		     default_quality_profile = $4, default_metadata_profile = $5,
		     calibre = $6
		 WHERE id = $1`,
		rf.ID, rf.Path, rf.Name,
		rf.DefaultQualityProfile, rf.DefaultMetadataProfile, calibre)
	if err != nil {
		return fmt.Errorf("rootfolders: update %d: %w", rf.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("rootfolders: no root folder with id %d", rf.ID)
	}
	return nil
}

// Delete removes a root folder by id; errors if no row matches.
func (r *RootFoldersRepo) Delete(id int64) error {
	res, err := r.db.Exec(`DELETE FROM root_folders WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("rootfolders: delete %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("rootfolders: no root folder with id %d", id)
	}
	return nil
}
