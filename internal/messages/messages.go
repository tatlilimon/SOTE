// Package messages handles message creation, encryption, sending, and receiving.
package messages

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"sote/internal/domain"
	"sote/internal/storage"
)

const (
	MaxMessageSize = 64 * 1024

	// 1-byte prefix in stored blobs so FetchMessages selects correct decryption.
	encTypeGPG = 0x01
	encTypeAES = 0x02
)

var (
	ErrMessageTooLarge = errors.New("message exceeds maximum size of 64KB")
	ErrInvalidPrefix   = errors.New("stored message has invalid encryption prefix")
)

// Service handles the message lifecycle: sending, receiving, and fetching.
type Service struct {
	db     *sql.DB
	crypto domain.CryptoService
}

// DecryptedMessage holds a fully decrypted message ready for display.
type DecryptedMessage struct {
	Sender    string
	Receiver  string
	Content   string
	Timestamp string
}

// NewService creates a new message Service.
func NewService(db *sql.DB, crypto domain.CryptoService) *Service {
	return &Service{db: db, crypto: crypto}
}

// SendMessage encrypts and persists an outgoing message.
//
// It performs two saves:
//  1. A GPG-encrypted copy (for the receiver) — prefixed with 0x01.
//  2. An AES-encrypted copy (for the sender's own sent-messages view) — prefixed with 0x02.
//
// Transport delivery is deferred to Task 13.
func (s *Service) SendMessage(ctx context.Context, sender, receiver, rawPassword, plaintext string) error {
	if len([]byte(plaintext)) > MaxMessageSize {
		return ErrMessageTooLarge
	}

	contact, err := storage.GetContactByUsername(s.db, receiver)
	if err != nil {
		return fmt.Errorf("lookup receiver public key: %w", err)
	}

	gpgCipher, err := s.crypto.EncryptMessage([]byte(plaintext), contact.PublicKey)
	if err != nil {
		return fmt.Errorf("gpg encrypt: %w", err)
	}

	aesCipher, err := s.crypto.EncryptAES([]byte(plaintext), rawPassword)
	if err != nil {
		return fmt.Errorf("aes encrypt: %w", err)
	}

	gpgBlob := append([]byte{encTypeGPG}, gpgCipher...)
	if err := storage.SaveMessage(s.db, sender, receiver, gpgBlob); err != nil {
		return fmt.Errorf("save gpg copy: %w", err)
	}

	aesBlob := append([]byte{encTypeAES}, aesCipher...)
	if err := storage.SaveMessage(s.db, sender, receiver, aesBlob); err != nil {
		return fmt.Errorf("save aes copy: %w", err)
	}

	// TODO(Task 13): send gpgCipher via transport
	_ = gpgCipher
	return nil
}

// ReceiveMessage stores an incoming GPG-encrypted message with 0x01 prefix.
func (s *Service) ReceiveMessage(ctx context.Context, sender, receiver string, encryptedContent []byte) error {
	blob := append([]byte{encTypeGPG}, encryptedContent...)
	if err := storage.SaveMessage(s.db, sender, receiver, blob); err != nil {
		return fmt.Errorf("save received message: %w", err)
	}
	return nil
}

// FetchMessages retrieves and decrypts the full conversation between two users.
//
// Messages are decrypted according to their stored encryption type:
//   - 0x01 (GPG): decrypted with the calling user's private key + password.
//   - 0x02 (AES): decrypted with the calling user's password.
func (s *Service) FetchMessages(ctx context.Context, username, otherUser, rawPassword string) ([]DecryptedMessage, error) {
	rows, err := storage.GetMessages(s.db, username, otherUser)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}

	user, err := storage.GetUser(s.db, username)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	out := make([]DecryptedMessage, 0, len(rows))
	for _, row := range rows {
		dm := DecryptedMessage{
			Sender:    row.Sender,
			Receiver:  row.Receiver,
			Timestamp: row.Timestamp,
		}

		if len(row.Message) < 1 {
			dm.Content = ""
			out = append(out, dm)
			continue
		}

		prefix := row.Message[0]
		payload := row.Message[1:]

		switch prefix {
		case encTypeGPG:
			plain, err := s.crypto.DecryptMessage(payload, user.PrivateKey, rawPassword)
			if err != nil {
				dm.Content = fmt.Sprintf("[decrypt error: %v]", err)
			} else {
				dm.Content = plain
			}

		case encTypeAES:
			plain, err := s.crypto.DecryptAES(payload, rawPassword)
			if err != nil {
				dm.Content = fmt.Sprintf("[decrypt error: %v]", err)
			} else {
				dm.Content = string(plain)
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
