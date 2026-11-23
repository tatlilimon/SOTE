package contacts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sote/internal/crypto"
	"sote/internal/peer"
	"sote/internal/storage"
)

var (
	// ErrIdentityMismatch: the key a requester introduced is not the key its
	// own onion address serves, so the request did not come from that address.
	ErrIdentityMismatch = errors.New("the requester's onion address serves a different key than the request carried")
	ErrAlreadyContact   = errors.New("already a contact")
	ErrSelfOnion        = errors.New("that is your own onion address")
)

// BuildRequest creates a signed contact request introducing owner to toOnion.
func BuildRequest(owner, ownOnion, toOnion string, id *crypto.Identity, now time.Time) (*peer.ContactRequest, error) {
	r := &peer.ContactRequest{
		Version:       peer.ProtocolVersion,
		RequestID:     peer.NewID(),
		FromUsername:  owner,
		FromOnion:     ownOnion,
		ToOnion:       toOnion,
		FromPublicKey: string(id.PublicKey()),
		SentAt:        now.Unix(),
	}
	sig, err := id.SignDetached(r.SigningBytes())
	if err != nil {
		return nil, err
	}
	r.Signature = sig
	return r, nil
}

// ReceiveRequest validates a contact request from a peer and queues it for
// the owner to answer. The signature proves the requester holds the private
// key it introduces; whether that key belongs to FromOnion is checked when the
// owner accepts (by fetching the requester's /identity over Tor).
// It reports whether a request was queued (false when the requester is
// already a contact with the same key, i.e. both sides added each other).
func (s *Service) ReceiveRequest(ctx context.Context, owner, ownOnion string, r *peer.ContactRequest, now time.Time) (bool, error) {
	if err := r.Validate(now); err != nil {
		return false, err
	}
	if r.ToOnion != ownOnion {
		return false, peer.ErrWrongRecipient
	}
	if _, err := crypto.PublicKeyFingerprint([]byte(r.FromPublicKey)); err != nil {
		return false, fmt.Errorf("%w: from_public_key", peer.ErrInvalid)
	}
	if err := crypto.VerifyDetached([]byte(r.FromPublicKey), r.SigningBytes(), r.Signature); err != nil {
		return false, peer.ErrBadSignature
	}

	// A request from someone who is already a contact with the same key is
	// the second half of a mutual add; nothing to ask the owner.
	if c, err := storage.GetContactByOnion(ctx, s.db, owner, r.FromOnion); err == nil && sameKey(c.PublicKey, []byte(r.FromPublicKey)) {
		return false, nil
	}

	_, err := storage.UpsertContactRequest(ctx, s.db, storage.ContactRequest{
		Owner: owner, RequestID: r.RequestID, FromUsername: r.FromUsername,
		FromOnion: r.FromOnion, FromPublicKey: []byte(r.FromPublicKey),
	})
	if errors.Is(err, storage.ErrTooManyRequests) {
		return false, peer.ErrTooMany
	}
	return err == nil, err
}

func sameKey(a, b []byte) bool {
	fa, errA := crypto.PublicKeyFingerprint(a)
	fb, errB := crypto.PublicKeyFingerprint(b)
	return errA == nil && errB == nil && fa == fb
}

// AddVerified stores a contact whose identity was fetched from its own onion
// address. The local name is the peer's username, suffixed if the owner
// already has a different contact with that name.
func (s *Service) AddVerified(ctx context.Context, owner string, id *peer.Identity) (*storage.Contact, error) {
	if err := id.Validate(); err != nil {
		return nil, err
	}
	if _, err := crypto.PublicKeyFingerprint([]byte(id.PublicKey)); err != nil {
		return nil, fmt.Errorf("%w: public_key", peer.ErrInvalid)
	}
	if existing, err := storage.GetContactByOnion(ctx, s.db, owner, id.Onion); err == nil {
		if sameKey(existing.PublicKey, []byte(id.PublicKey)) {
			return existing, ErrAlreadyContact
		}
		// Same address, new key (the peer re-created its account): replace.
		if err := storage.DeleteContact(ctx, s.db, owner, existing.ID); err != nil {
			return nil, err
		}
	}

	name := id.Username
	if name == owner {
		name = id.Username + "_" + id.Onion[:6]
	}
	if c, err := storage.GetContact(ctx, s.db, owner, name); err == nil && c.OnionAddress != id.Onion {
		name = id.Username + "_" + id.Onion[:6]
	}
	if err := storage.SaveContact(s.db, owner, name, id.Onion, []byte(id.PublicKey)); err != nil {
		return nil, fmt.Errorf("add contact: %w", err)
	}
	return storage.GetContactByOnion(ctx, s.db, owner, id.Onion)
}

// PendingRequests lists the owner's unanswered contact requests.
func (s *Service) PendingRequests(ctx context.Context, owner string) ([]storage.ContactRequest, error) {
	return storage.PendingContactRequests(ctx, s.db, owner)
}

// Request returns one of the owner's contact requests.
func (s *Service) Request(ctx context.Context, owner string, id int64) (*storage.ContactRequest, error) {
	return storage.GetContactRequest(ctx, s.db, owner, id)
}

// AcceptRequest adds the requester as a contact after checking that the
// identity fetched from its onion address carries the key from the request.
func (s *Service) AcceptRequest(ctx context.Context, owner string, requestID int64, fetched *peer.Identity) (*storage.Contact, error) {
	req, err := storage.GetContactRequest(ctx, s.db, owner, requestID)
	if err != nil {
		return nil, err
	}
	if fetched.Onion != req.FromOnion || !sameKey(req.FromPublicKey, []byte(fetched.PublicKey)) {
		return nil, ErrIdentityMismatch
	}
	c, err := s.AddVerified(ctx, owner, fetched)
	if err != nil && !errors.Is(err, ErrAlreadyContact) {
		return nil, err
	}
	if err := storage.SetContactRequestStatus(ctx, s.db, owner, requestID, storage.RequestAccepted); err != nil {
		return nil, err
	}
	return c, nil
}

// DeclineRequest records that the owner declined a request.
func (s *Service) DeclineRequest(ctx context.Context, owner string, requestID int64) error {
	return storage.SetContactRequestStatus(ctx, s.db, owner, requestID, storage.RequestDeclined)
}

// DeleteContact removes a contact. Their messages stay in the history.
func (s *Service) DeleteContact(ctx context.Context, owner string, id int) error {
	return storage.DeleteContact(ctx, s.db, owner, id)
}
