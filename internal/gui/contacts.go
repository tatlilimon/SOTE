//go:build !nogui

package gui

import (
	"context"
	"errors"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	domain "sote/internal/domain"
	contactSvc "sote/internal/contacts"
	st "sote/internal/storage"
)

var (
	errOnionEmpty   = errors.New("onion address cannot be empty")
	errOnionInvalid = errors.New("address must end with .onion")
)

func extractUsernameFromOnion(addr string) string {
	clean := strings.TrimPrefix(addr, "http://")
	clean = strings.TrimPrefix(clean, "https://")
	clean = strings.TrimSuffix(clean, ".onion")
	if idx := strings.LastIndex(clean, "/"); idx >= 0 {
		clean = clean[idx+1:]
	}
	if clean == "" {
		return "unknown"
	}
	return clean
}

func truncateOnion(addr string) string {
	if len(addr) > 24 {
		return addr[:20] + "..."
	}
	return addr
}

// ---------------------------------------------------------------------------
// contactRow — list item widget with right-click context menu support
// ---------------------------------------------------------------------------

var (
	_ fyne.Tappable          = (*contactRow)(nil)
	_ fyne.SecondaryTappable = (*contactRow)(nil)
)

type contactRow struct {
	widget.BaseWidget

	dot       *canvas.Circle
	username  *widget.Label
	onionAddr *widget.Label

	data         st.Contact
	onRightClick func(st.Contact)
}

func newContactRow() *contactRow {
	dot := canvas.NewCircle(fyne.CurrentApp().Settings().Theme().Color(
		theme.ColorNameSuccess, theme.VariantDark))
	dot.StrokeWidth = 0
	dot.Resize(fyne.NewSize(10, 10))

	username := widget.NewLabel("")
	username.TextStyle = fyne.TextStyle{Bold: true}

	onionAddr := widget.NewLabel("")
	onionAddr.TextStyle = fyne.TextStyle{Monospace: true}

	r := &contactRow{
		dot:       dot,
		username:  username,
		onionAddr: onionAddr,
	}
	r.ExtendBaseWidget(r)
	return r
}

func (r *contactRow) setContact(c st.Contact, pending bool) {
	r.data = c
	r.username.SetText(c.ContactUsername)
	r.onionAddr.SetText(truncateOnion(c.OnionAddress))

	th := fyne.CurrentApp().Settings().Theme()
	if pending {
		r.dot.FillColor = th.Color(theme.ColorNameWarning, theme.VariantDark)
	} else {
		r.dot.FillColor = th.Color(theme.ColorNameSuccess, theme.VariantDark)
	}
	r.dot.Refresh()
}

func (r *contactRow) Tapped(*fyne.PointEvent) {}

func (r *contactRow) TappedSecondary(*fyne.PointEvent) {
	if r.onRightClick != nil {
		r.onRightClick(r.data)
	}
}

func (r *contactRow) CreateRenderer() fyne.WidgetRenderer {
	dotWrapper := container.NewCenter(r.dot)
	textCol := container.NewVBox(r.username, container.NewPadded(r.onionAddr))
	row := container.NewBorder(nil, nil, container.NewPadded(dotWrapper), nil, textCol)
	return widget.NewSimpleRenderer(row)
}

// ---------------------------------------------------------------------------
// ContactsPage
// ---------------------------------------------------------------------------

type ContactsPage struct {
	contactSvc  *contactSvc.Service
	currentUser string
	window      fyne.Window

	contacts    []st.Contact
	pendingReqs []st.Contact

	List        *widget.List
	PendingList *widget.List
	Accordion   *widget.Accordion

	EmptyLabel *widget.Label
	EmptyBtn   *widget.Button
	AddBtn     *widget.Button
	PendingBtn *widget.Button

	content *fyne.Container
}

