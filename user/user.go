package user

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"sote/tor"

	"github.com/ProtonMail/gopenpgp/v2/crypto"
)

// User struct to hold user information
type User struct {
	Username      string
	Password      string
	PrivateKey    []byte
	PublicKey     []byte
	OnionAddress  string
	TorrcFilePath string
	RawPassword   string
}

// Contact struct to hold contact information
type Contact struct {
	Username     string
	OnionAddress string
	PublicKey    []byte
}

// CreateUser creates a new user with a username and password
func CreateUser(username, password string) (*User, error) {
	// Hash the password
	hashedPassword := HashPassword(password)

	// Generate GPG keys
	keyRing, err := crypto.GenerateKey(username, "", "rsa", 2048)
	if err != nil {
		fmt.Println("Error generating GPG keys:", err) // Debug print
		return nil, err
	}
	privateKey, err := keyRing.Armor()
	if err != nil {
		fmt.Println("Error armoring private key:", err) // Debug print
		return nil, err
	}
	publicKey, err := keyRing.GetArmoredPublicKey()
	if err != nil {
		fmt.Println("Error getting armored public key:", err) // Debug print
		return nil, err
	}

	// Encrypt the private key with AES256
	encryptedPrivateKey, err := EncryptAES256([]byte(privateKey), password)
	if err != nil {
		fmt.Println("Error encrypting private key:", err) // Debug print
		return nil, err
	}

	// Generate .onion address and get the torrc file path
	onionAddress, torrcFilePath, err := tor.GenerateOnionAddress()
	if err != nil {
		fmt.Println("Error generating .onion address:", err) // Debug print
		return nil, err
	}

	user := &User{
		Username:      username,
		Password:      hashedPassword,
		PrivateKey:    encryptedPrivateKey,
		PublicKey:     []byte(publicKey),
		OnionAddress:  onionAddress,
		TorrcFilePath: torrcFilePath,
		RawPassword:   password,
	}
	return user, nil
}

// encryptAES256 encrypts data using AES256
func EncryptAES256(data []byte, passphrase string) ([]byte, error) {
	key := createHash(passphrase)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return ciphertext, nil
}

// createHash creates a SHA256 hash of the passphrase and returns a 32-byte key
func createHash(passphrase string) []byte {
	hash := sha256.Sum256([]byte(passphrase))
	return hash[:]
}

// HashPassword hashes the password using SHA-256 21 times
func HashPassword(password string) string {
	hashedPassword := []byte(password)
	for i := 0; i < 21; i++ {
		hash := sha256.Sum256(hashedPassword)
		hashedPassword = hash[:]
	}
	return fmt.Sprintf("%x", hashedPassword)
}

func EncryptMessage(message []byte, publicKey []byte) ([]byte, error) {
	keyRing, err := crypto.NewKeyFromArmored(string(publicKey))
	if err != nil {
		return nil, fmt.Errorf("error creating key from armored public key: %v", err)
	}
	keyRingObj, err := crypto.NewKeyRing(keyRing)
	if err != nil {
		return nil, fmt.Errorf("error creating key ring: %v", err)
	}
	plainMessage := crypto.NewPlainMessage(message)
	encryptedMessage, err := keyRingObj.Encrypt(plainMessage, nil)
	if err != nil {
		return nil, fmt.Errorf("error encrypting message: %v", err)
	}
	encryptedData, _ := encryptedMessage.GetArmored()
	return []byte(encryptedData), nil
}

func DecryptMessage(encryptedMessage []byte, privateKey []byte, passphrase string) (string, error) {
	// Decrypt the private key using AES256
	decryptedPrivateKey, err := DecryptAES256(privateKey, passphrase)
	if err != nil {
		return "", fmt.Errorf("error decrypting private key: %v", err)
	}

	// Create a key from the decrypted private key
	key, err := crypto.NewKeyFromArmored(string(decryptedPrivateKey))
	if err != nil {
		return "", fmt.Errorf("error creating key from armored key: %v", err)
	}

	// Create a key ring from the key
	keyRing, err := crypto.NewKeyRing(key)
	if err != nil {
		return "", fmt.Errorf("error creating key ring: %v", err)
	}
	// Create a PGPMessage from the encrypted message
	message, _ := crypto.NewPGPMessageFromArmored(string(encryptedMessage))
	if message == nil {
		fmt.Println("Error creating PGPMessage", err)
		return "", err
	}

	// Decrypt the message
	decryptedMessage, err := keyRing.Decrypt(message, nil, 0)
	if err != nil {
		fmt.Println("error decrypting message", err)
		return "",
			fmt.Errorf("error decrypting message: %v", err)
	}

	return string(decryptedMessage.GetBinary()), nil
}

func DecryptAES256(data []byte, passphrase string) ([]byte, error) {
	key := createHash(passphrase)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}
