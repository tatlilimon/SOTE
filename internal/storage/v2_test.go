package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// A database written by the previous version: schema v1 plus the
// contact_requests table the transport server created on demand.
func writeV1Database(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE schema_version (id INTEGER PRIMARY KEY AUTOINCREMENT, version INTEGER NOT NULL, applied_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	_, err = db.Exec(schemaV1)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO schema_version (version) VALUES (1)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE contact_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		from_username TEXT NOT NULL,
		from_onion TEXT NOT NULL,
		from_public_key BLOB NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO messages (sender, receiver, message) VALUES ('alice', 'bob', X'02AABB')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO contact_requests (from_username, from_onion, from_public_key) VALUES ('old', 'x.onion', X'00')`)
	require.NoError(t, err)
}

func TestMigrationV2UpgradesV1DatabaseInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	writeV1Database(t, path)

	db, err := Initialize(path)
	require.NoError(t, err)
	defer db.Close()

	var version int
	require.NoError(t, db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version))
	assert.Equal(t, 2, version)

	// Existing rows survive and gain the new columns with empty values.
	msgs, err := GetMessages(db, "alice", "bob")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, []byte{0x02, 0xAA, 0xBB}, msgs[0].Message)
	assert.Empty(t, msgs[0].MessageID)
	assert.Empty(t, msgs[0].Delivery)

	var owner, requestID string
	require.NoError(t, db.QueryRow(`SELECT owner, request_id FROM contact_requests WHERE from_username = 'old'`).Scan(&owner, &requestID))
	assert.Empty(t, owner)

	// Running the migration code again is a no-op.
	db.Close()
	db2, err := Initialize(path)
	require.NoError(t, err)
	defer db2.Close()
	var rows int
	require.NoError(t, db2.QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&rows))
	assert.Equal(t, 2, rows)
}

func TestDatabaseFileIsOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	db, err := Initialize(path)
	require.NoError(t, err)
	defer db.Close()
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestMemoryDatabasesAreIsolated(t *testing.T) {
	a := helperInit(t)
	b := helperInit(t)
	require.NoError(t, SaveUser(a, "alice", "h", nil, nil, "", ""))
	n, err := CountUsers(context.Background(), b)
	require.NoError(t, err)
	assert.Zero(t, n, "two :memory: databases must not share rows")
}

func TestContactsAreScopedToTheirOwner(t *testing.T) {
	db := helperInit(t)
	ctx := context.Background()
	require.NoError(t, SaveContact(db, "alice", "bob", "bob.onion", []byte("k1")))
	require.NoError(t, SaveContact(db, "carol", "bob", "other.onion", []byte("k2")))

	c, err := GetContact(ctx, db, "carol", "bob")
	require.NoError(t, err)
	assert.Equal(t, "other.onion", c.OnionAddress)

	c, err = GetContactByOnion(ctx, db, "alice", "bob.onion")
	require.NoError(t, err)
	assert.Equal(t, "bob", c.ContactUsername)
	_, err = GetContactByOnion(ctx, db, "alice", "other.onion")
	assert.ErrorIs(t, err, ErrContactNotFound)

	require.NoError(t, DeleteContact(ctx, db, "alice", c.ID))
	_, err = GetContact(ctx, db, "alice", "bob")
	assert.ErrorIs(t, err, ErrContactNotFound)
	assert.ErrorIs(t, DeleteContact(ctx, db, "carol", c.ID), ErrContactNotFound)
}

func TestMarkSeenDetectsReplays(t *testing.T) {
	db := helperInit(t)
	ctx := context.Background()
	fresh, err := MarkSeen(ctx, db, "alice", "bob.onion", "id-1")
	require.NoError(t, err)
	assert.True(t, fresh)
	fresh, err = MarkSeen(ctx, db, "alice", "bob.onion", "id-1")
	require.NoError(t, err)
	assert.False(t, fresh)
	// The same ID from another sender is a different message.
	fresh, err = MarkSeen(ctx, db, "alice", "carol.onion", "id-1")
	require.NoError(t, err)
	assert.True(t, fresh)
}

func TestContactRequestsDedupeAndCap(t *testing.T) {
	db := helperInit(t)
	ctx := context.Background()
	id1, err := UpsertContactRequest(ctx, db, ContactRequest{Owner: "alice", RequestID: "r1", FromUsername: "bob", FromOnion: "bob.onion", FromPublicKey: []byte("k")})
	require.NoError(t, err)
	id2, err := UpsertContactRequest(ctx, db, ContactRequest{Owner: "alice", RequestID: "r2", FromUsername: "bobby", FromOnion: "bob.onion", FromPublicKey: []byte("k2")})
	require.NoError(t, err)
	assert.Equal(t, id1, id2, "a second request from the same onion replaces the pending one")

	pending, err := PendingContactRequests(ctx, db, "alice")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, "bobby", pending[0].FromUsername)
	assert.Equal(t, "r2", pending[0].RequestID)

	require.NoError(t, SetContactRequestStatus(ctx, db, "alice", id1, RequestDeclined))
	pending, err = PendingContactRequests(ctx, db, "alice")
	require.NoError(t, err)
	assert.Empty(t, pending)
	assert.ErrorIs(t, SetContactRequestStatus(ctx, db, "mallory", id1, RequestAccepted), ErrRequestNotFound)

	for i := 0; i < MaxPendingRequests; i++ {
		_, err := UpsertContactRequest(ctx, db, ContactRequest{Owner: "alice", FromUsername: "x", FromPublicKey: []byte("k"), FromOnion: fmt.Sprintf("peer%03d.onion", i)})
		require.NoError(t, err)
	}
	_, err = UpsertContactRequest(ctx, db, ContactRequest{Owner: "alice", FromUsername: "x", FromPublicKey: []byte("k"), FromOnion: "overflow.onion"})
	assert.ErrorIs(t, err, ErrTooManyRequests)
}

func TestOutboxLifecycle(t *testing.T) {
	db := helperInit(t)
	ctx := context.Background()
	now := time.Now()
	id, err := EnqueueOutbox(ctx, db, OutboxItem{Owner: "alice", Kind: OutboxMessage, ToOnion: "bob.onion", Path: "/receive-message", Body: []byte("{}"), MessageID: "m1", NextAttemptAt: now.Unix()})
	require.NoError(t, err)

	due, err := DueOutbox(ctx, db, "alice", now, 10)
	require.NoError(t, err)
	require.Len(t, due, 1)

	require.NoError(t, RetryOutbox(ctx, db, id, now.Add(time.Hour), "offline"))
	due, err = DueOutbox(ctx, db, "alice", now, 10)
	require.NoError(t, err)
	assert.Empty(t, due, "a rescheduled item is not due yet")

	due, err = DueOutbox(ctx, db, "alice", now.Add(2*time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, due, 1)
	assert.Equal(t, 1, due[0].Attempts)
	assert.Equal(t, "offline", due[0].LastError)

	require.NoError(t, FinishOutbox(ctx, db, id, OutboxDelivered, ""))
	n, err := PendingOutboxCount(ctx, db, "alice")
	require.NoError(t, err)
	assert.Zero(t, n)
}
