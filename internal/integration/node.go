// Package integration handles cross-module orchestration and wiring: the
// Node joins the Tor onion service, the peer protocol server, the delivery
// worker and the logged-in session, and both user interfaces drive it.
package integration

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"sote/internal/contacts"
	"sote/internal/crypto"
	"sote/internal/domain"
	"sote/internal/messages"
	"sote/internal/peer"
	"sote/internal/storage"
	"sote/internal/transport"
)

var (
	ErrNotLoggedIn = errors.New("not logged in")
	ErrTorNotReady = errors.New("Tor is not connected yet; try again in a moment")
)

// TorRuntime is the part of tor.TorService the node needs.
type TorRuntime interface {
	Start(ctx context.Context) error
	Stop() error
}

// EventKind classifies node events for the user interfaces.
type EventKind int

const (
	EventTorStarting EventKind = iota
	EventTorReady
	EventTorFailed
	EventMessageReceived
	EventContactRequest
	EventDelivery
)

// Event is something the user interface may want to show.
type Event struct {
	Kind EventKind
	// Peer is the contact's local name (messages, delivery) or the
	// requester's username (contact requests).
	Peer string
	// Detail is an error message or a delivery state.
	Detail string
}

// Options wires the node. Tor, Listener, OnionAddress and DialClient come
// from the Tor layer in production and from fakes in tests.
type Options struct {
	DB       *sql.DB
	Crypto   *crypto.Service
	Contacts *contacts.Service
	Messages *messages.Service

	Tor TorRuntime
	// Listener is the loopback listener the onion service forwards to.
	Listener net.Listener
	// OnionAddress is the node's own address, known from its onion key
	// before Tor starts.
	OnionAddress string
	TLS          *tls.Config
	// DialClient returns a peer client that dials through Tor. It is called
	// once Tor has started.
	DialClient func(ctx context.Context) (*transport.Client, error)

	Now          func() time.Time
	PollInterval time.Duration
	// DisableWorker turns off the background delivery loop (tests call
	// DeliverDue themselves).
	DisableWorker bool
	RetryBase     time.Duration
	RetryMax      time.Duration
}

type session struct {
	username string
	identity *crypto.Identity
}

// Node is one SOTE installation on the network.
type Node struct {
	opts   Options
	server *transport.Server

	mu       sync.Mutex
	sess     *session
	client   *transport.Client
	torState EventKind
	torErr   string
	onEvent  func(Event)

	kick    chan struct{}
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	started bool
}

// New creates a node. Call Start to run it.
func New(opts Options) *Node {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = 15 * time.Second
	}
	if opts.RetryBase == 0 {
		opts.RetryBase = 30 * time.Second
	}
	if opts.RetryMax == 0 {
		opts.RetryMax = time.Hour
	}
	n := &Node{opts: opts, kick: make(chan struct{}, 1), torState: EventTorStarting}
	n.server = transport.NewServer(n)
	if opts.TLS != nil {
		n.server.WithTLS(opts.TLS)
	}
	if opts.Messages != nil {
		opts.Messages.SetLocalOnion(n.OnionAddress)
		opts.Messages.SetOnQueued(n.Kick)
		opts.Messages.SetIdentitySource(n.identityFor)
	}
	return n
}

// Server returns the peer protocol server.
func (n *Node) Server() *transport.Server { return n.server }

// OnEvent sets the callback for events. It runs on node goroutines and must
// not block; user interfaces hand the event to their own loop.
func (n *Node) OnEvent(f func(Event)) {
	n.mu.Lock()
	n.onEvent = f
	n.mu.Unlock()
}

func (n *Node) emit(e Event) {
	n.mu.Lock()
	f := n.onEvent
	n.mu.Unlock()
	if f != nil {
		f(e)
	}
}

// OnionAddress returns the node's own onion address.
func (n *Node) OnionAddress() string { return n.opts.OnionAddress }

// TorStatus returns the last Tor state (starting, ready or failed) and the
// failure message, if any.
func (n *Node) TorStatus() (EventKind, string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.torState, n.torErr
}

