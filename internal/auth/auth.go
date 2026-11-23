package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"sote/internal/crypto"
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
	// ErrAccountExists: SOTE runs one account per installation, because the
	// installation has one onion address and every peer request is routed to
	// that single identity.
	ErrAccountExists = errors.New("an account already exists on this device; SOTE supports one account per installation")
	ErrSamePassword  = errors.New("new password must differ from current password")
)

// ValidUsername reports whether name is an acceptable SOTE username.
func ValidUsername(name string) bool { return usernameRegex.MatchString(name) }

// AuthService implements domain.AuthService for user registration and login.
type AuthService struct {
	db     *sql.DB
	crypto *crypto.Service
}

// New creates an AuthService backed by the given database.
func New(db *sql.DB) *AuthService {
	return &AuthService{db: db, crypto: crypto.New()}
}

// Register creates the local account. It validates input, hashes the
// password with bcrypt, generates a GPG key pair, encrypts the private key
// with AES-256-GCM under a PBKDF2 key derived from the password, and stores
// the user. Only one account per installation is allowed.
func (s *AuthService) Register(ctx context.Context, username, password string) (*domain.User, error) {
	if !usernameRegex.MatchString(username) {
		return nil, ErrInvalidUsername
	}
	if len(password) < 8 {
		return nil, ErrWeakPassword
	}

	switch n, err := storage.CountUsers(ctx, s.db); {
	case err != nil:
		return nil, err
	case n > 0:
		if _, err := storage.GetUser(s.db, username); err == nil {
			return nil, ErrDuplicateUser
		}
		return nil, ErrAccountExists
	}

	passwordHash, err := s.crypto.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	privKey, pubKey, err := crypto.GenerateKeyPairFor("sote", username+"@sote.local")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyGenFailed, err)
	}

	encryptedPrivKey, err := s.crypto.EncryptAES(privKey, password)
	if err != nil {
		return nil, fmt.Errorf("encrypt private key: %w", err)
	}

	if err := storage.SaveUser(s.db, username, passwordHash, encryptedPrivKey, pubKey, "", ""); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrDuplicateUser
		}
		return nil, fmt.Errorf("save user: %w", err)
	}

	return &domain.User{
		Username:            username,
		PasswordHash:        passwordHash,
		EncryptedPrivateKey: encryptedPrivKey,
		PublicKey:           pubKey,
		RawPassword:         password,
	}, nil
}

// Login authenticates a user by username and password. A private key still
// stored in an older encryption layout is re-encrypted in the current one.
func (s *AuthService) Login(ctx context.Context, username, password string) (*domain.User, error) {
	su, err := storage.GetUser(s.db, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("get user: %w", err)
	}

	if err := s.crypto.VerifyPassword(su.Password, password); err != nil {
		return nil, ErrInvalidCredentials
	}

	encKey := su.PrivateKey
	if crypto.NeedsUpgrade(encKey) {
		if upgraded, err := s.upgradePrivateKey(ctx, username, encKey, password); err != nil {
			slog.Warn("could not upgrade private key encryption", "error", err)
		} else {
			encKey = upgraded
		}
	}

	return &domain.User{
		Username:            su.Username,
		PasswordHash:        su.Password,
		EncryptedPrivateKey: encKey,
		PublicKey:           su.PublicKey,
		OnionAddress:        su.OnionAddress,
		TorrcPath:           su.TorrcFilePath,
		RawPassword:         password,
	}, nil
}

func (s *AuthService) upgradePrivateKey(ctx context.Context, username string, encKey []byte, password string) ([]byte, error) {
	priv, err := s.crypto.DecryptAES(encKey, password)
	if err != nil {
		return nil, err
	}
	upgraded, err := s.crypto.EncryptAES(priv, password)
	if err != nil {
		return nil, err
	}
	if err := storage.UpdateEncryptedPrivateKey(ctx, s.db, username, upgraded); err != nil {
		return nil, err
	}
	return upgraded, nil
}

// ChangePassword replaces the account password in one transaction: the bcrypt
// hash, the private key (re-encrypted under the new password) and any sent
// messages still stored as password-encrypted AES copies (converted to copies
// encrypted to the account's own key, so they no longer depend on a password).
func (s *AuthService) ChangePassword(ctx context.Context, username, currentPassword, newPassword string) error {
	if len(newPassword) < 8 {
		return ErrWeakPassword
	}
	if currentPassword == newPassword {
		return ErrSamePassword
	}

	su, err := storage.GetUser(s.db, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidCredentials
		}
		return fmt.Errorf("get user: %w", err)
	}
	if err := s.crypto.VerifyPassword(su.Password, currentPassword); err != nil {
		return errors.New("current password is incorrect")
	}

	priv, err := s.crypto.DecryptAES(su.PrivateKey, currentPassword)
	if err != nil {
		return fmt.Errorf("decrypt private key: %w", err)
	}
	id, err := crypto.IdentityFromArmoredPrivateKey(priv)
	if err != nil {
		return err
	}
	defer id.Close()

	newHash, err := s.crypto.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}
	newEncKey, err := s.crypto.EncryptAES(priv, newPassword)
	if err != nil {
		return fmt.Errorf("re-encrypt private key: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	if err := storage.UpdateUserSecrets(ctx, tx, username, newHash, newEncKey); err != nil {
		return err
	}

	legacy, err := storage.LegacySelfCopies(ctx, tx, username)
	if err != nil {
		return err
	}
	unreadable := 0
	for _, m := range legacy {
		plain, err := s.crypto.DecryptAES(m.Message[1:], currentPassword)
		if err != nil {
			// Already unreadable with the current password; nothing to keep.
			unreadable++
			continue
		}
		armored, err := id.EncryptToSelf(plain)
		if err != nil {
			return fmt.Errorf("convert sent message: %w", err)
		}
		if err := storage.UpdateMessageBlob(ctx, tx, m.ID, append([]byte{storage.BlobSelfGPG}, armored...)); err != nil {
			return err
		}
	}
	if unreadable > 0 {
		slog.Warn("sent messages that were already unreadable were left as they are", "count", unreadable)
	}

	return tx.Commit()
}

// Compile-time interface check.
var _ domain.AuthService = (*AuthService)(nil)
