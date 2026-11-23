package integration

import (
	"database/sql"

	"sote/internal/auth"
	"sote/internal/config"
	"sote/internal/contacts"
	"sote/internal/crypto"
	"sote/internal/messages"
	"sote/internal/tor"
)

// Deps is everything a user interface needs, built once in main.
type Deps struct {
	Node       *Node
	Config     *config.Config
	ConfigPath string
	DB         *sql.DB
	Auth       *auth.AuthService
	Crypto     *crypto.Service
	Contacts   *contacts.Service
	Messages   *messages.Service
	Tor        *tor.TorService
}

// SaveBridges validates bridge settings pasted into a settings screen and
// writes them to the configuration file. They take effect when Tor starts
// again (the next time SOTE starts).
func (d Deps) SaveBridges(use bool, text string) error {
	bridges, plugins, err := config.ParseBridgeText(text)
	if err != nil {
		return err
	}
	next := *d.Config
	next.UseBridges, next.Bridges, next.ClientTransportPlugins = use, bridges, plugins
	if err := config.SaveConfig(d.ConfigPath, &next); err != nil {
		return err
	}
	*d.Config = next
	return nil
}