func NewContactsPage(svc *contactSvc.Service, currentUser string, win fyne.Window) *ContactsPage {
	p := &ContactsPage{
		contactSvc:  svc,
		currentUser: currentUser,
		window:      win,
	}
	p.buildUI()
	return p
}

func (p *ContactsPage) Content() fyne.CanvasObject {
	return p.content
}

func (p *ContactsPage) buildUI() {
	p.AddBtn = widget.NewButtonWithIcon("Add Contact", theme.ContentAddIcon(), func() {
		p.showAddContactDialog()
	})
	p.PendingBtn = widget.NewButtonWithIcon("Pending Requests", theme.ViewRefreshIcon(), func() {
		p.Accordion.Open(0)
	})
	toolbar := container.NewHBox(p.AddBtn, p.PendingBtn, widget.NewSeparator())

	// Contact list
	p.List = widget.NewList(
		func() int { return len(p.contacts) },
		func() fyne.CanvasObject { return newContactRow() },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			row := obj.(*contactRow)
			row.setContact(p.contacts[id], false)
			row.onRightClick = func(c st.Contact) {
				p.showContactContextMenu(c, false)
			}
		},
	)

	// Pending requests
	p.PendingList = widget.NewList(
		func() int { return len(p.pendingReqs) },
		func() fyne.CanvasObject { return newContactRow() },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			row := obj.(*contactRow)
			row.setContact(p.pendingReqs[id], true)
			row.onRightClick = func(c st.Contact) {
				p.showContactContextMenu(c, true)
			}
		},
	)

	pendingSection := container.NewBorder(
		container.NewHBox(
			widget.NewButtonWithIcon("Accept", theme.ConfirmIcon(), func() {}),
			widget.NewButtonWithIcon("Decline", theme.CancelIcon(), func() {}),
		), nil, nil, nil, p.PendingList)

	accItem := widget.NewAccordionItem("Pending Requests", pendingSection)
	p.Accordion = widget.NewAccordion(accItem)

	// Empty state
	p.EmptyLabel = widget.NewLabel("No contacts yet.")
	p.EmptyLabel.Alignment = fyne.TextAlignCenter
	p.EmptyLabel.TextStyle = fyne.TextStyle{Bold: true}

	p.EmptyBtn = widget.NewButtonWithIcon("Add Your First Contact", theme.ContentAddIcon(), func() {
		p.showAddContactDialog()
	})
	p.EmptyBtn.Importance = widget.HighImportance

	p.refreshContent()

	body := container.NewMax(
		p.List,
		container.NewCenter(container.NewVBox(p.EmptyLabel, p.EmptyBtn)),
	)
	scrollBody := container.NewBorder(nil, p.Accordion, nil, nil, body)
	p.content = container.NewBorder(toolbar, nil, nil, nil, scrollBody)
}

func (p *ContactsPage) refreshContent() {
	if len(p.contacts) == 0 {
		p.List.Hide()
		p.EmptyLabel.Show()
		p.EmptyBtn.Show()
	} else {
		p.List.Show()
		p.EmptyLabel.Hide()
		p.EmptyBtn.Hide()
	}
}

func (p *ContactsPage) LoadContacts() {
	go func() {
		contacts, err := p.contactSvc.GetContacts(context.Background(), p.currentUser)
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, p.window)
				return
			}
			p.contacts = contacts
			p.refreshContent()
			p.List.Refresh()
		})
	}()
}

func (p *ContactsPage) SetContacts(contacts []st.Contact) {
	p.contacts = contacts
	p.refreshContent()
	p.List.Refresh()
}

func (p *ContactsPage) SetPendingRequests(reqs []st.Contact) {
	p.pendingReqs = reqs
	p.PendingList.Refresh()
}

// ---------------------------------------------------------------------------
// Dialogs & menus
// ---------------------------------------------------------------------------

