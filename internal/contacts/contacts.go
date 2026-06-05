// Package contacts manages the user's contact list and contact-related operations.
package contacts

import (
	"context"
	"database/sql"
	"fmt"

	"sote/internal/domain"
	"sote/internal/storage"
)

// Service provides contact management operations using the storage layer.
type Service struct {
	db *sql.DB
}

// NewService creates a new contacts Service with the given database handle.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// AddContact adds a new contact for the given owner. It delegates validation
// (duplicate detection, self-add check) to the storage layer.
// Returns storage.ErrContactExists if the contact is already present,
// or storage.ErrSelfAdd if the owner tries to add themselves.
func (s *Service) AddContact(ctx context.Context, ownerUsername string, contact domain.Contact) error {
	err := storage.SaveContact(s.db, ownerUsername, contact.Username, contact.OnionAddress, contact.PublicKey)
	if err != nil {
		return fmt.Errorf("add contact: %w", err)
	}
	return nil
}

// GetContacts returns all contacts for the given user.
func (s *Service) GetContacts(ctx context.Context, username string) ([]storage.Contact, error) {
	contacts, err := storage.GetAllContacts(s.db, username)
	if err != nil {
		return nil, fmt.Errorf("get contacts: %w", err)
	}
	return contacts, nil
}

// GetContactByUsername looks up a single contact by their username across
// all owners. The caller is responsible for authorization checks.
func (s *Service) GetContactByUsername(ctx context.Context, contactUsername string) (*storage.Contact, error) {
	c, err := storage.GetContactByUsername(s.db, contactUsername)
	if err != nil {
		return nil, fmt.Errorf("get contact by username: %w", err)
	}
	return c, nil
}

// AcceptContactRequest accepts an incoming contact request by saving the
// contact to the owner's list and returning the owner's public identity
// (public key + onion address) so the transport layer can send it back to
// the requester to complete the mutual handshake.
func (s *Service) AcceptContactRequest(ctx context.Context, ownerUsername string, contact domain.Contact) (*storage.User, error) {
	err := storage.SaveContact(s.db, ownerUsername, contact.Username, contact.OnionAddress, contact.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("accept contact request: %w", err)
	}

	user, err := storage.GetUser(s.db, ownerUsername)
	if err != nil {
		return nil, fmt.Errorf("get owner info: %w", err)
	}
	return user, nil
}
