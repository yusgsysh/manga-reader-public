// Package sync implements bidirectional replication of reading progress and
// bookshelf rows between two instances (typically the desktop app and a
// self-hosted server).
//
// Design summary:
//
//   - Local mutations are appended to the sync_change outbox (see Record).
//     The outbox's auto-increment id is the replication cursor, so cursors are
//     compared against one instance's own clock only.
//   - Peers exchange changes through POST /api/sync/push: the caller sends its
//     unpushed outbox entries (plus an explicit snapshot on bootstrap) and
//     receives everything the host recorded after the caller's cursor in the
//     same round trip.
//   - Merging is per-row last-write-wins on updated_at. Re-applying an
//     already-seen row is a no-op, which makes the protocol idempotent and
//     stops echoes from looping.
//   - All connections are initiated by the client side (the server cannot
//     dial into a NAT-ed desktop); the server signals fresh changes over SSE
//     (/api/sync/events) and the engine also polls as a fallback.
//   - Deletions are replicated as tombstones. Rows deleted by a retention
//     purge (reading progress cleanup) propagate as well, so history cleanup
//     is global.
package sync

import "time"

// Entities replicated by this package.
const (
	EntityReadingProgress = "reading_progress"
	EntityBookshelf       = "bookshelf"
)

// Change operations.
const (
	OpUpsert = "upsert"
	OpDelete = "delete"
)

// Mask is returned by config endpoints in place of a stored secret, mirroring
// the runtime settings page behaviour.
const Mask = "********"

// Row is the full replicated state of a synced record. Entity-specific fields
// (current_page/progress/completed) are only meaningful for reading_progress.
type Row struct {
	CurrentPage int       `json:"current_page,omitempty"`
	Progress    float64   `json:"progress,omitempty"`
	Completed   bool      `json:"completed,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Change is a single replicated mutation: an upsert carries the row state at
// send time, a delete carries the tombstone timestamp used for LWW.
type Change struct {
	Entity    string     `json:"entity"`
	GalleryID int64      `json:"gallery_id"`
	Token     string     `json:"token"`
	Op        string     `json:"op"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	Row       *Row       `json:"row,omitempty"`
}

// Key identifies a synced record by its composite primary key.
type Key struct {
	GalleryID int64  `json:"gallery_id"`
	Token     string `json:"token"`
}

// PushRequest is the body of POST /api/sync/push.
//
// Cursor is the caller's last processed host outbox id. Snapshot asks the
// host to send its full table state instead of outbox deltas; the client sets
// it exactly once, until its first successful push.
type PushRequest struct {
	Cursor   int      `json:"cursor"`
	Snapshot bool     `json:"snapshot"`
	Changes  []Change `json:"changes"`
}

// PushResponse returns host-side changes after the caller's cursor.
//
// Cursor is the host's current outbox maximum; the caller replaces its stored
// cursor with it unconditionally (a lower value means the host was reset and
// the caller must wind back to avoid skipping future entries).
type PushResponse struct {
	Cursor  int      `json:"cursor"`
	Changes []Change `json:"changes"`
	Applied int      `json:"applied"`
	Skipped int      `json:"skipped"`
}
