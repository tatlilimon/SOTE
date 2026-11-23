package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// DBTX is satisfied by *sql.DB and *sql.Tx, so the same helpers work inside
// and outside a transaction.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// First byte of a stored message blob.
const (
	// BlobReceived is a PGP message addressed to the owner and signed by the
	// sender. Before v2 the sender also stored a copy of what it sent with this
	// prefix (encrypted to the receiver, so unreadable by the sender).
	BlobReceived byte = 0x01
	// BlobLegacySelfAES is the sender's own copy, AES-encrypted under the
	// account password. Written before v2; converted on password change.
	BlobLegacySelfAES byte = 0x02
	// BlobSelfGPG is the sender's own copy, encrypted to the owner's key.
	BlobSelfGPG byte = 0x03
)

// Delivery states of a sent message.
const (
	DeliveryPending   = "pending"
	DeliveryDelivered = "delivered"
	DeliveryRejected  = "rejected"
)

// Contact request states.
const (
	RequestPending  = "pending"
	RequestAccepted = "accepted"
	RequestDeclined = "declined"
)

// Outbox item kinds and states.
const (
	OutboxMessage        = "message"
	OutboxContactRequest = "contact_request"

	OutboxPending   = "pending"
	OutboxDelivered = "delivered"
	OutboxRejected  = "rejected"
)

// MaxPendingRequests caps how many unanswered contact requests are kept, so a
// stranger cannot fill the database through the onion service.
const MaxPendingRequests = 200

var (
	ErrTooManyRequests = errors.New("too many pending contact requests")
	ErrRequestNotFound = errors.New("contact request not found")
	ErrContactNotFound = errors.New("contact not found")
)

// ContactRequest is a request received from a peer.
type ContactRequest struct {
	ID            int64
	Owner         string
	RequestID     string
	FromUsername  string
	FromOnion     string
	FromPublicKey []byte
	Status        string
	CreatedAt     string
}

// OutboxItem is a request waiting to be delivered to a peer.
type OutboxItem struct {
	ID            int64
	Owner         string
	Kind          string
	ToOnion       string
	Path          string
	Body          []byte
	MessageID     string
	Attempts      int
	NextAttemptAt int64
	Status        string
	LastError     string
}

