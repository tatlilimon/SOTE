package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sote/db"
	"sote/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cretz/bine/tor"
	"github.com/mdp/qrterminal/v3"
	"github.com/urfave/cli/v2"
	"golang.org/x/term"
)

var currentUser *user.User
var torInstance *tor.Tor
var version string = "SOTE_Alpha_v1.0"
var client = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

func main() {
	app := &cli.App{
		Name:  "SOTE Client",
		Usage: "A decentralized messaging app client",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "v",
				Usage: "Prints the version of the app",
				Action: func(c *cli.Context, v bool) error {
					fmt.Println(version)
					return nil
				},
			},
		},
		Commands: []*cli.Command{
			{
				Name:   "start",
				Usage:  "Start the client",
				Action: startClient,
			},
		},
		Before: func(c *cli.Context) error {
			// Set the TOR_SOCKS_PORT environment variable
			os.Setenv("TOR_SOCKS_PORT", "9060")

			// Start Tor service
			var err error
			torInstance, err = tor.Start(context.TODO(), nil)
			if err != nil {
				return err
			}
			db.Initialize()
			return nil
		},
		After: func(c *cli.Context) error {
			if torInstance != nil {
				return torInstance.Close()
			}
			return nil
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func startClient(c *cli.Context) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Do you want to (l)ogin or (r)egister? ")
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	if choice == "l" {
		if err := loginUser(); err != nil {
			return err
		}
	} else if choice == "r" {
		if err := registerUser(); err != nil {
			return err
		}
	} else {
		fmt.Println("Invalid choice")
		return nil
	}

	return showMainMenu()
}

func registerUser() error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter username: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)

	fmt.Print("Enter password: ")
	bytePassword, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		fmt.Println("error while reading password", err)
	}
	password := string(bytePassword)
	password = strings.TrimSpace(string(password))
	fmt.Print("\nEnter password again for verification:")
	bytePassword2, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		fmt.Println("error while reading password", err)
	}
	password2 := string(bytePassword2)
	password2 = strings.TrimSpace(string(password2))
	bytePassword2 = nil
	if password != password2 {
		fmt.Printf("\nYou entered different passwords. Please try again.\n")
		return registerUser()
	}
	// Send registration request to the node
	userData := map[string]string{
		"username": username,
		"password": password,
	}
	jsonData, err := json.Marshal(userData)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\nSending registration request...\n") // Debug print

	url := "https://localhost:18080/register"

	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		log.Fatalf("Failed to register user: %s", resp.Status)
	}

	fmt.Println("User registered successfully")
	return loginUser()
}

func loginUser() error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter username: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)

	fmt.Print("Enter password: ")
	// Reads password without revealing it on CLI
	bytePassword, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		fmt.Println("error while reading password", err)
	}
	password := string(bytePassword)
	password = strings.TrimSpace(string(password))

	// Send login request to the node
	userData := map[string]string{
		"username": username,
		"password": password,
	}
	jsonData, err := json.Marshal(userData)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Post(("https://localhost:18080/login"), "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Failed to login: %s", resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(&currentUser); err != nil {
		log.Fatal(err)
	}

	// Set the current user in the node process
	setCurrentUserInNode(currentUser)

	fmt.Printf("\nUser logged in: %s\n", currentUser.Username)
	return nil
}

func setCurrentUserInNode(user *user.User) {
	jsonData, err := json.Marshal(user)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Post(("https://localhost:18080/set-current-user"), "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Failed to set current user in node: %s", resp.Status)
	}
}

func showMainMenu() error {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println("\nMain Menu:")
		fmt.Println("____________________________")
		fmt.Println("|1| => |Show QR code|")
		fmt.Println("____________________________")
		fmt.Println("|2| => |Get .onion address|")
		fmt.Println("____________________________")
		fmt.Println("|3| => |Add contact|")
		fmt.Println("____________________________")
		fmt.Println("|4| => |Get contacts|")
		fmt.Println("____________________________")
		fmt.Println("|5| => |Send message|")
		fmt.Println("____________________________")
		fmt.Println("|6| => |Fetch messages|")
		fmt.Println("____________________________")
		fmt.Println("|7| => |Exit|")
		fmt.Println("____________________________")
		fmt.Print("Enter your choice => ")

		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)

		switch choice {
		case "1":
			showQR()
		case "2":
			getOnionAddress()
		case "3":
			addContact()
		case "4":
			getContacts()
		case "5":
			sendMessage()
		case "6":
			fetchMessages()
		case "7":
			fmt.Println("Exiting...")
			return nil
		default:
			fmt.Println("Invalid choice")
		}
	}
}

func getContacts() ([]user.Contact, error) {
	contacts, err := db.GetAllContacts()
	if err != nil {
		return nil, err
	}

	fmt.Println("Contacts:")
	for i, contact := range contacts {
		fmt.Printf("Contact %d: Username: %s\n", i+1, contact.Username)
	}
	return contacts, nil
}

