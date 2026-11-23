package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

const (
	defaultTimeout     = 30 * time.Second
	defaultMaxAttempts = 3
)

type ErrNetwork struct {
	Op  string
	Err error
}

func (e *ErrNetwork) Error() string {
	return fmt.Sprintf("network error during %s: %v", e.Op, e.Err)
}

func (e *ErrNetwork) Unwrap() error { return e.Err }

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	var ne *ErrNetwork
	if errAs(err, &ne) {
		return true
	}
	if strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "no such host") ||
		strings.Contains(err.Error(), "i/o timeout") ||
		strings.Contains(err.Error(), "EOF") ||
		strings.Contains(err.Error(), "circuit") ||
		strings.Contains(err.Error(), "SOCKS") ||
		strings.Contains(err.Error(), "connection reset") {
		return true
	}
	return false
}

func errAs(err error, target any) bool {
	type aser interface {
		As(any) bool
	}
	if e, ok := err.(aser); ok {
		return e.As(target)
	}
	return false
}

type ClientOption func(*Client)

func WithProxyDialer(d proxy.Dialer) ClientOption {
	return func(c *Client) { c.proxyDialer = d }
}

func WithProxyAddr(addr string) ClientOption {
	return func(c *Client) { c.proxyAddr = addr }
}

func WithFingerprint(fp string) ClientOption {
	return func(c *Client) { c.expectedFingerprint = fp }
}

func WithPeerCA(ca *x509.Certificate) ClientOption {
	return func(c *Client) { c.peerCACert = ca }
}

func WithTimeout(d time.Duration) ClientOption {
	return func(c *Client) { c.timeout = d }
}

type Client struct {
	proxyDialer         proxy.Dialer
	proxyAddr           string
	expectedFingerprint string
	peerCACert          *x509.Certificate
	timeout             time.Duration
	maxAttempts         int
	transport           *http.Transport
}

func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		timeout:     defaultTimeout,
		maxAttempts: defaultMaxAttempts,
	}
	for _, o := range opts {
		o(c)
	}
	c.transport = &http.Transport{
		DialContext:       c.dialContext,
		TLSClientConfig:   c.tlsConfig(),
		ForceAttemptHTTP2: true,
	}
	return c
}

func (c *Client) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := c.getDialer()
	if dialer == nil {
		d := net.Dialer{}
		return d.DialContext(ctx, network, addr)
	}
	if cd, ok := dialer.(interface {
		DialContext(ctx context.Context, network, addr string) (net.Conn, error)
	}); ok {
		return cd.DialContext(ctx, network, addr)
	}
	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := dialer.Dial(network, addr)
		ch <- result{conn, err}
	}()
	select {
	case r := <-ch:
		return r.conn, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *Client) getDialer() proxy.Dialer {
	if c.proxyDialer != nil {
		return c.proxyDialer
	}
	if c.proxyAddr != "" {
		d, err := proxy.SOCKS5("tcp", c.proxyAddr, nil, proxy.Direct)
		if err != nil {
			return nil
		}
		return d
	}
	return nil
}

func (c *Client) tlsConfig() *tls.Config {
	if c.expectedFingerprint != "" && c.peerCACert != nil {
		cfg, err := ClientTLSConfigWithFingerprint(c.peerCACert, c.expectedFingerprint)
		if err != nil {
			return &tls.Config{MinVersion: tls.VersionTLS13}
		}
		return cfg
	}
	if c.expectedFingerprint != "" {
		return &tls.Config{
			MinVersion: tls.VersionTLS12,
			// InsecureSkipVerify is required for Tor .onion fingerprint verification.
			// Go TLS calls VerifyConnection AFTER chain verification — self-signed
			// Tor certs fail chain verification before VerifyConnection runs.
			// Security: VerifyConnection below performs exact SHA-256 fingerprint match.
			InsecureSkipVerify: true, //nolint:gosec // Tor fingerprint trust anchor
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return &ErrNetwork{Op: "fingerprint-verify", Err: fmt.Errorf("no peer certificate")}
				}
				actual := Fingerprint(cs.PeerCertificates[0])
				if !strings.EqualFold(actual, c.expectedFingerprint) {
					return &ErrNetwork{
						Op:  "fingerprint-verify",
						Err: fmt.Errorf("mismatch: got %s, want %s", actual, c.expectedFingerprint),
					}
				}
				return nil
			},
		}
	}
	// No pinned fingerprint: this client talks only to .onion addresses
	// (enforced by checkHost) through Tor. A v3 onion address is the hash of
	// the service's key and Tor completes the circuit only to the holder of
	// that key, so the endpoint is already authenticated and the link
	// encrypted end to end; the peer's TLS certificate is self-signed and
	// there is no CA to check it against. TLS here is defense in depth.
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, //nolint:gosec // onion address authenticates the peer; see checkHost
	}
}

