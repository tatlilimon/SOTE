//go:build nogui

// Package gui is empty in builds made with the nogui tag. The real desktop
// interface (Fyne) needs the X11 and OpenGL development libraries at build
// time; terminal-only builds leave it out so they compile without them.
package gui

import (
	"errors"

	"sote/internal/integration"
)

// ErrNotBuilt is returned when the binary was built with the nogui tag.
var ErrNotBuilt = errors.New("this SOTE binary was built without the desktop interface (-tags nogui); rebuild without the tag to use --gui")

// Available reports whether the desktop interface is compiled in.
const Available = false

// Run always fails in nogui builds.
func Run(integration.Deps) error {
	return ErrNotBuilt
}
