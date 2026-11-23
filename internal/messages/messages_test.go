package messages

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/auth"
	"sote/internal/crypto"
	"sote/internal/domain"
	"sote/internal/peer"
	"sote/internal/storage"
	"sote/internal/tor"
)

func randomOnion(t *testing.T) string {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return tor.OnionAddressForKey(key)
}

type fixture struct {
	db         *sql.DB
	svc        *Service
	alice      *domain.User
	aliceOnion string
	bob        *crypto.Identity
	bobOnion   string
	queued     atomic.Int32
}

// newFixture: alice owns the database; bob is her contact (only his public
// key is stored, his identity is kept in memory to play the remote side).
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := storage.Initialize(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	alice, err := auth.New(db).Register(context.Background(), "alice", "alice-password")
	require.NoError(t, err)

	priv, pub, err := crypto.New().GenerateKeyPair()
	require.NoError(t, err)
	bob, err := crypto.IdentityFromArmoredPrivateKey(priv)
	require.NoError(t, err)
	t.Cleanup(bob.Close)

	f := &fixture{db: db, alice: alice, aliceOnion: randomOnion(t), bob: bob, bobOnion: randomOnion(t)}
	require.NoError(t, storage.SaveContact(db, "alice", "bob", f.bobOnion, pub))

	f.svc = NewService(db, crypto.New())
	f.svc.SetLocalOnion(func() string { return f.aliceOnion })
	f.svc.SetOnQueued(func() { f.queued.Add(1) })
	return f
}

func (f *fixture) envelopeFromBob(t *testing.T, text string) *peer.Envelope {
	t.Helper()
	aliceKey := f.alice.PublicKey
	payload, err := f.bob.EncryptFor(aliceKey, []byte(text))
	require.NoError(t, err)
	env := &peer.Envelope{Version: 1, MessageID: peer.NewID(), FromOnion: f.bobOnion, ToOnion: f.aliceOnion, SentAt: time.Now().Unix(), Payload: payload}
	env.Signature, err = f.bob.SignDetached(env.SigningBytes())
	require.NoError(t, err)
	return env
}

func TestSendMessageQueuesSignedEnvelopeAndOwnCopy(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	require.NoError(t, f.svc.SendMessage(ctx, "alice", "bob", "alice-password", "hello bob"))
	assert.Equal(t, int32(1), f.queued.Load(), "the delivery worker is woken")

	due, err := storage.DueOutbox(ctx, f.db, "alice", time.Now(), 10)
	require.NoError(t, err)
	require.Len(t, due, 1)
	assert.Equal(t, f.bobOnion, due[0].ToOnion)
	assert.Equal(t, peer.PathMessage, due[0].Path)

	var env peer.Envelope
	require.NoError(t, json.Unmarshal(due[0].Body, &env))
	require.NoError(t, env.Validate(time.Now()))
	assert.Equal(t, f.aliceOnion, env.FromOnion)
	assert.Equal(t, f.bobOnion, env.ToOnion)
	assert.NoError(t, crypto.VerifyDetached(f.alice.PublicKey, env.SigningBytes(), env.Signature))

	plain, err := f.bob.Decrypt(env.Payload, f.alice.PublicKey)
	require.NoError(t, err, "payload is for bob and signed by alice")
	assert.Equal(t, "hello bob", string(plain))

	conv, err := f.svc.FetchMessages(ctx, "alice", "bob", "alice-password")
	require.NoError(t, err)
	require.Len(t, conv, 1, "exactly one row for the sender (it used to show two, one undecryptable)")
	assert.Equal(t, "hello bob", conv[0].Content)
	assert.Equal(t, storage.DeliveryPending, conv[0].Delivery)
	assert.Equal(t, env.MessageID, conv[0].MessageID)
	assert.True(t, conv[0].Verified)
}

func TestSendMessageErrors(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	assert.ErrorIs(t, f.svc.SendMessage(ctx, "alice", "bob", "alice-password", ""), ErrEmptyMessage)
	assert.ErrorIs(t, f.svc.SendMessage(ctx, "alice", "bob", "alice-password", strings.Repeat("x", MaxMessageSize+1)), ErrMessageTooLarge)
	assert.ErrorIs(t, f.svc.SendMessage(ctx, "alice", "carol", "alice-password", "hi"), ErrUnknownContact)
	assert.Error(t, f.svc.SendMessage(ctx, "alice", "bob", "wrong-password", "hi"))

	f.svc.SetLocalOnion(func() string { return "" })
	assert.ErrorIs(t, f.svc.SendMessage(ctx, "alice", "bob", "alice-password", "hi"), ErrNoLocalOnion)

	n, err := storage.PendingOutboxCount(ctx, f.db, "alice")
	require.NoError(t, err)
	assert.Zero(t, n, "failed sends queue nothing")
}

