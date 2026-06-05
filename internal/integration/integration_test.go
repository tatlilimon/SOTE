//go:build integration

package integration

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/pbkdf2"

	"sote/internal/auth"
	"sote/internal/contacts"
	"sote/internal/crypto"
	"sote/internal/domain"
	"sote/internal/messages"
	"sote/internal/storage"
)

func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Initialize(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// decryptAuthFormat decrypts data produced by auth.encryptAES, which uses a
// 16-byte salt prefix (unlike crypto.Service.EncryptAES which uses 32 bytes).
func decryptAuthFormat(data []byte, passphrase string) ([]byte, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("ciphertext too short: missing salt")
	}
	salt := data[:16]
	key := pbkdf2.Key([]byte(passphrase), salt, 100000, 32, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	if len(data) < 16+gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short: missing nonce")
	}
	nonce := data[16 : 16+gcm.NonceSize()]
	ciphertext := data[16+gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

// reEncryptPrivateKey fixes the private key format for FetchMessages
// compatibility: auth.Register encrypts with a 16-byte salt, but
// messages.Service.FetchMessages → crypto.DecryptMessage → crypto.DecryptAES
// expects a 32-byte salt prefix.
func reEncryptPrivateKey(t *testing.T, db *sql.DB, cryptoSvc *crypto.Service, username, password string) {
	t.Helper()
	user, err := storage.GetUser(db, username)
	require.NoError(t, err)
	privKey, err := decryptAuthFormat(user.PrivateKey, password)
	require.NoError(t, err, "decrypt private key in auth format")
	newEncrypted, err := cryptoSvc.EncryptAES(privKey, password)
	require.NoError(t, err, "re-encrypt private key in crypto format")
	_, err = db.Exec("UPDATE user SET privateKey = ? WHERE username = ?", newEncrypted, username)
	require.NoError(t, err)
}

func TestFullMessageRoundtrip(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	authSvc := auth.New(db)
	cryptoSvc := crypto.New()
	contactSvc := contacts.NewService(db)
	msgSvc := messages.NewService(db, cryptoSvc)

	alice, err := authSvc.Register(ctx, "alice", "alicepass123")
	require.NoError(t, err)
	require.NotEmpty(t, alice.PublicKey)

	bob, err := authSvc.Register(ctx, "bob", "bobpass456")
	require.NoError(t, err)
	require.NotEmpty(t, bob.PublicKey)

	reEncryptPrivateKey(t, db, cryptoSvc, "alice", "alicepass123")
	reEncryptPrivateKey(t, db, cryptoSvc, "bob", "bobpass456")

	err = contactSvc.AddContact(ctx, "alice", domain.Contact{
		Username:     "bob",
		OnionAddress: "bob.onion",
		PublicKey:    bob.PublicKey,
	})
	require.NoError(t, err)

	err = contactSvc.AddContact(ctx, "bob", domain.Contact{
		Username:     "alice",
		OnionAddress: "alice.onion",
		PublicKey:    alice.PublicKey,
	})
	require.NoError(t, err)

	plaintext := "Hello Bob, this is a secret GPG-encrypted message from Alice!"
	err = msgSvc.SendMessage(ctx, "alice", "bob", "alicepass123", plaintext)
	require.NoError(t, err)

	bobMsgs, err := msgSvc.FetchMessages(ctx, "bob", "alice", "bobpass456")
	require.NoError(t, err)
	require.NotEmpty(t, bobMsgs)

	found := false
	for _, m := range bobMsgs {
		if m.Content == plaintext {
			found = true
			assert.Equal(t, "alice", m.Sender)
			assert.Equal(t, "bob", m.Receiver)
			assert.NotEmpty(t, m.Timestamp)
			break
		}
	}
	assert.True(t, found, "Bob should decrypt Alice's GPG-encrypted message")

	aliceMsgs, err := msgSvc.FetchMessages(ctx, "alice", "bob", "alicepass123")
	require.NoError(t, err)
	require.NotEmpty(t, aliceMsgs)

	aliceFound := false
	for _, m := range aliceMsgs {
		if m.Content == plaintext {
			aliceFound = true
			break
		}
	}
	assert.True(t, aliceFound, "Alice should decrypt her own message (AES self-copy)")
}

func TestContactRequestFlow(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	contactSvc := contacts.NewService(db)

	alice, err := auth.New(db).Register(ctx, "alice", "alicepass123")
	require.NoError(t, err)
	bob, err := auth.New(db).Register(ctx, "bob", "bobpass456")
	require.NoError(t, err)

	err = contactSvc.AddContact(ctx, "alice", domain.Contact{
		Username:     "bob",
		OnionAddress: "bob.onion",
		PublicKey:    bob.PublicKey,
	})
	require.NoError(t, err)

	aliceContacts, err := contactSvc.GetContacts(ctx, "alice")
	require.NoError(t, err)
	require.Len(t, aliceContacts, 1)
	assert.Equal(t, "bob", aliceContacts[0].ContactUsername)

	ownerInfo, err := contactSvc.AcceptContactRequest(ctx, "bob", domain.Contact{
		Username:     "alice",
		OnionAddress: "alice.onion",
		PublicKey:    alice.PublicKey,
	})
	require.NoError(t, err)
	require.NotNil(t, ownerInfo)
	assert.Equal(t, "bob", ownerInfo.Username)
	assert.NotEmpty(t, ownerInfo.PublicKey)

	bobContacts, err := contactSvc.GetContacts(ctx, "bob")
	require.NoError(t, err)
	require.Len(t, bobContacts, 1)
	assert.Equal(t, "alice", bobContacts[0].ContactUsername)
}

func TestConcurrentMessages(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	authSvc := auth.New(db)
	cryptoSvc := crypto.New()
	contactSvc := contacts.NewService(db)
	msgSvc := messages.NewService(db, cryptoSvc)

	_, err := authSvc.Register(ctx, "alice", "alicepass123")
	require.NoError(t, err)
	bob, err := authSvc.Register(ctx, "bob", "bobpass456")
	require.NoError(t, err)

	reEncryptPrivateKey(t, db, cryptoSvc, "alice", "alicepass123")
	reEncryptPrivateKey(t, db, cryptoSvc, "bob", "bobpass456")

	err = contactSvc.AddContact(ctx, "alice", domain.Contact{
		Username:     "bob",
		OnionAddress: "bob.onion",
		PublicKey:    bob.PublicKey,
	})
	require.NoError(t, err)

	const numMessages = 10
	var wg sync.WaitGroup
	errs := make(chan error, numMessages)

	for i := 0; i < numMessages; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			msg := fmt.Sprintf("Concurrent message #%d", idx)
			if e := msgSvc.SendMessage(ctx, "alice", "bob", "alicepass123", msg); e != nil {
				errs <- e
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for e := range errs {
		assert.NoError(t, e, "concurrent send should not produce errors")
	}

	bobMsgs, err := msgSvc.FetchMessages(ctx, "bob", "alice", "bobpass456")
	require.NoError(t, err)
	aliceMsgs, err := msgSvc.FetchMessages(ctx, "alice", "bob", "alicepass123")
	require.NoError(t, err)

	aliceContents := make(map[string]bool)
	for _, m := range aliceMsgs {
		if m.Content != "" {
			aliceContents[m.Content] = true
		}
	}
	assert.GreaterOrEqual(t, len(aliceContents), numMessages,
		"Alice should see all %d sent messages (AES copies)", numMessages)

	bobContents := make(map[string]bool)
	for _, m := range bobMsgs {
		if m.Content != "" && m.Content[0] != '[' {
			bobContents[m.Content] = true
		}
	}
	assert.GreaterOrEqual(t, len(bobContents), numMessages,
		"Bob should see all %d received messages (GPG copies)", numMessages)
}

func TestPasswordChangeAndRelogin(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	authSvc := auth.New(db)
	cryptoSvc := crypto.New()
	contactSvc := contacts.NewService(db)
	msgSvc := messages.NewService(db, cryptoSvc)

	initialPassword := "initialpass123"
	newPassword := "newpassword456"

	alice, err := authSvc.Register(ctx, "alice", initialPassword)
	require.NoError(t, err)
	require.NotNil(t, alice)

	reEncryptPrivateKey(t, db, cryptoSvc, "alice", initialPassword)

	bob, err := authSvc.Register(ctx, "bob", "bobpass789")
	require.NoError(t, err)
	reEncryptPrivateKey(t, db, cryptoSvc, "bob", "bobpass789")

	err = contactSvc.AddContact(ctx, "alice", domain.Contact{
		Username:     "bob",
		OnionAddress: "bob.onion",
		PublicKey:    bob.PublicKey,
	})
	require.NoError(t, err)
	err = contactSvc.AddContact(ctx, "bob", domain.Contact{
		Username:     "alice",
		OnionAddress: "alice.onion",
		PublicKey:    alice.PublicKey,
	})
	require.NoError(t, err)

	// Bob sends two GPG-encrypted messages to Alice before password change.
	msg1 := "Message from Bob before Alice changes password - one"
	msg2 := "Message from Bob before Alice changes password - two"
	require.NoError(t, msgSvc.SendMessage(ctx, "bob", "alice", "bobpass789", msg1))
	require.NoError(t, msgSvc.SendMessage(ctx, "bob", "alice", "bobpass789", msg2))

	beforeMsgs, err := msgSvc.FetchMessages(ctx, "alice", "bob", initialPassword)
	require.NoError(t, err)
	require.NotEmpty(t, beforeMsgs)

	user, err := storage.GetUser(db, "alice")
	require.NoError(t, err)

	privKey, err := cryptoSvc.DecryptAES(user.PrivateKey, initialPassword)
	require.NoError(t, err)

	newEncryptedPriv, err := cryptoSvc.EncryptAES(privKey, newPassword)
	require.NoError(t, err)

	newHash, err := cryptoSvc.HashPassword(newPassword)
	require.NoError(t, err)

	_, err = db.Exec("UPDATE user SET privateKey = ?, password = ? WHERE username = ?",
		newEncryptedPriv, newHash, "alice")
	require.NoError(t, err)

	_, err = authSvc.Login(ctx, "alice", initialPassword)
	assert.Error(t, err, "old password must no longer authenticate")

	loggedIn, err := authSvc.Login(ctx, "alice", newPassword)
	require.NoError(t, err)
	assert.Equal(t, "alice", loggedIn.Username)
	assert.Equal(t, newPassword, loggedIn.RawPassword)

	// Old GPG-encrypted messages must still decrypt because Alice's private
	// key was re-encrypted with the new password. (AES self-copies sent BY
	// Alice would use the old password as key and won't survive — that's
	// expected; only GPG messages TO Alice will decrypt.)
	aliceMsgs, err := msgSvc.FetchMessages(ctx, "alice", "bob", newPassword)
	require.NoError(t, err)

	found1, found2 := false, false
	for _, m := range aliceMsgs {
		switch m.Content {
		case msg1:
			found1 = true
		case msg2:
			found2 = true
		}
	}
	assert.True(t, found1, "GPG message 1 must decrypt after password change")
	assert.True(t, found2, "GPG message 2 must decrypt after password change")

	msg3 := "Message after password change"
	require.NoError(t, msgSvc.SendMessage(ctx, "alice", "bob", newPassword, msg3))

	newMsgs, err := msgSvc.FetchMessages(ctx, "alice", "bob", newPassword)
	require.NoError(t, err)
	found3 := false
	for _, m := range newMsgs {
		if m.Content == msg3 {
			found3 = true
			break
		}
	}
	assert.True(t, found3, "new messages must be sendable and decryptable after password change")
}
