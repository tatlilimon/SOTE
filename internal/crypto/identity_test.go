package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/pbkdf2"
)

// legacyBlob reproduces the header-less layouts written before this version:
// salt(saltLength) | nonce(12) | ciphertext, PBKDF2-SHA256 with 100,000 rounds.
// auth.Register used a 16-byte salt, crypto.EncryptAES a 32-byte one.
func legacyBlob(t *testing.T, plain []byte, pass string, saltLength int) []byte {
	t.Helper()
	salt := make([]byte, saltLength)
	_, err := io.ReadFull(rand.Reader, salt)
	require.NoError(t, err)
	key := pbkdf2.Key([]byte(pass), salt, 100_000, 32, sha256.New)
	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)
	nonce := make([]byte, gcm.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	require.NoError(t, err)
	out := append(append([]byte{}, salt...), nonce...)
	return gcm.Seal(out, nonce, plain, nil)
}

func TestDecryptAESReadsBothLegacyLayouts(t *testing.T) {
	svc := New()
	for _, sl := range []int{16, 32} {
		blob := legacyBlob(t, []byte("private key bytes"), "correct horse", sl)
		plain, err := svc.DecryptAES(blob, "correct horse")
		require.NoError(t, err, "salt length %d", sl)
		assert.Equal(t, "private key bytes", string(plain))
		assert.True(t, NeedsUpgrade(blob))

		_, err = svc.DecryptAES(blob, "wrong")
		assert.ErrorIs(t, err, ErrWrongPassphrase)
	}
}

func TestEncryptAESWritesVersionedHeader(t *testing.T) {
	svc := New()
	blob, err := svc.EncryptAES([]byte("hello"), "pass")
	require.NoError(t, err)
	require.True(t, isVersionedBlob(blob))
	assert.Equal(t, uint32(pbkdf2Iterations), binary.BigEndian.Uint32(blob[5:9]))
	assert.False(t, NeedsUpgrade(blob))

	plain, err := svc.DecryptAES(blob, "pass")
	require.NoError(t, err)
	assert.Equal(t, "hello", string(plain))
}

func TestVersionedHeaderIsAuthenticated(t *testing.T) {
	svc := New()
	blob, err := svc.EncryptAES([]byte("hello"), "pass")
	require.NoError(t, err)

	// Lowering the iteration count (to make brute force cheaper, or by damage)
	// must not produce a different but "valid" decryption.
	tampered := append([]byte(nil), blob...)
	binary.BigEndian.PutUint32(tampered[5:9], legacyPBKDF2Iterations)
	_, err = svc.DecryptAES(tampered, "pass")
	assert.ErrorIs(t, err, ErrWrongPassphrase)

	// Absurd counts are refused before any key derivation runs.
	binary.BigEndian.PutUint32(tampered[5:9], 0xFFFFFFFF)
	_, err = svc.DecryptAES(tampered, "pass")
	assert.ErrorIs(t, err, ErrWrongPassphrase)
}

func TestUnlockLegacyEncryptedPrivateKey(t *testing.T) {
	svc := New()
	priv, pub, err := svc.GenerateKeyPair()
	require.NoError(t, err)

	// The layout auth.Register wrote. crypto.DecryptAES used to read only the
	// 32-byte-salt layout, so these keys never opened and every received
	// message showed a decrypt error.
	enc := legacyBlob(t, priv, "hunter22", 16)
	id, err := svc.Unlock(enc, "hunter22")
	require.NoError(t, err)
	defer id.Close()

	gotFP, err := PublicKeyFingerprint(pub)
	require.NoError(t, err)
	assert.Equal(t, gotFP, id.Fingerprint())

	ct, err := svc.EncryptMessage([]byte("hi"), pub)
	require.NoError(t, err)
	plain, err := svc.DecryptMessage(ct, enc, "hunter22")
	require.NoError(t, err)
	assert.Equal(t, "hi", plain)
}

func newIdentity(t *testing.T) *Identity {
	t.Helper()
	priv, _, err := New().GenerateKeyPair()
	require.NoError(t, err)
	id, err := IdentityFromArmoredPrivateKey(priv)
	require.NoError(t, err)
	t.Cleanup(id.Close)
	return id
}

func TestEncryptForIsSignedBySender(t *testing.T) {
	alice, bob, mallory := newIdentity(t), newIdentity(t), newIdentity(t)

	ct, err := alice.EncryptFor(bob.PublicKey(), []byte("meet at noon"))
	require.NoError(t, err)

	plain, err := bob.Decrypt(ct, alice.PublicKey())
	require.NoError(t, err)
	assert.Equal(t, "meet at noon", string(plain))

	// Claiming the message came from someone else fails verification.
	_, err = bob.Decrypt(ct, mallory.PublicKey())
	assert.ErrorIs(t, err, ErrBadSignature)

	// An unsigned message fails when a signer is required.
	unsigned, err := New().EncryptMessage([]byte("forged"), bob.PublicKey())
	require.NoError(t, err)
	_, err = bob.Decrypt(string(unsigned), alice.PublicKey())
	assert.ErrorIs(t, err, ErrBadSignature)

	// Only the recipient can read it.
	_, err = mallory.Decrypt(ct, nil)
	assert.Error(t, err)
}

func TestEncryptToSelf(t *testing.T) {
	alice := newIdentity(t)
	ct, err := alice.EncryptToSelf([]byte("note to self"))
	require.NoError(t, err)
	plain, err := alice.Decrypt(ct, alice.PublicKey())
	require.NoError(t, err)
	assert.Equal(t, "note to self", string(plain))
}

func TestDetachedSignatures(t *testing.T) {
	alice, mallory := newIdentity(t), newIdentity(t)
	sig, err := alice.SignDetached([]byte("envelope bytes"))
	require.NoError(t, err)

	assert.NoError(t, VerifyDetached(alice.PublicKey(), []byte("envelope bytes"), sig))
	assert.ErrorIs(t, VerifyDetached(alice.PublicKey(), []byte("envelope bytes!"), sig), ErrBadSignature)
	assert.ErrorIs(t, VerifyDetached(mallory.PublicKey(), []byte("envelope bytes"), sig), ErrBadSignature)
	assert.ErrorIs(t, VerifyDetached(alice.PublicKey(), []byte("envelope bytes"), "not a signature"), ErrBadSignature)
}

func TestPublicKeyFingerprintRejectsPrivateKeysAndGarbage(t *testing.T) {
	priv, pub, err := New().GenerateKeyPair()
	require.NoError(t, err)

	_, err = PublicKeyFingerprint(pub)
	assert.NoError(t, err)

	_, err = PublicKeyFingerprint(priv)
	assert.True(t, errors.Is(err, ErrNotPublicKey))

	_, err = PublicKeyFingerprint([]byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\nnope\n"))
	assert.True(t, errors.Is(err, ErrNotPublicKey))
}
