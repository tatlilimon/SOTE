package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sote/db"
	"sote/tor"
	"sote/user"
	"strings"
	"sync"
	"time"
)

var currentUser *user.User
var mu sync.Mutex
var port string = "18080"

func main() {
	s, _ := os.Stat("server.key")
	if s == nil {
		fmt.Println("SSL Certificates not found. Creating...")
		cmd := exec.Command("openssl", "genrsa", "-out", "server.key", "2048")
		cmd.Run()
		cmd = exec.Command("openssl", "req", "-new", "-x509", "-key", "server.key", "-out", "server.crt", "-days", "3650", "-subj", "/C=US/ST=State/L=City/O=Organization/OU=Department/CN=localhost")
		cmd.Run()
	}
	keyFile := "./server.key"
	certFile := "./server.crt"
	// Initialize database
	db.Initialize()

	http.HandleFunc("/register", registerHandler)
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/get-onion-address", getOnionAddressHandler)
	http.HandleFunc("/add-contact", addContactHandler)
	http.HandleFunc("/receive-contact-request", receiveContactRequestHandler)
	http.HandleFunc("/set-current-user", setCurrentUserHandler)
	http.HandleFunc("/send-message", sendMessageHandler)
	http.HandleFunc("/fetch-messages", fetchMessagesHandler)
	http.HandleFunc("/receive-message", receiveMessageHandler)

	fmt.Printf("Node is running on port %s\n", port)
	log.Fatal(http.ListenAndServeTLS(":"+port, certFile, keyFile, nil))
}

func setCurrentUserHandler(w http.ResponseWriter, r *http.Request) {
	var req user.User
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	mu.Lock()
	currentUser = &req
	mu.Unlock()

	fmt.Println("Current user set to:", currentUser.Username) // Debug print
	w.WriteHeader(http.StatusOK)
}