// Start serves the peer protocol, starts the delivery worker and starts Tor
// in the background. It returns immediately.
func (n *Node) Start(ctx context.Context) {
	n.mu.Lock()
	if n.started {
		n.mu.Unlock()
		return
	}
	n.started = true
	ctx, n.cancel = context.WithCancel(ctx)
	n.mu.Unlock()

	if n.opts.Listener != nil {
		n.wg.Add(1)
		go func() {
			defer n.wg.Done()
			if err := n.server.Serve(n.opts.Listener); err != nil {
				slog.Error("peer server stopped", "error", err)
			}
		}()
	}

	if !n.opts.DisableWorker {
		n.wg.Add(1)
		go func() {
			defer n.wg.Done()
			n.worker(ctx)
		}()
	}

	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		n.startTor(ctx)
	}()
}

func (n *Node) startTor(ctx context.Context) {
	n.emit(Event{Kind: EventTorStarting})
	fail := func(err error) {
		n.mu.Lock()
		n.torState, n.torErr = EventTorFailed, err.Error()
		n.mu.Unlock()
		n.emit(Event{Kind: EventTorFailed, Detail: err.Error()})
	}
	if n.opts.Tor == nil || n.opts.DialClient == nil {
		fail(errors.New("Tor is not configured"))
		return
	}
	startCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := n.opts.Tor.Start(startCtx); err != nil {
		if ctx.Err() == nil {
			fail(err)
		}
		return
	}
	client, err := n.opts.DialClient(ctx)
	if err != nil {
		fail(err)
		return
	}
	n.mu.Lock()
	n.client = client
	n.torState, n.torErr = EventTorReady, ""
	n.mu.Unlock()
	n.emit(Event{Kind: EventTorReady})
	n.Kick()
}