func TestAcceptEnvelope(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	env := f.envelopeFromBob(t, "hi alice")
	name, fresh, err := f.svc.AcceptEnvelope(ctx, "alice", f.aliceOnion, env)
	require.NoError(t, err)
	assert.Equal(t, "bob", name)
	assert.True(t, fresh)

	_, fresh, err = f.svc.AcceptEnvelope(ctx, "alice", f.aliceOnion, env)
	require.NoError(t, err, "a replay is acknowledged")
	assert.False(t, fresh, "but not stored twice")

	conv, err := f.svc.FetchMessages(ctx, "alice", "bob", "alice-password")
	require.NoError(t, err)
	require.Len(t, conv, 1)
	assert.Equal(t, "hi alice", conv[0].Content)
	assert.True(t, conv[0].Verified)

	wrong := f.envelopeFromBob(t, "x")
	wrong.ToOnion = randomOnion(t)
	_, _, err = f.svc.AcceptEnvelope(ctx, "alice", f.aliceOnion, wrong)
	assert.ErrorIs(t, err, peer.ErrWrongRecipient)

	stranger := f.envelopeFromBob(t, "x")
	stranger.FromOnion = randomOnion(t)
	_, _, err = f.svc.AcceptEnvelope(ctx, "alice", f.aliceOnion, stranger)
	assert.ErrorIs(t, err, peer.ErrUnknownSender)

	tampered := f.envelopeFromBob(t, "original")
	tampered.SentAt++
	_, _, err = f.svc.AcceptEnvelope(ctx, "alice", f.aliceOnion, tampered)
	assert.ErrorIs(t, err, peer.ErrBadSignature)

	bad := f.envelopeFromBob(t, "x")
	bad.MessageID = "not-hex"
	_, _, err = f.svc.AcceptEnvelope(ctx, "alice", f.aliceOnion, bad)
	assert.ErrorIs(t, err, peer.ErrInvalid)
}

func TestFetchMessagesHandlesOlderRows(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	cs := crypto.New()

	// A pre-v2 sent copy (AES under the password) is still readable.
	legacy, err := cs.EncryptAES([]byte("old sent"), "alice-password")
	require.NoError(t, err)
	_, err = storage.SaveMessageMeta(ctx, f.db, "alice", "bob", append([]byte{storage.BlobLegacySelfAES}, legacy...), "", "")
	require.NoError(t, err)

	// The pre-v2 outbound copy encrypted to bob is not shown to alice.
	outbound, err := cs.EncryptMessage([]byte("old sent"), []byte("unused"))
	if err != nil {
		outbound = []byte("-----BEGIN PGP MESSAGE-----")
	}
	_, err = storage.SaveMessageMeta(ctx, f.db, "alice", "bob", append([]byte{storage.BlobReceived}, outbound...), "", "")
	require.NoError(t, err)

	// A received message whose inner signature is not bob's is hidden.
	other, _, err := cs.GenerateKeyPair()
	require.NoError(t, err)
	mallory, err := crypto.IdentityFromArmoredPrivateKey(other)
	require.NoError(t, err)
	defer mallory.Close()
	forged, err := mallory.EncryptFor(f.alice.PublicKey, []byte("forged"))
	require.NoError(t, err)
	_, err = storage.SaveMessageMeta(ctx, f.db, "bob", "alice", append([]byte{storage.BlobReceived}, forged...), "", "")
	require.NoError(t, err)

	_, err = storage.SaveMessageMeta(ctx, f.db, "bob", "alice", []byte{0x7F, 1, 2}, "", "")
	require.NoError(t, err)

	conv, err := f.svc.FetchMessages(ctx, "alice", "bob", "alice-password")
	require.NoError(t, err)
	require.Len(t, conv, 3)
	assert.Equal(t, "old sent", conv[0].Content)
	assert.True(t, conv[0].Verified)
	assert.Contains(t, conv[1].Content, "signature does not match")
	assert.False(t, conv[1].Verified)
	assert.NotContains(t, conv[1].Content, "forged")
	assert.Contains(t, conv[2].Content, "unknown encryption prefix")

	_, err = f.svc.FetchMessages(ctx, "alice", "bob", "wrong-password")
	assert.Error(t, err)
}

func TestFetchEmptyConversation(t *testing.T) {
	f := newFixture(t)
	conv, err := f.svc.FetchMessages(context.Background(), "alice", "bob", "irrelevant")
	require.NoError(t, err)
	assert.Empty(t, conv)
}

func TestIdentitySourceIsUsed(t *testing.T) {
	f := newFixture(t)
	id, err := crypto.New().Unlock(f.alice.EncryptedPrivateKey, "alice-password")
	require.NoError(t, err)
	defer id.Close()
	f.svc.SetIdentitySource(func(u string) *crypto.Identity {
		if u == "alice" {
			return id
		}
		return nil
	})
	// With the session identity, the password is not needed for new rows.
	require.NoError(t, f.svc.SendMessage(context.Background(), "alice", "bob", "", "no password needed"))
	conv, err := f.svc.FetchMessages(context.Background(), "alice", "bob", "")
	require.NoError(t, err)
	require.Len(t, conv, 1)
	assert.Equal(t, "no password needed", conv[0].Content)
}

func Test_MaxMessageSize(t *testing.T) {
	assert.Equal(t, 64*1024, MaxMessageSize)
}

func TestRetry(t *testing.T) {
	t.Run("succeeds on first attempt", func(t *testing.T) {
		calls := 0
		err := retry(context.Background(), 3, func() error { calls++; return nil })
		assert.NoError(t, err)
		assert.Equal(t, 1, calls)
	})

	t.Run("succeeds on third attempt", func(t *testing.T) {
		calls := 0
		err := retry(context.Background(), 3, func() error {
			calls++
			if calls < 3 {
				return errors.New("fail")
			}
			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, 3, calls)
	})

	t.Run("exhausts retries", func(t *testing.T) {
		calls := 0
		err := retry(context.Background(), 3, func() error { calls++; return errors.New("persistent fail") })
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "retry exhausted")
		assert.Equal(t, 3, calls)
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			errCh <- retry(ctx, 5, func() error {
				time.Sleep(50 * time.Millisecond)
				return errors.New("fail")
			})
		}()
		time.Sleep(80 * time.Millisecond)
		cancel()
		assert.ErrorIs(t, <-errCh, context.Canceled)
	})
}
