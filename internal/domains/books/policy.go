package books

// Author monitoring policies (Readarr B2 add/edit "Monitor" select). The
// policy decides which titles get monitored when a title is added to the
// author (an explicit monitored value still wins). Mirrors Lidarr L2's
// artist policies (domains/music); "" = all (legacy rows).
//
// B2 stores and validates the policy; the policy evaluation at title-add
// time (first/last by year, future by publish date, ...) is a later phase —
// the books model carries no title publish dates yet.
const (
	PolicyAll      = "all"      // monitor everything
	PolicyFuture   = "future"   // only releases after the import date
	PolicyMissing  = "missing"  // only what's missing from the library
	PolicyExisting = "existing" // only what already exists
	PolicyFirst    = "first"    // only the author's first book
	PolicyLast     = "last"     // only the author's last book
	PolicyNone     = "none"     // monitor nothing
)

// ValidMonitorPolicy reports whether p is a known author monitor policy.
// The empty string is valid: rows created before B2 carry "" (= all).
func ValidMonitorPolicy(p string) bool {
	switch p {
	case PolicyAll, PolicyFuture, PolicyMissing, PolicyExisting,
		PolicyFirst, PolicyLast, PolicyNone, "":
		return true
	default:
		return false
	}
}
