package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/brandenk514/mediarr/internal/dashboard"
)

// DashboardRepo is the Postgres adapter for dashboard.Repo. It issues a small
// fixed set of aggregate queries (one per concern) so a dashboard load is a
// handful of round-trips regardless of library size — it never loads whole
// tables. The four kinds share identical column shapes (release_title,
// indexer, state, progress, updated_at on the queue tables; event, detail, at
// on the history tables), so the recent-feed queries are symmetric UNION ALLs.
type DashboardRepo struct {
	db *sql.DB
}

// NewDashboardRepo wraps a *sql.DB as a dashboard.Repo.
func NewDashboardRepo(db *sql.DB) *DashboardRepo { return &DashboardRepo{db: db} }

var _ dashboard.Repo = (*DashboardRepo)(nil)

const defaultRecent = 10

// kindLabel maps a canonical kind to its UI label.
func kindLabel(k dashboard.Kind) string {
	switch k {
	case dashboard.KindMovie:
		return "Movies"
	case dashboard.KindTV:
		return "TV"
	case dashboard.KindMusic:
		return "Music"
	case dashboard.KindBook:
		return "Books"
	}
	return string(k)
}

// libraryRows is the single round-trip that rolls up every kind:
// (kind, library_count, active_wants, in_flight_queue).
const librarySQL = `
SELECT 'movie'::text,
       (SELECT count(*) FROM movies),
       (SELECT count(*) FROM wanted      WHERE status = 'pending'),
       (SELECT count(*) FROM queue       WHERE state IN ('queued','downloading'))
UNION ALL
SELECT 'tv',
       (SELECT count(*) FROM tv_series),
       (SELECT count(*) FROM tv_wanted   WHERE status = 'pending'),
       (SELECT count(*) FROM tv_queue    WHERE state IN ('queued','downloading'))
UNION ALL
SELECT 'music',
       (SELECT count(*) FROM music_artists),
       (SELECT count(*) FROM music_wanted WHERE status = 'pending'),
       (SELECT count(*) FROM music_queue  WHERE state IN ('queued','downloading'))
UNION ALL
SELECT 'book',
       (SELECT count(*) FROM book_authors),
       (SELECT count(*) FROM book_wanted  WHERE status = 'pending'),
       (SELECT count(*) FROM book_queue   WHERE state IN ('queued','downloading'))
`

// historySQL is a symmetric UNION ALL of the four history feeds joined to the
// parent's human name, ordered newest-first. The outer ORDER BY / LIMIT apply
// to the combined set.
const historySQL = `
SELECT 'movie' AS kind, m.title AS name, h.event, h.detail, h.at
FROM history h JOIN movies m ON m.id = h.movie_id
UNION ALL
SELECT 'tv', s.title, h.event, h.detail, h.at
FROM tv_history h JOIN tv_series s ON s.id = h.series_id
UNION ALL
SELECT 'music', a.name, h.event, h.detail, h.at
FROM music_history h JOIN music_artists a ON a.id = h.artist_id
UNION ALL
SELECT 'book', au.name, h.event, h.detail, h.at
FROM book_history h JOIN book_authors au ON au.id = h.author_id
ORDER BY at DESC
LIMIT $1
`

// queueSQL is the recent queue activity across all four kinds, newest-first.
const queueSQL = `
SELECT 'movie' AS kind, q.release_title, q.indexer, q.state, q.progress, q.updated_at
FROM queue q
UNION ALL
SELECT 'tv', q.release_title, q.indexer, q.state, q.progress, q.updated_at
FROM tv_queue q
UNION ALL
SELECT 'music', q.release_title, q.indexer, q.state, q.progress, q.updated_at
FROM music_queue q
UNION ALL
SELECT 'book', q.release_title, q.indexer, q.state, q.progress, q.updated_at
FROM book_queue q
ORDER BY updated_at DESC
LIMIT $1
`

func (r *DashboardRepo) libraries(ctx context.Context) ([]dashboard.Library, error) {
	rows, err := r.db.QueryContext(ctx, librarySQL)
	if err != nil {
		return nil, fmt.Errorf("dashboard: query libraries: %w", err)
	}
	defer rows.Close()

	var libs []dashboard.Library
	for rows.Next() {
		var (
			kind  string
			count int64
			wants int64
			queue int64
		)
		if err := rows.Scan(&kind, &count, &wants, &queue); err != nil {
			return nil, fmt.Errorf("dashboard: scan library: %w", err)
		}
		k := dashboard.Kind(kind)
		libs = append(libs, dashboard.Library{
			Kind:  k,
			Label: kindLabel(k),
			Count: count,
			Wants: wants,
			Queue: queue,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard: iterate libraries: %w", err)
	}
	return libs, nil
}

func (r *DashboardRepo) history(ctx context.Context, limit int) ([]dashboard.HistoryItem, error) {
	rows, err := r.db.QueryContext(ctx, historySQL, limit)
	if err != nil {
		return nil, fmt.Errorf("dashboard: query history: %w", err)
	}
	defer rows.Close()

	var out []dashboard.HistoryItem
	for rows.Next() {
		var (
			kind   string
			name   string
			event  string
			detail string
			at     time.Time
		)
		if err := rows.Scan(&kind, &name, &event, &detail, &at); err != nil {
			return nil, fmt.Errorf("dashboard: scan history: %w", err)
		}
		out = append(out, dashboard.HistoryItem{
			Kind:   dashboard.Kind(kind),
			Name:   name,
			Event:  event,
			Detail: detail,
			At:     at,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard: iterate history: %w", err)
	}
	return out, nil
}

func (r *DashboardRepo) queue(ctx context.Context, limit int) ([]dashboard.QueueItem, error) {
	rows, err := r.db.QueryContext(ctx, queueSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("dashboard: query queue: %w", err)
	}
	defer rows.Close()

	var out []dashboard.QueueItem
	for rows.Next() {
		var (
			kind        string
			release     string
			indexer     string
			state       string
			progress    int
			updatedAt   time.Time
		)
		if err := rows.Scan(&kind, &release, &indexer, &state, &progress, &updatedAt); err != nil {
			return nil, fmt.Errorf("dashboard: scan queue: %w", err)
		}
		out = append(out, dashboard.QueueItem{
			Kind:         dashboard.Kind(kind),
			ReleaseTitle: release,
			Indexer:      indexer,
			State:        state,
			Progress:     progress,
			UpdatedAt:    updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dashboard: iterate queue: %w", err)
	}
	return out, nil
}

// Snapshot implements dashboard.Repo.
func (r *DashboardRepo) Snapshot(ctx context.Context, maxRecent int) (*dashboard.Snapshot, error) {
	if maxRecent <= 0 {
		maxRecent = defaultRecent
	}

	libraries, err := r.libraries(ctx)
	if err != nil {
		return nil, err
	}
	history, err := r.history(ctx, maxRecent)
	if err != nil {
		return nil, err
	}
	queue, err := r.queue(ctx, maxRecent)
	if err != nil {
		return nil, err
	}

	var totals dashboard.Totals
	for _, l := range libraries {
		totals.Libraries += l.Count
		totals.Wants += l.Wants
		totals.Queue += l.Queue
	}

	return &dashboard.Snapshot{
		Libraries:     libraries,
		Totals:        totals,
		RecentHistory: history,
		RecentQueue:   queue,
		GeneratedAt:   time.Now().UTC(),
	}, nil
}
