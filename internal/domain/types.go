package domain

// User represents an authenticated SOTE user with their cryptographic
// identity and Tor hidden service configuration.
type User struct {
	Username            string `json:"username"`
	PasswordHash        string `json:"password_hash"`
	EncryptedPrivateKey []byte `json:"encrypted_private_key"`
	PublicKey           []byte `json:"public_key"`
	OnionAddress        string `json:"onion_address"`
	TorrcPath           string `json:"torrc_path"`
	RawPassword         string `json:"-"`
}

// Contact represents a saved peer for messaging over Tor.
type Contact struct {
	Username     string `json:"username"`
	OnionAddress string `json:"onion_address"`
	PublicKey    []byte `json:"public_key"`
}

// Message represents an encrypted message between two users.
type Message struct {
	Sender           string `json:"sender"`
	Receiver         string `json:"receiver"`
	EncryptedContent []byte `json:"encrypted_content"`
	Timestamp        string `json:"timestamp"`
}