// Stop shuts the node down: the worker, the peer server and Tor.
func (n *Node) Stop() error {
	n.mu.Lock()
	cancel := n.cancel
	n.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	var errs []error
	if err := n.server.Stop(); err != nil {
		errs = append(errs, err)
	}
	if n.opts.Tor != nil {
		if err := n.opts.Tor.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	if n.opts.Listener != nil {
		_ = n.opts.Listener.Close()
	}
	n.wg.Wait()
	n.Logout()
	return errors.Join(errs...)
}

// Kick asks the delivery worker to run now.
func (n *Node) Kick() {
	select {
	case n.kick <- struct{}{}:
	default:
	}
}

// Login unlocks the user's private key for the session and records the
// node's onion address on the account.
func (n *Node) Login(user *domain.User) error {
	if user == nil {
		return ErrNotLoggedIn
	}
	id, err := n.opts.Crypto.Unlock(user.EncryptedPrivateKey, user.RawPassword)
	if err != nil {
		return err
	}
	if n.opts.OnionAddress != "" {
		if err := storage.SetUserOnion(context.Background(), n.opts.DB, user.Username, n.opts.OnionAddress); err != nil {
			id.Close()
			return err
		}
		user.OnionAddress = n.opts.OnionAddress
	}
	n.mu.Lock()
	old := n.sess
	n.sess = &session{username: user.Username, identity: id}
	n.mu.Unlock()
	if old != nil {
		old.identity.Close()
	}
	n.Kick()
	return nil
}

// Logout forgets the unlocked key.
func (n *Node) Logout() {
	n.mu.Lock()
	old := n.sess
	n.sess = nil
	n.mu.Unlock()
	if old != nil {
		old.identity.Close()
	}
}

func (n *Node) current() (*session, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.sess == nil {
		return nil, ErrNotLoggedIn
	}
	return n.sess, nil
}

func (n *Node) identityFor(username string) *crypto.Identity {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.sess != nil && n.sess.username == username {
		return n.sess.identity
	}
	return nil
}

func (n *Node) peerClient() (*transport.Client, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.client == nil {
		return nil, ErrTorNotReady
	}
	return n.client, nil
}

func (n *Node) fetchIdentity(ctx context.Context, onion string) (*peer.Identity, error) {
	client, err := n.peerClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var id peer.Identity
	if err := client.GetJSON(ctx, onion, peer.PathIdentity, &id, peer.MaxIdentityBytes); err != nil {
		var se *transport.StatusError
		if errors.As(err, &se) && se.Code == http.StatusServiceUnavailable {
			return nil, fmt.Errorf("%s is online but not logged in; try again later", onion)
		}
		return nil, fmt.Errorf("could not reach %s: %w", onion, err)
	}
	if err := id.Validate(); err != nil {
		return nil, err
	}
	if id.Onion != onion {
		return nil, contacts.ErrIdentityMismatch
	}
	return &id, nil
}

// AddContact fetches the identity served at the given onion address, stores
// it as a contact and queues a contact request so the other side can add the
// user back. The address may be pasted with a scheme, port or spaces.
func (n *Node) AddContact(ctx context.Context, address string) (*storage.Contact, error) {
	onion, err := peer.NormalizeOnion(address)
	if err != nil {
		return nil, err
	}
	if onion == n.opts.OnionAddress {
		return nil, contacts.ErrSelfOnion
	}
	sess, err := n.current()
	if err != nil {
		return nil, err
	}
	id, err := n.fetchIdentity(ctx, onion)
	if err != nil {
		return nil, err
	}
	c, err := n.opts.Contacts.AddVerified(ctx, sess.username, id)
	if errors.Is(err, contacts.ErrAlreadyContact) {
		return c, err
	}
	if err != nil {
		return nil, err
	}
	req, err := contacts.BuildRequest(sess.username, n.opts.OnionAddress, onion, sess.identity, n.opts.Now())
	if err != nil {
		return c, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return c, err
	}
	if _, err := storage.EnqueueOutbox(ctx, n.opts.DB, storage.OutboxItem{
		Owner: sess.username, Kind: storage.OutboxContactRequest, ToOnion: onion,
		Path: peer.PathContactRequest, Body: body, NextAttemptAt: n.opts.Now().Unix(),
	}); err != nil {
		return c, err
	}
	n.Kick()
	return c, nil
}

// PendingRequests lists contact requests waiting for an answer.
func (n *Node) PendingRequests(ctx context.Context) ([]storage.ContactRequest, error) {
	sess, err := n.current()
	if err != nil {
		return nil, err
	}
	return n.opts.Contacts.PendingRequests(ctx, sess.username)
}

// AcceptRequest adds the requester after checking, over Tor, that its onion
// address serves the key the request introduced.
func (n *Node) AcceptRequest(ctx context.Context, requestID int64) (*storage.Contact, error) {
	sess, err := n.current()
	if err != nil {
		return nil, err
	}
	req, err := n.opts.Contacts.Request(ctx, sess.username, requestID)
	if err != nil {
		return nil, err
	}
	id, err := n.fetchIdentity(ctx, req.FromOnion)
	if err != nil {
		return nil, err
	}
	return n.opts.Contacts.AcceptRequest(ctx, sess.username, requestID, id)
}

// DeclineRequest declines a contact request.
func (n *Node) DeclineRequest(ctx context.Context, requestID int64) error {
	sess, err := n.current()
	if err != nil {
		return err
	}
	return n.opts.Contacts.DeclineRequest(ctx, sess.username, requestID)
}

// DeleteContact removes a contact (messages are kept).
func (n *Node) DeleteContact(ctx context.Context, id int) error {
	sess, err := n.current()
	if err != nil {
		return err
	}
	return n.opts.Contacts.DeleteContact(ctx, sess.username, id)
}

// ── transport.PeerHandler ──────────────────────────────────────────────────

// Identity answers GET /identity.
func (n *Node) Identity(ctx context.Context) (*peer.Identity, error) {
	sess, err := n.current()
	if err != nil || n.opts.OnionAddress == "" {
		return nil, peer.ErrNotReady
	}
	return &peer.Identity{
		Version:   peer.ProtocolVersion,
		Username:  sess.username,
		Onion:     n.opts.OnionAddress,
		PublicKey: string(sess.identity.PublicKey()),
	}, nil
}

// HandleContactRequest answers POST /receive-contact-request.
func (n *Node) HandleContactRequest(ctx context.Context, r *peer.ContactRequest) error {
	sess, err := n.current()
	if err != nil {
		return peer.ErrNotReady
	}
	queued, err := n.opts.Contacts.ReceiveRequest(ctx, sess.username, n.opts.OnionAddress, r, n.opts.Now())
	if err != nil {
		return err
	}
	if queued {
		n.emit(Event{Kind: EventContactRequest, Peer: r.FromUsername})
	}
	return nil
}

// HandleMessage answers POST /receive-message.
func (n *Node) HandleMessage(ctx context.Context, e *peer.Envelope) error {
	sess, err := n.current()
	if err != nil {
		return peer.ErrNotReady
	}
	name, fresh, err := n.opts.Messages.AcceptEnvelope(ctx, sess.username, n.opts.OnionAddress, e)
	if err != nil {
		return err
	}
	if fresh {
		n.emit(Event{Kind: EventMessageReceived, Peer: name})
	}
	return nil
}

// ── delivery ────────────────────────────────────────────────────────────────

func (n *Node) worker(ctx context.Context) {
	ticker := time.NewTicker(n.opts.PollInterval)
	defer ticker.Stop()
	for {
		n.DeliverDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-n.kick:
		}
	}
}

