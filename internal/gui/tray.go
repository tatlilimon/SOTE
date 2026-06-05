package gui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log/slog"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// TrayManager manages the system tray icon, menu, and desktop notifications.
// If desktop features are unavailable (headless/test environments), the manager
// gracefully disables tray functionality without crashing.
type TrayManager struct {
	app       fyne.App
	window    fyne.Window
	connected bool
}

// NewTrayManager creates and initializes the system tray.
// If the app does not support desktop features, a warning is logged
// and tray functionality is skipped — all methods remain safe to call.
func NewTrayManager(app fyne.App, window fyne.Window) *TrayManager {
	tm := &TrayManager{
		app:    app,
		window: window,
	}
	tm.setup()
	return tm
}

func (tm *TrayManager) setup() {
	desk, ok := tm.app.(desktop.App)
	if !ok {
		slog.Warn("desktop features not available — tray disabled")
		return
	}
	desk.SetSystemTrayMenu(tm.buildMenu())
	desk.SetSystemTrayIcon(tm.trayIcon())
}

// buildMenu constructs the system tray menu with Show, status, and Quit items.
func (tm *TrayManager) buildMenu() *fyne.Menu {
	showItem := fyne.NewMenuItem("Show SOTE", func() {
		tm.window.Show()
		tm.window.RequestFocus()
	})

	statusLabel := "Connection: Disconnected"
	if tm.connected {
		statusLabel = "Connection: Connected"
	}
	statusItem := fyne.NewMenuItem(statusLabel, nil)
	statusItem.Disabled = true

	quitItem := fyne.NewMenuItem("Quit", func() {
		tm.app.Quit()
	})

	return fyne.NewMenu("SOTE",
		showItem,
		fyne.NewMenuItemSeparator(),
		statusItem,
		fyne.NewMenuItemSeparator(),
		quitItem,
	)
}

// SetConnected updates the connection status and refreshes the tray menu and icon.
func (tm *TrayManager) SetConnected(connected bool) {
	tm.connected = connected
	tm.refresh()
}

func (tm *TrayManager) refresh() {
	desk, ok := tm.app.(desktop.App)
	if !ok {
		return
	}
	desk.SetSystemTrayMenu(tm.buildMenu())
	desk.SetSystemTrayIcon(tm.trayIcon())
}

// trayIcon returns a small colored circle resource: green when connected, red otherwise.
func (tm *TrayManager) trayIcon() fyne.Resource {
	c := color.NRGBA{R: 0xEF, G: 0x44, B: 0x44, A: 0xFF}
	if tm.connected {
		c = color.NRGBA{R: 0x10, G: 0xB9, B: 0x81, A: 0xFF}
	}
	return makeCircleIcon(c)
}

// NotifyIncoming sends a desktop notification for an incoming message.
func (tm *TrayManager) NotifyIncoming(sender string) {
	tm.app.SendNotification(fyne.NewNotification("SOTE",
		fmt.Sprintf("New message from %s", sender)))
}

// IsDesktopAvailable returns whether the app supports desktop features.
func (tm *TrayManager) IsDesktopAvailable() bool {
	_, ok := tm.app.(desktop.App)
	return ok
}

// makeCircleIcon creates a fyne.Resource containing a small (16×16) colored circle as PNG.
func makeCircleIcon(c color.Color) fyne.Resource {
	const size = 16
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	cx, cy, r := size/2, size/2, size/2-1

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := x - cx
			dy := y - cy
			if dx*dx+dy*dy <= r*r {
				img.Set(x, y, c)
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		slog.Error("failed to encode tray icon", "error", err)
		return nil
	}
	return fyne.NewStaticResource("sote-tray.png", buf.Bytes())
}
