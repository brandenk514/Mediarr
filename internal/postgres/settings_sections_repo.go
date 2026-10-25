package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/brandenk514/mediarr/internal/settings"
)

// Section documents (P5) ride the same settings key/value table, under the
// key "section.<slug>" (settings.Section.Key). The two documents that carry
// secrets — connect (notification tokens) and import-lists (Sonarr API keys) —
// follow the same at-rest contract as the download-client document: the
// secret fields are sealed with the app vault before they touch the table and
// opened on the way out (PLAN §7). Everything else in a section document is
// stored as-is.

// connectNotifStored is the on-wire shape of one connect notification: the
// options token holds hex ciphertext (or "" when unset).
type connectNotifStored struct {
	ID      int                    `json:"id"`
	Name    string                 `json:"name"`
	Kind    string                 `json:"kind"`
	Enabled bool                   `json:"enabled"`
	Options connectOptionsStored   `json:"options"`
}

type connectOptionsStored struct {
	URL       string `json:"url"`
	SectionID string `json:"section_id"`
	Token     string `json:"token"`
	TokenSet  bool   `json:"token_set"`
}

type connectStored struct {
	Notifications []connectNotifStored `json:"notifications"`
}

// importListStored is the on-wire shape of one import list: the api_key holds
// hex ciphertext (or "" when unset).
type importListStored struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	URL       string `json:"url"`
	Enabled   bool   `json:"enabled"`
	APIKey    string `json:"api_key"`
	APIKeySet bool   `json:"api_key_set"`
}

type importListsStored struct {
	Lists []importListStored `json:"lists"`
}

// GetSectionDocument returns the stored JSON for section s, or nil (no error)
// when the section has never been saved. Secrets are decrypted before the
// document is returned, so the API layer only ever sees plaintext in memory.
func (r *SettingsRepo) GetSectionDocument(ctx context.Context, s settings.Section) ([]byte, error) {
	raw, ok, err := r.getJSON(ctx, s.Key())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	switch s {
	case settings.SectionConnect:
		var st connectStored
		if err := json.Unmarshal([]byte(raw), &st); err != nil {
			return nil, fmt.Errorf("settings: parse %s: %w", s, err)
		}
		out := &settings.ConnectSettings{Notifications: make([]settings.Notification, 0, len(st.Notifications))}
		for _, n := range st.Notifications {
			t, err := r.decryptField(n.Options.Token)
			if err != nil {
				return nil, err
			}
			out.Notifications = append(out.Notifications, settings.Notification{
				ID: n.ID, Name: n.Name, Kind: n.Kind, Enabled: n.Enabled,
				Options: settings.NotificationOptions{
					URL: n.Options.URL, SectionID: n.Options.SectionID,
					Token: t, TokenSet: n.Options.TokenSet,
				},
			})
		}
		return json.Marshal(out)
	case settings.SectionImportLists:
		var st importListsStored
		if err := json.Unmarshal([]byte(raw), &st); err != nil {
			return nil, fmt.Errorf("settings: parse %s: %w", s, err)
		}
		out := &settings.ImportListsSettings{Lists: make([]settings.ImportList, 0, len(st.Lists))}
		for _, l := range st.Lists {
			k, err := r.decryptField(l.APIKey)
			if err != nil {
				return nil, err
			}
			out.Lists = append(out.Lists, settings.ImportList{
				ID: l.ID, Name: l.Name, Kind: l.Kind, URL: l.URL,
				Enabled: l.Enabled, APIKey: k, APIKeySet: l.APIKeySet,
			})
		}
		return json.Marshal(out)
	default:
		return []byte(raw), nil
	}
}

// PutSectionDocument upserts the stored JSON for section s, sealing its
// secret fields with the vault first.
func (r *SettingsRepo) PutSectionDocument(ctx context.Context, s settings.Section, doc []byte) error {
	switch s {
	case settings.SectionConnect:
		var in settings.ConnectSettings
		if err := json.Unmarshal(doc, &in); err != nil {
			return fmt.Errorf("settings: decode %s: %w", s, err)
		}
		st := connectStored{Notifications: make([]connectNotifStored, 0, len(in.Notifications))}
		for _, n := range in.Notifications {
			ct, err := r.encryptField(n.Options.Token)
			if err != nil {
				return err
			}
			st.Notifications = append(st.Notifications, connectNotifStored{
				ID: n.ID, Name: n.Name, Kind: n.Kind, Enabled: n.Enabled,
				Options: connectOptionsStored{
					URL: n.Options.URL, SectionID: n.Options.SectionID,
					Token: ct, TokenSet: n.Options.TokenSet,
				},
			})
		}
		raw, err := json.Marshal(st)
		if err != nil {
			return fmt.Errorf("settings: marshal %s: %w", s, err)
		}
		return r.putJSON(ctx, s.Key(), string(raw))
	case settings.SectionImportLists:
		var in settings.ImportListsSettings
		if err := json.Unmarshal(doc, &in); err != nil {
			return fmt.Errorf("settings: decode %s: %w", s, err)
		}
		st := importListsStored{Lists: make([]importListStored, 0, len(in.Lists))}
		for _, l := range in.Lists {
			ct, err := r.encryptField(l.APIKey)
			if err != nil {
				return err
			}
			st.Lists = append(st.Lists, importListStored{
				ID: l.ID, Name: l.Name, Kind: l.Kind, URL: l.URL,
				Enabled: l.Enabled, APIKey: ct, APIKeySet: l.APIKeySet,
			})
		}
		raw, err := json.Marshal(st)
		if err != nil {
			return fmt.Errorf("settings: marshal %s: %w", s, err)
		}
		return r.putJSON(ctx, s.Key(), string(raw))
	default:
		return r.putJSON(ctx, s.Key(), string(doc))
	}
}
