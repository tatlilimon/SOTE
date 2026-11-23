package peer

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/cretz/bine/torutil"
	torEd "github.com/cretz/bine/torutil/ed25519"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func randomOnion(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return torutil.OnionServiceIDFromV3PublicKey(torEd.PublicKey(pub)) + ".onion"
}

func TestValidOnion(t *testing.T) {
	good := randomOnion(t)
	assert.True(t, ValidOnion(good))

	// One flipped character breaks the checksum.
	b := []byte(good)
	if b[0] == 'a' {
		b[0] = 'b'
	} else {
		b[0] = 'a'
	}
	assert.False(t, ValidOnion(string(b)))
	assert.False(t, ValidOnion(strings.ToUpper(good)))
	assert.False(t, ValidOnion(strings.TrimSuffix(good, ".onion")))
	assert.False(t, ValidOnion("short.onion"))
	assert.False(t, ValidOnion("127.0.0.1"))
	assert.False(t, ValidOnion(""))
}

func TestNormalizeOnion(t *testing.T) {
	o := randomOnion(t)
	for _, in := range []string{o, "  " + o + "\n", "https://" + o + "/", "http://" + strings.ToUpper(o) + ":443/path", o + ":443"} {
		got, err := NormalizeOnion(in)
		require.NoError(t, err, in)
		assert.Equal(t, o, got)
	}
	for _, in := range []string{"", "example.com", "https://example.com", "bob.onion"} {
		_, err := NormalizeOnion(in)
		assert.ErrorIs(t, err, ErrInvalidOnion, in)
	}
}

func validRequest(t *testing.T) ContactRequest {
	return ContactRequest{
		Version: ProtocolVersion, RequestID: NewID(), FromUsername: "alice",
		FromOnion: randomOnion(t), ToOnion: randomOnion(t),
		FromPublicKey: "-----BEGIN PGP PUBLIC KEY BLOCK-----", SentAt: time.Now().Unix(), Signature: "sig",
	}
}

func TestContactRequestValidate(t *testing.T) {
	now := time.Now()
	r := validRequest(t)
	require.NoError(t, r.Validate(now))

	cases := map[string]func(*ContactRequest){
		"version":      func(r *ContactRequest) { r.Version = 2 },
		"request id":   func(r *ContactRequest) { r.RequestID = "../../etc" },
		"username":     func(r *ContactRequest) { r.FromUsername = "alice\nbob" },
		"from onion":   func(r *ContactRequest) { r.FromOnion = "evil.onion" },
		"to self":      func(r *ContactRequest) { r.ToOnion = r.FromOnion },
		"empty key":    func(r *ContactRequest) { r.FromPublicKey = "" },
		"huge key":     func(r *ContactRequest) { r.FromPublicKey = strings.Repeat("a", MaxPublicKeyBytes+1) },
		"no signature": func(r *ContactRequest) { r.Signature = "" },
		"future":       func(r *ContactRequest) { r.SentAt = now.Add(2 * time.Hour).Unix() },
		"zero time":    func(r *ContactRequest) { r.SentAt = 0 },
	}
	for name, mutate := range cases {
		r := validRequest(t)
		mutate(&r)
		assert.ErrorIs(t, r.Validate(now), ErrInvalid, name)
	}
}

func TestEnvelopeValidateAndSigningBytes(t *testing.T) {
	now := time.Now()
	e := Envelope{Version: 1, MessageID: NewID(), FromOnion: randomOnion(t), ToOnion: randomOnion(t), SentAt: now.Unix(), Payload: "p", Signature: "s"}
	require.NoError(t, e.Validate(now))

	base := string(e.SigningBytes())
	assert.True(t, strings.HasPrefix(base, "sote-message/1\n"))

	// Every signed field changes the signed text.
	for _, mutate := range []func(*Envelope){
		func(x *Envelope) { x.MessageID = NewID() },
		func(x *Envelope) { x.FromOnion = randomOnion(t) },
		func(x *Envelope) { x.ToOnion = randomOnion(t) },
		func(x *Envelope) { x.SentAt++ },
		func(x *Envelope) { x.Payload = "q" },
	} {
		c := e
		mutate(&c)
		assert.NotEqual(t, base, string(c.SigningBytes()))
	}
	// The signature itself is not part of the signed text.
	c := e
	c.Signature = "other"
	assert.Equal(t, base, string(c.SigningBytes()))

	e.Payload = strings.Repeat("x", MaxPayloadBytes+1)
	assert.ErrorIs(t, e.Validate(now), ErrInvalid)
}

func TestSigningDomainsDiffer(t *testing.T) {
	r := validRequest(t)
	assert.True(t, strings.HasPrefix(string(r.SigningBytes()), "sote-contact-request/1\n"))
}

func TestNewIDIsRandomHex(t *testing.T) {
	a, b := NewID(), NewID()
	assert.Len(t, a, 32)
	assert.NotEqual(t, a, b)
	assert.True(t, idRe.MatchString(a))
}
