package contacts

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/domain"
	"sote/internal/storage"
)

func setupService(t *testing.T) (*Service, func()) {
	t.Helper()
	db, err := storage.Initialize(":memory:")
	require.NoError(t, err)
	cleanup := func() { db.Close() }
	svc := NewService(db)

	// Create a test user so contact operations have a valid owner.
	err = storage.SaveUser(db, "alice", "hash", []byte("priv"), []byte("pub"), "onion.onion", "/tmp/torrc")
	require.NoError(t, err)

	return svc, cleanup
}

func TestAddContact(t *testing.T) {
	svc, cleanup := setupService(t)
	defer cleanup()
	ctx := context.Background()

	contact := domain.Contact{
		Username:     "bob",
		OnionAddress: "bob.onion",
		PublicKey:    []byte("bob-pubkey"),
	}

	err := svc.AddContact(ctx, "alice", contact)
	assert.NoError(t, err)
}

func TestAddDuplicateContact(t *testing.T) {
	svc, cleanup := setupService(t)
	defer cleanup()
	ctx := context.Background()

	contact := domain.Contact{
		Username:     "bob",
		OnionAddress: "bob.onion",
		PublicKey:    []byte("bob-pubkey"),
	}

	err := svc.AddContact(ctx, "alice", contact)
	require.NoError(t, err)

	err = svc.AddContact(ctx, "alice", contact)
	assert.ErrorIs(t, err, storage.ErrContactExists)
}

func TestAddSelfAsContact(t *testing.T) {
	svc, cleanup := setupService(t)
	defer cleanup()
	ctx := context.Background()

	contact := domain.Contact{
		Username:     "alice",
		OnionAddress: "alice.onion",
		PublicKey:    []byte("alice-pubkey"),
	}

	err := svc.AddContact(ctx, "alice", contact)
	assert.ErrorIs(t, err, storage.ErrSelfAdd)
}

func TestGetContacts(t *testing.T) {
	svc, cleanup := setupService(t)
	defer cleanup()
	ctx := context.Background()

	// Initially empty.
	contacts, err := svc.GetContacts(ctx, "alice")
	require.NoError(t, err)
	assert.Empty(t, contacts)

	bob := domain.Contact{Username: "bob", OnionAddress: "bob.onion", PublicKey: []byte("bob-pubkey")}
	err = svc.AddContact(ctx, "alice", bob)
	require.NoError(t, err)

	eve := domain.Contact{Username: "eve", OnionAddress: "eve.onion", PublicKey: []byte("eve-pubkey")}
	err = svc.AddContact(ctx, "alice", eve)
	require.NoError(t, err)

	contacts, err = svc.GetContacts(ctx, "alice")
	require.NoError(t, err)
	assert.Len(t, contacts, 2)

	// Verify the contacts belong to alice.
	for _, c := range contacts {
		assert.Equal(t, "alice", c.Username)
	}
}

func TestGetContactByUsername(t *testing.T) {
	svc, cleanup := setupService(t)
	defer cleanup()
	ctx := context.Background()

	bob := domain.Contact{Username: "bob", OnionAddress: "bob.onion", PublicKey: []byte("bob-pubkey")}
	err := svc.AddContact(ctx, "alice", bob)
	require.NoError(t, err)

	found, err := svc.GetContactByUsername(ctx, "bob")
	require.NoError(t, err)
	assert.Equal(t, "bob", found.ContactUsername)
	assert.Equal(t, "bob.onion", found.OnionAddress)
	assert.Equal(t, []byte("bob-pubkey"), found.PublicKey)
}

func TestAcceptContactRequest(t *testing.T) {
	svc, cleanup := setupService(t)
	defer cleanup()
	ctx := context.Background()

	contact := domain.Contact{
		Username:     "bob",
		OnionAddress: "bob.onion",
		PublicKey:    []byte("bob-pubkey"),
	}

	ownerInfo, err := svc.AcceptContactRequest(ctx, "alice", contact)
	require.NoError(t, err)

	// Owner info returned for transport layer.
	require.NotNil(t, ownerInfo)
	assert.Equal(t, "alice", ownerInfo.Username)
	assert.Equal(t, "onion.onion", ownerInfo.OnionAddress)
	assert.Equal(t, []byte("pub"), ownerInfo.PublicKey)

	// Contact was actually saved.
	contacts, err := svc.GetContacts(ctx, "alice")
	require.NoError(t, err)
	assert.Len(t, contacts, 1)
	assert.Equal(t, "bob", contacts[0].ContactUsername)

	// Duplicate accept should fail.
	_, err = svc.AcceptContactRequest(ctx, "alice", contact)
	assert.True(t, errors.Is(err, storage.ErrContactExists), "expected ErrContactExists, got %v", err)
}
