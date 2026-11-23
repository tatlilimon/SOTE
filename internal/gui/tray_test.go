//go:build !nogui

package gui

import (
	"testing"

	"fyne.io/fyne/v2/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrayMenuItems(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")

	tm := NewTrayManager(a, w)
	require.NotNil(t, tm)

	menu := tm.buildMenu()
	require.NotNil(t, menu)
	assert.Equal(t, "SOTE", menu.Label)

	items := menu.Items
	require.Len(t, items, 5)

	assert.Equal(t, "Show SOTE", items[0].Label)
	assert.NotNil(t, items[0].Action, "Show SOTE must have an action")

	assert.True(t, items[1].IsSeparator)

	assert.Contains(t, items[2].Label, "Connection:")
	assert.True(t, items[2].Disabled)

	assert.True(t, items[3].IsSeparator)

	assert.Equal(t, "Quit", items[4].Label)
	assert.NotNil(t, items[4].Action, "Quit must have an action")
}

func TestTrayRestoreWindow(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")

	tm := NewTrayManager(a, w)
	require.NotNil(t, tm)

	menu := tm.buildMenu()
	showItem := menu.Items[0]

	assert.NotPanics(t, func() {
		showItem.Action()
	})
}

func TestNotificationSent(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")

	tm := NewTrayManager(a, w)
	require.NotNil(t, tm)

	assert.NotPanics(t, func() {
		tm.NotifyIncoming("bob")
	})
}

func TestTrayWithoutDesktop(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")

	tm := NewTrayManager(a, w)
	require.NotNil(t, tm, "TrayManager must be created even without desktop support")

	isDesktop := tm.IsDesktopAvailable()
	t.Logf("desktop features available: %v", isDesktop)

	assert.NotPanics(t, func() { tm.SetConnected(true) })
	assert.NotPanics(t, func() { tm.SetConnected(false) })
	assert.NotPanics(t, func() { tm.NotifyIncoming("alice") })
}

func TestConnectionStatusLabel(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")

	tm := NewTrayManager(a, w)
	require.NotNil(t, tm)

	menu := tm.buildMenu()
	assert.Contains(t, menu.Items[2].Label, "Disconnected")

	tm.SetConnected(true)
	menu = tm.buildMenu()
	assert.Contains(t, menu.Items[2].Label, "Connected")

	tm.SetConnected(false)
	menu = tm.buildMenu()
	assert.Contains(t, menu.Items[2].Label, "Disconnected")
}

func TestTrayIconGeneration(t *testing.T) {
	tm := &TrayManager{connected: false}

	icon := tm.trayIcon()
	require.NotNil(t, icon, "disconnected icon must not be nil")
	assert.Contains(t, icon.Name(), "sote-tray")
	assert.True(t, len(icon.Content()) > 0, "icon must have content")

	tm.connected = true
	iconConnected := tm.trayIcon()
	require.NotNil(t, iconConnected, "connected icon must not be nil")
	assert.True(t, len(iconConnected.Content()) > 0, "connected icon must have content")

	assert.NotEqual(t, icon.Content(), iconConnected.Content(),
		"connected/disconnected icons must differ")
}

func TestTrayIconNilApp(t *testing.T) {
	tm := &TrayManager{connected: true}
	icon := tm.trayIcon()
	require.NotNil(t, icon)
}

func TestRefreshWithoutDesktop(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")

	tm := &TrayManager{app: a, window: w, connected: true}
	assert.NotPanics(t, func() { tm.refresh() })
}

func TestSetConnectedPreservesState(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("test")

	tm := NewTrayManager(a, w)
	require.False(t, tm.connected)

	tm.SetConnected(true)
	assert.True(t, tm.connected)

	tm.SetConnected(false)
	assert.False(t, tm.connected)
}

func TestMakeCircleIcon(t *testing.T) {
	icon := makeCircleIcon(nil)
	require.NotNil(t, icon)
	assert.NotEmpty(t, icon.Content())
}
