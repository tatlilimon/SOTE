// Package tor manages Tor hidden service setup, lifecycle, and connectivity
// using the cretz/bine Go library. It provides dynamic SOCKS port allocation
// and automatic hidden service creation.
package tor

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"sync"

	btor "github.com/cretz/bine/tor"
)

// TorService implements domain.TorService using cretz/bine for Tor lifecycle:
// starting/stopping a Tor process, creating a v3 hidden service, and
// providing the .onion address.
type TorService struct {
	mu        sync.Mutex
	tor       *btor.Tor
	onionSvc  *btor.OnionService
	onionAddr string

	dataDir    string
	listenPort int
	socksPort  int
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

// Start launches a Tor process via bine, creates a v3 hidden service that
// forwards to listenPort, and waits for the onion address. The context is
// used for both process startup and waiting for the hidden service to be
// published. Returns an error if the tor binary is not found in PATH.
func (s *TorService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tor != nil {
		return fmt.Errorf("tor already running")
	}

	if _, err := exec.LookPath("tor"); err != nil {
		return fmt.Errorf("Tor not found. Install: apt install tor")
	}

	socksPort, err := findAvailablePort(s.socksPort, 100)
	if err != nil {
		return fmt.Errorf("find available SOCKS port: %w", err)
	}
	s.socksPort = socksPort

	t, err := btor.Start(ctx, &btor.StartConf{
		NoAutoSocksPort: true,
		ExtraArgs: []string{
			"--SocksPort", fmt.Sprintf("127.0.0.1:%d", socksPort),
		},
		DataDir: s.dataDir,
	})
	if err != nil {
		return fmt.Errorf("start tor: %w", err)
	}
	t.StopProcessOnClose = true

	onion, err := t.Listen(ctx, &btor.ListenConf{
		RemotePorts: []int{s.listenPort},
		Version3:    true,
		NoWait:      false,
	})
	if err != nil {
		t.Close()
		return fmt.Errorf("create hidden service: %w", err)
	}

	s.tor = t
	s.onionSvc = onion
	s.onionAddr = onion.ID + ".onion"

	return nil
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
