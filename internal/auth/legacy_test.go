package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/pbkdf2"
)

// legacyBlob16 writes data in the layout auth.Register used before v2:
// salt(16) | nonce(12) | ciphertext, PBKDF2-SHA256 with 100,000 rounds.
func legacyBlob16(t *testing.T, data []byte, pass string) []byte {
	t.Helper()
	salt := make([]byte, 16)
	_, err := io.ReadFull(rand.Reader, salt)
	require.NoError(t, err)
	block, err := aes.NewCipher(pbkdf2.Key([]byte(pass), salt, 100_000, 32, sha256.New))
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)
	nonce := make([]byte, gcm.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	require.NoError(t, err)
	return gcm.Seal(append(append([]byte{}, salt...), nonce...), nonce, data, nil)
}
