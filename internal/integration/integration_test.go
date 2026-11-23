package integration

// End-to-end tests: two (or three) complete nodes, each with its own database,
// keys, onion address, TLS peer server and delivery queue, talking through a
// fake Tor that routes "<onion>:443" to the other node's loopback listener.
// Everything above the SOCKS dialer is the production code path.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sote/internal/auth"
	"sote/internal/contacts"
	"sote/internal/crypto"
	"sote/internal/domain"
	"sote/internal/messages"
	"sote/internal/peer"
	"sote/internal/storage"
	"sote/internal/tor"
	"sote/internal/transport"
)

// fakeTor routes onion addresses to loopback listeners.
type fakeTor struct {
	mu     sync.Mutex
	routes map[string]string
}

func (f *fakeTor) Start(context.Context) error { return nil }
func (f *fakeTor) Stop() error                 { return nil }

func (f *fakeTor) route(onion, addr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[net.JoinHostPort(onion, "443")] = addr
}

func (f *fakeTor) unroute(onion string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.routes, net.JoinHostPort(onion, "443"))
}

func (f *fakeTor) Dial(network, addr string) (net.Conn, error) {
	return f.DialContext(context.Background(), network, addr)
}

func (f *fakeTor) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	f.mu.Lock()
	target, ok := f.routes[addr]
	f.mu.Unlock()
	if !ok {
		return nil, errors.New("SOCKS: general failure (host unreachable)")
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp", target)
}

type testNode struct {
	*Node
	db    *sql.DB
	user  *domain.User
	onion string
	auth  *auth.AuthService
	msgs  *messages.Service
}

