// Package storage provides persistent data storage using SQLite.
//
// Reference database schema preserved from the original codebase (db/db.go).
// The schema defines three tables: user, contacts, and messages.
// This file serves as a reference for the storage layer that will be
// re-implemented using modernc.org/sqlite (pure Go, no CGo).

package storage

// Original SQLite schema (from db/db.go):
//
// Table: user
//
//	CREATE TABLE IF NOT EXISTS user (
//	    "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
//	    "username" TEXT UNIQUE,
//	    "password" TEXT,
//	    "privateKey" BLOB,
//	    "publicKey" BLOB,
//	    "onionAddress" TEXT,
//	    "torrcFilePath" TEXT
//	);
//
// Table: contacts
//
//	CREATE TABLE IF NOT EXISTS contacts (
//	    "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
//	    "username" TEXT,
//	    "contactUsername" TEXT,
//	    "contactOnionAddress" TEXT,
//	    "contactPublicKey" BLOB
//	);
//
// Table: messages
//
//	CREATE TABLE IF NOT EXISTS messages (
//	    "id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
//	    "sender" TEXT,
//	    "receiver" TEXT,
//	    "message" BLOB,
//	    "timestamp" DATETIME DEFAULT CURRENT_TIMESTAMP
//	);
//
// Original Message struct (from db/db.go):
//
//	type Message struct {
//		Sender    string
//		Receiver  string
//		Message   []byte
//		Timestamp string
//	}
//
// Original User/Contact structs (from user/user.go):
//
//	type User struct {
//		Username      string
//		Password      string
//		PrivateKey    []byte
//		PublicKey     []byte
//		OnionAddress  string
//		TorrcFilePath string
//		RawPassword   string
//	}
//
//	type Contact struct {
//		Username     string
//		OnionAddress string
//		PublicKey    []byte
//	}
