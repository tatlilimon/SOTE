package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

// Known error for duplicate or self-add contacts.
var (
	ErrContactExists      = errors.New("contact already exists")
	ErrSelfAdd            = errors.New("cannot add yourself as a contact")
	ErrMigrationFailed    = errors.New("migration failed")
)

// User represents a row in the user table.
type User struct {
	ID            int
	Username      string
	Password      string
	PrivateKey    []byte
	PublicKey     []byte
	OnionAddress  string
	TorrcFilePath string
}

// Contact represents a row in the contacts table.
type Contact struct {
	ID              int
	Username        string
	ContactUsername string
	OnionAddress    string
	PublicKey       []byte
}

// Message represents a row in the messages table.
type Message struct {
	ID        int
	Sender    string
	Receiver  string
	Message   []byte
	Timestamp string
	// MessageID is the protocol ID (empty for rows written before v2).
	MessageID string
	// Delivery is the delivery state of the sender's own copy, or empty.
	Delivery string
}

const schemaV1 = `
CREATE TABLE IF NOT EXISTS user (
    "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    "username" TEXT UNIQUE,
    "password" TEXT,
    "privateKey" BLOB,
    "publicKey" BLOB,
    "onionAddress" TEXT,
    "torrcFilePath" TEXT
);

CREATE TABLE IF NOT EXISTS contacts (
    "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    "username" TEXT,
    "contactUsername" TEXT,
    "contactOnionAddress" TEXT,
    "contactPublicKey" BLOB
);

CREATE TABLE IF NOT EXISTS messages (
    "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    "sender" TEXT,
    "receiver" TEXT,
    "message" BLOB,
    "timestamp" DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

// Initialize opens or creates the SQLite database at dbPath, applies WAL mode,
// sets writer limits, and runs migrations. Use ":memory:" for in-memory DB.
func Initialize(dbPath string) (*sql.DB, error) {
	dsn := buildDSN(dbPath)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}

	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("db.Ping: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	restrictFilePermissions(dbPath)

	return db, nil
}

var memoryDBCounter atomic.Int64

func buildDSN(dbPath string) string {
	if dbPath == ":memory:" {
		// A distinct name per call: every Initialize(":memory:") gets its own
		// database, shared only by that pool's connections. A single shared
		// "file::memory:" leaked rows between tests and between simulated peers.
		name := fmt.Sprintf("sote-mem-%d-%d", os.Getpid(), memoryDBCounter.Add(1))
		return "file:" + name + "?mode=memory&cache=shared" +
			"&_pragma=journal_mode(WAL)" +
			"&_pragma=busy_timeout(5000)" +
			"&_txlock=immediate"
	}
	return "file:" + dbPath +
		"?cache=shared" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_txlock=immediate"
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version INTEGER NOT NULL,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var current int
	err = db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&current)
	if err != nil {
		return fmt.Errorf("query schema_version: %w", err)
	}

	if current < 1 {
		if err := runMigrationV1(db); err != nil {
			return err
		}
	}
	if current < 2 {
		if err := runMigrationV2(db); err != nil {
			return err
		}
	}

	return nil
}

func runMigrationV1(db *sql.DB) error {
	_, err := db.Exec(schemaV1)
	if err != nil {
		return fmt.Errorf("exec v1 schema: %w", err)
	}

	_, err = db.Exec("INSERT INTO schema_version (version) VALUES (1)")
	if err != nil {
		return fmt.Errorf("record v1: %w", err)
	}

	return nil
}

// SaveUser inserts a new user. Returns an error if the username already exists.
func SaveUser(db *sql.DB, username, password string, privateKey, publicKey []byte, onionAddress, torrcFilePath string) error {
	stmt, err := db.Prepare(`INSERT INTO user (username, password, privateKey, publicKey, onionAddress, torrcFilePath)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert user: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(username, password, privateKey, publicKey, onionAddress, torrcFilePath)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