func newTestNode(t *testing.T, net_ *fakeTor, username string) *testNode {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Initialize(filepath.Join(dir, "data.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	onion := tor.OnionAddressForKey(key)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	net_.route(onion, ln.Addr().String())

	certs := transport.NewCertManager(filepath.Join(dir, "certs"))
	require.NoError(t, certs.LoadOrGenerate())
	tlsCfg, err := certs.ServerTLSConfig()
	require.NoError(t, err)

	cs := crypto.New()
	msgs := messages.NewService(db, cs)
	n := New(Options{
		DB: db, Crypto: cs, Contacts: contacts.NewService(db), Messages: msgs,
		Tor: net_, Listener: ln, OnionAddress: onion, TLS: tlsCfg,
		DialClient: func(context.Context) (*transport.Client, error) {
			return transport.NewClient(transport.WithProxyDialer(net_), transport.WithTimeout(5*time.Second)), nil
		},
		DisableWorker: true,
		RetryBase:     time.Millisecond,
	})
	n.Start(context.Background())
	t.Cleanup(func() { n.Stop() })
	require.Eventually(t, func() bool { s, _ := n.TorStatus(); return s == EventTorReady }, 5*time.Second, 10*time.Millisecond)

	authSvc := auth.New(db)
	user, err := authSvc.Register(context.Background(), username, username+"-password")
	require.NoError(t, err)
	require.NoError(t, n.Login(user))

	return &testNode{Node: n, db: db, user: user, onion: onion, auth: authSvc, msgs: msgs}
}

func (tn *testNode) conversation(t *testing.T, other string) []messages.DecryptedMessage {
	t.Helper()
	msgs, err := tn.msgs.FetchMessages(context.Background(), tn.user.Username, other, tn.user.RawPassword)
	require.NoError(t, err)
	return msgs
}

func pendingOutbox(t *testing.T, tn *testNode) int {
	t.Helper()
	n, err := storage.PendingOutboxCount(context.Background(), tn.db, tn.user.Username)
	require.NoError(t, err)
	return n
}

// befriend runs the whole contact flow: a adds b by onion address, the request
// is delivered, b accepts it.
func befriend(t *testing.T, a, b *testNode) {
	t.Helper()
	ctx := context.Background()
	c, err := a.AddContact(ctx, "https://"+b.onion+"/")
	require.NoError(t, err)
	assert.Equal(t, b.user.Username, c.ContactUsername)

	a.DeliverDue(ctx)
	require.Zero(t, pendingOutbox(t, a), "contact request must be delivered")

	reqs, err := b.PendingRequests(ctx)
	require.NoError(t, err)
	require.Len(t, reqs, 1)
	assert.Equal(t, a.user.Username, reqs[0].FromUsername)
	assert.Equal(t, a.onion, reqs[0].FromOnion)

	c, err = b.AcceptRequest(ctx, reqs[0].ID)
	require.NoError(t, err)
	assert.Equal(t, a.user.Username, c.ContactUsername)
}

func TestTwoNodesExchangeMessages(t *testing.T) {
	ctx := context.Background()
	tor := &fakeTor{routes: map[string]string{}}
	alice := newTestNode(t, tor, "alice")
	bob := newTestNode(t, tor, "bob")

	var bobEvents []Event
	var evMu sync.Mutex
	bob.OnEvent(func(e Event) { evMu.Lock(); bobEvents = append(bobEvents, e); evMu.Unlock() })

	befriend(t, alice, bob)

	require.NoError(t, alice.msgs.SendMessage(ctx, "alice", "bob", alice.user.RawPassword, "hello bob, it's alice"))
	sent := alice.conversation(t, "bob")
	require.Len(t, sent, 1)
	assert.Equal(t, storage.DeliveryPending, sent[0].Delivery)

	alice.DeliverDue(ctx)
	sent = alice.conversation(t, "bob")
	require.Len(t, sent, 1, "the sender sees one copy of its message")
	assert.Equal(t, "hello bob, it's alice", sent[0].Content)
	assert.Equal(t, storage.DeliveryDelivered, sent[0].Delivery)

	got := bob.conversation(t, "alice")
	require.Len(t, got, 1)
	assert.Equal(t, "alice", got[0].Sender)
	assert.Equal(t, "hello bob, it's alice", got[0].Content)
	assert.True(t, got[0].Verified, "signature must match alice's stored key")
	assert.Equal(t, sent[0].MessageID, got[0].MessageID)

	// Reply in the other direction.
	require.NoError(t, bob.msgs.SendMessage(ctx, "bob", "alice", bob.user.RawPassword, "hi alice"))
	bob.DeliverDue(ctx)
	conv := alice.conversation(t, "bob")
	require.Len(t, conv, 2)
	assert.Equal(t, "hi alice", conv[1].Content)
	assert.Equal(t, "bob", conv[1].Sender)
	assert.True(t, conv[1].Verified)

	evMu.Lock()
	defer evMu.Unlock()
	kinds := map[EventKind]int{}
	for _, e := range bobEvents {
		kinds[e.Kind]++
	}
	assert.Equal(t, 1, kinds[EventContactRequest])
	assert.Equal(t, 1, kinds[EventMessageReceived])
}

// postRaw sends a request to a node's peer server the way a remote peer would.
func postRaw(t *testing.T, tor *fakeTor, toOnion, path string, v any) int {
	t.Helper()
	client := transport.NewClient(transport.WithProxyDialer(tor), transport.WithTimeout(5*time.Second))
	err := client.PostJSON(context.Background(), toOnion, path, v)
	if err == nil {
		return http.StatusOK
	}
	var se *transport.StatusError
	require.True(t, errors.As(err, &se), "unexpected error: %v", err)
	return se.Code
}

func TestReplayedMessageIsStoredOnce(t *testing.T) {
	ctx := context.Background()
	tor := &fakeTor{routes: map[string]string{}}
	alice := newTestNode(t, tor, "alice")
	bob := newTestNode(t, tor, "bob")
	befriend(t, alice, bob)

	require.NoError(t, alice.msgs.SendMessage(ctx, "alice", "bob", alice.user.RawPassword, "only once"))
	due, err := storage.DueOutbox(ctx, alice.db, "alice", time.Now(), 10)
	require.NoError(t, err)
	require.Len(t, due, 1)
	var env peer.Envelope
	require.NoError(t, json.Unmarshal(due[0].Body, &env))

	alice.DeliverDue(ctx)
	// An attacker (or a retry after a lost acknowledgement) sends it again.
	assert.Equal(t, http.StatusOK, postRaw(t, tor, bob.onion, peer.PathMessage, env))
	assert.Len(t, bob.conversation(t, "alice"), 1)
}

func TestForgedAndMisdirectedMessagesAreRejected(t *testing.T) {
	ctx := context.Background()
	tor := &fakeTor{routes: map[string]string{}}
	alice := newTestNode(t, tor, "alice")
	bob := newTestNode(t, tor, "bob")
	mallory := newTestNode(t, tor, "mallory")
	befriend(t, alice, bob)

	// Mallory is not bob's contact: her own messages are refused...
	_, err := mallory.AddContact(ctx, bob.onion)
	require.NoError(t, err, "mallory can store bob's public identity")
	require.NoError(t, mallory.msgs.SendMessage(ctx, "mallory", "bob", mallory.user.RawPassword, "spam"))
	mallory.DeliverDue(ctx)
	conv := mallory.conversation(t, "bob")
	require.Len(t, conv, 1)
	assert.Equal(t, storage.DeliveryRejected, conv[0].Delivery)

	// ...and claiming alice's onion address does not help: the signature is
	// checked against alice's key.
	mid, err := mallory.opts.Crypto.Unlock(mallory.user.EncryptedPrivateKey, mallory.user.RawPassword)
	require.NoError(t, err)
	defer mid.Close()
	bobContact, err := storage.GetContact(ctx, mallory.db, "mallory", "bob")
	require.NoError(t, err)
	payload, err := mid.EncryptFor(bobContact.PublicKey, []byte("I am alice, send money"))
	require.NoError(t, err)
	forged := peer.Envelope{Version: 1, MessageID: peer.NewID(), FromOnion: alice.onion, ToOnion: bob.onion, SentAt: time.Now().Unix(), Payload: payload}
	forged.Signature, err = mid.SignDetached(forged.SigningBytes())
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, postRaw(t, tor, bob.onion, peer.PathMessage, forged))

	// A genuine envelope for someone else is refused by bob.
	misdirected := forged
	misdirected.ToOnion = mallory.onion
	assert.Equal(t, http.StatusMisdirectedRequest, postRaw(t, tor, bob.onion, peer.PathMessage, misdirected))

	// Garbage and oversized bodies.
	assert.Equal(t, http.StatusBadRequest, postRaw(t, tor, bob.onion, peer.PathMessage, map[string]any{"version": 1}))
	big := forged
	big.Payload = string(bytes.Repeat([]byte("A"), peer.MaxEnvelopeBytes))
	assert.Equal(t, http.StatusRequestEntityTooLarge, postRaw(t, tor, bob.onion, peer.PathMessage, big))

	assert.Empty(t, bob.conversation(t, "alice"), "nothing forged reached bob's history")
}

func TestContactRequestForSomeoneElsesOnionIsNotAccepted(t *testing.T) {
	ctx := context.Background()
	tor := &fakeTor{routes: map[string]string{}}
	alice := newTestNode(t, tor, "alice")
	bob := newTestNode(t, tor, "bob")
	mallory := newTestNode(t, tor, "mallory")

	// Mallory introduces her own key under alice's onion address. The
	// signature is valid (it is her key), so bob sees a pending request...
	mid, err := mallory.opts.Crypto.Unlock(mallory.user.EncryptedPrivateKey, mallory.user.RawPassword)
	require.NoError(t, err)
	defer mid.Close()
	req, err := contacts.BuildRequest("alice", alice.onion, bob.onion, mid, time.Now())
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, postRaw(t, tor, bob.onion, peer.PathContactRequest, req))
	reqs, err := bob.PendingRequests(ctx)
	require.NoError(t, err)
	require.Len(t, reqs, 1)

	// ...but accepting it fetches the identity alice's address really serves,
	// and the keys differ.
	_, err = bob.AcceptRequest(ctx, reqs[0].ID)
	assert.ErrorIs(t, err, contacts.ErrIdentityMismatch)
	_, err = storage.GetContactByOnion(ctx, bob.db, "bob", alice.onion)
	assert.ErrorIs(t, err, storage.ErrContactNotFound)

	// A request with a signature that does not match the introduced key.
	req2, err := contacts.BuildRequest("mallory", mallory.onion, bob.onion, mid, time.Now())
	require.NoError(t, err)
	aid, err := alice.opts.Crypto.Unlock(alice.user.EncryptedPrivateKey, alice.user.RawPassword)
	require.NoError(t, err)
	defer aid.Close()
	req2.FromPublicKey = string(aid.PublicKey())
	assert.Equal(t, http.StatusUnauthorized, postRaw(t, tor, bob.onion, peer.PathContactRequest, req2))
}

