// Package indexers is the Prowlarr-equivalent indexer layer. In v1 it defines
// the search surface the media pipelines program against plus a fake indexer
// for the M1 spike and end-to-end tests. Real indexer adapters (TorrentRSS,
// Torznab, usenet NZB) arrive in M5.
package indexers

import "context"

// SearchQuery asks an indexer for releases matching a movie/series name.
type SearchQuery struct {
	Term string // the movie/series title (or season/episode terms for tv)
	Year int    // optional year hint
}

// SearchResult is a single candidate release from an indexer.
type SearchResult struct {
	// Title is the raw scene release name (e.g. "Movie.2020.1080p.WEB.x264").
	Title string
	// SizeBytes is the reported file size.
	SizeBytes int64
	// Indexer is the name of the indexer that produced this result.
	Indexer string
}

// Searcher is the narrow interface the media pipelines need to find releases.
// The consumer (the service) defines this; the concrete fakes and, later,
// real adapters implement it. Keeping it in the indexer layer means the movie
// service depends on this one method, not on a whole client SDK.
type Searcher interface {
	Name() string
	Search(ctx context.Context, q SearchQuery) ([]SearchResult, error)
}
