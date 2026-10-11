// Package indexers is the Prowlarr-equivalent indexer layer. In v1 it defines
// the search surface the media pipelines program against plus a fake indexer
// for the M1 spike and end-to-end tests. Real indexer adapters (TorrentRSS,
// Torznab, usenet NZB) arrive in M5.
package indexers

import "context"

// SearchQuery asks an indexer for releases matching a movie/series name.
type SearchQuery struct {
	// Term is the movie/series title. For TV it may be combined with Season and
	// Episode so an adapter can request a specific release (e.g. Torznab's
	// season/episode parameters).
	Term string
	// Year is an optional year hint.
	Year int
	// Season, when set, restricts a TV search to a season (Torznab "season" /
	// "seasonsearch"). Zero means "not a per-season query".
	Season int
	// Episode, when set together with Season, restricts a TV search to a single
	// episode (Torznab "imdbid"/"ep" parameters). Zero means "season-wide".
	Episode int
}

// SearchResult is a single candidate release from an indexer.
type SearchResult struct {
	// Title is the raw scene release name (e.g. "Movie.2020.1080p.WEB.x264").
	Title string
	// SizeBytes is the reported file size.
	SizeBytes int64
	// Indexer is the name of the indexer that produced this result.
	Indexer string
	// Info carries the release's download mechanism and any protocol-specific
	// metadata the ranking layer can use. It is empty for the fake indexer and
	// for fixtures that only exercise the parse path; real adapters populate it
	// (a magnet/.torrent URL for torrent kinds, an NZB URL + usenet fields for
	// the usenet kind).
	Info ReleaseInfo
}

// ReleaseInfo is the protocol-specific metadata a real indexer surfaces for a
// single release. It exists so the shared release model (Title/SizeBytes) can
// be extended without the domain layer learning protocol specifics: the
// download client reads Info.Protocol/Info.URL to decide how to fetch the
// release, and usenet ranking factors (age, parity) are carried here for the
// picker (PLAN §7, #33 "usenet-specific fields surfaced to ranking").
type ReleaseInfo struct {
	// Protocol is the download protocol the release is available via. One of
	// ProtocolTorrent (magnet), ProtocolTorrentFile (.torrent), or
	// ProtocolNZB. Empty means "unspecified" (fake/fixture releases).
	Protocol string
	// URL is the download endpoint: a magnet:? uri or .torrent link for
	// torrent protocols, an NZB download link for usenet. The download client
	// hands this to the appropriate backend.
	URL string
	// Seeders, when known, is the number of seeders reported by the indexer.
	// Usable by ranking as a health/freshness signal.
	Seeders int
	// AgeHours, when known, is the age of the usenet release in hours. Younger
	// is generally preferred for usenet ranking.
	AgeHours float64
	// Articles, when known, is the number of usenet articles/segments in the
	// release. Surfaced for ranking and logging (#33 "article-count").
	Articles int
	// Parity is usenet-specific: true when the release includes parity/overflow
	// articles, which improves recovery odds.
	Parity bool
}

// Protocol constants for ReleaseInfo.Protocol.
const (
	ProtocolTorrent     = "torrent"      // magnet link
	ProtocolTorrentFile = "torrent-file" // .torrent URL
	ProtocolNZB         = "nzb"          // NZB download link
)

// Searcher is the narrow interface the media pipelines need to find releases.
// The consumer (the service) defines this; the concrete fakes and, later,
// real adapters implement it. Keeping it in the indexer layer means the movie
// service depends on this one method, not on a whole client SDK.
type Searcher interface {
	Name() string
	Search(ctx context.Context, q SearchQuery) ([]SearchResult, error)
}