func TestUndeliveredMessagesWaitForThePeer(t *testing.T) {
	ctx := context.Background()
	tor := &fakeTor{routes: map[string]string{}}
	alice := newTestNode(t, tor, "alice")
	bob := newTestNode(t, tor, "bob")
	befriend(t, alice, bob)

	// Bob goes offline: his peer server stops (closing kept-alive
	// connections) and his onion address is unreachable.
	tor.unroute(bob.onion)
	require.NoError(t, bob.Server().Shutdown(ctx))
	require.NoError(t, alice.msgs.SendMessage(ctx, "alice", "bob", alice.user.RawPassword, "are you there?"))
	alice.DeliverDue(ctx)
	assert.Equal(t, 1, pendingOutbox(t, alice), "kept for a later attempt")
	assert.Equal(t, storage.DeliveryPending, alice.conversation(t, "bob")[0].Delivery)

	// Bob comes back but is not logged in yet: 503, still pending.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go bob.Server().Serve(ln)
	tor.route(bob.onion, ln.Addr().String())
	bob.Logout()
	time.Sleep(5 * time.Millisecond) // RetryBase is 1ms in tests
	alice.DeliverDue(ctx)
	assert.Equal(t, 1, pendingOutbox(t, alice))

	require.NoError(t, bob.Login(bob.user))
	time.Sleep(10 * time.Millisecond)
	alice.DeliverDue(ctx)
	assert.Zero(t, pendingOutbox(t, alice))
	assert.Equal(t, "are you there?", bob.conversation(t, "alice")[0].Content)
}

