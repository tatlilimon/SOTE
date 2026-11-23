package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helperInit opens an in-memory SQLite DB with WAL mode for tests.
func helperInit(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Initialize(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// TestWALMode verifies the database is opened in WAL journal mode.
// Uses file-based DB because in-memory SQLite uses "memory" journal mode.
func TestWALMode(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "wal-test.db")

	db, err := Initialize(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	require.NoError(t, err)
	assert.Equal(t, "wal", journalMode)
}

// TestSaveAndGetUser verifies user CRUD: save then retrieve.
func TestSaveAndGetUser(t *testing.T) {
	db := helperInit(t)

	privKey := []byte("private-key-data")
	pubKey := []byte("public-key-data")

	err := SaveUser(db, "alice", "secret123", privKey, pubKey, "alice.onion", "/etc/tor/torrc")
	require.NoError(t, err)

	user, err := GetUser(db, "alice")
	require.NoError(t, err)
	assert.Equal(t, "alice", user.Username)
	assert.Equal(t, "secret123", user.Password)
	assert.Equal(t, privKey, user.PrivateKey)
	assert.Equal(t, pubKey, user.PublicKey)
	assert.Equal(t, "alice.onion", user.OnionAddress)
	assert.Equal(t, "/etc/tor/torrc", user.TorrcFilePath)
}

// TestGetUserNotFound verifies error for nonexistent user.
func TestGetUserNotFound(t *testing.T) {
	db := helperInit(t)

	_, err := GetUser(db, "nonexistent")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

// TestSaveContact verifies saving a contact succeeds.
func TestSaveContact(t *testing.T) {
	db := helperInit(t)

	err := SaveContact(db, "alice", "bob", "bob.onion", []byte("bob-pubkey"))
	require.NoError(t, err)

	contacts, err := GetAllContacts(db, "alice")
	require.NoError(t, err)
	require.Len(t, contacts, 1)
	assert.Equal(t, "bob", contacts[0].ContactUsername)
	assert.Equal(t, "bob.onion", contacts[0].OnionAddress)
	assert.Equal(t, []byte("bob-pubkey"), contacts[0].PublicKey)
}

// TestDuplicateContact verifies saving a duplicate contact returns an error.
func TestDuplicateContact(t *testing.T) {
	db := helperInit(t)

	err := SaveContact(db, "alice", "bob", "bob.onion", []byte("bob-pubkey"))
	require.NoError(t, err)

	// Attempt duplicate — must return an error, not silently return nil.
	err = SaveContact(db, "alice", "bob", "bob.onion", []byte("bob-pubkey"))
	assert.Error(t, err, "duplicate contact should return an error")
}

// TestSaveContactSelfAdd verifies that adding yourself as a contact returns an error.
func TestSaveContactSelfAdd(t *testing.T) {
	db := helperInit(t)

	err := SaveContact(db, "alice", "alice", "alice.onion", []byte("alice-pubkey"))
	assert.Error(t, err, "self-add should return an error")
}

// TestGetAllContactsOwnerFilter verifies contacts are filtered by owner username.
func TestGetAllContactsOwnerFilter(t *testing.T) {
	db := helperInit(t)

	// Alice has contacts bob and charlie
	require.NoError(t, SaveContact(db, "alice", "bob", "b.onion", []byte("bk")))
	require.NoError(t, SaveContact(db, "alice", "charlie", "c.onion", []byte("ck")))

	// Dave has contact eve (different owner)
	require.NoError(t, SaveContact(db, "dave", "eve", "e.onion", []byte("ek")))

	// Alice should only see her 2 contacts
	aliceContacts, err := GetAllContacts(db, "alice")
	require.NoError(t, err)
	assert.Len(t, aliceContacts, 2)

	// Dave should only see his 1 contact
	daveContacts, err := GetAllContacts(db, "dave")
	require.NoError(t, err)
	assert.Len(t, daveContacts, 1)
	assert.Equal(t, "eve", daveContacts[0].ContactUsername)
}

// TestSaveAndGetMessages verifies message CRUD.
func TestSaveAndGetMessages(t *testing.T) {
	db := helperInit(t)

	err := SaveMessage(db, "alice", "bob", []byte("Hello Bob!"))
	require.NoError(t, err)

	err = SaveMessage(db, "bob", "alice", []byte("Hi Alice!"))
	require.NoError(t, err)

	err = SaveMessage(db, "alice", "charlie", []byte("Hey Charlie"))
	require.NoError(t, err)

	// Get alice<->bob conversation
	msgs, err := GetMessages(db, "alice", "bob")
	require.NoError(t, err)
	assert.Len(t, msgs, 2)

	// Verify both messages are present
	senders := map[string]bool{}
	for _, m := range msgs {
		senders[m.Sender] = true
	}
	assert.True(t, senders["alice"])
	assert.True(t, senders["bob"])

	// alice<->charlie should have 1 message
	msgsAC, err := GetMessages(db, "alice", "charlie")
	require.NoError(t, err)
	assert.Len(t, msgsAC, 1)
	assert.Equal(t, "alice", msgsAC[0].Sender)
}

// TestMigration verifies the migration system works.
func TestMigration(t *testing.T) {
	db := helperInit(t)

	// Verify schema_version table exists with correct version
	var version int
	err := db.QueryRow("SELECT version FROM schema_version ORDER BY version DESC LIMIT 1").Scan(&version)
	require.NoError(t, err)
	assert.Equal(t, 2, version)

	// Verify all 3 tables exist
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('user', 'contacts', 'messages')").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

// TestFileBasedDB verifies that a file-based DB can be created and cleaned up.
func TestFileBasedDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Initialize(dbPath)
	require.NoError(t, err)
	defer db.Close()

	// Verify the file exists
	_, err = os.Stat(dbPath)
	require.NoError(t, err, "database file should exist on disk")

	// Verify WAL mode on file-based DB too
	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	require.NoError(t, err)
	assert.Equal(t, "wal", journalMode)
}

// TestUserUniqueness verifies duplicate username returns error.
func TestUserUniqueness(t *testing.T) {
	db := helperInit(t)

	err := SaveUser(db, "alice", "pass1", nil, nil, "", "")
	require.NoError(t, err)

	err = SaveUser(db, "alice", "pass2", nil, nil, "", "")
	assert.Error(t, err, "duplicate username should return UNIQUE constraint error")
}