// ErrNotOnion is returned for a peer address that is not an onion address
// when no certificate fingerprint is pinned.
var ErrNotOnion = errors.New("peers are reached only at .onion addresses through Tor")

// checkHost refuses hosts that would make skipping certificate verification
// unsafe: without a pinned fingerprint only .onion addresses are allowed.
func (c *Client) checkHost(addr string) error {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	if strings.HasSuffix(host, ".onion") || c.expectedFingerprint != "" {
		return nil
	}
	return fmt.Errorf("refusing to connect to %q: %w", host, ErrNotOnion)
}

// maxResponseBytes bounds how much of a peer's response is read.
const maxResponseBytes = 1 << 20

func (c *Client) SendPost(ctx context.Context, onionAddr, path string, body io.Reader) (*http.Response, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = io.ReadAll(body); err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
	}
	return c.do(ctx, http.MethodPost, onionAddr, path, payload)
}

// do sends a request, retrying network failures with backoff. The body is
// buffered so every attempt sends it in full (retries used to send an
// already-consumed reader, i.e. an empty body).
func (c *Client) do(ctx context.Context, method, addr, path string, payload []byte) (*http.Response, error) {
	if err := c.checkHost(addr); err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://%s%s", addr, path)

	var lastErr error
	for attempt := 0; attempt < c.maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}

		var reqBody io.Reader
		if payload != nil {
			reqBody = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		hc := &http.Client{
			Transport: c.transport,
			Timeout:   c.timeout,
			// Peers never need redirects; following one could leave Tor.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		resp, err := hc.Do(req)
		if err != nil {
			lastErr = &ErrNetwork{Op: strings.ToLower(method), Err: err}
			if !isNetworkError(err) || attempt == c.maxAttempts-1 {
				return nil, lastErr
			}
			c.transport.CloseIdleConnections()
			continue
		}

		return resp, nil
	}

	return nil, lastErr
}

// StatusError is a non-2xx answer from a peer.
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string { return fmt.Sprintf("peer answered %d: %s", e.Code, e.Body) }

// PostJSON sends v as JSON and returns nil for a 2xx answer, a *StatusError
// for other answers, or a network error.
func (c *Client) PostJSON(ctx context.Context, onionAddr, path string, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodPost, onionAddr, path, payload)
	if err != nil {
		return err
	}
	return readStatus(resp, nil, maxResponseBytes)
}

// GetJSON fetches path and decodes a 2xx JSON answer of at most limit bytes.
func (c *Client) GetJSON(ctx context.Context, onionAddr, path string, out any, limit int64) error {
	resp, err := c.do(ctx, http.MethodGet, onionAddr, path, nil)
	if err != nil {
		return err
	}
	return readStatus(resp, out, limit)
}

func readStatus(resp *http.Response, out any, limit int64) error {
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return &ErrNetwork{Op: "read", Err: err}
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("peer response larger than %d bytes", limit)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return &StatusError{Code: resp.StatusCode, Body: msg}
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode peer response: %w", err)
		}
	}
	return nil
}

func backoff(attempt int) time.Duration {
	durations := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	if attempt < 0 || attempt >= len(durations) {
		return durations[len(durations)-1]
	}
	return durations[attempt]
}
