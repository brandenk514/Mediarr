// Package settings (Readarr B5) — Books import lists.
//
// The books import-lists document is a Books-specific section (its own slug,
// "book-import-lists") because the per-kind shapes diverge from the shared
// movie import-lists document: Goodreads is a list id, LazyLibrarian and the
// Readarr bridge are base-URL + API key (masked, vault-encrypted at rest), and
// the Readarr bridge adds source-side filters (profiles / tags / root folders).
// It mirrors the shared import-lists section exactly: the Section machinery
// (decode / validate / mask / merge) handles the masked API keys, and a
// dedicated import endpoint fetches from the list (see the domain importer).
package settings

import "fmt"

// BookImportListKinds are the book (Readarr) import-list sources.
var BookImportListKinds = []string{"goodreads", "lazylibrarian", "readarr"}

// BookImportList is one configured book import list.
//
//   - goodreads: a Goodreads list by ListID on BaseURL (the goodreads host).
//   - lazylibrarian: a LazyLibrarian instance by BaseURL + APIKey (masked).
//   - readarr: another Readarr instance by BaseURL + APIKey (masked), with
//     optional source-side ProfileIDs / TagIDs / RootFolderPaths filters.
type BookImportList struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Enabled bool   `json:"enabled"`

	// BaseURL: the Goodreads host (goodreads; default www.goodreads.com), or
	// the full http(s) base URL for lazylibrarian / readarr.
	BaseURL string `json:"base_url"`

	// ListID is the Goodreads list id (goodreads only).
	ListID int `json:"list_id"`

	// APIKey is the list source's API key (lazylibrarian / readarr); masked on
	// read, and a blank value on write keeps the stored (encrypted) key.
	APIKey    string `json:"api_key"`
	APIKeySet bool   `json:"api_key_set"`

	// Readarr-instance filters (readarr only): which profiles / tags / root
	// folders on the source instance to import (empty = all).
	ProfileIDs      []int    `json:"profile_ids"`
	TagIDs          []int    `json:"tag_ids"`
	RootFolderPaths []string `json:"root_folder_paths"`
}

// BookImportListExclusion is a book a list should never import (matched by
// the source's foreign id or its title/name).
type BookImportListExclusion struct {
	ForeignID string `json:"foreign_id"`
	Name      string `json:"name"`
}

// BookImportListsSettings is the books import-lists document.
type BookImportListsSettings struct {
	Lists      []BookImportList          `json:"lists"`
	Exclusions []BookImportListExclusion `json:"exclusions"`
}

// DefaultBookImportLists returns the empty books import-lists document.
func DefaultBookImportLists() BookImportListsSettings {
	return BookImportListsSettings{Lists: []BookImportList{}, Exclusions: []BookImportListExclusion{}}
}

// Validate checks a books import-lists document: each list needs a unique
// id, a name, a known kind, and the kind-specific required fields (goodreads
// needs a ListID; lazylibrarian / readarr need a valid base URL). The API key
// is NOT validated here (matching the shared import-lists model) — it is a
// masked secret, and its presence is a write-time concern, not a document
// invariant.
func (b BookImportListsSettings) Validate() string {
	ids := map[int]bool{}
	for n, l := range b.Lists {
		if l.ID < 1 {
			return fmt.Sprintf("lists[%d]: id must be >= 1", n)
		}
		if ids[l.ID] {
			return fmt.Sprintf("lists[%d]: duplicate id %d", n, l.ID)
		}
		ids[l.ID] = true
		if l.Name == "" {
			return fmt.Sprintf("lists[%d]: name is required", n)
		}
		if !containsString(BookImportListKinds, l.Kind) {
			return fmt.Sprintf("lists[%d]: unknown kind %q", n, l.Kind)
		}
		switch l.Kind {
		case "goodreads":
			if l.ListID < 1 {
				return fmt.Sprintf("lists[%d]: list_id must be >= 1 for goodreads lists", n)
			}
			if l.BaseURL == "" {
				return fmt.Sprintf("lists[%d]: base_url is required for goodreads lists", n)
			}
		case "lazylibrarian", "readarr":
			if !isHTTPURLValue(l.BaseURL) {
				return fmt.Sprintf("lists[%d]: base_url must be an http/https URL for %s lists", n, l.Kind)
			}
		}
	}
	return ""
}