func TestIdentityEndpointAndSelfAdd(t *testing.T) {
	ctx := context.Background()
	tor := &fakeTor{routes: map[string]string{}}
	alice := newTestNode(t, tor, "alice")

	id, err := alice.Identity(ctx)
	require.NoError(t, err)
	assert.Equal(t, "alice", id.Username)
	assert.Equal(t, alice.onion, id.Onion)
	// Same key (the armor headers may be ordered differently).
	want, err := crypto.PublicKeyFingerprint(alice.user.PublicKey)
	require.NoError(t, err)
	got, err := crypto.PublicKeyFingerprint([]byte(id.PublicKey))
	require.NoError(t, err)
	assert.Equal(t, want, got)

	_, err = alice.AddContact(ctx, alice.onion)
	assert.ErrorIs(t, err, contacts.ErrSelfOnion)
	_, err = alice.AddContact(ctx, "not-an-onion.example")
	assert.ErrorIs(t, err, peer.ErrInvalidOnion)

	alice.Logout()
	_, err = alice.Identity(ctx)
	assert.ErrorIs(t, err, peer.ErrNotReady)
}

func TestPasswordChangeKeepsTheConversationReadable(t *testing.T) {
	ctx := context.Background()
	tor := &fakeTor{routes: map[string]string{}}
	alice := newTestNode(t, tor, "alice")
	bob := newTestNode(t, tor, "bob")
	befriend(t, alice, bob)

	require.NoError(t, alice.msgs.SendMessage(ctx, "alice", "bob", alice.user.RawPassword, "before"))
	alice.DeliverDue(ctx)
	require.NoError(t, bob.msgs.SendMessage(ctx, "bob", "alice", bob.user.RawPassword, "reply"))
	bob.DeliverDue(ctx)

	require.NoError(t, alice.auth.ChangePassword(ctx, "alice", alice.user.RawPassword, "a-brand-new-pass"))
	alice.Logout()
	user, err := alice.auth.Login(ctx, "alice", "a-brand-new-pass")
	require.NoError(t, err)
	require.NoError(t, alice.Login(user))
	alice.user = user

	conv := alice.conversation(t, "bob")
	require.Len(t, conv, 2)
	assert.Equal(t, "before", conv[0].Content)
	assert.Equal(t, "reply", conv[1].Content)
}
