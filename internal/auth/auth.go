package auth

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
	"regexp"
	"strings"

	"github.com/ProtonMail/gopenpgp/v2/crypto"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"

	"sote/internal/domain"
	"sote/internal/storage"
)

var (
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

	ErrInvalidUsername    = errors.New("invalid username: must be 3-32 alphanumeric characters or underscores")
	ErrWeakPassword       = errors.New("password must be at least 8 characters")
	ErrDuplicateUser      = errors.New("username already exists")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrKeyGenFailed       = errors.New("failed to generate GPG key pair")
)

// AuthService implements domain.AuthService for user registration and login.
type AuthService struct {
	db *sql.DB
}

// New creates an AuthService backed by the given database.
func New(db *sql.DB) *AuthService {
	return &AuthService{db: db}
}

// Register creates a new user with the given username and password.
// It validates input, hashes the password with bcrypt, generates GPG keys,
// encrypts the private key with AES (PBKDF2-derived key from raw password),
// and persists the user to storage.
func (s *AuthService) Register(ctx context.Context, username, password string) (*domain.User, error) {
	if !usernameRegex.MatchString(username) {
		return nil, ErrInvalidUsername
	}
	if len(password) < 8 {
		return nil, ErrWeakPassword
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	privKey, pubKey, err := generateGPGKeyPair(username)
	if err != nil {
		return nil, err
	}

	onionAddr := ""
	torrcPath := ""

	encryptedPrivKey, err := encryptAES(privKey, password)
	if err != nil {
		return nil, fmt.Errorf("encrypt private key: %w", err)
	}

	if err := storage.SaveUser(s.db, username, passwordHash, encryptedPrivKey, pubKey, onionAddr, torrcPath); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrDuplicateUser
		}
		return nil, fmt.Errorf("save user: %w", err)
	}

	return &domain.User{
		Username:           username,
		PasswordHash:       passwordHash,
		EncryptedPrivateKey: encryptedPrivKey,
		PublicKey:          pubKey,
		OnionAddress:       onionAddr,
		TorrcPath:          torrcPath,
		RawPassword:        password,
	}, nil
}

// Login authenticates a user by username and password.
// It retrieves the stored bcrypt hash, verifies the password,
// and returns the user with RawPassword set for session use.
func (s *AuthService) Login(ctx context.Context, username, password string) (*domain.User, error) {
	su, err := storage.GetUser(s.db, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("get user: %w", err)
	}

	if err := verifyPassword(su.Password, password); err != nil {
		return nil, ErrInvalidCredentials
	}

	return &domain.User{
		Username:           su.Username,
		PasswordHash:       su.Password,
		EncryptedPrivateKey: su.PrivateKey,
		PublicKey:          su.PublicKey,
		OnionAddress:       su.OnionAddress,
		TorrcPath:          su.TorrcFilePath,
		RawPassword:        password,
	}, nil
}

// hashPassword hashes a password using bcrypt with cost ≥ 12.
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// verifyPassword checks a password against a bcrypt hash.
func verifyPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// generateGPGKeyPair creates a new RSA 2048-bit GPG key pair for the user.
// Returns armored private and public key bytes.
func generateGPGKeyPair(username string) (privateKey, publicKey []byte, err error) {
	key, err := crypto.GenerateKey("sote", username+"@sote.local", "rsa", 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrKeyGenFailed, err)
	}

	armoredPriv, err := key.Armor()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: armor private key: %v", ErrKeyGenFailed, err)
	}

	armoredPub, err := key.GetArmoredPublicKey()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: armor public key: %v", ErrKeyGenFailed, err)
	}

	return []byte(armoredPriv), []byte(armoredPub), nil
}

// encryptAES encrypts data using AES-256-GCM with a PBKDF2-derived key from the passphrase.
// Output format: salt (16 bytes) || nonce (12 bytes) || ciphertext+tag
func encryptAES(data []byte, passphrase string) ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	key := pbkdf2.Key([]byte(passphrase), salt, 100000, 32, sha256.New)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, data, nil)

	result := make([]byte, 0, len(salt)+len(nonce)+len(ciphertext))
	result = append(result, salt...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, nil
}

// decryptAES reverses encryptAES. It extracts the salt and nonce from the
// ciphertext, derives the key using PBKDF2, and decrypts with AES-256-GCM.
func decryptAES(data []byte, passphrase string) ([]byte, error) {
	if len(data) < 16 {
		return nil, errors.New("ciphertext too short: missing salt")
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
		return nil, errors.New("ciphertext too short: missing nonce")
	}

	nonce := data[16 : 16+gcm.NonceSize()]
	ciphertext := data[16+gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}

// Compile-time interface check.
var _ domain.AuthService = (*AuthService)(nil)
