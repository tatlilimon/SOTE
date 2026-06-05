package messages

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/storage"
)

type mockCrypto struct {
	keyPairs map[string]struct{ priv, pub []byte }
}

func newMockCrypto() *mockCrypto {
	return &mockCrypto{keyPairs: make(map[string]struct{ priv, pub []byte })}
}

func (m *mockCrypto) generateAndStore(username string) ([]byte, []byte, error) {
	keyRing, err := crypto.GenerateKey(username, "", "rsa", 2048)
	if err != nil {
		return nil, nil, err
	}
	priv, err := keyRing.Armor()
	if err != nil {
		return nil, nil, err
	}
	pub, err := keyRing.GetArmoredPublicKey()
	if err != nil {
		return nil, nil, err
	}
	m.keyPairs[username] = struct{ priv, pub []byte }{[]byte(priv), []byte(pub)}
	return []byte(priv), []byte(pub), nil
}

func (m *mockCrypto) HashPassword(password string) (string, error) {
	data := []byte(password)
	for i := 0; i < 21; i++ {
		h := sha256.Sum256(data)
		data = h[:]
	}
	return fmt.Sprintf("%x", data), nil
}

func (m *mockCrypto) VerifyPassword(hash, password string) error {
	computed, err := m.HashPassword(password)
	if err != nil {
		return err
	}
	if hash != computed {
		return errors.New("password mismatch")
	}
	return nil
}

func (m *mockCrypto) GenerateKeyPair() ([]byte, []byte, error) {
	keyRing, err := crypto.GenerateKey("test", "", "rsa", 2048)
	if err != nil {
		return nil, nil, err
	}
	priv, err := keyRing.Armor()
	if err != nil {
		return nil, nil, err
	}
	pub, err := keyRing.GetArmoredPublicKey()
	if err != nil {
		return nil, nil, err
	}
	return []byte(priv), []byte(pub), nil
}

func (m *mockCrypto) EncryptAES(data []byte, passphrase string) ([]byte, error) {
	key := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, data, nil), nil
}

func (m *mockCrypto) DecryptAES(data []byte, passphrase string) ([]byte, error) {
	key := sha256.Sum256([]byte(passphrase))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
}

func (m *mockCrypto) EncryptMessage(plaintext, publicKey []byte) ([]byte, error) {
	key, err := crypto.NewKeyFromArmored(string(publicKey))
	if err != nil {
		return nil, err
	}
	keyRing, err := crypto.NewKeyRing(key)
	if err != nil {
		return nil, err
	}
	enc, err := keyRing.Encrypt(crypto.NewPlainMessage(plaintext), nil)
	if err != nil {
		return nil, err
	}
	armored, _ := enc.GetArmored()
	return []byte(armored), nil
}

