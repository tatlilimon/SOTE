//go:build nogui

// Package gui is empty in builds made with the nogui tag. The real desktop
// interface (Fyne) needs the X11 and OpenGL development libraries at build
// time; terminal-only builds leave it out so they compile without them.
package gui

import (
	"database/sql"
	"errors"

	"sote/internal/auth"
	"sote/internal/config"
	"sote/internal/contacts"
	"sote/internal/crypto"
	"sote/internal/messages"
	"sote/internal/tor"
	"sote/internal/transport"
)

// ErrNotBuilt is returned when the binary was built with the nogui tag.
var ErrNotBuilt = errors.New("this SOTE binary was built without the desktop interface (-tags nogui); rebuild without the tag to use --gui")

// Available reports whether the desktop interface is compiled in.
const Available = false

// Run always fails in nogui builds.
func Run(cfg *config.Config, db *sql.DB,
	authSvc *auth.AuthService, cryptoSvc *crypto.Service,
	contactSvc *contacts.Service, msgSvc *messages.Service,
	torSvc *tor.TorService, srv *transport.Server, client *transport.Client,
) error {
	return ErrNotBuilt
}
