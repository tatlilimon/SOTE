package domain

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserJSONRoundtrip(t *testing.T) {
	u := User{
		Username:            "alice",
		PasswordHash:        "$2a$10$hashedpassword",
		EncryptedPrivateKey: []byte("encrypted-priv-key-data"),
		PublicKey:           []byte("public-key-data"),
		OnionAddress:        "alice1234.onion",
		TorrcPath:           "/tmp/torrc-alice",
		RawPassword:         "supersecret",
	}

	data, err := json.Marshal(u)
	require.NoError(t, err)

	// RawPassword must NOT appear in JSON (json:"-")
	var raw map[string]interface{}
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	_, hasRaw := raw["RawPassword"]
	assert.False(t, hasRaw, "RawPassword must be omitted from JSON serialization")

	// Verify all expected fields are present
	assert.Equal(t, "alice", raw["username"])
	assert.Equal(t, "$2a$10$hashedpassword", raw["password_hash"])
	assert.Equal(t, "alice1234.onion", raw["onion_address"])
	assert.Equal(t, "/tmp/torrc-alice", raw["torrc_path"])

	// EncryptedPrivateKey and PublicKey are []byte → base64 in JSON
	encodedPriv, ok := raw["encrypted_private_key"].(string)
	assert.True(t, ok, "encrypted_private_key must be a string (base64)")
	assert.NotEmpty(t, encodedPriv)

	encodedPub, ok := raw["public_key"].(string)
	assert.True(t, ok, "public_key must be a string (base64)")
	assert.NotEmpty(t, encodedPub)

	// Roundtrip: unmarshal back
	var u2 User
	err = json.Unmarshal(data, &u2)
	require.NoError(t, err)

	assert.Equal(t, u.Username, u2.Username)
	assert.Equal(t, u.PasswordHash, u2.PasswordHash)
	assert.Equal(t, u.OnionAddress, u2.OnionAddress)
	assert.Equal(t, u.TorrcPath, u2.TorrcPath)
	assert.Equal(t, u.EncryptedPrivateKey, u2.EncryptedPrivateKey)
	assert.Equal(t, u.PublicKey, u2.PublicKey)
	// RawPassword should be empty after unmarshal (wasn't serialized)
	assert.Empty(t, u2.RawPassword)
}

func TestContactJSONRoundtrip(t *testing.T) {
	c := Contact{
		Username:     "bob",
		OnionAddress: "bob5678.onion",
		PublicKey:    []byte("bob-public-key"),
	}

	data, err := json.Marshal(c)
	require.NoError(t, err)

	var c2 Contact
	err = json.Unmarshal(data, &c2)
	require.NoError(t, err)

	assert.Equal(t, c.Username, c2.Username)
	assert.Equal(t, c.OnionAddress, c2.OnionAddress)
	assert.Equal(t, c.PublicKey, c2.PublicKey)
}

func TestMessageJSONRoundtrip(t *testing.T) {
	m := Message{
		Sender:           "alice",
		Receiver:         "bob",
		EncryptedContent: []byte("encrypted-message-data"),
		Timestamp:        "2026-06-05T12:00:00Z",
	}

	data, err := json.Marshal(m)
	require.NoError(t, err)

	var m2 Message
	err = json.Unmarshal(data, &m2)
	require.NoError(t, err)

	assert.Equal(t, m.Sender, m2.Sender)
	assert.Equal(t, m.Receiver, m2.Receiver)
	assert.Equal(t, m.EncryptedContent, m2.EncryptedContent)
	assert.Equal(t, m.Timestamp, m2.Timestamp)
}

// compile-time interface satisfaction checks
// These are assertions, not runtime tests — they verify the interfaces
// are defined with the correct type signatures.
func TestInterfacesCompile(t *testing.T) {
	var _ AuthService = (*authServiceStub)(nil)
	var _ CryptoService = (*cryptoServiceStub)(nil)
	var _ StorageService = (*storageServiceStub)(nil)
	var _ TorService = (*torServiceStub)(nil)
	var _ TransportService = (*transportServiceStub)(nil)
}

// Stub types for compile-time interface checks

type authServiceStub struct{}

func (a *authServiceStub) Register(ctx context.Context, username, password string) (*User, error) {
	return nil, nil
}
func (a *authServiceStub) Login(ctx context.Context, username, password string) (*User, error) {
	return nil, nil
}

type cryptoServiceStub struct{}

func (c *cryptoServiceStub) HashPassword(password string) (string, error) {
	return "", nil
}
func (c *cryptoServiceStub) VerifyPassword(hash, password string) error {
	return nil
}
func (c *cryptoServiceStub) GenerateKeyPair() (privateKey, publicKey []byte, err error) {
	return nil, nil, nil
}
func (c *cryptoServiceStub) EncryptAES(data []byte, passphrase string) ([]byte, error) {
	return nil, nil
}
func (c *cryptoServiceStub) DecryptAES(data []byte, passphrase string) ([]byte, error) {
	return nil, nil
}
func (c *cryptoServiceStub) EncryptMessage(plaintext []byte, publicKey []byte) ([]byte, error) {
	return nil, nil
}
func (c *cryptoServiceStub) DecryptMessage(ciphertext []byte, privateKey []byte, passphrase string) (string, error) {
	return "", nil
}

type storageServiceStub struct{}

func (s *storageServiceStub) SaveUser(ctx context.Context, u *User) error {
	return nil
}
func (s *storageServiceStub) GetUser(ctx context.Context, username string) (*User, error) {
	return nil, nil
}
func (s *storageServiceStub) SaveContact(ctx context.Context, ownerUsername string, c Contact) error {
	return nil
}
func (s *storageServiceStub) GetContactByUsername(ctx context.Context, username string) (*Contact, error) {
	return nil, nil
}
func (s *storageServiceStub) GetAllContacts(ctx context.Context, ownerUsername string) ([]Contact, error) {
	return nil, nil
}
func (s *storageServiceStub) SaveMessage(ctx context.Context, msg *Message) error {
	return nil
}
func (s *storageServiceStub) GetMessages(ctx context.Context, user1, user2 string) ([]Message, error) {
	return nil, nil
}

type torServiceStub struct{}

func (t *torServiceStub) Start(ctx context.Context) error {
	return nil
}
func (t *torServiceStub) Stop() error {
	return nil
}
func (t *torServiceStub) GetOnionAddress() string {
	return ""
}

type transportServiceStub struct{}

func (t *transportServiceStub) SendMessage(ctx context.Context, onionAddr string, msg *Message) error {
	return nil
}
func (t *transportServiceStub) StartServer(ctx context.Context, addr string, handler http.Handler) error {
	return nil
}
