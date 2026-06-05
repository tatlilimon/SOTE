// Package crypto provides cryptographic operations for SOTE.
//
// Reference implementation preserved from the original codebase (user/user.go).
// This file serves as a reference for the cryptographic primitives used:
//   - AES-256-GCM encryption/decryption for private key storage
//   - SHA-256 based password hashing (21-round iterative hash)
//   - GPG asymmetric encryption via ProtonMail/gopenpgp
//
// These functions will be refactored and hardened in subsequent tasks.

package crypto

// Original AES-256-GCM encryption function (from user/user.go):
//
//	func EncryptAES256(data []byte, passphrase string) ([]byte, error) {
//		key := createHash(passphrase)
//		block, err := aes.NewCipher(key)
//		if err != nil {
//			return nil, err
//		}
//		gcm, err := cipher.NewGCM(block)
//		if err != nil {
//			return nil, err
//		}
//		nonce := make([]byte, gcm.NonceSize())
//		if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
//			return nil, err
//		}
//		ciphertext := gcm.Seal(nonce, nonce, data, nil)
//		return ciphertext, nil
//	}

// Original AES-256-GCM decryption function (from user/user.go):
//
//	func DecryptAES256(data []byte, passphrase string) ([]byte, error) {
//		key := createHash(passphrase)
//		block, err := aes.NewCipher(key)
//		if err != nil {
//			return nil, err
//		}
//		gcm, err := cipher.NewGCM(block)
//		if err != nil {
//			return nil, err
//		}
//		nonceSize := gcm.NonceSize()
//		if len(data) < nonceSize {
//			return nil, fmt.Errorf("ciphertext too short")
//		}
//		nonce, ciphertext := data[:nonceSize], data[nonceSize:]
//		plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
//		if err != nil {
//			return nil, err
//		}
//		return plaintext, nil
//	}

// Original SHA-256 key derivation (from user/user.go):
//
//	func createHash(passphrase string) []byte {
//		hash := sha256.Sum256([]byte(passphrase))
//		return hash[:]
//	}

// Original password hashing (from user/user.go) — 21-round SHA-256:
//
//	func HashPassword(password string) string {
//		hashedPassword := []byte(password)
//		for i := 0; i < 21; i++ {
//			hash := sha256.Sum256(hashedPassword)
//			hashedPassword = hash[:]
//		}
//		return fmt.Sprintf("%x", hashedPassword)
//	}

// Original GPG message encryption (from user/user.go):
//
//	func EncryptMessage(message []byte, publicKey []byte) ([]byte, error) {
//		keyRing, err := crypto.NewKeyFromArmored(string(publicKey))
//		if err != nil {
//			return nil, fmt.Errorf("error creating key from armored public key: %v", err)
//		}
//		keyRingObj, err := crypto.NewKeyRing(keyRing)
//		if err != nil {
//			return nil, fmt.Errorf("error creating key ring: %v", err)
//		}
//		plainMessage := crypto.NewPlainMessage(message)
//		encryptedMessage, err := keyRingObj.Encrypt(plainMessage, nil)
//		if err != nil {
//			return nil, fmt.Errorf("error encrypting message: %v", err)
//		}
//		encryptedData, _ := encryptedMessage.GetArmored()
//		return []byte(encryptedData), nil
//	}

// Original GPG message decryption (from user/user.go):
//
//	func DecryptMessage(encryptedMessage []byte, privateKey []byte, passphrase string) (string, error) {
//		decryptedPrivateKey, err := DecryptAES256(privateKey, passphrase)
//		if err != nil {
//			return "", fmt.Errorf("error decrypting private key: %v", err)
//		}
//		key, err := crypto.NewKeyFromArmored(string(decryptedPrivateKey))
//		if err != nil {
//			return "", fmt.Errorf("error creating key from armored key: %v", err)
//		}
//		keyRing, err := crypto.NewKeyRing(key)
//		if err != nil {
//			return "", fmt.Errorf("error creating key ring: %v", err)
//		}
//		message, _ := crypto.NewPGPMessageFromArmored(string(encryptedMessage))
//		if message == nil {
//			return "", err
//		}
//		decryptedMessage, err := keyRing.Decrypt(message, nil, 0)
//		if err != nil {
//			return "",
//				fmt.Errorf("error decrypting message: %v", err)
//		}
//		return string(decryptedMessage.GetBinary()), nil
//	}
