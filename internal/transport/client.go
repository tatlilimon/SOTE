package transport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, //nolint:gosec // fingerprint is trust anchor
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
	return &tls.Config{MinVersion: tls.VersionTLS13}
}

func (c *Client) SendPost(ctx context.Context, onionAddr, path string, body io.Reader) (*http.Response, error) {
	url := fmt.Sprintf("https://%s%s", onionAddr, path)

	var lastErr error
	for attempt := 0; attempt < c.maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		hc := &http.Client{
			Transport: c.transport,
			Timeout:   c.timeout,
		}
		resp, err := hc.Do(req)
		if err != nil {
			lastErr = &ErrNetwork{Op: "post", Err: err}
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

func backoff(attempt int) time.Duration {
	durations := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	if attempt < 0 || attempt >= len(durations) {
		return durations[len(durations)-1]
	}
	return durations[attempt]
}
