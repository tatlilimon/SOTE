// Package messages handles message creation, encryption, queuing for
// delivery, verification of received messages, and reading conversations.
package messages

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"sote/internal/crypto"
	"sote/internal/domain"
	"sote/internal/peer"
	"sote/internal/storage"
)

const (
	MaxMessageSize = 64 * 1024

	// Kept for readers of older code; the values live in storage.
	encTypeGPG = storage.BlobReceived
	encTypeAES = storage.BlobLegacySelfAES
)

var (
	ErrMessageTooLarge = errors.New("message exceeds maximum size of 64KB")
	ErrEmptyMessage    = errors.New("message is empty")
	ErrInvalidPrefix   = errors.New("stored message has invalid encryption prefix")
	ErrNoLocalOnion    = errors.New("your onion address is not known yet; wait until Tor has started")
	ErrUnknownContact  = errors.New("unknown contact")
	ErrNoUnlock        = errors.New("crypto service cannot unlock private keys")
)

// unlocker is implemented by *crypto.Service.
type unlocker interface {
	Unlock(encryptedPrivateKey []byte, passphrase string) (*crypto.Identity, error)
}

// Service handles the message lifecycle: sending, receiving, and fetching.
type Service struct {
	db     *sql.DB
	crypto domain.CryptoService

	localOnion func() string
	onQueued   func()
	idSource   func(username string) *crypto.Identity
	now        func() time.Time
}

// DecryptedMessage holds a fully decrypted message ready for display.
type DecryptedMessage struct {
	Sender    string
	Receiver  string
	Content   string
	Timestamp string
	// MessageID is the protocol ID (empty for messages stored before v2).
	MessageID string
	// Delivery is "pending", "delivered" or "rejected" for messages the user
	// sent, empty otherwise.
	Delivery string
	// Verified is true when the content is known to come from the shown
	// sender: the user's own copies, and received messages whose signature
	// matched the contact's key.
	Verified bool
}

// NewService creates a new message Service.
func NewService(db *sql.DB, crypto domain.CryptoService) *Service {
	return &Service{db: db, crypto: crypto, now: time.Now}
}

// SetLocalOnion tells the service how to find the node's own onion address,
// which is part of every outgoing envelope.
func (s *Service) SetLocalOnion(f func() string) { s.localOnion = f }

// SetIdentitySource lets the service reuse an identity unlocked at login
// instead of deriving the key from the password for every operation. The
// source returns nil when it has no identity for that user.
func (s *Service) SetIdentitySource(f func(username string) *crypto.Identity) { s.idSource = f }

// SetOnQueued registers a callback run after a message is queued, so the
// delivery worker can start right away instead of on its next tick.
func (s *Service) SetOnQueued(f func()) { s.onQueued = f }

// identity returns the user's unlocked identity and a release function.
func (s *Service) identity(username, rawPassword string) (*crypto.Identity, func(), error) {
	if s.idSource != nil {
		if id := s.idSource(username); id != nil {
			return id, func() {}, nil
		}
	}
	u, ok := s.crypto.(unlocker)
	if !ok {
		return nil, nil, ErrNoUnlock
	}
	user, err := storage.GetUser(s.db, username)
	if err != nil {
		return nil, nil, fmt.Errorf("get user: %w", err)
	}
	id, err := u.Unlock(user.PrivateKey, rawPassword)
	if err != nil {
		return nil, nil, err
	}
	return id, id.Close, nil
}