// permanent reports whether a peer's answer means retrying cannot help.
func permanent(err error) bool {
	var se *transport.StatusError
	if !errors.As(err, &se) {
		return errors.Is(err, transport.ErrNotOnion)
	}
	switch se.Code {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType,
		http.StatusMisdirectedRequest, http.StatusNotFound, http.StatusMethodNotAllowed:
		return true
	}
	return false
}

func (n *Node) backoff(attempts int) time.Duration {
	d := n.opts.RetryBase
	for i := 0; i < attempts && d < n.opts.RetryMax; i++ {
		d *= 2
	}
	if d > n.opts.RetryMax {
		d = n.opts.RetryMax
	}
	return d
}

// DeliverDue sends every queued item that is due. Exported for tests.
func (n *Node) DeliverDue(ctx context.Context) {
	sess, err := n.current()
	if err != nil {
		return
	}
	client, err := n.peerClient()
	if err != nil {
		return
	}
	items, err := storage.DueOutbox(ctx, n.opts.DB, sess.username, n.opts.Now(), 20)
	if err != nil {
		slog.Error("read outbox", "error", err)
		return
	}
	for _, it := range items {
		if ctx.Err() != nil {
			return
		}
		sendCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		err := client.PostJSON(sendCtx, it.ToOnion, it.Path, json.RawMessage(it.Body))
		cancel()
		n.record(ctx, sess.username, it, err)
	}
}

func (n *Node) record(ctx context.Context, owner string, it storage.OutboxItem, err error) {
	switch {
	case err == nil:
		if e := storage.FinishOutbox(ctx, n.opts.DB, it.ID, storage.OutboxDelivered, ""); e != nil {
			slog.Error("outbox", "error", e)
		}
		n.setDelivery(ctx, owner, it, storage.DeliveryDelivered)
	case permanent(err):
		slog.Warn("delivery rejected", "kind", it.Kind, "error", err)
		if e := storage.FinishOutbox(ctx, n.opts.DB, it.ID, storage.OutboxRejected, err.Error()); e != nil {
			slog.Error("outbox", "error", e)
		}
		n.setDelivery(ctx, owner, it, storage.DeliveryRejected)
	default:
		next := n.opts.Now().Add(n.backoff(it.Attempts))
		if e := storage.RetryOutbox(ctx, n.opts.DB, it.ID, next, err.Error()); e != nil {
			slog.Error("outbox", "error", e)
		}
	}
}

func (n *Node) setDelivery(ctx context.Context, owner string, it storage.OutboxItem, state string) {
	if it.Kind != storage.OutboxMessage || it.MessageID == "" {
		return
	}
	if err := storage.SetDelivery(ctx, n.opts.DB, owner, it.MessageID, state); err != nil {
		slog.Error("set delivery", "error", err)
		return
	}
	peerName := it.ToOnion
	if c, err := storage.GetContactByOnion(ctx, n.opts.DB, owner, it.ToOnion); err == nil {
		peerName = c.ContactUsername
	}
	n.emit(Event{Kind: EventDelivery, Peer: peerName, Detail: state})
}

// Compile-time interface check.
var _ transport.PeerHandler = (*Node)(nil)
