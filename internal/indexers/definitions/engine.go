package definitions

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ValidationError describes a single contract violation. Field carries the
// offending contract field name; the API maps it to a 400.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return e.Message
}

// IsValidationError reports whether err (possibly wrapped) is a contract
// validation failure (as opposed to a transport-level problem).
func IsValidationError(err error) bool {
	var v *ValidationError
	return errors.As(err, &v)
}

// Resolve validates values against the definition's config contract and
// returns a NEW normalized map:
//   - unknown field names, values sent for info fields, and type mismatches
//     (bad number, non-bool checkbox, out-of-option select) are rejected;
//   - required fields (e.g. baseurl) must be present and non-empty;
//   - values are normalized to canonical types (string / float64 / bool);
//   - missing input fields are filled with their contract defaults.
//
// A nil values map is treated as empty. Resolve never mutates its input.
func Resolve(d *Definition, values map[string]any) (map[string]any, error) {
	if d == nil {
		return nil, &ValidationError{Message: "definition is nil"}
	}
	out := make(map[string]any, len(d.Fields))
	for name, raw := range values {
		f := d.Field(name)
		if f == nil {
			return nil, &ValidationError{Field: name, Message: "unknown field"}
		}
		if f.Type == FieldTypeInfo {
			return nil, &ValidationError{Field: name, Message: "info field is not an input"}
		}
		v, err := normalizeValue(f, raw)
		if err != nil {
			return nil, &ValidationError{Field: name, Message: err.Error()}
		}
		out[name] = v
	}
	// Apply contract defaults to missing input fields.
	for i := range d.Fields {
		f := &d.Fields[i]
		if !f.IsInput() {
			continue
		}
		if _, ok := out[f.Name]; ok {
			continue
		}
		if f.Default == nil {
			continue
		}
		v, err := normalizeValue(f, f.Default)
		if err != nil {
			// A malformed seed default is a build-time defect; surface it
			// loudly rather than silently dropping the value.
			return nil, fmt.Errorf("definition %q: default for %q: %w", d.ID, f.Name, err)
		}
		out[f.Name] = v
	}
	// Required fields must end up present and non-empty (covers both absent
	// keys and explicitly empty strings).
	for i := range d.Fields {
		f := &d.Fields[i]
		if !f.Required {
			continue
		}
		v, ok := out[f.Name]
		if !ok {
			return nil, &ValidationError{Field: f.Name, Message: "is required"}
		}
		if s, isStr := v.(string); isStr && s == "" {
			return nil, &ValidationError{Field: f.Name, Message: "is required"}
		}
	}
	return out, nil
}

// normalizeValue coerces a raw JSON value to the canonical type for the field.
func normalizeValue(f *Field, raw any) (any, error) {
	switch f.Type {
	case FieldTypeTextbox, FieldTypePassword:
		switch v := raw.(type) {
		case string:
			return v, nil
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64), nil
		case nil:
			return "", nil
		default:
			return nil, fmt.Errorf("expected string or number")
		}
	case FieldTypeNumber:
		switch v := raw.(type) {
		case float64:
			return v, nil
		case string:
			n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				return nil, fmt.Errorf("invalid number %q", v)
			}
			return n, nil
		default:
			return nil, fmt.Errorf("expected number")
		}
	case FieldTypeCheckbox:
		switch v := raw.(type) {
		case bool:
			return v, nil
		case string:
			b, err := strconv.ParseBool(strings.TrimSpace(v))
			if err != nil {
				return nil, fmt.Errorf("expected boolean")
			}
			return b, nil
		default:
			return nil, fmt.Errorf("expected boolean")
		}
	case FieldTypeSelect:
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected option value string")
		}
		if f.Options != nil {
			if _, valid := f.Options[s]; !valid {
				return nil, fmt.Errorf("%q is not a valid option", s)
			}
		}
		return s, nil
	default:
		// Unknown contract type (should not happen with a validated seed):
		// degrade to string so data is not lost.
		switch v := raw.(type) {
		case string:
			return v, nil
		case nil:
			return "", nil
		default:
			return fmt.Sprint(raw), nil
		}
	}
}

// MaskSecrets returns a copy of values with every password field value
// replaced by "" (the API never echoes stored secrets; empty string matches
// the legacy api_key masking convention). Nil values yields an empty map so
// responses always carry a JSON object.
func MaskSecrets(d *Definition, values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for k, v := range values {
		out[k] = v
	}
	if d != nil {
		for _, name := range d.SecretFieldNames() {
			if _, ok := out[name]; ok {
				out[name] = ""
			}
		}
	}
	return out
}