func addContact() error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter the .onion address of the contact: ")
	onionAddress, _ := reader.ReadString('\n')
	onionAddress = strings.TrimSpace(onionAddress)

	// Wait at most a few minutes to start network and get a connection
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer dialCancel()

	// Make connection
	dialer, err := torInstance.Dialer(dialCtx, &tor.DialConf{
		ProxyAddress: "127.0.0.1:9060",
	})
	if err != nil {
		log.Fatal("Can not dial to the proxy address", err)
	}

	// Create HTTP client
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, addr)
			},
		},
	}

	// Send contact request to the given .onion address
	contactData := map[string]interface{}{
		"username":     currentUser.Username,
		"onionAddress": currentUser.OnionAddress,
		"publicKey":    currentUser.PublicKey,
	}
	jsonData, err := json.Marshal(contactData)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Sending contact request to:", onionAddress) // Debug print
	resp, err := client.Post(fmt.Sprintf("https://%s:18080/receive-contact-request", onionAddress), "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatalf("Failed to send contact request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Failed to send contact request: %s", resp.Status)
		fmt.Println(resp.Body)
		fmt.Println(resp.Header)
		fmt.Println(resp.Request.Header)
	}

	// Send contact data to the local addContactHandler
	localResp, err := client.Post(("https://localhost:18080/add-contact"), "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatalf("Failed to save contact... %v", err)
	}
	defer localResp.Body.Close()

	if localResp.StatusCode != http.StatusOK {
		log.Fatalf("Failed to save contact... %s", localResp.Status)
	}

	fmt.Println("Contact request sent and saved successfully")
	return nil
}

func getOnionAddress() error {
	if currentUser == nil {
		fmt.Println("No user logged in.")
		return nil
	}

	// Send request to get onion address
	userData := map[string]string{
		"username": currentUser.Username,
		"password": currentUser.Password,
	}
	jsonData, err := json.Marshal(userData)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Post(("https://localhost:18080/get-onion-address"), "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Failed to get onion address: %s", resp.Status)
	}

	var response map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Your .onion address: %s\n", response["onionAddress"])
	return nil
}

func showQR() error {
	if currentUser == nil {
		fmt.Println("No user logged in.")
		return nil
	}

	qrterminal.GenerateWithConfig(currentUser.OnionAddress, qrterminal.Config{
		Level:     qrterminal.M,
		Writer:    os.Stdout,
		BlackChar: qrterminal.BLACK,
		WhiteChar: qrterminal.WHITE,
		QuietZone: 1,
	})
	return nil
}

func sendMessage() error {
	contacts, err := getContacts()
	if err != nil {
		return err
	}

	if len(contacts) == 0 {
		fmt.Println("No contacts available.")
		return nil
	}

	fmt.Println("Select a contact to send a message:")
	for i, contact := range contacts {
		fmt.Printf("%d. %s\n", i+1, contact.Username)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter the number of the contact: ")
	choiceStr, _ := reader.ReadString('\n')
	choiceStr = strings.TrimSpace(choiceStr)
	choice, err := strconv.Atoi(choiceStr)
	if err != nil || choice < 1 || choice > len(contacts) {
		fmt.Println("Invalid choice")
		return nil
	}

	selectedContact := contacts[choice-1]

	fmt.Print("Enter your message: ")
	message, _ := reader.ReadString('\n')
	message = strings.TrimSpace(message)
	// Send the message to the node
	messageData := map[string]interface{}{
		"sender":   currentUser.Username,
		"receiver": selectedContact.Username,
		"message":  message,
	}
	jsonData, err := json.Marshal(messageData)
	if err != nil {
		return err
	}

	resp, err := client.Post(("https://localhost:18080/send-message"), "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to send message: %s", resp.Status)
	}

	fmt.Println("Message sent successfully")
	return nil
}

func fetchMessages() error {
	contacts, err := getContacts()
	if err != nil {
		return err
	}

	if len(contacts) == 0 {
		fmt.Println("No contacts available.")
		return nil
	}

	fmt.Println("Select a contact to fetch messages:")
	for i, contact := range contacts {
		fmt.Printf("%d. %s\n", i+1, contact.Username)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter the number of the contact: ")
	choiceStr, _ := reader.ReadString('\n')
	choiceStr = strings.TrimSpace(choiceStr)
	choice, err := strconv.Atoi(choiceStr)
	if err != nil || choice < 1 || choice > len(contacts) {
		fmt.Println("Invalid choice")
		return nil
	}

	selectedContact := contacts[choice-1]

	// Send request to fetch messages from the node
	messageData := map[string]interface{}{
		"sender":   currentUser.Username,
		"receiver": selectedContact.Username,
	}
	jsonData, err := json.Marshal(messageData)
	if err != nil {
		return err
	}

	resp, err := client.Post(("https://localhost:18080/fetch-messages"), "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch messages: %s", resp.Status)
	}

	var messages []db.Message
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return err
	}

	if len(messages) == 0 {
		fmt.Println("No messages found.")
		return nil
	}

	fmt.Println("Messages with", selectedContact.Username)
	for _, msg := range messages {
		if currentUser.Username == msg.Sender {
			decryptedMessage, err := user.DecryptAES256(msg.Message, currentUser.RawPassword)
			if err != nil {
				fmt.Println("Failed to decrypt sended message: ", err)
				return err
			}
			fmt.Printf("[%s] %s: %s\n", msg.Timestamp, msg.Sender, decryptedMessage)
		} else {
			decryptedMessage, err := user.DecryptMessage(msg.Message, currentUser.PrivateKey, currentUser.RawPassword)
			if err != nil {
				return err
			}
			fmt.Printf("[%s] %s: %s\n", msg.Timestamp, msg.Sender, decryptedMessage)
		}
	}

	return nil
}
