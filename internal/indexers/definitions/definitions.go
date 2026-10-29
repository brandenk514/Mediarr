// Package definitions holds the built-in indexer definition library
// (the Prowlarr community Cardigann set plus hand-written built-in
// usenet/generic definitions) and the config-contract field engine used to
// apply defaults to, validate, and mask per-indexer contract values.
//
// The seed is a committed, versioned JSON file (definitions.json) generated
// from github.com/Prowlarr/Indexers; the seed revision identifies the
// upstream commit it was pulled from so a later definition-update task can
// detect staleness.
package definitions

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

//go:embed definitions.json
var seedFS embed.FS

// Field types in a definition's config contract.
const (
	FieldTypeTextbox  = "textbox"
	FieldTypePassword = "password"
	FieldTypeNumber   = "number"
	FieldTypeCheckbox = "checkbox"
	FieldTypeSelect   = "select"
	// FieldTypeInfo is a description/hint entry (Cardigann info,
	// info_flaresolverr, info_cookie, ...). It is rendered as static text
	// in the UI and is never an input.
	FieldTypeInfo = "info"
)

// Field is one entry of a definition's config contract.
type Field struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Label    string            `json:"label,omitempty"`
	Hint     string            `json:"hint,omitempty"`
	Required bool              `json:"required,omitempty"`
	Advanced bool              `json:"advanced,omitempty"`
	Default  any               `json:"default,omitempty"`
	Options  map[string]string `json:"options,omitempty"` // select: value -> display
}

// IsInput reports whether the field accepts user input (info fields do not).
func (f Field) IsInput() bool { return f.Type != FieldTypeInfo }

// Definition is one indexer definition in the library.
type Definition struct {
	ID         string   `json:"id"`       // Cardigann id (built-in id for built-ins)
	Name       string   `json:"name"`     // display name (e.g. "1337x (API)")
	Protocol   string   `json:"protocol"` // "torrent" | "usenet"
	Privacy    string   `json:"privacy"`  // "public" | "private" | "semi-private"
	Language   string   `json:"language"`
	Categories []string `json:"categories"`
	URLs       []string `json:"urls"`
	LegacyURLs []string `json:"legacyUrls"`
	Fields     []Field  `json:"fields"`
	Raw        string   `json:"raw"` // original Cardigann YAML (opaque; for future adapters)
}

// Field looks up a contract field by name.
func (d *Definition) Field(name string) *Field {
	for i := range d.Fields {
		if d.Fields[i].Name == name {
			return &d.Fields[i]
		}
	}
	return nil
}

// IsSecretField reports whether the named contract field holds a secret
// (password) value.
func (d *Definition) IsSecretField(name string) bool {
	f := d.Field(name)
	return f != nil && f.Type == FieldTypePassword
}

// SecretFieldNames lists the contract field names whose values are secrets.
func (d *Definition) SecretFieldNames() []string {
	var out []string
	for _, f := range d.Fields {
		if f.Type == FieldTypePassword {
			out = append(out, f.Name)
		}
	}
	return out
}

// seedFile is the on-disk shape of definitions.json.
type seedFile struct {
	Revision    string       `json:"revision"`
	Definitions []Definition `json:"definitions"`
}

// Catalog is the parsed, indexed definition library.
type Catalog struct {
	Revision    string
	Definitions []Definition

	byID   map[string]*Definition
	byName map[string]*Definition
}

// Load parses the embedded seed file.
func Load() (*Catalog, error) {
	b, err := seedFS.ReadFile("definitions.json")
	if err != nil {
		return nil, fmt.Errorf("read embedded definitions: %w", err)
	}
	return ParseSeed(b)
}

// ParseSeed parses a definitions.json payload (separated from Load for tests).
func ParseSeed(b []byte) (*Catalog, error) {
	var sf seedFile
	if err := json.Unmarshal(b, &sf); err != nil {
		return nil, fmt.Errorf("parse definitions seed: %w", err)
	}
	if len(sf.Definitions) == 0 {
		return nil, fmt.Errorf("definitions seed is empty")
	}
	c := &Catalog{
		Revision:    sf.Revision,
		Definitions: sf.Definitions,
		byID:        make(map[string]*Definition, len(sf.Definitions)),
		byName:      make(map[string]*Definition, len(sf.Definitions)),
	}
	for i := range c.Definitions {
		d := &c.Definitions[i]
		if d.ID == "" {
			return nil, fmt.Errorf("definitions seed: definition %d has empty id", i)
		}
		if _, dup := c.byID[d.ID]; dup {
			return nil, fmt.Errorf("definitions seed: duplicate id %q", d.ID)
		}
		c.byID[d.ID] = d
		c.byName[strings.ToLower(d.Name)] = d
	}
	return c, nil
}

var (
	defaultOnce sync.Once
	defaultCat  *Catalog
	defaultErr  error
)

// Default returns the process-wide loaded catalog. It panics if the embedded
// seed fails to parse: the seed is committed, versioned code, so a parse
// failure is a build-time defect, not a runtime condition.
func Default() *Catalog {
	defaultOnce.Do(func() {
		defaultCat, defaultErr = Load()
	})
	if defaultErr != nil {
		panic(defaultErr)
	}
	return defaultCat
}

// GetByID returns the definition with the given id, or nil.
func (c *Catalog) GetByID(id string) *Definition { return c.byID[id] }

// ResolveDefinition finds a definition by exact id first, then by
// case-insensitive display name (the UI sends definition_name, which may be
// either). Returns nil when unknown.
func (c *Catalog) ResolveDefinition(nameOrID string) *Definition {
	if nameOrID == "" {
		return nil
	}
	if d := c.byID[nameOrID]; d != nil {
		return d
	}
	return c.byName[strings.ToLower(nameOrID)]
}

// SortByDisplay sorts definitions by display name (case-insensitive, id as
// tiebreaker) for stable, human-readable listings.
func SortByDisplay(defs []Definition) {
	sort.SliceStable(defs, func(i, j int) bool {
		a, b := defs[i].Name, defs[j].Name
		if strings.EqualFold(a, b) {
			return defs[i].ID < defs[j].ID
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
}
