package transport

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSendPost(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("echo: " + string(body)))
	}))
	defer ts.Close()

	fingerprint := certSHA256(ts.Certificate())
	client := NewClient(WithFingerprint(fingerprint))

	ctx := context.Background()
	resp, err := client.SendPost(ctx, ts.Listener.Addr().String(), "/test", strings.NewReader("hello"))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "echo: hello", string(body))
}

func TestRetryOnFailure(t *testing.T) {
	realServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	realServer.StartTLS()
	defer realServer.Close()

	fingerprint := certSHA256(realServer.Certificate())

	failCount := 2
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	addr := listener.Addr().String()

	cl := &countingListener{Listener: listener, failN: failCount}
	go func() {
		for {
			conn, err := cl.Accept()
			if err != nil {
				return
			}
			if cl.shouldFail() {
				conn.Close()
				continue
			}
			rconn, err := net.Dial("tcp", realServer.Listener.Addr().String())
			if err != nil {
				conn.Close()
				continue
			}
			go pipeConns(conn, rconn)
		}
	}()

	client := NewClient(WithFingerprint(fingerprint))

	start := time.Now()
	resp, err := client.SendPost(context.Background(), addr, "/ping", strings.NewReader("test"))
	elapsed := time.Since(start)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.GreaterOrEqual(t, elapsed, 2*time.Second)
	assert.GreaterOrEqual(t, int(cl.accepted.Load()), failCount+1, "should have accepted at least failCount+1 connections")
}

func TestRetryExhausted(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	addr := listener.Addr().String()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	client := NewClient(WithFingerprint("aaaa"))

	resp, err := client.SendPost(context.Background(), addr, "/ping", strings.NewReader("test"))
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "network error"),
		"expected network error, got: %v", err)
}

func TestContextTimeout(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	fingerprint := certSHA256(ts.Certificate())
	client := NewClient(WithFingerprint(fingerprint), WithTimeout(100*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	resp, err := client.SendPost(ctx, ts.Listener.Addr().String(), "/slow", strings.NewReader("test"))
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "deadline") || strings.Contains(err.Error(), "context"),
		"expected deadline/context error, got: %v", err)
}

func TestClientFingerprintVerification(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	correctFingerprint := certSHA256(ts.Certificate())

	t.Run("correct fingerprint accepted", func(t *testing.T) {
		client := NewClient(WithFingerprint(correctFingerprint))
		resp, err := client.SendPost(context.Background(),
			ts.Listener.Addr().String(), "/ok", strings.NewReader("test"))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("wrong fingerprint rejected", func(t *testing.T) {
		client := NewClient(
			WithFingerprint("deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"),
		)
		resp, err := client.SendPost(context.Background(),
			ts.Listener.Addr().String(), "/nope", strings.NewReader("test"))
		assert.Nil(t, resp)
		assert.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "fingerprint") || strings.Contains(err.Error(), "mismatch"),
			"expected fingerprint error, got: %v", err)
	})

	t.Run("empty fingerprint uses standard PKI", func(t *testing.T) {
		client := NewClient()
		resp, err := client.SendPost(context.Background(),
			ts.Listener.Addr().String(), "/nofp", strings.NewReader("test"))
		assert.Nil(t, resp)
		assert.Error(t, err)
	})
}

func TestNoRetryOnHTTPError(t *testing.T) {
	callCount := 0
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	}))
	defer ts.Close()

	fingerprint := certSHA256(ts.Certificate())
	client := NewClient(WithFingerprint(fingerprint))

	resp, err := client.SendPost(context.Background(), ts.Listener.Addr().String(), "/err", strings.NewReader("test"))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, 1, callCount)
}

func TestErrNetworkUnwrap(t *testing.T) {
	orig := assert.AnError
	ne := &ErrNetwork{Op: "dial", Err: orig}
	assert.ErrorIs(t, ne, orig)
	assert.True(t, strings.Contains(ne.Error(), "network error"))
	assert.True(t, strings.Contains(ne.Error(), "dial"))
}

func TestBackoff(t *testing.T) {
	assert.Equal(t, 1*time.Second, backoff(0))
	assert.Equal(t, 2*time.Second, backoff(1))
	assert.Equal(t, 4*time.Second, backoff(2))
	assert.Equal(t, 4*time.Second, backoff(3))
	assert.Equal(t, 4*time.Second, backoff(99))
}

func certSHA256(cert *x509.Certificate) string {
	h := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(h[:])
}

type countingListener struct {
	net.Listener
	accepted atomic.Int32
	failN    int
}

func (cl *countingListener) Accept() (net.Conn, error) {
	conn, err := cl.Listener.Accept()
	if err != nil {
		return conn, err
	}
	cl.accepted.Add(1)
	return conn, nil
}

func (cl *countingListener) shouldFail() bool {
	return int(cl.accepted.Load()) <= cl.failN
}

func pipeConns(a, b net.Conn) {
	defer a.Close()
	defer b.Close()
	go func() { io.Copy(b, a) }()
	io.Copy(a, b)
}
