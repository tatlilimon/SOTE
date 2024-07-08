package tor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// StartTor starts the Tor client
func StartTor() error {
	cmd := exec.Command("tor")
	err := cmd.Start()
	if err != nil {
		return err
	}
	fmt.Println("Tor client started")
	return nil
}

// GenerateOnionAddress generates a new .onion address
func GenerateOnionAddress() (string, string, error) {
	// Create a temporary directory for the hidden service
	hiddenServiceDir, err := os.MkdirTemp("/var/tmp", "hidden_service")
	if err != nil {
		fmt.Println("Error creating temporary directory:", err) // Debug print
		return "", "", err
	}
	fmt.Println("Temporary directory created:", hiddenServiceDir) // Debug print

	// Write the hidden service configuration
	hiddenServiceConfig := fmt.Sprintf(`
HiddenServiceDir %s
CookieAuthentication 1
HiddenServicePort 18080 127.0.0.1:18080
SocksPort 127.0.0.1:9060
ControlPort 9061
`, hiddenServiceDir)

	configFile := filepath.Join(hiddenServiceDir, "torrc")
	err = os.WriteFile(configFile, []byte(hiddenServiceConfig), 0600)
	if err != nil {
		fmt.Println("Error writing hidden service configuration:", err) // Debug print
		return "", "", err
	}
	fmt.Println("Hidden service configuration written to:", configFile) // Debug print

	// Start Tor with the hidden service configuration
	cmd := exec.Command("tor", "-f", configFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	time.Sleep(3 * time.Second)
	defer cmd.Process.Kill()
	if err != nil {
		fmt.Println("Error starting Tor with this torrc file:", configFile, err) // Debug print
		return "", "", err
	}
	fmt.Println("Tor started with hidden service configuration") // Debug print

	// Wait for the hidden service to be created
	onionAddressFile := filepath.Join(hiddenServiceDir, "hostname")
	for {
		if _, err := os.Stat(onionAddressFile); err == nil {
			break
		}
		fmt.Println("Waiting for hidden service to be created...") // Debug print
		time.Sleep(1 * time.Second)                                // Add a small delay to avoid busy-waiting
	}

	// Read the .onion address
	onionAddress, err := os.ReadFile(onionAddressFile)
	if err != nil {
		fmt.Println("Error reading .onion address:", err) // Debug print
		return "", "", err
	}

	fmt.Println("Onion address generated:", string(onionAddress)) // Debug print
	return string(onionAddress), configFile, nil
}

// StartTorWithConfig starts the Tor client with a specified configuration file
func StartTorWithConfig(configFile string) error {
	cmd := exec.Command("tor", "-f", configFile)
	err := cmd.Start()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err != nil {
		return err
	}
	fmt.Println("Tor client started with config file:", configFile)
	return nil
}
