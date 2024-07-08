package db

import (
	"database/sql"
	"fmt"
	"log"
	"sote/user"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

// Initialize initializes the database
func Initialize() {
	var err error
	db, err = sql.Open("sqlite3", "./localDB.db")
	if err != nil {
		log.Fatal(err)
	}

	createTable()
}

// createTable creates the user and contacts tables if they don't exist
func createTable() {
	createUserTableSQL := `CREATE TABLE IF NOT EXISTS user (
        "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
        "username" TEXT UNIQUE,
        "password" TEXT,
        "privateKey" BLOB,
        "publicKey" BLOB,
        "onionAddress" TEXT,
        "torrcFilePath" TEXT
    );`

	createContactsTableSQL := `CREATE TABLE IF NOT EXISTS contacts (
        "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
        "username" TEXT,
        "contactUsername" TEXT,
        "contactOnionAddress" TEXT,
        "contactPublicKey" BLOB
    );`

	createMessagesTableSQL := `CREATE TABLE IF NOT EXISTS messages (
        "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
        "sender" TEXT,
        "receiver" TEXT,
        "message" BLOB,
        "timestamp" DATETIME DEFAULT CURRENT_TIMESTAMP
    );`

	_, err := db.Exec(createUserTableSQL)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(createContactsTableSQL)
	if err != nil {
		log.Fatal(err)
	}

	_, err = db.Exec(createMessagesTableSQL)
	if err != nil {
		log.Fatal(err)
	}
}

// Message struct to hold message information
type Message struct {
	Sender    string
	Receiver  string
	Message   []byte
	Timestamp string
}

// SaveUser saves a user to the database
func SaveUser(username, password string, privateKey, publicKey []byte, onionAddress, torrcFilePath string) error {
	insertUserSQL := `INSERT INTO user (username, password, privateKey, publicKey, onionAddress, torrcFilePath) VALUES (?, ?, ?, ?, ?, ?)`
	statement, err := db.Prepare(insertUserSQL)
	if err != nil {
		return err
	}
	_, err = statement.Exec(username, password, privateKey, publicKey, onionAddress, torrcFilePath)
	if err != nil {
		log.Println("Error saving user:", err) // Debug print
	}
	return err
}

// GetUser retrieves a user by username
func GetUser(username string) (string, string, []byte, []byte, string, string, error) {
	row := db.QueryRow("SELECT username, password, privateKey, publicKey, onionAddress, torrcFilePath FROM user WHERE username = ?", username)

	var (
		uUsername      string
		uPassword      string
		uOnionAddress  string
		uPrivateKey    []byte
		uPublicKey     []byte
		uTorrcFilePath string
	)
	err := row.Scan(&uUsername, &uPassword, &uPrivateKey, &uPublicKey, &uOnionAddress, &uTorrcFilePath)
	if err != nil {
		return "", "", nil, nil, "", "", err
	}
	return uUsername, uPassword, uPrivateKey, uPublicKey, uOnionAddress, uTorrcFilePath, nil
}

// SaveContact saves a contact to the database
func SaveContact(username, contactUsername, contactOnionAddress string, contactPublicKey []byte) error {
	c, err := GetContactByUsername(contactUsername)
	if err != nil {
		return nil
	}
	if username == contactUsername {
		// Check if username is the same as contactUsername
		return nil
	} else if c.Username == contactUsername {
		// Check if contactUsername is already in the database
		fmt.Println("Contact already exists")
		return nil
	} else {
		insertContactSQL := `INSERT INTO contacts (username, contactUsername, contactOnionAddress, contactPublicKey) VALUES (?, ?, ?, ?)`
		statement, err := db.Prepare(insertContactSQL)
		if err != nil {
			return err
		}
		_, err = statement.Exec(username, contactUsername, contactOnionAddress, contactPublicKey)
		return err
	}
}

// GetAllContacts retrieves all contacts from the database
func GetAllContacts() ([]user.Contact, error) {
	rows, err := db.Query("SELECT username, contactUsername, contactOnionAddress, contactPublicKey FROM contacts")
	if err != nil {
		log.Println("Error querying contacts:", err) // Debug print
		return nil, err
	}
	defer rows.Close()

	var contacts []user.Contact
	for rows.Next() {
		var contact user.Contact
		var username sql.NullString
		var contactUsername sql.NullString
		var contactOnionAddress sql.NullString
		var contactPublicKey []byte

		err := rows.Scan(&username, &contactUsername, &contactOnionAddress, &contactPublicKey)
		if err != nil {
			log.Println("Error scanning contact:", err) // Debug print
			return nil, err
		}

		contact.Username = contactUsername.String
		contact.OnionAddress = contactOnionAddress.String
		contact.PublicKey = contactPublicKey

		contacts = append(contacts, contact)
	}
	return contacts, nil
}

// GetContactByUsername retrieves specified contact from the database
func GetContactByUsername(username string) (user.Contact, error) {
	var contact user.Contact

	row := db.QueryRow("SELECT contactUsername, contactOnionAddress, contactPublicKey FROM contacts WHERE contactUsername = ?", username)

	var contactUsername string
	var contactOnionAddress string
	var contactPublicKey []byte

	err := row.Scan(&contactUsername, &contactOnionAddress, &contactPublicKey)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Println("No contact found for username:", username) // Debug print
			return contact, nil                                     // Return an empty contact and no error
		}
		log.Println("Error scanning contact:", err) // Debug print
		return contact, err
	}

	contact.Username = contactUsername
	contact.OnionAddress = contactOnionAddress
	contact.PublicKey = contactPublicKey
	return contact, nil
}

// GetMessages retrieves messages between two users from the database
func GetMessages(sender, receiver string) ([]Message, error) {
	rows, err := db.Query("SELECT sender, receiver, message, timestamp FROM messages WHERE (sender = ? AND receiver = ?) OR (sender = ? AND receiver = ?)", sender, receiver, receiver, sender)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		err := rows.Scan(&msg.Sender, &msg.Receiver, &msg.Message, &msg.Timestamp)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

// SaveMessage saves a message to the database with a timestamp
func SaveMessage(sender, receiver string, message []byte) error {
	insertMessageSQL := `INSERT INTO messages (sender, receiver, message, timestamp) VALUES (?, ?, ?, CURRENT_TIMESTAMP)`
	statement, err := db.Prepare(insertMessageSQL)
	if err != nil {
		return err
	}
	_, err = statement.Exec(sender, receiver, message)
	return err
}
