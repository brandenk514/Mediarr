package downloadclients

import "testing"

func TestKindValid(t *testing.T) {
	valid := map[Kind]bool{KindQB: true, KindSAB: true, KindTransmission: true}
	for k, want := range valid {
		if got := k.Valid(); got != want {
			t.Errorf("%q.Valid() = %v, want %v", k, got, want)
		}
	}
	for _, bad := range []Kind{"", "mock", "QBITTORRENT", "aria2"} {
		if bad.Valid() {
			t.Errorf("%q must not be valid", bad)
		}
	}
}

func TestKindHandlesProtocol(t *testing.T) {
	cases := []struct {
		kind     Kind
		protocol string
		want     bool
	}{
		{KindQB, "torrent", true},
		{KindQB, "torrent-file", true},
		{KindQB, "nzb", false},
		{KindQB, "link", false},
		{KindTransmission, "torrent", true},
		{KindTransmission, "torrent-file", true},
		{KindTransmission, "nzb", false},
		{KindSAB, "nzb", true},
		{KindSAB, "torrent", false},
		{KindSAB, "torrent-file", false},
		{Kind("mock"), "torrent", false},
		{Kind(""), "nzb", false},
	}
	for _, c := range cases {
		if got := c.kind.HandlesProtocol(c.protocol); got != c.want {
			t.Errorf("%q.HandlesProtocol(%q) = %v, want %v", c.kind, c.protocol, got, c.want)
		}
	}
}
