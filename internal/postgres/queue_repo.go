package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/brandenk514/mediarr/internal/queue"
)

// QueueRepo is the Postgres adapter for queue.Repo (#43). It is the
// dedicated-queue-view analogue of DashboardRepo: symmetric UNION ALLs across
// the four queue tables (which share identical column shapes:
// release_title, indexer, state, progress, updated_at) joined to the parent's
// human name, plus a per-parent history read. The four kinds share identical
// column shapes on the queue and history tables, so the queries are symmetric.
type QueueRepo struct {
	db *sql.DB
}

// NewQueueRepo wraps a *sql.DB as a queue.Repo.
func NewQueueRepo(db *sql.DB) *QueueRepo { return &QueueRepo{db: db} }

var _ queue.Repo = (*QueueRepo)(nil)

const defaultHistoryLimit = 50

// queueListSQL is the full queue across all four kinds, each row joined to its
// parent's human name (movie/series title, artist/author name). Unlike the
// dashboard's recent tail, this is unbounded — the HTTP layer filters and
// pages it in memory via queue.Apply (mirrors the per-kind list endpoints).
const queueListSQL = `
SELECT 'movie'::text AS kind, q.movie_id, m.title,
       q.release_title, q.indexer, q.state, q.progress, q.updated_at
FROM queue q JOIN movies m ON m.id = q.movie_id
UNION ALL
SELECT 'tv', q.series_id, s.title,
       q.release_title, q.indexer, q.state, q.progress, q.updated_at
FROM tv_queue q JOIN tv_series s ON s.id = q.series_id
UNION ALL
SELECT 'music', q.artist_id, a.name,
       q.release_title, q.indexer, q.state, q.progress, q.updated_at
FROM music_queue q JOIN music_artists a ON a.id = q.artist_id
UNION ALL
SELECT 'book', q.author_id, au.name,
       q.release_title, q.indexer, q.state, q.progress, q.updated_at
FROM book_queue q JOIN book_authors au ON au.id = q.author_id
ORDER BY updated_at DESC
`

// historySQLs maps a kind to its parent-scoped history query. Each selects the
// (event, detail, at) triple for one parent id, newest first, capped by LIMIT.
var historySQLs = map[queue.Kind]string{
	queue.KindMovie: `SELECT event, detail, at FROM history WHERE movie_id = $1 ORDER BY at DESC LIMIT $2`,
	queue.KindTV:    `SELECT event, detail, at FROM tv_history WHERE series_id = $1 ORDER BY at DESC LIMIT $2`,
	queue.KindMusic: `SELECT event, detail, at FROM music_history WHERE artist_id = $1 ORDER BY at DESC LIMIT $2`,
	queue.KindBook:  `SELECT event, detail, at FROM book_history WHERE author_id = $1 ORDER BY at DESC LIMIT $2`,
}

// List implements queue.Repo: every queue job, newest-updated-first, joined to
// the parent's name.
func (r *QueueRepo) List(ctx context.Context) ([]queue.Item, error) {
	rows, err := r.db.QueryContext(ctx, queueListSQL)
	if err != nil {
		return nil, fmt.Errorf("queue: query queue: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop above

	var out []queue.Item
	for rows.Next() {
		var (
			kind      string
			parentID  int64
			parent    string
			release   string
			indexer   string
			state     string
			progress  int
			updatedAt time.Time
		)
		if err := rows.Scan(&kind, &parentID, &parent, &release, &indexer, &state, &progress, &updatedAt); err != nil {
			return nil, fmt.Errorf("queue: scan item: %w", err)
		}
		out = append(out, queue.Item{
			Kind:         queue.Kind(kind),
			ParentID:     parentID,
			ParentName:   parent,
			ReleaseTitle: release,
			Indexer:      indexer,
			State:        state,
			Progress:     progress,
			UpdatedAt:    updatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("queue: iterate items: %w", err)
	}
	if out == nil {
		out = []queue.Item{}
	}
	return out, nil
}

// History implements queue.Repo: the lifecycle events for one parent, newest
// first. An unknown kind (or a parent that never had a job) yields an empty,
// non-nil slice — the shape is stable for the UI, and "no history yet" is a
// legitimate state, not an error.
func (r *QueueRepo) History(ctx context.Context, kind queue.Kind, parentID int64, limit int) ([]queue.HistoryEvent, error) {
	q, ok := historySQLs[kind]
	if !ok {
		return []queue.HistoryEvent{}, nil
	}
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	rows, err := r.db.QueryContext(ctx, q, parentID, limit)
	if err != nil {
		return nil, fmt.Errorf("queue: query history: %w", err)
	}
	defer func() { _ = rows.Close() }() // rows consumed by the loop above

	var out []queue.HistoryEvent
	for rows.Next() {
		var (
			event  string
			detail string
			at     time.Time
		)
		if err := rows.Scan(&event, &detail, &at); err != nil {
			return nil, fmt.Errorf("queue: scan history: %w", err)
		}
		out = append(out, queue.HistoryEvent{Event: event, Detail: detail, At: at})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("queue: iterate history: %w", err)
	}
	if out == nil {
		out = []queue.HistoryEvent{}
	}
	return out, nil
}
