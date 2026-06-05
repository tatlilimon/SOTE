package gui

import (
	"database/sql"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"

	"sote/internal/auth"
	"sote/internal/config"
	"sote/internal/contacts"
	"sote/internal/crypto"
	"sote/internal/messages"
	"sote/internal/tor"
	"sote/internal/transport"
)

func Run(cfg *config.Config, db *sql.DB,
	authSvc *auth.AuthService, cryptoSvc *crypto.Service,
	contactSvc *contacts.Service, msgSvc *messages.Service,
	torSvc *tor.TorService, srv *transport.Server, client *transport.Client,
) {
	a := app.NewWithID("sote.gui")
	a.Settings().SetTheme(NewSOTETheme())
	w := createWindow(a)

	NewTrayManager(a, w)

	w.ShowAndRun()
	_ = cfg
	_ = db
	_ = authSvc
	_ = cryptoSvc
	_ = contactSvc
	_ = msgSvc
	_ = torSvc
	_ = srv
	_ = client
}

func createWindow(a fyne.App) fyne.Window {
	w := a.NewWindow("SOTE — Decentralized Messenger")
	w.Resize(fyne.NewSize(1024, 768))
	w.SetMaster()

	tabs := container.NewAppTabs()
	w.SetContent(tabs)
	return w
}
