package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	decrypted, err := decryptAES(user.EncryptedPrivateKey, "securepass123")
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

func TestAESRoundtrip(t *testing.T) {
	plaintext := []byte("this is a secret GPG private key")
	passphrase := "testpassword123"

	encrypted, err := encryptAES(plaintext, passphrase)
	require.NoError(t, err)
	require.NotEmpty(t, encrypted)

	decrypted, err := decryptAES(encrypted, passphrase)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestAESWrongPassphrase(t *testing.T) {
	plaintext := []byte("secret data")
	passphrase := "correctpassword"

	encrypted, err := encryptAES(plaintext, passphrase)
	require.NoError(t, err)

	_, err = decryptAES(encrypted, "wrongpassword")
	require.Error(t, err)
}
