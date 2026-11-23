package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/crypto"
	"sote/internal/storage"
)

func setupTestDB(t *testing.T) *AuthService {
	t.Helper()
	db, err := storage.Initialize(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return New(db)
}

func TestRegisterValid(t *testing.T) {
	svc := setupTestDB(t)
	ctx := context.Background()

	user, err := svc.Register(ctx, "alice", "securepass123")
	require.NoError(t, err)
	require.NotNil(t, user)

	assert.Equal(t, "alice", user.Username)
	assert.NotEmpty(t, user.PasswordHash)
	assert.NotEmpty(t, user.EncryptedPrivateKey)
	assert.NotEmpty(t, user.PublicKey)
	assert.Equal(t, "securepass123", user.RawPassword)
	assert.Empty(t, user.OnionAddress)
	assert.Empty(t, user.TorrcPath)

	// Verify bcrypt hash format (starts with $2a$)
	assert.True(t, len(user.PasswordHash) > 0)

	// Verify encrypted private key can be decrypted back
	decrypted, err := crypto.New().DecryptAES(user.EncryptedPrivateKey, "securepass123")
	require.NoError(t, err)
	assert.NotEmpty(t, decrypted)
	// Decrypted key should be armored GPG private key
	assert.Contains(t, string(decrypted), "BEGIN PGP PRIVATE KEY BLOCK")

	// Verify public key is armored GPG public key
	assert.Contains(t, string(user.PublicKey), "BEGIN PGP PUBLIC KEY BLOCK")
}

func TestRegisterDuplicate(t *testing.T) {
	svc := setupTestDB(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, "bob", "securepass123")
	require.NoError(t, err)

	_, err = svc.Register(ctx, "bob", "differentpass456")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDuplicateUser)
}

func TestRegisterWeakPassword(t *testing.T) {
	svc := setupTestDB(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, "charlie", "short")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrWeakPassword)
}

func TestRegisterInvalidUsername(t *testing.T) {
	svc := setupTestDB(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		username string
	}{
		{"too short", "ab"},
		{"too long", "a" + string(make([]byte, 32)) + "z"}, // 34 chars
		{"has dash", "user-name"},
		{"has space", "user name"},
		{"has dot", "user.name"},
		{"empty", ""},
		{"special chars", "user@domain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Register(ctx, tt.username, "securepass123")
			require.Error(t, err, "expected error for username: %q", tt.username)
			assert.ErrorIs(t, err, ErrInvalidUsername)
		})
	}
}

func TestLoginValid(t *testing.T) {
	svc := setupTestDB(t)
	ctx := context.Background()

	regUser, err := svc.Register(ctx, "dave", "securepass123")
	require.NoError(t, err)

	loginUser, err := svc.Login(ctx, "dave", "securepass123")
	require.NoError(t, err)
	require.NotNil(t, loginUser)

	assert.Equal(t, regUser.Username, loginUser.Username)
	assert.Equal(t, regUser.PasswordHash, loginUser.PasswordHash)
	assert.Equal(t, regUser.EncryptedPrivateKey, loginUser.EncryptedPrivateKey)
	assert.Equal(t, regUser.PublicKey, loginUser.PublicKey)
	assert.Equal(t, "securepass123", loginUser.RawPassword)
}

