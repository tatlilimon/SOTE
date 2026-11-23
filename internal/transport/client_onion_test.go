package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pinnedClient(ts *httptest.Server) *Client {
	return NewClient(WithFingerprint(Fingerprint(ts.Certificate())), WithTimeout(5*time.Second))
}

func TestClientRefusesNonOnionWithoutPin(t *testing.T) {
	// Certificate checks are skipped only for onion addresses (Tor
	// authenticates them); anything else needs a pinned fingerprint.
	c := NewClient()
	_, err := c.SendPost(context.Background(), "127.0.0.1:9", "/x", strings.NewReader("{}"))
	assert.ErrorIs(t, err, ErrNotOnion)
	err = c.GetJSON(context.Background(), "example.com", "/identity", nil, 10)
	assert.ErrorIs(t, err, ErrNotOnion)
}

func TestRetriesResendTheFullBody(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	attempt := 0
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		attempt++
		n := attempt
		bodies = append(bodies, string(b))
		mu.Unlock()
		if n == 1 {
			// Drop the connection: a network error the client retries.
			hj, ok := w.(http.Hijacker)
			require.True(t, ok)
			conn, _, err := hj.Hijack()
			require.NoError(t, err)
			conn.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := pinnedClient(ts)
	resp, err := c.SendPost(context.Background(), ts.Listener.Addr().String(), "/m", strings.NewReader(`{"hello":"world"}`))
	require.NoError(t, err)
	resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 2)
	assert.Equal(t, `{"hello":"world"}`, bodies[0])
	assert.Equal(t, `{"hello":"world"}`, bodies[1], "the retry must carry the same body (it used to be empty)")
}

func TestPostJSONReportsStatus(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		http.Error(w, `{"error":"not a contact"}`, http.StatusForbidden)
	}))
	defer ts.Close()
	err := pinnedClient(ts).PostJSON(context.Background(), ts.Listener.Addr().String(), "/receive-message", map[string]int{"version": 1})
	var se *StatusError
	require.True(t, errors.As(err, &se))
	assert.Equal(t, http.StatusForbidden, se.Code)
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/", http.StatusFound)
	}))
	defer ts.Close()
	err := pinnedClient(ts).GetJSON(context.Background(), ts.Listener.Addr().String(), "/identity", nil, 1024)
	var se *StatusError
	require.True(t, errors.As(err, &se), "a redirect could lead outside Tor")
	assert.Equal(t, http.StatusFound, se.Code)
}

func TestGetJSONLimitsResponseSize(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"x":"` + strings.Repeat("a", 4096) + `"}`))
	}))
	defer ts.Close()
	var out map[string]string
	err := pinnedClient(ts).GetJSON(context.Background(), ts.Listener.Addr().String(), "/identity", &out, 1024)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "larger than")
}