func getCurrentUser() (*user.User, error) {
	mu.Lock()
	defer mu.Unlock()
	if currentUser == nil {
		return nil, errors.New("no user is currently logged in")
	}
	return currentUser, nil
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	fmt.Println("Received registration request for username:", req.Username) // Debug print

	newUser, err := user.CreateUser(req.Username, req.Password)
	if err != nil {
		fmt.Println("Error creating user:", err) // Debug print
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Println("User created successfully:", newUser.Username) // Debug print

	err = db.SaveUser(newUser.Username, newUser.Password, newUser.PrivateKey, newUser.PublicKey, newUser.OnionAddress, newUser.TorrcFilePath)
	if err != nil {
		fmt.Println("Error saving user:", err) // Debug print
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Println("User saved successfully in the database") // Debug print

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newUser)
	fmt.Println("User has registered successfully!") // Debug print
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Retrieve user data from the database
	uUsername, uPassword, uPrivateKey, uPublicKey, uOnionAddress, uTorrcFilePath, err := db.GetUser(req.Username)
	if err != nil {
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	// Hash the provided password
	hashedPassword := user.HashPassword(req.Password)

	// Compare the hashed password with the stored hashed password
	if hashedPassword != uPassword {
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	currentUser := &user.User{
		Username:      uUsername,
		Password:      uPassword,
		PrivateKey:    uPrivateKey,
		PublicKey:     uPublicKey,
		OnionAddress:  uOnionAddress,
		TorrcFilePath: uTorrcFilePath,
		RawPassword:   req.Password,
	}

	// Start Tor hidden service using the torrc file path
	err = tor.StartTorWithConfig(uTorrcFilePath)
	if err != nil {
		http.Error(w, "Failed to start Tor hidden service", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(currentUser)
}

func getOnionAddressHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, _, _, _, uOnionAddress, _, err := db.GetUser(req.Username)
	if err != nil {
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	currentUser := &user.User{
		OnionAddress: uOnionAddress,
	}

	json.NewEncoder(w).Encode(map[string]string{
		"onionAddress": currentUser.OnionAddress,
	})
}

func addContactHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username     string `json:"username"`
		OnionAddress string `json:"onionAddress"`
		PublicKey    []byte `json:"publicKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Save contact to database
	err := db.SaveContact(currentUser.Username, req.Username, req.OnionAddress, req.PublicKey)
	fmt.Println("Attempting to save contact to database...")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		fmt.Println("Error saving contact:")
		return
	}
	fmt.Println("Contact saved to db successfully")
	w.WriteHeader(http.StatusOK)
}

func receiveContactRequestHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username     string `json:"username"`
		OnionAddress string `json:"onionAddress"`
		PublicKey    []byte `json:"publicKey"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	fmt.Printf("[%v]Incoming contact request from Username: %s, onionAdress:(%s)\n", time.Now(), req.Username, req.OnionAddress)
	fmt.Printf("Do you accept this contact request? (y/n): ")

	var response string
	fmt.Scanln(&response)

	if response == "y" {
		// Get own contact information
		currentUser, err := getCurrentUser()
		if err != nil {
			fmt.Println("Error getting current user:", err) // Debug print
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Check if the contact being added is not the current user
		if req.Username != currentUser.Username && req.OnionAddress != currentUser.OnionAddress {
			// Save contact to database
			err := db.SaveContact(currentUser.Username, req.Username, req.OnionAddress, req.PublicKey)
			if err != nil {
				fmt.Println("Error saving contact:", err) // Debug print
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			fmt.Println("Attempted to add self as contact, skipping...")
		}

		// Clean the .onion address
		cleanedOnionAddress := strings.TrimSpace(req.OnionAddress)

		// Send own contact information back to the requester
		ownContactData := map[string]interface{}{
			"username":     currentUser.Username,
			"onionAddress": currentUser.OnionAddress,
			"publicKey":    currentUser.PublicKey,
		}
		jsonData, err := json.Marshal(ownContactData)
		if err != nil {
			fmt.Println("Error marshalling own contact data:", err) // Debug print
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Configure Tor proxy
		proxyURL, err := url.Parse("socks5://127.0.0.1:9060")
		if err != nil {
			fmt.Println("Error parsing proxy URL:", err) // Debug print
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Create HTTP client with Tor proxy
		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				Proxy:           http.ProxyURL(proxyURL),
			},
		}

		resp, err := client.Post(fmt.Sprintf("https://%s:18080/add-contact", cleanedOnionAddress), "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Println("Error sending own contact data:", err) // Debug print
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			fmt.Println("Failed to send own contact data:", resp.Status) // Debug print
			http.Error(w, "Failed to send own contact data", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusForbidden)
	}
}

func sendMessageHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Sender   string `json:"sender"`
		Receiver string `json:"receiver"`
		Message  string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Encrypt the message
	receiver, err := db.GetContactByUsername(req.Receiver)
	if err != nil {
		http.Error(w, "Receiver not found", http.StatusNotFound)
		return
	}
	encryptedMessage, err := user.EncryptMessage([]byte(req.Message), receiver.PublicKey)
	if err != nil {
		http.Error(w, "Failed to encrypt message", http.StatusInternalServerError)
		return
	}

	// Configure Tor proxy
	proxyURL, err := url.Parse("socks5://127.0.0.1:9060")
	if err != nil {
		http.Error(w, "Failed to parse proxy URL", http.StatusInternalServerError)
		return
	}

	// Create HTTP client with Tor proxy
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			Proxy:           http.ProxyURL(proxyURL),
		},
	}
	// encrypt sended message symmetrically
	ownEncryptedMessage, err := user.EncryptAES256([]byte(req.Message), currentUser.RawPassword)
	if err != nil {
		fmt.Println("Failed to enncrypted sended message: ", err)
		return
	}
	req.Message = string(encryptedMessage)

	// Marshal the updated request struct to JSON
	jsonData, err := json.Marshal(req)
	if err != nil {
		http.Error(w, "Failed to marshal JSON", http.StatusInternalServerError)
		return
	}

	// Clean the receiver's .onion address
	cleanedOnionAddress := strings.TrimSpace(receiver.OnionAddress)

	// Send the message to the receiver's .onion address
	url := fmt.Sprintf("https://%s:18080/receive-message", cleanedOnionAddress)
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("Failed to send message to receiver: %v\n", err)
		http.Error(w, "Failed to send message to receiver", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	fmt.Println("Message posted succesfully")

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Failed to send message: %s\n", resp.Status)
		http.Error(w, fmt.Sprintf("Failed to send message: %s", resp.Status), http.StatusInternalServerError)
		return
	}

	// Save the message to the database
	err = db.SaveMessage(req.Sender, req.Receiver, ownEncryptedMessage)
	if err != nil {
		http.Error(w, "Failed to save message", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func receiveMessageHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Sender   string `json:"sender"`
		Receiver string `json:"receiver"`
		Message  string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Save the encrypted message to the database
	err := db.SaveMessage(req.Sender, req.Receiver, []byte(req.Message))
	if err != nil {
		http.Error(w, "Failed to save message", http.StatusInternalServerError)
		return
	}
	// Print a notification that a message has been received
	fmt.Printf("New message received from %s\n", req.Sender)

	w.WriteHeader(http.StatusOK)
}

func fetchMessagesHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Sender   string `json:"sender"`
		Receiver string `json:"receiver"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Fetch messages from the database
	messages, err := db.GetMessages(req.Sender, req.Receiver)
	if err != nil {
		http.Error(w, "Failed to fetch messages", http.StatusInternalServerError)
		return
	}

	// Check if messages are empty
	if len(messages) == 0 {
		fmt.Println("No messages found")
	}

	// Write the messages as JSON response
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(messages); err != nil {
		http.Error(w, "Failed to encode messages to JSON", http.StatusInternalServerError)
		return
	}
}