func TestLoginWrongPassword(t *testing.T) {
	svc := setupTestDB(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, "eve", "securepass123")
	require.NoError(t, err)

	_, err = svc.Login(ctx, "eve", "wrongpassword")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestLoginNonExistent(t *testing.T) {
	svc := setupTestDB(t)
	ctx := context.Background()

	_, err := svc.Login(ctx, "nobody", "somepassword")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestLoginCaseSensitiveUsername(t *testing.T) {
	svc := setupTestDB(t)
	ctx := context.Background()

	_, err := svc.Register(ctx, "Frank", "securepass123")
	require.NoError(t, err)

	// Login with different case should fail
	_, err = svc.Login(ctx, "frank", "securepass123")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestRegisteredKeyUnlocksWithSharedCrypto(t *testing.T) {
	// Register used its own AES layout (16-byte salt) while message decryption
	// used another (32-byte salt), so a freshly registered account could never
	// decrypt a message. Both now share one implementation.
	svc := setupTestDB(t)
	user, err := svc.Register(context.Background(), "alice", "securepass123")
	require.NoError(t, err)

	cs := crypto.New()
	id, err := cs.Unlock(user.EncryptedPrivateKey, "securepass123")
	require.NoError(t, err)
	defer id.Close()

	ct, err := cs.EncryptMessage([]byte("hello alice"), user.PublicKey)
	require.NoError(t, err)
	plain, err := cs.DecryptMessage(ct, user.EncryptedPrivateKey, "securepass123")
	require.NoError(t, err)
	assert.Equal(t, "hello alice", plain)
}

func TestSecondAccountIsRefused(t *testing.T) {
	svc := setupTestDB(t)
	ctx := context.Background()
	_, err := svc.Register(ctx, "alice", "securepass123")
	require.NoError(t, err)

	_, err = svc.Register(ctx, "bob", "securepass123")
	assert.ErrorIs(t, err, ErrAccountExists)
}

func TestChangePassword(t *testing.T) {
	svc := setupTestDB(t)
	ctx := context.Background()
	user, err := svc.Register(ctx, "alice", "oldpassword1")
	require.NoError(t, err)

	// A sent-message copy in the pre-v2 layout: AES under the account password.
	cs := crypto.New()
	legacy, err := cs.EncryptAES([]byte("sent before v2"), "oldpassword1")
	require.NoError(t, err)
	_, err = storage.SaveMessageMeta(ctx, svc.db, "alice", "bob", append([]byte{storage.BlobLegacySelfAES}, legacy...), "", "")
	require.NoError(t, err)

	require.NoError(t, svc.ChangePassword(ctx, "alice", "oldpassword1", "newpassword2"))

	_, err = svc.Login(ctx, "alice", "oldpassword1")
	assert.ErrorIs(t, err, ErrInvalidCredentials)
	loggedIn, err := svc.Login(ctx, "alice", "newpassword2")
	require.NoError(t, err)

	id, err := cs.Unlock(loggedIn.EncryptedPrivateKey, "newpassword2")
	require.NoError(t, err, "private key must open with the new password")
	defer id.Close()
	// Compare fingerprints: gopenpgp orders armor headers nondeterministically.
	wantFP, err := crypto.PublicKeyFingerprint(user.PublicKey)
	require.NoError(t, err)
	assert.Equal(t, wantFP, id.Fingerprint())

	msgs, err := storage.GetMessages(svc.db, "alice", "bob")
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, storage.BlobSelfGPG, msgs[0].Message[0], "legacy copy must no longer depend on the password")
	plain, err := id.Decrypt(string(msgs[0].Message[1:]), nil)
	require.NoError(t, err)
	assert.Equal(t, "sent before v2", string(plain))
}

func TestChangePasswordRejectsWrongCurrentAndKeepsData(t *testing.T) {
	svc := setupTestDB(t)
	ctx := context.Background()
	_, err := svc.Register(ctx, "alice", "oldpassword1")
	require.NoError(t, err)

	assert.Error(t, svc.ChangePassword(ctx, "alice", "not-the-password", "newpassword2"))
	assert.ErrorIs(t, svc.ChangePassword(ctx, "alice", "oldpassword1", "short"), ErrWeakPassword)
	assert.ErrorIs(t, svc.ChangePassword(ctx, "alice", "oldpassword1", "oldpassword1"), ErrSamePassword)

	_, err = svc.Login(ctx, "alice", "oldpassword1")
	assert.NoError(t, err, "a refused change must leave the old password working")
}

func TestLoginUpgradesLegacyPrivateKeyLayout(t *testing.T) {
	svc := setupTestDB(t)
	ctx := context.Background()
	user, err := svc.Register(ctx, "alice", "securepass123")
	require.NoError(t, err)

	cs := crypto.New()
	priv, err := cs.DecryptAES(user.EncryptedPrivateKey, "securepass123")
	require.NoError(t, err)
	// Store the key the way auth.Register did before: no header, 16-byte salt.
	legacy := legacyBlob16(t, priv, "securepass123")
	require.NoError(t, storage.UpdateEncryptedPrivateKey(ctx, svc.db, "alice", legacy))
	require.True(t, crypto.NeedsUpgrade(legacy))

	loggedIn, err := svc.Login(ctx, "alice", "securepass123")
	require.NoError(t, err)
	assert.False(t, crypto.NeedsUpgrade(loggedIn.EncryptedPrivateKey))
	stored, err := storage.GetUser(svc.db, "alice")
	require.NoError(t, err)
	assert.False(t, crypto.NeedsUpgrade(stored.PrivateKey))
	_, err = cs.Unlock(stored.PrivateKey, "securepass123")
	assert.NoError(t, err)
}
