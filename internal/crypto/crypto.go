// Package crypto provides cryptographic operations for SOTE.
// Implements domain.CryptoService with bcrypt password hashing,
// PBKDF2 key derivation, AES-256-GCM encryption, and GPG
// asymmetric encryption via ProtonMail gopenpgp.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/ProtonMail/gopenpgp/v2/crypto"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
)

const (
	// bcryptCost is the cost factor for bcrypt password hashing.
	bcryptCost = 12

	// pbkdf2Iterations is the number of PBKDF2 iterations for key derivation.
	pbkdf2Iterations = 100_000

	// pbkdf2KeyLen is the derived key length in bytes (AES-256).
	pbkdf2KeyLen = 32

	// saltLen is the length of the random salt for PBKDF2.
	saltLen = 32

	// gpgKeyName is the default name used when generating GPG keys.
	gpgKeyName = "sote-user"
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
	keyRing, err := crypto.GenerateKey(gpgKeyName, "", "rsa", 2048)
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
// PBKDF2 (HMAC-SHA256, 100k iterations, crypto/rand salt).
// Output format: salt(32 bytes) | nonce(12 bytes) | ciphertext.
func (s *Service) EncryptAES(data []byte, passphrase string) ([]byte, error) {
	salt, key, err := deriveKey(passphrase)
	if err != nil {
		return nil, fmt.Errorf("encrypt aes: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("encrypt aes: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("encrypt aes: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("encrypt aes: %w", err)
	}

	// Prepend nonce to ciphertext (GCM.Seal with dst=nonce)
	ciphertext := gcm.Seal(nonce, nonce, data, nil)

	// Prepend salt for storage
	result := make([]byte, 0, saltLen+len(ciphertext))
	result = append(result, salt...)
	result = append(result, ciphertext...)
	return result, nil
}

// DecryptAES decrypts data previously encrypted with EncryptAES.
// Expects format: salt(32 bytes) | nonce(12 bytes) | ciphertext.
func (s *Service) DecryptAES(data []byte, passphrase string) ([]byte, error) {
	if len(data) < saltLen {
		return nil, fmt.Errorf("decrypt aes: ciphertext too short")
	}

	salt := data[:saltLen]
	rest := data[saltLen:]

	key := deriveKeyFromSalt(passphrase, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("decrypt aes: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("decrypt aes: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(rest) < nonceSize {
		return nil, fmt.Errorf("decrypt aes: ciphertext too short for nonce")
	}

	nonce, ciphertext := rest[:nonceSize], rest[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt aes: %w", err)
	}

	return plaintext, nil
}

// EncryptMessage encrypts plaintext with the recipient's armored GPG public key.
func (s *Service) EncryptMessage(plaintext []byte, publicKey []byte) ([]byte, error) {
	key, err := crypto.NewKeyFromArmored(string(publicKey))
	if err != nil {
		return nil, fmt.Errorf("encrypt message: invalid public key: %w", err)
	}

	keyRing, err := crypto.NewKeyRing(key)
	if err != nil {
		return nil, fmt.Errorf("encrypt message: create keyring: %w", err)
	}

	plainMessage := crypto.NewPlainMessage(plaintext)
	encryptedMessage, err := keyRing.Encrypt(plainMessage, nil)
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
	decryptedPrivateKey, err := s.DecryptAES(privateKey, passphrase)
	if err != nil {
		return "", fmt.Errorf("decrypt message: %w", err)
	}

	key, err := crypto.NewKeyFromArmored(string(decryptedPrivateKey))
	if err != nil {
		return "", fmt.Errorf("decrypt message: invalid private key: %w", err)
	}

	keyRing, err := crypto.NewKeyRing(key)
	if err != nil {
		return "", fmt.Errorf("decrypt message: create keyring: %w", err)
	}

	pgpMessage, err := crypto.NewPGPMessageFromArmored(string(ciphertext))
	if err != nil {
		return "", fmt.Errorf("decrypt message: parse armored message: %w", err)
	}

	decryptedMessage, err := keyRing.Decrypt(pgpMessage, nil, 0)
	if err != nil {
		return "", fmt.Errorf("decrypt message: %w", err)
	}

	return string(decryptedMessage.GetBinary()), nil
}

// deriveKey generates a random salt and derives a 32-byte key using PBKDF2.
func deriveKey(passphrase string) (salt, key []byte, err error) {
	salt = make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, nil, fmt.Errorf("derive key: %w", err)
	}
	key = pbkdf2.Key([]byte(passphrase), salt, pbkdf2Iterations, pbkdf2KeyLen, sha256.New)
	return salt, key, nil
}

// deriveKeyFromSalt derives a 32-byte key from a passphrase and existing salt.
func deriveKeyFromSalt(passphrase string, salt []byte) []byte {
	return pbkdf2.Key([]byte(passphrase), salt, pbkdf2Iterations, pbkdf2KeyLen, sha256.New)
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