func (m *mockCrypto) DecryptMessage(ciphertext, privateKey []byte, passphrase string) (string, error) {
	decryptedKey, err := m.DecryptAES(privateKey, passphrase)
	if err != nil {
		return "", fmt.Errorf("decrypt private key: %w", err)
	}
	key, err := crypto.NewKeyFromArmored(string(decryptedKey))
	if err != nil {
		return "", err
	}
	keyRing, err := crypto.NewKeyRing(key)
	if err != nil {
		return "", err
	}
	msg, err := crypto.NewPGPMessageFromArmored(string(ciphertext))
	if err != nil {
		return "", err
	}
	dec, err := keyRing.Decrypt(msg, nil, 0)
	if err != nil {
		return "", err
	}
	return string(dec.GetBinary()), nil
}

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Initialize(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func seedContact(t *testing.T, db *sql.DB, owner, contactUsername, onion string, publicKey []byte) {
	t.Helper()
	err := storage.SaveContact(db, owner, contactUsername, onion, publicKey)
	require.NoError(t, err)
}

func seedUser(t *testing.T, db *sql.DB, username, password string) ([]byte, []byte) {
	t.Helper()
	mc := newMockCrypto()
	priv, pub, err := mc.generateAndStore(username)
	require.NoError(t, err)
	hashed, err := mc.HashPassword(password)
	require.NoError(t, err)
	aesPriv, err := mc.EncryptAES(priv, password)
	require.NoError(t, err)
	err = storage.SaveUser(db, username, hashed, aesPriv, pub, "onion-"+username+".onion", "/tmp/"+username+".torrc")
	require.NoError(t, err)
	return priv, pub
}

func TestSendMessage(t *testing.T) {
	db := setupTestDB(t)
	mc := newMockCrypto()

	alicePriv, _ := seedUser(t, db, "alice", "alicepass")
	_, bobPub := seedUser(t, db, "bob", "bobpass")
	seedContact(t, db, "alice", "bob", "bob.onion", bobPub)

	svc := NewService(db, mc)

	err := svc.SendMessage(context.Background(), "alice", "bob", "alicepass", "Hello Bob!")
	require.NoError(t, err)

	rows, err := storage.GetMessages(db, "alice", "bob")
	require.NoError(t, err)
	assert.Len(t, rows, 2, "expected 2 message rows (GPG + AES)")

	foundGPG, foundAES := false, false
	for _, row := range rows {
		assert.Greater(t, len(row.Message), 1, "message blob too short")
		switch row.Message[0] {
		case encTypeGPG:
			foundGPG = true
		case encTypeAES:
			foundAES = true
		}
	}
	assert.True(t, foundGPG, "missing GPG-encrypted row")
	assert.True(t, foundAES, "missing AES-encrypted row")

	for _, row := range rows {
		if row.Message[0] != encTypeAES {
			continue
		}
		plain, err := mc.DecryptAES(row.Message[1:], "alicepass")
		require.NoError(t, err)
		assert.Equal(t, "Hello Bob!", string(plain))
	}

	for _, row := range rows {
		if row.Message[0] != encTypeGPG {
			continue
		}
		bobUser, err := storage.GetUser(db, "bob")
		require.NoError(t, err)
		plain, err := mc.DecryptMessage(row.Message[1:], bobUser.PrivateKey, "bobpass")
		require.NoError(t, err)
		assert.Equal(t, "Hello Bob!", string(plain))
	}

	_ = alicePriv
}

func TestReceiveMessage(t *testing.T) {
	db := setupTestDB(t)
	mc := newMockCrypto()

	seedUser(t, db, "bob", "bobpass")
	seedUser(t, db, "alice", "alicepass")

	svc := NewService(db, mc)
	gpgEncrypted := []byte("-----BEGIN PGP MESSAGE-----\nsimulated\n-----END PGP MESSAGE-----")

	err := svc.ReceiveMessage(context.Background(), "alice", "bob", gpgEncrypted)
	require.NoError(t, err)

	rows, err := storage.GetMessages(db, "bob", "alice")
	require.NoError(t, err)
	assert.Len(t, rows, 1)

	assert.Equal(t, uint8(encTypeGPG), rows[0].Message[0])
	assert.Equal(t, gpgEncrypted, rows[0].Message[1:])
}

func TestFetchMessages(t *testing.T) {
	db := setupTestDB(t)
	mc := newMockCrypto()

	alicePriv, _ := seedUser(t, db, "alice", "alicepass")
	_, bobPub := seedUser(t, db, "bob", "bobpass")
	seedContact(t, db, "alice", "bob", "bob.onion", bobPub)

	svc := NewService(db, mc)

	err := svc.SendMessage(context.Background(), "alice", "bob", "alicepass", "Hi Bob")
	require.NoError(t, err)

	messages, err := svc.FetchMessages(context.Background(), "alice", "bob", "alicepass")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(messages), 1)

	hasContent := false
	for _, m := range messages {
		if m.Content == "Hi Bob" {
			hasContent = true
			assert.Equal(t, "alice", m.Sender)
			assert.Equal(t, "bob", m.Receiver)
		}
	}
	assert.True(t, hasContent, "expected to find decrypted 'Hi Bob' in messages")

	_ = alicePriv
}

