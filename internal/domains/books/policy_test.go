package books

import "testing"

// TestValidMonitorPolicy is a table-driven check of the Readarr B2 author
// monitor-policy enum: all seven Lidarr L2 states plus the empty legacy
// value are valid; anything else is rejected.
func TestValidMonitorPolicy(t *testing.T) {
	cases := []struct {
		policy string
		want   bool
	}{
		// The seven Lidarr L2 states.
		{PolicyAll, true},
		{PolicyFuture, true},
		{PolicyMissing, true},
		{PolicyExisting, true},
		{PolicyFirst, true},
		{PolicyLast, true},
		{PolicyNone, true},
		// "" is the legacy default (= all) for rows created before B2.
		{"", true},
		// Unknown values are rejected.
		{"weekly", false},
		{"ALL", false}, // the enum is lowercase
		{" ", false},
		{"first ", false},
	}
	for _, c := range cases {
		if got := ValidMonitorPolicy(c.policy); got != c.want {
			t.Errorf("ValidMonitorPolicy(%q) = %v, want %v", c.policy, got, c.want)
		}
	}
}