// GetUser retrieves a user by username. Returns sql.ErrNoRows if not found.
func GetUser(db *sql.DB, username string) (*User, error) {
	stmt, err := db.Prepare(`SELECT id, username, password, privateKey, publicKey, onionAddress, torrcFilePath
		FROM user WHERE username = ?`)
	if err != nil {
		return nil, fmt.Errorf("prepare get user: %w", err)
	}
	defer stmt.Close()

	u := &User{}
	err = stmt.QueryRow(username).Scan(
		&u.ID, &u.Username, &u.Password,
		&u.PrivateKey, &u.PublicKey,
		&u.OnionAddress, &u.TorrcFilePath,
	)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// SaveContact adds a contact for the given owner. Returns ErrSelfAdd if owner
// tries to add themselves, or ErrContactExists if the contact already exists.
func SaveContact(db *sql.DB, username, contactUsername, contactOnionAddress string, contactPublicKey []byte) error {
	if username == contactUsername {
		return ErrSelfAdd
	}

	exists, err := contactExists(db, username, contactUsername)
	if err != nil {
		return fmt.Errorf("check contact exists: %w", err)
	}
	if exists {
		return ErrContactExists
	}

	stmt, err := db.Prepare(`INSERT INTO contacts (username, contactUsername, contactOnionAddress, contactPublicKey)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert contact: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(username, contactUsername, contactOnionAddress, contactPublicKey)
	if err != nil {
		return fmt.Errorf("insert contact: %w", err)
	}
	return nil
}

func contactExists(db *sql.DB, owner, contactUsername string) (bool, error) {
	stmt, err := db.Prepare(`SELECT COUNT(*) FROM contacts WHERE username = ? AND contactUsername = ?`)
	if err != nil {
		return false, fmt.Errorf("prepare check contact: %w", err)
	}
	defer stmt.Close()

	var count int
	err = stmt.QueryRow(owner, contactUsername).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetAllContacts returns all contacts owned by the given username.
func GetAllContacts(db *sql.DB, username string) ([]Contact, error) {
	stmt, err := db.Prepare(`SELECT id, username, contactUsername, contactOnionAddress, contactPublicKey
		FROM contacts WHERE username = ?`)
	if err != nil {
		return nil, fmt.Errorf("prepare get contacts: %w", err)
	}
	defer stmt.Close()

	rows, err := stmt.Query(username)
	if err != nil {
		return nil, fmt.Errorf("query contacts: %w", err)
	}
	defer rows.Close()

	var contacts []Contact
	for rows.Next() {
		var c Contact
		err := rows.Scan(&c.ID, &c.Username, &c.ContactUsername, &c.OnionAddress, &c.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("scan contact: %w", err)
		}
		contacts = append(contacts, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return contacts, nil
}

// SaveMessage inserts a message with the current timestamp.
func SaveMessage(db *sql.DB, sender, receiver string, message []byte) error {
	stmt, err := db.Prepare(`INSERT INTO messages (sender, receiver, message, timestamp)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)`)
	if err != nil {
		return fmt.Errorf("prepare insert message: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(sender, receiver, message)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

// GetMessages retrieves the conversation between two users (bidirectional).
func GetMessages(db *sql.DB, sender, receiver string) ([]Message, error) {
	stmt, err := db.Prepare(`SELECT id, sender, receiver, message, timestamp, COALESCE(message_id, ''), delivery FROM messages
		WHERE (sender = ? AND receiver = ?) OR (sender = ? AND receiver = ?)
		ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("prepare get messages: %w", err)
	}
	defer stmt.Close()

	rows, err := stmt.Query(sender, receiver, receiver, sender)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		err := rows.Scan(&m.ID, &m.Sender, &m.Receiver, &m.Message, &m.Timestamp, &m.MessageID, &m.Delivery)
		if err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return messages, nil
}

// GetContactByUsername looks up a contact by their username within all contacts.
// Deprecated: it ignores the owner; use GetContact.
func GetContactByUsername(db *sql.DB, contactUsername string) (*Contact, error) {
	stmt, err := db.Prepare(`SELECT id, username, contactUsername, contactOnionAddress, contactPublicKey
		FROM contacts WHERE contactUsername = ?`)
	if err != nil {
		return nil, fmt.Errorf("prepare get contact by username: %w", err)
	}
	defer stmt.Close()

	c := &Contact{}
	err = stmt.QueryRow(contactUsername).Scan(
		&c.ID, &c.Username, &c.ContactUsername,
		&c.OnionAddress, &c.PublicKey,
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// DSN returns the full DSN for the given database path (useful for connection reuse).
func DSN(dbPath string) string {
	return buildDSN(dbPath)
}