func TestMessageTooLarge(t *testing.T) {
	db := setupTestDB(t)
	mc := newMockCrypto()
	svc := NewService(db, mc)

	big := strings.Repeat("x", MaxMessageSize+1)
	err := svc.SendMessage(context.Background(), "alice", "bob", "pwd", big)
	assert.ErrorIs(t, err, ErrMessageTooLarge)

	seedUser(t, db, "alice", "pwd")
	_, bobPub := seedUser(t, db, "bob", "pwd")
	seedContact(t, db, "alice", "bob", "bob.onion", bobPub)

	atLimit := strings.Repeat("y", MaxMessageSize)
	err = svc.SendMessage(context.Background(), "alice", "bob", "pwd", atLimit)
	assert.NoError(t, err)
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	db := setupTestDB(t)
	mc := newMockCrypto()

	alicePriv, _ := seedUser(t, db, "alice", "alicepass")
	_, bobPub := seedUser(t, db, "bob", "bobpass")
	seedContact(t, db, "alice", "bob", "bob.onion", bobPub)

	bobUser, err := storage.GetUser(db, "bob")
	require.NoError(t, err)
	seedContact(t, db, "bob", "alice", "alice.onion", bobUser.PublicKey)

	svc := NewService(db, mc)

	err = svc.SendMessage(context.Background(), "alice", "bob", "alicepass", "Secret roundtrip!")
	require.NoError(t, err)

	aliceMsgs, err := svc.FetchMessages(context.Background(), "alice", "bob", "alicepass")
	require.NoError(t, err)
	foundAlice := false
	for _, m := range aliceMsgs {
		if m.Content == "Secret roundtrip!" {
			foundAlice = true
		}
	}
	assert.True(t, foundAlice, "Alice should see her sent message")

	rows, err := storage.GetMessages(db, "alice", "bob")
	require.NoError(t, err)
	var gpgPayload []byte
	for _, row := range rows {
		if row.Message[0] == encTypeGPG {
			gpgPayload = row.Message[1:]
			break
		}
	}
	require.NotNil(t, gpgPayload, "should have a GPG copy")
	err = svc.ReceiveMessage(context.Background(), "alice", "bob", gpgPayload)
	require.NoError(t, err)

	bobMsgs, err := svc.FetchMessages(context.Background(), "bob", "alice", "bobpass")
	require.NoError(t, err)
	foundBob := false
	for _, m := range bobMsgs {
		if strings.Contains(m.Content, "Secret roundtrip!") {
			foundBob = true
		}
	}
	assert.True(t, foundBob, "Bob should see the received message")

	_ = alicePriv
}

func TestRetry(t *testing.T) {
	t.Run("succeeds on first attempt", func(t *testing.T) {
		ctx := context.Background()
		calls := 0
		err := retry(ctx, 3, func() error {
			calls++
			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, 1, calls)
	})

	t.Run("succeeds on third attempt", func(t *testing.T) {
		ctx := context.Background()
		calls := 0
		err := retry(ctx, 3, func() error {
			calls++
			if calls < 3 {
				return errors.New("fail")
			}
			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, 3, calls)
	})

	t.Run("exhausts retries", func(t *testing.T) {
		ctx := context.Background()
		calls := 0
		err := retry(ctx, 3, func() error {
			calls++
			return errors.New("persistent fail")
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "retry exhausted")
		assert.Equal(t, 3, calls)
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		errCh := make(chan error, 1)
		go func() {
			errCh <- retry(ctx, 5, func() error {
				calls++
				time.Sleep(50 * time.Millisecond)
				return errors.New("fail")
			})
		}()
		time.Sleep(80 * time.Millisecond)
		cancel()
		err := <-errCh
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestEmptyMessage(t *testing.T) {
	db := setupTestDB(t)
	mc := newMockCrypto()
	svc := NewService(db, mc)

	seedUser(t, db, "alice", "pw")
	_, bobPub := seedUser(t, db, "bob", "pw")
	seedContact(t, db, "alice", "bob", "bob.onion", bobPub)

	err := svc.SendMessage(context.Background(), "alice", "bob", "pw", "")
	require.NoError(t, err)

	msgs, err := svc.FetchMessages(context.Background(), "alice", "bob", "pw")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(msgs), 1)
}

func TestInvalidPrefixHandling(t *testing.T) {
	db := setupTestDB(t)
	mc := newMockCrypto()

	seedUser(t, db, "alice", "pw")
	err := storage.SaveMessage(db, "alice", "bob", []byte{0xFF, 0x00, 0x01})
	require.NoError(t, err)

	svc := NewService(db, mc)
	msgs, err := svc.FetchMessages(context.Background(), "alice", "bob", "pw")
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Contains(t, msgs[0].Content, "unknown encryption prefix")
}

func TestNewService(t *testing.T) {
	db := setupTestDB(t)
	mc := newMockCrypto()
	svc := NewService(db, mc)
	assert.NotNil(t, svc)
	assert.Equal(t, db, svc.db)
}

func Test_MaxMessageSize(t *testing.T) {
	assert.Equal(t, 64*1024, MaxMessageSize)
}
