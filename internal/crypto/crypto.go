// Package crypto provides cryptographic operations for SOTE.
// Implements domain.CryptoService with bcrypt password hashing,
// PBKDF2 key derivation, AES-256-GCM encryption, and GPG
// asymmetric encryption and signatures via ProtonMail gopenpgp.
package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/ProtonMail/gopenpgp/v2/crypto"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
)

const (
	// bcryptCost is the cost factor for bcrypt password hashing.
	bcryptCost = 12

	// pbkdf2Iterations is used for every blob written now (OWASP 2023 guidance
	// for PBKDF2-HMAC-SHA256). The count is stored in the blob header, so
	// raising it later keeps older blobs readable.
	pbkdf2Iterations = 600_000

	// legacyPBKDF2Iterations is the count used by blobs written before the
	// format had a header.
	legacyPBKDF2Iterations = 100_000

	// maxPBKDF2Iterations bounds the count read from a header so a damaged or
	// crafted blob cannot make a decryption run for minutes.
	maxPBKDF2Iterations = 10_000_000

	// pbkdf2KeyLen is the derived key length in bytes (AES-256).
	pbkdf2KeyLen = 32

	// saltLen is the length of the random salt for PBKDF2.
	saltLen = 32

	// legacySaltLen is the salt length auth.Register used before both
	// packages shared one implementation.
	legacySaltLen = 16

	// gpgKeyName is the default name used when generating GPG keys.
	gpgKeyName = "sote-user"

	blobVersion = 1
)

// blobMagic starts every AES blob written by this version:
// magic(4) | version(1) | iterations(4, big endian) | salt(32) | nonce(12) | ciphertext+tag.
// The first nine bytes are authenticated as additional data.
var blobMagic = []byte("SOTE")

const blobHeaderLen = 4 + 1 + 4

var (
	// ErrWrongPassphrase means the passphrase does not open the blob, or the
	// blob is damaged. AES-GCM cannot tell the two apart.
	ErrWrongPassphrase = errors.New("wrong passphrase or corrupted data")

	// ErrBadSignature means a signature is missing or does not match the
	// expected signer's public key.
	ErrBadSignature = errors.New("signature verification failed")

	// ErrNotPublicKey means armored data that should be a public key is a
	// private key or cannot be used for encryption.
	ErrNotPublicKey = errors.New("not a usable public key")
)

// Service implements domain.CryptoService.
type Service struct{}

// New creates a new CryptoService.
func New() *Service {
	return &Service{}
}

// HashPassword hashes a password using bcrypt with cost ≥ 12.
func (s *Service) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword checks a password against a bcrypt hash.
func (s *Service) VerifyPassword(hash, password string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	return nil
}

// GenerateKeyPair generates an RSA-2048 GPG key pair.
// Returns armored private key and armored public key.
func (s *Service) GenerateKeyPair() (privateKey, publicKey []byte, err error) {
	return GenerateKeyPairFor(gpgKeyName, "")
}

// GenerateKeyPairFor generates an RSA-2048 GPG key pair with the given user ID.
func GenerateKeyPairFor(name, email string) (privateKey, publicKey []byte, err error) {
	keyRing, err := crypto.GenerateKey(name, email, "rsa", 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key pair: %w", err)
	}

	privateKeyStr, err := keyRing.Armor()
	if err != nil {
		return nil, nil, fmt.Errorf("armor private key: %w", err)
	}

	publicKeyStr, err := keyRing.GetArmoredPublicKey()
	if err != nil {
		return nil, nil, fmt.Errorf("armor public key: %w", err)
	}

	return []byte(privateKeyStr), []byte(publicKeyStr), nil
}