// SendMessage encrypts a message for the receiver, signs it, stores the
// sender's own copy (encrypted to the sender's key) and queues the envelope
// for delivery over Tor. Delivery happens in the background; the returned
// error covers only local work.
func (s *Service) SendMessage(ctx context.Context, sender, receiver, rawPassword, plaintext string) error {
	if len(plaintext) == 0 {
		return ErrEmptyMessage
	}
	if len([]byte(plaintext)) > MaxMessageSize {
		return ErrMessageTooLarge
	}

	contact, err := storage.GetContact(ctx, s.db, sender, receiver)
	if err != nil {
		if errors.Is(err, storage.ErrContactNotFound) {
			return fmt.Errorf("%w: %s", ErrUnknownContact, receiver)
		}
		return fmt.Errorf("lookup receiver: %w", err)
	}
	from := ""
	if s.localOnion != nil {
		from = s.localOnion()
	}
	if from == "" {
		return ErrNoLocalOnion
	}

	id, release, err := s.identity(sender, rawPassword)
	if err != nil {
		return err
	}
	defer release()

	payload, err := id.EncryptFor(contact.PublicKey, []byte(plaintext))
	if err != nil {
		return fmt.Errorf("encrypt for %s: %w", receiver, err)
	}
	env := peer.Envelope{
		Version:   peer.ProtocolVersion,
		MessageID: peer.NewID(),
		FromOnion: from,
		ToOnion:   contact.OnionAddress,
		SentAt:    s.now().Unix(),
		Payload:   payload,
	}
	if env.Signature, err = id.SignDetached(env.SigningBytes()); err != nil {
		return err
	}
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	selfCopy, err := id.EncryptToSelf([]byte(plaintext))
	if err != nil {
		return fmt.Errorf("encrypt own copy: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := storage.SaveMessageMeta(ctx, tx, sender, receiver,
		append([]byte{storage.BlobSelfGPG}, selfCopy...), env.MessageID, storage.DeliveryPending); err != nil {
		return err
	}
	if _, err := storage.EnqueueOutbox(ctx, tx, storage.OutboxItem{
		Owner: sender, Kind: storage.OutboxMessage, ToOnion: contact.OnionAddress,
		Path: peer.PathMessage, Body: body, MessageID: env.MessageID, NextAttemptAt: s.now().Unix(),
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.onQueued != nil {
		s.onQueued()
	}
	return nil
}

// AcceptEnvelope authenticates a message received from a peer and stores it.
// It checks the envelope structure, that it is addressed to ownOnion, that
// the sender's onion address belongs to one of owner's contacts, and the
// detached signature against that contact's key; a message ID seen before
// from the same sender is acknowledged without being stored again.
// Returns the contact's local name when a new message was stored.
func (s *Service) AcceptEnvelope(ctx context.Context, owner, ownOnion string, env *peer.Envelope) (string, bool, error) {
	if err := env.Validate(s.now()); err != nil {
		return "", false, err
	}
	if env.ToOnion != ownOnion {
		return "", false, peer.ErrWrongRecipient
	}
	contact, err := storage.GetContactByOnion(ctx, s.db, owner, env.FromOnion)
	if err != nil {
		if errors.Is(err, storage.ErrContactNotFound) {
			return "", false, peer.ErrUnknownSender
		}
		return "", false, err
	}
	if err := crypto.VerifyDetached(contact.PublicKey, env.SigningBytes(), env.Signature); err != nil {
		return "", false, peer.ErrBadSignature
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	fresh, err := storage.MarkSeen(ctx, tx, owner, env.FromOnion, env.MessageID)
	if err != nil {
		return "", false, err
	}
	if fresh {
		if _, err := storage.SaveMessageMeta(ctx, tx, contact.ContactUsername, owner,
			append([]byte{storage.BlobReceived}, env.Payload...), env.MessageID, ""); err != nil {
			return "", false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return contact.ContactUsername, fresh, nil
}

// FetchMessages retrieves and decrypts the conversation between username and
// otherUser. The private key is unlocked once for the whole conversation.
func (s *Service) FetchMessages(ctx context.Context, username, otherUser, rawPassword string) ([]DecryptedMessage, error) {
	rows, err := storage.GetMessages(s.db, username, otherUser)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	if len(rows) == 0 {
		return []DecryptedMessage{}, nil
	}

	id, release, err := s.identity(username, rawPassword)
	if err != nil {
		return nil, err
	}
	defer release()

	var contactKey []byte
	if c, err := storage.GetContact(ctx, s.db, username, otherUser); err == nil {
		contactKey = c.PublicKey
	}

	out := make([]DecryptedMessage, 0, len(rows))
	for _, row := range rows {
		dm := DecryptedMessage{
			Sender:    row.Sender,
			Receiver:  row.Receiver,
			Timestamp: row.Timestamp,
			MessageID: row.MessageID,
			Delivery:  row.Delivery,
		}
		if len(row.Message) < 1 {
			continue
		}
		prefix, payload := row.Message[0], row.Message[1:]

		switch prefix {
		case storage.BlobReceived:
			if row.Sender == username {
				// Before v2 the sender stored the copy it would deliver,
				// encrypted to the receiver; the sender cannot read it and
				// the readable copy is stored next to it.
				continue
			}
			plain, err := id.Decrypt(string(payload), contactKey)
			switch {
			case err == nil:
				dm.Content, dm.Verified = string(plain), len(contactKey) > 0
			case errors.Is(err, crypto.ErrBadSignature):
				// Never show text whose author cannot be confirmed.
				dm.Content = "[message hidden: its signature does not match this contact]"
			default:
				dm.Content = "[message could not be decrypted]"
			}

		case storage.BlobLegacySelfAES:
			plain, err := s.crypto.DecryptAES(payload, rawPassword)
			if err != nil {
				dm.Content = "[message could not be decrypted]"
			} else {
				dm.Content, dm.Verified = string(plain), true
			}

		case storage.BlobSelfGPG:
			plain, err := id.Decrypt(string(payload), nil)
			if err != nil {
				dm.Content = "[message could not be decrypted]"
			} else {
				dm.Content, dm.Verified = string(plain), true
			}

		default:
			dm.Content = fmt.Sprintf("[unknown encryption prefix: 0x%02x]", prefix)
		}

		out = append(out, dm)
	}

	return out, nil
}

func retry(ctx context.Context, maxAttempts int, fn func() error) error {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		if attempt < maxAttempts {
			backoff := time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	return fmt.Errorf("retry exhausted after %d attempts: %w", maxAttempts, lastErr)
}
