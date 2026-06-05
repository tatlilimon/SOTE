package crypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashPassword(t *testing.T) {
	svc := New()
	password := "correct-horse-battery-staple"

	hash, err := svc.HashPassword(password)
	require.NoError(t, err)
	require.NotEmpty(t, hash)

	// Hash must not contain the original password.
	assert.NotContains(t, hash, password)
	// Each call must produce a different hash (bcrypt uses random salt).
	hash2, err := svc.HashPassword(password)
	require.NoError(t, err)
	assert.NotEqual(t, hash, hash2)
}

func TestVerifyPassword(t *testing.T) {
	svc := New()
	hash, err := svc.HashPassword("my-secret")
	require.NoError(t, err)

	err = svc.VerifyPassword(hash, "my-secret")
	assert.NoError(t, err)
}

func TestVerifyWrongPassword(t *testing.T) {
	svc := New()
	hash, err := svc.HashPassword("correct")
	require.NoError(t, err)

	err = svc.VerifyPassword(hash, "wrong-passphrase")
	assert.Error(t, err)
}

func TestAESRoundtrip(t *testing.T) {
	svc := New()
	plaintext := []byte("sensitive private key material goes here")
	passphrase := "aes-passphrase"

	ciphertext, err := svc.EncryptAES(plaintext, passphrase)
	require.NoError(t, err)
	require.NotEmpty(t, ciphertext)

	// Must be at least salt(32) + nonce(12).
	assert.True(t, len(ciphertext) > saltLen+12, "ciphertext too short")

	decrypted, err := svc.DecryptAES(ciphertext, passphrase)
	require.NoError(t, err)

	assert.Equal(t, plaintext, decrypted)
}

func TestAESWrongPassword(t *testing.T) {
	svc := New()
	ciphertext, err := svc.EncryptAES([]byte("data"), "correct-pass")
	require.NoError(t, err)

	_, err = svc.DecryptAES(ciphertext, "wrong-pass")
	assert.Error(t, err, "decryption with wrong password must fail")
}

func TestAESEmptyData(t *testing.T) {
	svc := New()
	ciphertext, err := svc.EncryptAES([]byte{}, "pass")
	require.NoError(t, err)

	decrypted, err := svc.DecryptAES(ciphertext, "pass")
	require.NoError(t, err)
	assert.Empty(t, decrypted)
}

func TestAESDecryptTooShort(t *testing.T) {
	svc := New()
	_, err := svc.DecryptAES([]byte("short"), "pass")
	assert.Error(t, err)
}

func TestGPGEncryptDecrypt(t *testing.T) {
	svc := New()
	privateKey, publicKey, err := svc.GenerateKeyPair()
	require.NoError(t, err)
	require.NotEmpty(t, privateKey)
	require.NotEmpty(t, publicKey)
	assert.Contains(t, string(privateKey), "-----BEGIN PGP PRIVATE KEY BLOCK-----")
	assert.Contains(t, string(publicKey), "-----BEGIN PGP PUBLIC KEY BLOCK-----")

	// Encrypt the private key with AES (as it would be stored).
	passphrase := "gpg-key-passphrase"
	encryptedPrivateKey, err := svc.EncryptAES(privateKey, passphrase)
	require.NoError(t, err)

	plaintext := []byte("Hello, this is a secret GPG message!")
	ciphertext, err := svc.EncryptMessage(plaintext, publicKey)
	require.NoError(t, err)
	require.NotEmpty(t, ciphertext)
	assert.Contains(t, string(ciphertext), "-----BEGIN PGP MESSAGE-----")

	// Decrypt using the encrypted private key.
	decrypted, err := svc.DecryptMessage(ciphertext, encryptedPrivateKey, passphrase)
	require.NoError(t, err)
	assert.Equal(t, string(plaintext), decrypted)
}

func TestGPGBadKey(t *testing.T) {
	svc := New()

	_, err := svc.EncryptMessage([]byte("msg"), []byte("not-a-valid-key"))
	assert.Error(t, err)

	_, err = svc.DecryptMessage([]byte("bad"), []byte("bad"), "bad")
	assert.Error(t, err)
}

func TestGPGDecryptWrongPassphrase(t *testing.T) {
	svc := New()
	privateKey, publicKey, err := svc.GenerateKeyPair()
	require.NoError(t, err)

	encryptedPrivateKey, err := svc.EncryptAES(privateKey, "correct-pass")
	require.NoError(t, err)

	ciphertext, err := svc.EncryptMessage([]byte("secret"), publicKey)
	require.NoError(t, err)

	// Decrypt with wrong AES passphrase for private key.
	_, err = svc.DecryptMessage(ciphertext, encryptedPrivateKey, "wrong-pass")
	assert.Error(t, err)
}

func TestGenerateKeyPairDeterministic(t *testing.T) {
	svc := New()
	priv1, pub1, err := svc.GenerateKeyPair()
	require.NoError(t, err)

	priv2, pub2, err := svc.GenerateKeyPair()
	require.NoError(t, err)

	// Each call must produce unique keys (RSA generates random primes).
	assert.NotEqual(t, priv1, priv2)
	assert.NotEqual(t, pub1, pub2)
}

func TestInterfaceCompliance(t *testing.T) {
	var svc CryptoService = New()
	assert.NotNil(t, svc)
}
