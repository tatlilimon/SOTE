package domain

import (
	"context"
	"net/http"
)

// AuthService handles user registration and authentication.
type AuthService interface {
	Register(ctx context.Context, username, password string) (*User, error)
	Login(ctx context.Context, username, password string) (*User, error)
}

// CryptoService handles all cryptographic operations including key
// generation, password hashing, AES encryption of private keys, and
// GPG message encryption/decryption.
type CryptoService interface {
	HashPassword(password string) (string, error)
	VerifyPassword(hash, password string) error
	GenerateKeyPair() (privateKey, publicKey []byte, err error)
	EncryptAES(data []byte, passphrase string) ([]byte, error)
	DecryptAES(data []byte, passphrase string) ([]byte, error)
	EncryptMessage(plaintext []byte, publicKey []byte) ([]byte, error)
	DecryptMessage(ciphertext []byte, privateKey []byte, passphrase string) (string, error)
}

// StorageService persists and retrieves domain entities.
type StorageService interface {
	SaveUser(ctx context.Context, u *User) error
	GetUser(ctx context.Context, username string) (*User, error)
	SaveContact(ctx context.Context, ownerUsername string, c Contact) error
	GetContactByUsername(ctx context.Context, username string) (*Contact, error)
	GetAllContacts(ctx context.Context, ownerUsername string) ([]Contact, error)
	SaveMessage(ctx context.Context, msg *Message) error
	GetMessages(ctx context.Context, user1, user2 string) ([]Message, error)
}

// TorService manages the Tor hidden service lifecycle.
type TorService interface {
	Start(ctx context.Context) error
	Stop() error
	GetOnionAddress() string
}

// TransportService handles message delivery over the Tor network.
type TransportService interface {
	SendMessage(ctx context.Context, onionAddr string, msg *Message) error
	StartServer(ctx context.Context, addr string, handler http.Handler) error
}