// EncryptAES encrypts data using AES-256-GCM with a key derived via
// PBKDF2-HMAC-SHA256 from the passphrase and a fresh random salt.
// Output: magic | version | iterations | salt | nonce | ciphertext.
func (s *Service) EncryptAES(data []byte, passphrase string) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("encrypt aes: %w", err)
	}

	header := make([]byte, blobHeaderLen)
	copy(header, blobMagic)
	header[4] = blobVersion
	binary.BigEndian.PutUint32(header[5:], pbkdf2Iterations)

	gcm, err := newGCM(passphrase, salt, pbkdf2Iterations)
	if err != nil {
		return nil, fmt.Errorf("encrypt aes: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("encrypt aes: %w", err)
	}

	out := make([]byte, 0, blobHeaderLen+saltLen+len(nonce)+len(data)+gcm.Overhead())
	out = append(out, header...)
	out = append(out, salt...)
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, data, header), nil
}

// DecryptAES decrypts data written by EncryptAES. It also reads the two
// header-less layouts written by earlier versions (32- and 16-byte salt,
// 100,000 iterations), so existing private keys and messages stay readable.
// Returns ErrWrongPassphrase when nothing opens the blob.
func (s *Service) DecryptAES(data []byte, passphrase string) ([]byte, error) {
	if isVersionedBlob(data) {
		return decryptVersioned(data, passphrase)
	}
	for _, sl := range []int{saltLen, legacySaltLen} {
		if plain, err := decryptLegacy(data, passphrase, sl); err == nil {
			return plain, nil
		}
	}
	return nil, fmt.Errorf("decrypt aes: %w", ErrWrongPassphrase)
}

// NeedsUpgrade reports whether a blob was written in an older layout or with
// fewer iterations than new blobs use, so callers can re-encrypt it.
func NeedsUpgrade(data []byte) bool {
	if !isVersionedBlob(data) {
		return true
	}
	return binary.BigEndian.Uint32(data[5:9]) < pbkdf2Iterations
}

func isVersionedBlob(data []byte) bool {
	return len(data) >= blobHeaderLen && bytes.Equal(data[:4], blobMagic) && data[4] == blobVersion
}

func decryptVersioned(data []byte, passphrase string) ([]byte, error) {
	iterations := int(binary.BigEndian.Uint32(data[5:9]))
	if iterations < legacyPBKDF2Iterations || iterations > maxPBKDF2Iterations {
		return nil, fmt.Errorf("decrypt aes: unsupported iteration count %d: %w", iterations, ErrWrongPassphrase)
	}
	rest := data[blobHeaderLen:]
	if len(rest) < saltLen {
		return nil, fmt.Errorf("decrypt aes: ciphertext too short: %w", ErrWrongPassphrase)
	}
	salt, rest := rest[:saltLen], rest[saltLen:]

	gcm, err := newGCM(passphrase, salt, iterations)
	if err != nil {
		return nil, fmt.Errorf("decrypt aes: %w", err)
	}
	if len(rest) < gcm.NonceSize() {
		return nil, fmt.Errorf("decrypt aes: ciphertext too short for nonce: %w", ErrWrongPassphrase)
	}
	nonce, ciphertext := rest[:gcm.NonceSize()], rest[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, data[:blobHeaderLen])
	if err != nil {
		return nil, fmt.Errorf("decrypt aes: %w", ErrWrongPassphrase)
	}
	return plain, nil
}

func decryptLegacy(data []byte, passphrase string, sl int) ([]byte, error) {
	if len(data) < sl {
		return nil, ErrWrongPassphrase
	}
	salt, rest := data[:sl], data[sl:]
	gcm, err := newGCM(passphrase, salt, legacyPBKDF2Iterations)
	if err != nil {
		return nil, err
	}
	if len(rest) < gcm.NonceSize() {
		return nil, ErrWrongPassphrase
	}
	nonce, ciphertext := rest[:gcm.NonceSize()], rest[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrWrongPassphrase
	}
	return plain, nil
}