func runMigrationV2(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin v2: %w", err)
	}
	defer tx.Rollback()

	if err := addColumnIfMissing(tx, "messages", "message_id", "TEXT"); err != nil {
		return err
	}
	if err := addColumnIfMissing(tx, "messages", "delivery", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	// The transport server created this table on demand before v2; keep its
	// columns and add the ones v2 needs.
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS contact_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		from_username TEXT NOT NULL,
		from_onion TEXT NOT NULL,
		from_public_key BLOB NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("v2 contact_requests: %w", err)
	}
	if err := addColumnIfMissing(tx, "contact_requests", "owner", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addColumnIfMissing(tx, "contact_requests", "request_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS outbox (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			owner TEXT NOT NULL,
			kind TEXT NOT NULL,
			to_onion TEXT NOT NULL,
			path TEXT NOT NULL,
			body BLOB NOT NULL,
			message_id TEXT NOT NULL DEFAULT '',
			attempts INTEGER NOT NULL DEFAULT 0,
			next_attempt_at INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'pending',
			last_error TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			delivered_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS seen_messages (
			owner TEXT NOT NULL,
			from_onion TEXT NOT NULL,
			message_id TEXT NOT NULL,
			received_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (owner, from_onion, message_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_outbox_due ON outbox(owner, status, next_attempt_at)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_pair ON messages(sender, receiver)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_message_id ON messages(message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_contacts_onion ON contacts(username, contactOnionAddress)`,
		`CREATE INDEX IF NOT EXISTS idx_requests_onion ON contact_requests(owner, from_onion, status)`,
		`INSERT INTO schema_version (version) VALUES (2)`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("v2: %w", err)
		}
	}
	return tx.Commit()
}

func addColumnIfMissing(tx *sql.Tx, table, column, definition string) error {
	rows, err := tx.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	found := false
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return fmt.Errorf("inspect %s: %w", table, err)
		}
		if strings.EqualFold(name, column) {
			found = true
		}
	}
	rows.Close()
	if found {
		return nil
	}
	if _, err := tx.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

// restrictFilePermissions keeps the database (encrypted private key, message
// ciphertext and contact metadata) readable by the owner only.
func restrictFilePermissions(dbPath string) {
	if dbPath == ":memory:" || dbPath == "" {
		return
	}
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if _, err := os.Stat(p); err == nil {
			_ = os.Chmod(p, 0o600)
		}
	}
}

// ── users ────────────────────────────────────────────────────────────────────

// CountUsers returns the number of local accounts.
func CountUsers(ctx context.Context, q DBTX) (int, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM user`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// UpdateUserSecrets replaces the password hash and the encrypted private key.
func UpdateUserSecrets(ctx context.Context, q DBTX, username, passwordHash string, encryptedPrivateKey []byte) error {
	res, err := q.ExecContext(ctx, `UPDATE user SET password = ?, privateKey = ? WHERE username = ?`,
		passwordHash, encryptedPrivateKey, username)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("update user: %w", sql.ErrNoRows)
	}
	return nil
}

// UpdateEncryptedPrivateKey replaces only the encrypted private key.
func UpdateEncryptedPrivateKey(ctx context.Context, q DBTX, username string, encryptedPrivateKey []byte) error {
	_, err := q.ExecContext(ctx, `UPDATE user SET privateKey = ? WHERE username = ?`, encryptedPrivateKey, username)
	if err != nil {
		return fmt.Errorf("update private key: %w", err)
	}
	return nil
}

// SetUserOnion records the onion address the account is reachable at.
func SetUserOnion(ctx context.Context, q DBTX, username, onion string) error {
	_, err := q.ExecContext(ctx, `UPDATE user SET onionAddress = ? WHERE username = ?`, onion, username)
	if err != nil {
		return fmt.Errorf("set onion address: %w", err)
	}
	return nil
}

// ── contacts (always scoped to their owner) ─────────────────────────────────

func scanContact(row interface{ Scan(...any) error }) (*Contact, error) {
	c := &Contact{}
	if err := row.Scan(&c.ID, &c.Username, &c.ContactUsername, &c.OnionAddress, &c.PublicKey); err != nil {
		return nil, err
	}
	return c, nil
}

// GetContact returns the owner's contact with the given local name.
func GetContact(ctx context.Context, q DBTX, owner, contactUsername string) (*Contact, error) {
	c, err := scanContact(q.QueryRowContext(ctx, `SELECT id, username, contactUsername, contactOnionAddress, contactPublicKey
		FROM contacts WHERE username = ? AND contactUsername = ?`, owner, contactUsername))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrContactNotFound
	}
	return c, err
}

// GetContactByOnion returns the owner's contact reachable at the onion address.
func GetContactByOnion(ctx context.Context, q DBTX, owner, onion string) (*Contact, error) {
	c, err := scanContact(q.QueryRowContext(ctx, `SELECT id, username, contactUsername, contactOnionAddress, contactPublicKey
		FROM contacts WHERE username = ? AND contactOnionAddress = ? ORDER BY id LIMIT 1`, owner, onion))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrContactNotFound
	}
	return c, err
}

// DeleteContact removes one of the owner's contacts. Messages are kept.
func DeleteContact(ctx context.Context, q DBTX, owner string, id int) error {
	res, err := q.ExecContext(ctx, `DELETE FROM contacts WHERE username = ? AND id = ?`, owner, id)
	if err != nil {
		return fmt.Errorf("delete contact: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrContactNotFound
	}
	return nil
}

// ── messages ────────────────────────────────────────────────────────────────

// SaveMessageMeta inserts a message with its protocol ID and delivery state.
func SaveMessageMeta(ctx context.Context, q DBTX, sender, receiver string, blob []byte, messageID, delivery string) (int64, error) {
	res, err := q.ExecContext(ctx, `INSERT INTO messages (sender, receiver, message, timestamp, message_id, delivery)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?, ?)`, sender, receiver, blob, nullIfEmpty(messageID), delivery)
	if err != nil {
		return 0, fmt.Errorf("insert message: %w", err)
	}
	return res.LastInsertId()
}

// LegacySelfCopies returns the owner's sent messages still stored as AES blobs.
func LegacySelfCopies(ctx context.Context, q DBTX, owner string) ([]Message, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, sender, receiver, message, timestamp, COALESCE(message_id, ''), delivery
		FROM messages WHERE sender = ? AND substr(message, 1, 1) = ?`, owner, []byte{BlobLegacySelfAES})
	if err != nil {
		return nil, fmt.Errorf("query legacy copies: %w", err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Sender, &m.Receiver, &m.Message, &m.Timestamp, &m.MessageID, &m.Delivery); err != nil {
			return nil, fmt.Errorf("scan legacy copy: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpdateMessageBlob replaces a stored message blob.
func UpdateMessageBlob(ctx context.Context, q DBTX, id int, blob []byte) error {
	_, err := q.ExecContext(ctx, `UPDATE messages SET message = ? WHERE id = ?`, blob, id)
	if err != nil {
		return fmt.Errorf("update message: %w", err)
	}
	return nil
}

// SetDelivery records the delivery state of the sender's copy of a message.
func SetDelivery(ctx context.Context, q DBTX, sender, messageID, delivery string) error {
	_, err := q.ExecContext(ctx, `UPDATE messages SET delivery = ? WHERE sender = ? AND message_id = ?`,
		delivery, sender, messageID)
	if err != nil {
		return fmt.Errorf("set delivery: %w", err)
	}
	return nil
}

// MarkSeen records a received message ID. It reports false when the same
// sender already delivered this ID (a replay or a retried delivery).
func MarkSeen(ctx context.Context, q DBTX, owner, fromOnion, messageID string) (bool, error) {
	res, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO seen_messages (owner, from_onion, message_id) VALUES (?, ?, ?)`,
		owner, fromOnion, messageID)
	if err != nil {
		return false, fmt.Errorf("mark seen: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// ── contact requests ────────────────────────────────────────────────────────

// UpsertContactRequest stores a pending request. A newer request from the same
// onion address replaces the pending one instead of adding a duplicate.
func UpsertContactRequest(ctx context.Context, q DBTX, r ContactRequest) (int64, error) {
	var existing int64
	err := q.QueryRowContext(ctx, `SELECT id FROM contact_requests WHERE owner = ? AND from_onion = ? AND status = ?`,
		r.Owner, r.FromOnion, RequestPending).Scan(&existing)
	switch {
	case err == nil:
		_, err = q.ExecContext(ctx, `UPDATE contact_requests SET from_username = ?, from_public_key = ?, request_id = ?, created_at = CURRENT_TIMESTAMP WHERE id = ?`,
			r.FromUsername, r.FromPublicKey, r.RequestID, existing)
		if err != nil {
			return 0, fmt.Errorf("update contact request: %w", err)
		}
		return existing, nil
	case !errors.Is(err, sql.ErrNoRows):
		return 0, fmt.Errorf("find contact request: %w", err)
	}

	var pending int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM contact_requests WHERE owner = ? AND status = ?`,
		r.Owner, RequestPending).Scan(&pending); err != nil {
		return 0, fmt.Errorf("count contact requests: %w", err)
	}
	if pending >= MaxPendingRequests {
		return 0, ErrTooManyRequests
	}
	res, err := q.ExecContext(ctx, `INSERT INTO contact_requests (owner, request_id, from_username, from_onion, from_public_key, status)
		VALUES (?, ?, ?, ?, ?, ?)`, r.Owner, r.RequestID, r.FromUsername, r.FromOnion, r.FromPublicKey, RequestPending)
	if err != nil {
		return 0, fmt.Errorf("insert contact request: %w", err)
	}
	return res.LastInsertId()
}

func scanRequest(row interface{ Scan(...any) error }) (*ContactRequest, error) {
	r := &ContactRequest{}
	var created sql.NullString
	if err := row.Scan(&r.ID, &r.Owner, &r.RequestID, &r.FromUsername, &r.FromOnion, &r.FromPublicKey, &r.Status, &created); err != nil {
		return nil, err
	}
	r.CreatedAt = created.String
	return r, nil
}

const requestColumns = `id, owner, request_id, from_username, from_onion, from_public_key, status, created_at`

// PendingContactRequests lists the owner's unanswered requests, oldest first.
func PendingContactRequests(ctx context.Context, q DBTX, owner string) ([]ContactRequest, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+requestColumns+` FROM contact_requests WHERE owner = ? AND status = ? ORDER BY id`,
		owner, RequestPending)
	if err != nil {
		return nil, fmt.Errorf("query contact requests: %w", err)
	}
	defer rows.Close()
	var out []ContactRequest
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, fmt.Errorf("scan contact request: %w", err)
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// GetContactRequest returns one of the owner's requests.
func GetContactRequest(ctx context.Context, q DBTX, owner string, id int64) (*ContactRequest, error) {
	r, err := scanRequest(q.QueryRowContext(ctx, `SELECT `+requestColumns+` FROM contact_requests WHERE owner = ? AND id = ?`, owner, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRequestNotFound
	}
	return r, err
}

// SetContactRequestStatus records the owner's answer to a request.
func SetContactRequestStatus(ctx context.Context, q DBTX, owner string, id int64, status string) error {
	res, err := q.ExecContext(ctx, `UPDATE contact_requests SET status = ? WHERE owner = ? AND id = ?`, status, owner, id)
	if err != nil {
		return fmt.Errorf("update contact request: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrRequestNotFound
	}
	return nil
}

// ── outbox ──────────────────────────────────────────────────────────────────

// EnqueueOutbox stores a request for delivery.
func EnqueueOutbox(ctx context.Context, q DBTX, it OutboxItem) (int64, error) {
	res, err := q.ExecContext(ctx, `INSERT INTO outbox (owner, kind, to_onion, path, body, message_id, next_attempt_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, it.Owner, it.Kind, it.ToOnion, it.Path, it.Body, it.MessageID, it.NextAttemptAt, OutboxPending)
	if err != nil {
		return 0, fmt.Errorf("enqueue: %w", err)
	}
	return res.LastInsertId()
}

// DueOutbox returns pending items whose next attempt is due.
func DueOutbox(ctx context.Context, q DBTX, owner string, now time.Time, limit int) ([]OutboxItem, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, owner, kind, to_onion, path, body, message_id, attempts, next_attempt_at, status, last_error
		FROM outbox WHERE owner = ? AND status = ? AND next_attempt_at <= ? ORDER BY next_attempt_at, id LIMIT ?`,
		owner, OutboxPending, now.Unix(), limit)
	if err != nil {
		return nil, fmt.Errorf("query outbox: %w", err)
	}
	defer rows.Close()
	var out []OutboxItem
	for rows.Next() {
		var it OutboxItem
		if err := rows.Scan(&it.ID, &it.Owner, &it.Kind, &it.ToOnion, &it.Path, &it.Body, &it.MessageID,
			&it.Attempts, &it.NextAttemptAt, &it.Status, &it.LastError); err != nil {
			return nil, fmt.Errorf("scan outbox: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// FinishOutbox marks an item delivered or rejected.
func FinishOutbox(ctx context.Context, q DBTX, id int64, status, lastError string) error {
	_, err := q.ExecContext(ctx, `UPDATE outbox SET status = ?, last_error = ?, attempts = attempts + 1,
		delivered_at = CASE WHEN ? = 'delivered' THEN CURRENT_TIMESTAMP ELSE delivered_at END WHERE id = ?`,
		status, lastError, status, id)
	if err != nil {
		return fmt.Errorf("finish outbox: %w", err)
	}
	return nil
}

// RetryOutbox records a failed attempt and when to try again.
func RetryOutbox(ctx context.Context, q DBTX, id int64, next time.Time, lastError string) error {
	_, err := q.ExecContext(ctx, `UPDATE outbox SET attempts = attempts + 1, next_attempt_at = ?, last_error = ? WHERE id = ?`,
		next.Unix(), lastError, id)
	if err != nil {
		return fmt.Errorf("retry outbox: %w", err)
	}
	return nil
}

// PendingOutboxCount returns how many items still wait for delivery.
func PendingOutboxCount(ctx context.Context, q DBTX, owner string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE owner = ? AND status = ?`, owner, OutboxPending).Scan(&n)
	return n, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
