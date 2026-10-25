package providers

import (
	"context"
	"errors"
	"time"
)

// P5.7: the per-kind source fallback chain. The metadata-source settings
// section carries an ordered source list per media kind (keyed primary
// first, keyless fallbacks after — e.g. tv: [tvdb, tvmaze, wikidata]).
// The chain walks that list: the first source that answers with results
// wins; 503 (ErrAllSourcesFailed) only when every configured source failed
// or was unconfigured. A zero-key install therefore still searches through
// the keyless adapters (TVmaze, Wikidata, MusicBrainz, Open Library).

// ErrAllSourcesFailed is the chain's terminal failure: every configured
// source in the list failed or was unconfigured. The API maps it to 503 and
// still returns the per-source attempt log.
var ErrAllSourcesFailed = errors.New("providers: all configured sources failed")

// Attempt is one source's outcome in the fallback walk.
type Attempt struct {
	Source    string `json:"source"`
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error"` // empty when ok
}

// Source is one configured entry in a kind's source list.
type Source struct {
	// Name is the source slug as stored in the settings list
	// ("tvdb", "tvmaze", "wikidata", ...).
	Name string
	// Searcher is the adapter to call; nil = the source is not configured
	// (e.g. a keyed provider without its key) and the chain skips it.
	Searcher Searcher
}

// Chain is an ordered per-kind source list.
type Chain struct {
	Sources []Source
}

// NewChain builds a chain from ordered source entries.
func NewChain(sources ...Source) *Chain {
	return &Chain{Sources: sources}
}

// Search walks the chain for q and returns (the source name that answered,
// its results, the full attempt log). When no source answers it returns
// ErrAllSourcesFailed with the attempt log still populated.
func (c *Chain) Search(ctx context.Context, q string) (string, []Result, []Attempt, error) {
	attempts := make([]Attempt, 0, len(c.Sources))
	for _, s := range c.Sources {
		if s.Searcher == nil {
			attempts = append(attempts, Attempt{Source: s.Name, OK: false, Error: "not configured"})
			continue
		}
		start := time.Now()
		results, err := s.Searcher.Search(ctx, q)
		at := Attempt{Source: s.Name, LatencyMS: time.Since(start).Milliseconds()}
		if err != nil {
			at.Error = err.Error()
			attempts = append(attempts, at)
			continue
		}
		if len(results) == 0 {
			// An empty answer is not a failure to fall back through: a
			// source that answered "no results" wins (the query was
			// serviceable).
			at.OK = true
			attempts = append(attempts, at)
			return s.Name, []Result{}, attempts, nil
		}
		at.OK = true
		attempts = append(attempts, at)
		return s.Name, results, attempts, nil
	}
	return "", nil, attempts, ErrAllSourcesFailed
}
