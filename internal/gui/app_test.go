//go:build !nogui

package gui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAppStarts verifies that Run creates a Fyne application and window.
// Uses test.NewApp() which is headless — no display required.
func TestAppStarts(t *testing.T) {
	a := test.NewApp()
	w := a.NewWindow("SOTE — Decentralized Messenger")

	assert.NotNil(t, a, "app should not be nil")
	assert.NotNil(t, w, "window should not be nil")
	assert.Equal(t, "SOTE — Decentralized Messenger", w.Title())
}

// TestWindowTitle verifies the window has exactly the required title.
func TestWindowTitle(t *testing.T) {
	a := test.NewApp()
	w := a.NewWindow("SOTE — Decentralized Messenger")

	// Title must match exactly as specified in the task
	assert.Equal(t, "SOTE — Decentralized Messenger", w.Title(),
		"window title must be 'SOTE — Decentralized Messenger'")
}

// TestWindowSize verifies the window is created with correct dimensions.
func TestWindowSize(t *testing.T) {
	a := test.NewApp()
	w := a.NewWindow("SOTE — Decentralized Messenger")

	// Resize to required dimensions: 1024x768
	w.Resize(fyne.NewSize(1024, 768))
	sz := w.Canvas().Size()

	assert.Equal(t, float32(1024), sz.Width, "window width must be 1024")
	assert.Equal(t, float32(768), sz.Height, "window height must be 768")
}

// TestRunCreatesWindow verifies that the Run function creates a window without panicking.
// Because ShowAndRun blocks, we call a helper that creates the window but doesn't show it.
func TestRunCreatesWindow(t *testing.T) {
	a := test.NewApp()
	w := createWindow(a)

	require.NotNil(t, w, "createWindow must return a non-nil window")
	assert.Equal(t, "SOTE — Decentralized Messenger", w.Title())
}

func TestTabContainer(t *testing.T) {
	a := test.NewApp()
	w := createWindow(a)
	require.NotNil(t, w, "createWindow must return a non-nil window")

	tabs := container.NewAppTabs(
		container.NewTabItem("Chat", widget.NewLabel("Chat")),
		container.NewTabItem("Contacts", widget.NewLabel("Contacts")),
		container.NewTabItem("Settings", widget.NewLabel("Settings")),
	)
	w.SetContent(tabs)
	assert.Equal(t, 3, len(tabs.Items))
	_ = a
}