func newGCM(passphrase string, salt []byte, iterations int) (cipher.AEAD, error) {
	key := pbkdf2.Key([]byte(passphrase), salt, iterations, pbkdf2KeyLen, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// EncryptMessage encrypts plaintext with the recipient's armored GPG public key.
// The result is not signed; peers use Identity.EncryptFor instead.
func (s *Service) EncryptMessage(plaintext []byte, publicKey []byte) ([]byte, error) {
	keyRing, err := publicKeyRing(publicKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt message: %w", err)
	}

	encryptedMessage, err := keyRing.Encrypt(crypto.NewPlainMessage(plaintext), nil)
	if err != nil {
		return nil, fmt.Errorf("encrypt message: %w", err)
	}

	encryptedData, err := encryptedMessage.GetArmored()
	if err != nil {
		return nil, fmt.Errorf("encrypt message: armor: %w", err)
	}

	return []byte(encryptedData), nil
}

// DecryptMessage decrypts a GPG-encrypted message using an AES-encrypted
// private key and the passphrase to unlock it.
func (s *Service) DecryptMessage(ciphertext []byte, privateKey []byte, passphrase string) (string, error) {
	id, err := s.Unlock(privateKey, passphrase)
	if err != nil {
		return "", fmt.Errorf("decrypt message: %w", err)
	}
	defer id.Close()

	plain, err := id.Decrypt(string(ciphertext), nil)
	if err != nil {
		return "", fmt.Errorf("decrypt message: %w", err)
	}
	return string(plain), nil
}

// ── Identity: an unlocked private key for one session ───────────────────────

// Identity holds an unlocked GPG private key. Unlock it once (one PBKDF2
// derivation) and reuse it, instead of deriving a key for every message.
// Call Close when the session ends to wipe the private parameters.
type Identity struct {
	ring        *crypto.KeyRing
	publicKey   []byte
	fingerprint string
}

// Unlock decrypts an AES-protected armored private key with the passphrase.
func (s *Service) Unlock(encryptedPrivateKey []byte, passphrase string) (*Identity, error) {
	armored, err := s.DecryptAES(encryptedPrivateKey, passphrase)
	if err != nil {
		return nil, fmt.Errorf("unlock private key: %w", err)
	}
	return IdentityFromArmoredPrivateKey(armored)
}

// IdentityFromArmoredPrivateKey builds an Identity from an unencrypted armored
// private key (as produced by GenerateKeyPair).
func IdentityFromArmoredPrivateKey(armored []byte) (*Identity, error) {
	key, err := crypto.NewKeyFromArmored(string(armored))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	if !key.IsPrivate() {
		return nil, errors.New("parse private key: armored key is public")
	}
	if locked, err := key.IsLocked(); err != nil || locked {
		return nil, errors.New("parse private key: key is locked with a GPG passphrase")
	}
	pub, err := key.GetArmoredPublicKey()
	if err != nil {
		return nil, fmt.Errorf("export public key: %w", err)
	}
	ring, err := crypto.NewKeyRing(key)
	if err != nil {
		return nil, fmt.Errorf("create keyring: %w", err)
	}
	return &Identity{ring: ring, publicKey: []byte(pub), fingerprint: key.GetFingerprint()}, nil
}

// PublicKey returns the armored public key.
func (id *Identity) PublicKey() []byte { return append([]byte(nil), id.publicKey...) }

// Fingerprint returns the hex fingerprint of the key.
func (id *Identity) Fingerprint() string { return id.fingerprint }

// SignDetached returns an armored detached signature over data.
func (id *Identity) SignDetached(data []byte) (string, error) {
	sig, err := id.ring.SignDetached(crypto.NewPlainMessage(data))
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}
	armored, err := sig.GetArmored()
	if err != nil {
		return "", fmt.Errorf("armor signature: %w", err)
	}
	return armored, nil
}

// EncryptFor encrypts plaintext to the recipient's public key and signs it
// with this identity, so the recipient can check who wrote it.
func (id *Identity) EncryptFor(recipientPublicKey []byte, plaintext []byte) (string, error) {
	ring, err := publicKeyRing(recipientPublicKey)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}
	msg, err := ring.Encrypt(crypto.NewPlainMessage(plaintext), id.ring)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}
	armored, err := msg.GetArmored()
	if err != nil {
		return "", fmt.Errorf("encrypt: armor: %w", err)
	}
	return armored, nil
}

