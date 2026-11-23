package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Bridges let users in places that block Tor reach the network. Lines use the
// same syntax as Tor's Bridge and ClientTransportPlugin options, e.g. what
// https://bridges.torproject.org hands out:
//
//	obfs4 192.0.2.10:443 0123456789ABCDEF0123456789ABCDEF01234567 cert=... iat-mode=0
//	ClientTransportPlugin obfs4 exec /usr/bin/lyrebird

var (
	transportRe   = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
	fingerprintRe = regexp.MustCompile(`^[0-9A-Fa-f]{40}$`)
	argKeyRe      = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

	ErrBridgesRequired = errors.New("use_bridges is on but no bridge lines are configured")
)

const maxBridgeLineLen = 2048

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// NormalizeBridgeLine checks one Bridge line (a leading "Bridge " is
// accepted) and returns it with single spaces between fields.
func NormalizeBridgeLine(line string) (string, error) {
	line = strings.TrimSpace(line)
	if len(line) > maxBridgeLineLen || hasControl(line) {
		return "", fmt.Errorf("invalid bridge line: too long or contains control characters")
	}
	fields := strings.Fields(line)
	if len(fields) > 0 && strings.EqualFold(fields[0], "Bridge") {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return "", errors.New("invalid bridge line: empty")
	}
	i := 0
	if !strings.Contains(fields[0], ":") {
		if !transportRe.MatchString(fields[0]) {
			return "", fmt.Errorf("invalid bridge line %q: bad transport name", line)
		}
		i = 1
	}
	if i >= len(fields) {
		return "", fmt.Errorf("invalid bridge line %q: missing address:port", line)
	}
	host, port, err := net.SplitHostPort(fields[i])
	if err != nil || net.ParseIP(host) == nil {
		return "", fmt.Errorf("invalid bridge line %q: address must be IP:port", line)
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return "", fmt.Errorf("invalid bridge line %q: bad port", line)
	}
	i++
	if i < len(fields) && !strings.Contains(fields[i], "=") {
		if !fingerprintRe.MatchString(fields[i]) {
			return "", fmt.Errorf("invalid bridge line %q: fingerprint must be 40 hex characters", line)
		}
		i++
	}
	for ; i < len(fields); i++ {
		k, _, ok := strings.Cut(fields[i], "=")
		if !ok || !argKeyRe.MatchString(k) {
			return "", fmt.Errorf("invalid bridge line %q: unexpected %q", line, fields[i])
		}
	}
	return strings.Join(fields, " "), nil
}

// BridgeTransport returns the pluggable transport a bridge line uses, or ""
// for a plain bridge.
func BridgeTransport(line string) string {
	fields := strings.Fields(line)
	if len(fields) > 0 && strings.EqualFold(fields[0], "Bridge") {
		fields = fields[1:]
	}
	if len(fields) == 0 || strings.Contains(fields[0], ":") {
		return ""
	}
	return fields[0]
}

// NormalizePluginLine checks one ClientTransportPlugin line (a leading
// "ClientTransportPlugin " is accepted): "<transport>[,<transport>] exec
// <absolute path> [args...]". Returns the line and the transports it serves.
func NormalizePluginLine(line string) (string, []string, error) {
	line = strings.TrimSpace(line)
	if len(line) > maxBridgeLineLen || hasControl(line) {
		return "", nil, errors.New("invalid transport plugin line: too long or contains control characters")
	}
	fields := strings.Fields(line)
	if len(fields) > 0 && strings.EqualFold(fields[0], "ClientTransportPlugin") {
		fields = fields[1:]
	}
	if len(fields) < 3 || fields[1] != "exec" {
		return "", nil, fmt.Errorf("invalid transport plugin line %q: expected \"<transport> exec <path>\"", line)
	}
	transports := strings.Split(fields[0], ",")
	for _, t := range transports {
		if !transportRe.MatchString(t) {
			return "", nil, fmt.Errorf("invalid transport plugin line %q: bad transport name %q", line, t)
		}
	}
	if !filepath.IsAbs(fields[2]) {
		return "", nil, fmt.Errorf("invalid transport plugin line %q: the plugin path must be absolute", line)
	}
	return strings.Join(fields, " "), transports, nil
}

// ValidateBridges checks bridge and plugin lines together: every line must be
// well-formed, and every transport a bridge uses must have a plugin.
func ValidateBridges(use bool, bridges, plugins []string) error {
	if use && len(bridges) == 0 {
		return ErrBridgesRequired
	}
	provided := map[string]bool{}
	for _, p := range plugins {
		_, ts, err := NormalizePluginLine(p)
		if err != nil {
			return err
		}
		for _, t := range ts {
			provided[t] = true
		}
	}
	var missing []string
	for _, b := range bridges {
		if _, err := NormalizeBridgeLine(b); err != nil {
			return err
		}
		if t := BridgeTransport(b); t != "" && !provided[t] && !contains(missing, t) {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("bridges use transport %s but no client_transport_plugins line provides it", strings.Join(missing, ", "))
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// ParseBridgeText splits text pasted into a settings editor into bridge and
// plugin lines. Blank lines and lines starting with # are ignored; lines
// starting with "ClientTransportPlugin" are plugin lines.
func ParseBridgeText(text string) (bridges, plugins []string, err error) {
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if f := strings.Fields(line); strings.EqualFold(f[0], "ClientTransportPlugin") {
			p, _, err := NormalizePluginLine(line)
			if err != nil {
				return nil, nil, err
			}
			plugins = append(plugins, p)
			continue
		}
		b, err := NormalizeBridgeLine(line)
		if err != nil {
			return nil, nil, err
		}
		bridges = append(bridges, b)
	}
	return bridges, plugins, nil
}

// BridgeText renders bridge and plugin lines for a settings editor.
func BridgeText(bridges, plugins []string) string {
	var sb strings.Builder
	for _, b := range bridges {
		sb.WriteString(b + "\n")
	}
	for _, p := range plugins {
		sb.WriteString("ClientTransportPlugin " + p + "\n")
	}
	return sb.String()
}

// SaveConfig writes the configuration atomically, readable by the owner only:
// unlisted bridge addresses are sensitive in places that block Tor.
func SaveConfig(path string, cfg *Config) error {
	if err := validateConfig(cfg); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}
	return nil
}