func (p *ContactsPage) showAddContactDialog() {
	onionEntry := widget.NewEntry()
	onionEntry.SetPlaceHolder("Enter .onion address (e.g., abcdef...onion)")

	d := dialog.NewForm("Add Contact", "Add", "Cancel", []*widget.FormItem{
		widget.NewFormItem("Onion Address", onionEntry),
	}, func(ok bool) {
		if !ok {
			return
		}
		addr := strings.TrimSpace(onionEntry.Text)
		if addr == "" || !strings.HasSuffix(addr, ".onion") {
			return
		}
		username := extractUsernameFromOnion(addr)
		if username == p.currentUser {
			dialog.ShowInformation("Cannot Add", "You cannot add yourself as a contact.", p.window)
			return
		}
		c := domain.Contact{
			Username:     username,
			OnionAddress: addr,
		}
		go func() {
			err := p.contactSvc.AddContact(context.Background(), p.currentUser, c)
			fyne.Do(func() {
				if err != nil {
					dialog.ShowError(err, p.window)
					return
				}
				p.LoadContacts()
			})
		}()
	}, p.window)
	d.Resize(fyne.NewSize(420, 0))
	d.Show()
}

func (p *ContactsPage) showContactContextMenu(c st.Contact, pending bool) {
	copyItem := fyne.NewMenuItem("Copy Onion Address", func() {
		p.window.Clipboard().SetContent(c.OnionAddress)
	})
	copyItem.Icon = theme.ContentCopyIcon()

	if pending {
		acceptItem := fyne.NewMenuItem("Accept Request", func() {
			p.handleAcceptPending(c)
		})
		acceptItem.Icon = theme.ConfirmIcon()
		declineItem := fyne.NewMenuItem("Decline Request", func() {
			p.handleDeclinePending(c)
		})
		declineItem.Icon = theme.CancelIcon()
		menu := fyne.NewMenu("", acceptItem, declineItem, fyne.NewMenuItemSeparator(), copyItem)
		popup := widget.NewPopUpMenu(menu, p.window.Canvas())
		popup.ShowAtPosition(fyne.NewPos(0, 0))
		return
	}

	deleteItem := fyne.NewMenuItem("Delete Contact", func() {
		p.showDeleteConfirm(c)
	})
	deleteItem.Icon = theme.DeleteIcon()
	menu := fyne.NewMenu("", copyItem, fyne.NewMenuItemSeparator(), deleteItem)
	popup := widget.NewPopUpMenu(menu, p.window.Canvas())
	popup.ShowAtPosition(fyne.NewPos(0, 0))
}

func (p *ContactsPage) showDeleteConfirm(c st.Contact) {
	dialog.ShowConfirm("Delete Contact",
		"Are you sure you want to delete "+c.ContactUsername+"?",
		func(ok bool) {
			if ok {
				p.deleteContact(c)
			}
		}, p.window)
}

func (p *ContactsPage) deleteContact(c st.Contact) {
	filtered := make([]st.Contact, 0, len(p.contacts))
	for _, existing := range p.contacts {
		if existing.ID != c.ID {
			filtered = append(filtered, existing)
		}
	}
	p.SetContacts(filtered)
}

func (p *ContactsPage) handleAcceptPending(c st.Contact) {
	go func() {
		contact := domain.Contact{
			Username:     c.ContactUsername,
			OnionAddress: c.OnionAddress,
			PublicKey:    c.PublicKey,
		}
		_, err := p.contactSvc.AcceptContactRequest(context.Background(), p.currentUser, contact)
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(err, p.window)
				return
			}
			p.removePending(c)
			p.LoadContacts()
		})
	}()
}

func (p *ContactsPage) handleDeclinePending(c st.Contact) {
	p.removePending(c)
}

func (p *ContactsPage) removePending(c st.Contact) {
	filtered := make([]st.Contact, 0, len(p.pendingReqs))
	for _, r := range p.pendingReqs {
		if r.ID != c.ID {
			filtered = append(filtered, r)
		}
	}
	p.SetPendingRequests(filtered)
}
