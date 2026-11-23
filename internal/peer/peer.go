// Package peer defines the wire protocol SOTE nodes speak to each other over
// Tor onion services: the identity document, signed contact requests and
// signed message envelopes, plus the validation shared by both sides.
//
// Trust model. A v3 onion address is derived from the onion service's key, and
// Tor only completes a connection to the holder of that key. Fetching
// /identity from an onion address therefore proves which GPG public key that
// address serves. Requests a node receives come from anonymous Tor clients, so
// every request carries a detached GPG signature: contact requests are signed
// by the key they introduce (and the key is later checked against the
// requester's own /identity), and messages are signed by a key the receiver
// already stores for that contact.
package peer

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cretz/bine/torutil"
)

const (
	// ProtocolVersion is the version carried by every document.
	ProtocolVersion = 1

	// Port is the virtual port peers connect to on an onion address.
	Port = 443

	PathIdentity       = "/identity"
	PathContactRequest = "/receive-contact-request"
	PathMessage        = "/receive-message"

	// Body limits. A message body is at most 64 KiB of plaintext; its
	// encrypted, signed and armored form stays well below MaxEnvelopeBytes.
	MaxEnvelopeBytes  = 512 << 10
	MaxRequestBytes   = 64 << 10
	MaxIdentityBytes  = 64 << 10
	MaxPublicKeyBytes = 16 << 10
	MaxPayloadBytes   = 256 << 10
	MaxSignatureBytes = 8 << 10

	// MaxClockSkew bounds how far in the future a document may be dated.
	MaxClockSkew = time.Hour
)

var (
	ErrInvalid        = errors.New("invalid request")
	ErrBadSignature   = errors.New("bad signature")
	ErrUnknownSender  = errors.New("sender is not a contact")
	ErrWrongRecipient = errors.New("addressed to another onion address")
	ErrNotReady       = errors.New("node is not ready")
	ErrTooMany        = errors.New("too many pending requests")
	ErrInvalidOnion   = errors.New("not a valid v3 onion address")
)

var (
	usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)
	idRe       = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// Identity is what a node serves at /identity.
type Identity struct {
	Version   int    `json:"version"`
	Username  string `json:"username"`
	Onion     string `json:"onion"`
	PublicKey string `json:"public_key"`
}

// ContactRequest asks the receiver to add the sender as a contact.
type ContactRequest struct {
	Version       int    `json:"version"`
	RequestID     string `json:"request_id"`
	FromUsername  string `json:"from_username"`
	FromOnion     string `json:"from_onion"`
	ToOnion       string `json:"to_onion"`
	FromPublicKey string `json:"from_public_key"`
	SentAt        int64  `json:"sent_at"`
	Signature     string `json:"signature"`
}