// EncryptToSelf encrypts plaintext to this identity's own public key, for the
// sender's copy of a message.
func (id *Identity) EncryptToSelf(plaintext []byte) (string, error) {
	return id.EncryptFor(id.publicKey, plaintext)
}

// Decrypt decrypts an armored PGP message addressed to this identity. When
// signer is not empty, the message must carry a valid signature by that
// public key, otherwise ErrBadSignature is returned.
func (id *Identity) Decrypt(armored string, signer []byte) ([]byte, error) {
	msg, err := crypto.NewPGPMessageFromArmored(armored)
	if err != nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}
	var verify *crypto.KeyRing
	if len(signer) > 0 {
		if verify, err = publicKeyRing(signer); err != nil {
			return nil, fmt.Errorf("signer key: %w", err)
		}
	}
	plain, err := id.ring.Decrypt(msg, verify, crypto.GetUnixTime())
	if err != nil {
		var sigErr crypto.SignatureVerificationError
		if errors.As(err, &sigErr) {
			return nil, ErrBadSignature
		}
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plain.GetBinary(), nil
}

// Close wipes the private key material held by the identity.
func (id *Identity) Close() {
	if id != nil && id.ring != nil {
		id.ring.ClearPrivateParams()
	}
}

// VerifyDetached checks an armored detached signature over data against an
// armored public key. Returns ErrBadSignature when it does not match.
func VerifyDetached(publicKey []byte, data []byte, armoredSignature string) error {
	ring, err := publicKeyRing(publicKey)
	if err != nil {
		return err
	}
	sig, err := crypto.NewPGPSignatureFromArmored(armoredSignature)
	if err != nil {
		return ErrBadSignature
	}
	if err := ring.VerifyDetached(crypto.NewPlainMessage(data), sig, crypto.GetUnixTime()); err != nil {
		return ErrBadSignature
	}
	return nil
}

// PublicKeyFingerprint validates an armored public key and returns its
// fingerprint. Private keys are rejected, so a peer cannot hand us one by
// mistake and we never store foreign secret material.
func PublicKeyFingerprint(publicKey []byte) (string, error) {
	key, err := parsePublicKey(publicKey)
	if err != nil {
		return "", err
	}
	return key.GetFingerprint(), nil
}

func parsePublicKey(publicKey []byte) (*crypto.Key, error) {
	key, err := crypto.NewKeyFromArmored(string(publicKey))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotPublicKey, err)
	}
	if key.IsPrivate() || !key.CanEncrypt() {
		return nil, ErrNotPublicKey
	}
	return key, nil
}

func publicKeyRing(publicKey []byte) (*crypto.KeyRing, error) {
	key, err := parsePublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	ring, err := crypto.NewKeyRing(key)
	if err != nil {
		return nil, fmt.Errorf("create keyring: %w", err)
	}
	return ring, nil
}

// Compile-time interface check.
var _ CryptoService = (*Service)(nil)

// CryptoService is the local alias for the domain interface to avoid circular imports.
type CryptoService interface {
	HashPassword(password string) (string, error)
	VerifyPassword(hash, password string) error
	GenerateKeyPair() (privateKey, publicKey []byte, err error)
	EncryptAES(data []byte, passphrase string) ([]byte, error)
	DecryptAES(data []byte, passphrase string) ([]byte, error)
	EncryptMessage(plaintext []byte, publicKey []byte) ([]byte, error)
	DecryptMessage(ciphertext []byte, privateKey []byte, passphrase string) (string, error)
}
