package paging

import (
	"errors"
	"testing"
)

// Issue #150: the per-kind list endpoints validate ?sort= against a
// closed per-kind allowlist server-side; unknown field or direction is
// an error the API maps to 400, never a silent default.

func TestValidateSort(t *testing.T) {
	fields := []string{"title", "year", "added", "release_date", "rating"}
	if err := ValidateSort("", fields); err != nil {
		t.Errorf("empty sort: err = %v, want nil (natural order)", err)
	}
	for _, s := range []string{"title-asc", "title-desc", "rating-desc", "release_date-asc"} {
		if err := ValidateSort(s, fields); err != nil {
			t.Errorf("ValidateSort(%q): err = %v, want nil", s, err)
		}
	}
	for _, s := range []string{
		"bogus-asc",      // unknown field
		"title-sideways", // unknown direction
		"title",          // missing direction
		"-asc",           // missing field
		"title-asc-x",    // trailing garbage
	} {
		err := ValidateSort(s, fields)
		if !errors.Is(err, ErrInvalidSort) {
			t.Errorf("ValidateSort(%q): err = %v, want ErrInvalidSort", s, err)
		}
	}
}
