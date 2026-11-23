// Package tor manages Tor hidden service setup, lifecycle, and connectivity
// using the cretz/bine Go library. It provides dynamic SOCKS port allocation,
// a persistent v3 onion service and optional bridges.
package tor

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"os/exec"
	"sync"

	btor "github.com/cretz/bine/tor"
	"github.com/cretz/bine/torutil"
	torEd "github.com/cretz/bine/torutil/ed25519"
)

// RemotePort is the virtual port peers connect to on the onion address.
const RemotePort = 443

// TorService implements domain.TorService using cretz/bine for Tor lifecycle:
// starting/stopping a Tor process, publishing a v3 onion service, and
// providing the .onion address and a SOCKS dialer.
type TorService struct {
	mu        sync.Mutex
	tor       *btor.Tor
	onionSvc  *btor.OnionService
	onionAddr string

	dataDir    string
	listenPort int
	socksPort  int

	// onionKey keeps the onion address stable across restarts. Without it Tor
	// generates a new address on every start and contacts lose the user.
	onionKey ed25519.PrivateKey
	listener net.Listener

	useBridges bool
	bridges    []string
	plugins    []string
}

// New creates a TorService with the given configuration. The Tor process is
// not started until Start is called. socksPort is the initial SOCKS port to
// try; if it is in use, Start auto-increments until a free port is found.
func New(dataDir string, listenPort, socksPort int) *TorService {
	return &TorService{
		dataDir:    dataDir,
		listenPort: listenPort,
		socksPort:  socksPort,
	}
}

// SetOnionKey sets the onion service key. Call before Start.
func (s *TorService) SetOnionKey(key ed25519.PrivateKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onionKey = key
}

// SetListener sets the local listener the onion service forwards to. Call
// before Start. Without one, a listener on 127.0.0.1:listenPort is created.
func (s *TorService) SetListener(ln net.Listener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listener = ln
}

// SetBridges configures bridges (validated by the config package). Call
// before Start; a running Tor keeps its settings until restarted.
func (s *TorService) SetBridges(use bool, bridges, plugins []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.useBridges = use
	s.bridges = append([]string(nil), bridges...)
	s.plugins = append([]string(nil), plugins...)
}

// UsingBridges reports whether Start will use bridges.
func (s *TorService) UsingBridges() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.useBridges && len(s.bridges) > 0
}

// OnionAddressForKey returns the v3 onion address of an onion service key.
func OnionAddressForKey(key ed25519.PrivateKey) string {
	pub := key.Public().(ed25519.PublicKey)
	return torutil.OnionServiceIDFromV3PublicKey(torEd.PublicKey(pub)) + ".onion"
}

// torArgs builds the command-line options for the Tor process. Bridge and
// plugin lines are passed as separate arguments (never through a torrc
// file), so a line cannot inject other options.
func torArgs(socksPort int, useBridges bool, bridges, plugins []string) []string {
	args := []string{"--SocksPort", fmt.Sprintf("127.0.0.1:%d", socksPort)}
	if useBridges && len(bridges) > 0 {
		args = append(args, "--UseBridges", "1")
		for _, p := range plugins {
			args = append(args, "--ClientTransportPlugin", p)
		}
		for _, b := range bridges {
			args = append(args, "--Bridge", b)
		}
	}
	return args
}

// Start launches a Tor process via bine, publishes the v3 onion service that
// forwards RemotePort to the local listener, and waits for it to be
// published. Returns an error if the tor binary is not found in PATH.
func (s *TorService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tor != nil {
		return fmt.Errorf("tor already running")
	}

	if _, err := exec.LookPath("tor"); err != nil {
		return fmt.Errorf("Tor not found. Install it with your package manager (e.g. dnf install tor, apt install tor)")
	}

	socksPort, err := findAvailablePort(s.socksPort, 100)
	if err != nil {
		return fmt.Errorf("find available SOCKS port: %w", err)
	}
	s.socksPort = socksPort

	t, err := btor.Start(ctx, &btor.StartConf{
		NoAutoSocksPort: true,
		ExtraArgs:       torArgs(socksPort, s.useBridges, s.bridges, s.plugins),
		DataDir:         s.dataDir,
	})
	if err != nil {
		return fmt.Errorf("start tor: %w", err)
	}
	t.StopProcessOnClose = true

	conf := &btor.ListenConf{
		LocalListener: s.listener,
		LocalPort:     s.listenPort,
		RemotePorts:   []int{RemotePort},
		Version3:      true,
		NoWait:        false,
	}
	if s.onionKey != nil {
		conf.Key = s.onionKey
	}
	onion, err := t.Listen(ctx, conf)
	if err != nil {
		t.Close()
		return fmt.Errorf("create hidden service: %w", err)
	}

	s.tor = t
	s.onionSvc = onion
	s.onionAddr = onion.ID + ".onion"
	if s.listener == nil {
		s.listener = onion.LocalListener
	}

	return nil
}

// LocalListener returns the listener the onion service forwards to.
func (s *TorService) LocalListener() net.Listener {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listener
}

// SocksPort returns the SOCKS port Tor listens on (after Start).
func (s *TorService) SocksPort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.socksPort
}

// Stop gracefully tears down the hidden service and Tor process. Safe to
// call even if Start was never called or already stopped.
func (s *TorService) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tor == nil {
		return nil
	}

	if s.onionSvc != nil {
		s.onionSvc.Close()
		s.onionSvc = nil
	}

	err := s.tor.Close()
	s.tor = nil
	s.onionAddr = ""

	return err
}

// GetOnionAddress returns the .onion hostname of the hidden service, or
// an empty string if the service has not been started.
func (s *TorService) GetOnionAddress() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.onionAddr
}

// Dialer returns a bine SOCKS5 dialer that routes connections through the
// Tor process. The caller can use the returned Dialer.DialContext method
// to make proxied connections. The context is used for the initial
// EnableNetwork call; callers must ensure the Tor process is running.
func (s *TorService) Dialer(ctx context.Context) (*btor.Dialer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tor == nil {
		return nil, fmt.Errorf("tor: not started")
	}

	return s.tor.Dialer(ctx, &btor.DialConf{})
}

// findAvailablePort probes ports starting at initialPort and incrementing
// until a free TCP port is found. It tries up to maxAttempts before failing.
// The returned port is not reserved — there is a TOCTOU window between the
// probe and actual use, but in practice this is sufficient.
func findAvailablePort(initialPort, maxAttempts int) (int, error) {
	for port := initialPort; port < initialPort+maxAttempts; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		ln.Close()
		return port, nil
	}
	return 0, fmt.Errorf("no free port in range %d-%d", initialPort, initialPort+maxAttempts-1)
}