// Envelope carries one message. Payload is a PGP message encrypted to the
// receiver and signed by the sender; Signature is a detached signature over
// SigningBytes, so the receiver can authenticate the sender and reject
// replays before it needs its private key.
type Envelope struct {
	Version   int    `json:"version"`
	MessageID string `json:"message_id"`
	FromOnion string `json:"from_onion"`
	ToOnion   string `json:"to_onion"`
	SentAt    int64  `json:"sent_at"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

// NewID returns a random 128-bit identifier in lowercase hex.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// ValidUsername reports whether a username is acceptable on the wire.
func ValidUsername(s string) bool { return usernameRe.MatchString(s) }

// ValidOnion reports whether addr is a well-formed v3 onion address
// ("<56 base32 characters>.onion", lowercase, valid checksum and version).
func ValidOnion(addr string) bool {
	id, ok := strings.CutSuffix(addr, ".onion")
	if !ok || len(id) != 56 || strings.ToLower(id) != id {
		return false
	}
	_, err := torutil.PublicKeyFromV3OnionServiceID(id)
	return err == nil
}

// NormalizeOnion accepts what a user may paste (spaces, upper case, an
// http(s):// prefix, a trailing slash or port) and returns the bare address.
func NormalizeOnion(input string) (string, error) {
	s := strings.TrimSpace(input)
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", ErrInvalidOnion
		}
		s = u.Hostname()
	} else {
		s = strings.TrimSuffix(s, "/")
		if i := strings.LastIndex(s, ":"); i > 0 {
			s = s[:i]
		}
	}
	s = strings.ToLower(s)
	if !ValidOnion(s) {
		return "", ErrInvalidOnion
	}
	return s, nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// SigningBytes is the canonical text the sender signs. Every field is
// validated to be single-line first, so the encoding is unambiguous; the
// prefix keeps these signatures from being valid for anything else.
func (r *ContactRequest) SigningBytes() []byte {
	return []byte(strings.Join([]string{
		"sote-contact-request/" + strconv.Itoa(r.Version),
		r.RequestID, r.FromUsername, r.FromOnion, r.ToOnion,
		sha256Hex(r.FromPublicKey),
		strconv.FormatInt(r.SentAt, 10),
	}, "\n"))
}

// SigningBytes is the canonical text the sender signs for a message.
func (e *Envelope) SigningBytes() []byte {
	return []byte(strings.Join([]string{
		"sote-message/" + strconv.Itoa(e.Version),
		e.MessageID, e.FromOnion, e.ToOnion,
		strconv.FormatInt(e.SentAt, 10),
		sha256Hex(e.Payload),
	}, "\n"))
}

func checkTime(sentAt int64, now time.Time) error {
	if sentAt <= 0 || time.Unix(sentAt, 0).After(now.Add(MaxClockSkew)) {
		return fmt.Errorf("%w: implausible timestamp", ErrInvalid)
	}
	return nil
}

// Validate checks the structure of a contact request (not its signature).
func (r *ContactRequest) Validate(now time.Time) error {
	switch {
	case r.Version != ProtocolVersion:
		return fmt.Errorf("%w: unsupported version", ErrInvalid)
	case !idRe.MatchString(r.RequestID):
		return fmt.Errorf("%w: request_id", ErrInvalid)
	case !ValidUsername(r.FromUsername):
		return fmt.Errorf("%w: from_username", ErrInvalid)
	case !ValidOnion(r.FromOnion):
		return fmt.Errorf("%w: from_onion", ErrInvalid)
	case !ValidOnion(r.ToOnion):
		return fmt.Errorf("%w: to_onion", ErrInvalid)
	case r.FromOnion == r.ToOnion:
		return fmt.Errorf("%w: request to self", ErrInvalid)
	case r.FromPublicKey == "" || len(r.FromPublicKey) > MaxPublicKeyBytes:
		return fmt.Errorf("%w: from_public_key", ErrInvalid)
	case r.Signature == "" || len(r.Signature) > MaxSignatureBytes:
		return fmt.Errorf("%w: signature", ErrInvalid)
	}
	return checkTime(r.SentAt, now)
}

// Validate checks the structure of an envelope (not its signature).
func (e *Envelope) Validate(now time.Time) error {
	switch {
	case e.Version != ProtocolVersion:
		return fmt.Errorf("%w: unsupported version", ErrInvalid)
	case !idRe.MatchString(e.MessageID):
		return fmt.Errorf("%w: message_id", ErrInvalid)
	case !ValidOnion(e.FromOnion):
		return fmt.Errorf("%w: from_onion", ErrInvalid)
	case !ValidOnion(e.ToOnion):
		return fmt.Errorf("%w: to_onion", ErrInvalid)
	case e.Payload == "" || len(e.Payload) > MaxPayloadBytes:
		return fmt.Errorf("%w: payload", ErrInvalid)
	case e.Signature == "" || len(e.Signature) > MaxSignatureBytes:
		return fmt.Errorf("%w: signature", ErrInvalid)
	}
	return checkTime(e.SentAt, now)
}

// Validate checks an identity document.
func (i *Identity) Validate() error {
	switch {
	case i.Version != ProtocolVersion:
		return fmt.Errorf("%w: unsupported version", ErrInvalid)
	case !ValidUsername(i.Username):
		return fmt.Errorf("%w: username", ErrInvalid)
	case !ValidOnion(i.Onion):
		return fmt.Errorf("%w: onion", ErrInvalid)
	case i.PublicKey == "" || len(i.PublicKey) > MaxPublicKeyBytes:
		return fmt.Errorf("%w: public_key", ErrInvalid)
	}
	return nil
}
