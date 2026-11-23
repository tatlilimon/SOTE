//go:build !nogui

package gui

import (
	"context"
	"fmt"
	"image/color"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"sote/internal/messages"
	"sote/internal/storage"
)

var (
	sentColor     = color.NRGBA{R: 0x7C, G: 0x3A, B: 0xED, A: 0xFF}
	receivedColor = color.NRGBA{R: 0x06, G: 0xB6, B: 0xD4, A: 0xFF}
)

type ChatView struct {
	contactSelect *widget.Select
	messageBox    *fyne.Container
	scroll        *container.Scroll
	input         *widget.Entry
	sendBtn       *widget.Button
	errLabel      *widget.Label

	container *fyne.Container

	username        string
	password        string
	contacts        []storage.Contact
	selectedContact string
	displayMsgs     []messages.DecryptedMessage

	msgSvc *messages.Service

	mu sync.Mutex
	// loads tracks background message loads (tests wait for them; fyne's
	// test driver runs fyne.Do callbacks on the calling goroutine).
	loads sync.WaitGroup

	// OnIncomingMessage is called when messages from a different user arrive.
	// The sender parameter is the username of the person who sent the message.
	// Set this callback to trigger desktop notifications or UI updates.
	OnIncomingMessage func(sender string)
}

func NewChatView(msgSvc *messages.Service, username, password string, contacts []storage.Contact) *ChatView {
	cv := &ChatView{
		msgSvc:   msgSvc,
		username: username,
		password: password,
		contacts: contacts,
	}
	cv.buildUI()

	if len(contacts) > 0 {
		// SetSelected fires the select's OnChanged, which loads the
		// conversation; loading again here started two concurrent loads.
		cv.contactSelect.SetSelected(contacts[0].ContactUsername)
	}

	return cv
}

func (cv *ChatView) buildUI() {
	names := make([]string, len(cv.contacts))
	for i, c := range cv.contacts {
		names[i] = c.ContactUsername
	}
	cv.contactSelect = widget.NewSelect(names, func(s string) {
		cv.selectedContact = s
		cv.displayMsgs = nil
		cv.errLabel.SetText("")
		cv.refreshMessageList()
		cv.updateSendState()
		cv.loadMessages()
	})
	cv.contactSelect.PlaceHolder = "Select contact..."

	cv.messageBox = container.NewVBox()
	cv.scroll = container.NewScroll(cv.messageBox)

	cv.input = widget.NewEntry()
	cv.input.PlaceHolder = "Type a message..."
	cv.input.OnChanged = func(_ string) { cv.updateSendState() }
	cv.input.OnSubmitted = func(_ string) { cv.sendMessage() }

	cv.sendBtn = widget.NewButton("Send", func() { cv.sendMessage() })
	cv.sendBtn.Importance = widget.HighImportance
	cv.sendBtn.Disable()

	cv.errLabel = widget.NewLabel("")

	inputRow := container.NewBorder(nil, nil, nil, cv.sendBtn, cv.input)
	bottom := container.NewVBox(cv.errLabel, inputRow)
	cv.container = container.NewBorder(cv.contactSelect, bottom, nil, nil, cv.scroll)
}

func (cv *ChatView) Container() *fyne.Container { return cv.container }

// waitLoads blocks until background message loads have finished.
func (cv *ChatView) waitLoads() { cv.loads.Wait() }

func (cv *ChatView) SelectedContact() string { return cv.selectedContact }

func (cv *ChatView) Messages() []messages.DecryptedMessage { return cv.displayMsgs }

func (cv *ChatView) SetContacts(contacts []storage.Contact) {
	cv.contacts = contacts
	names := make([]string, len(contacts))
	for i, c := range contacts {
		names[i] = c.ContactUsername
	}
	cv.contactSelect.Options = names
	cv.contactSelect.Refresh()

	if len(contacts) == 0 {
		cv.selectedContact = ""
		cv.contactSelect.ClearSelected()
	} else if cv.selectedContact == "" {
		cv.contactSelect.SetSelected(contacts[0].ContactUsername)
	}
	cv.updateSendState()
}

func (cv *ChatView) sendMessage() {
	text := cv.input.Text
	if text == "" || cv.selectedContact == "" {
		return
	}
	cv.input.SetText("")
	cv.updateSendState()

	sender, receiver, password := cv.username, cv.selectedContact, cv.password
	svc := cv.msgSvc

	go func() {
		err := svc.SendMessage(context.Background(), sender, receiver, password, text)
		fyne.Do(func() {
			if err != nil {
				cv.errLabel.SetText("Send failed")
			} else {
				cv.loadMessages()
			}
		})
	}()
}

func (cv *ChatView) loadMessages() {
	if cv.selectedContact == "" {
		return
	}
	user, contact, password := cv.username, cv.selectedContact, cv.password
	svc := cv.msgSvc

	cv.loads.Add(1)
	go func() {
		defer cv.loads.Done()
		msgs, err := svc.FetchMessages(context.Background(), user, contact, password)
		fyne.Do(func() {
			cv.mu.Lock()
			if err != nil {
				cv.errLabel.SetText("Failed to load messages")
				cv.mu.Unlock()
			} else {
				cv.displayMsgs = msgs
				cv.errLabel.SetText("")
				cv.mu.Unlock()
				cv.refreshMessageList()
				cv.scroll.ScrollToBottom()
				cv.notifyIncoming(msgs)
			}
		})
	}()
}

func (cv *ChatView) updateSendState() {
	disabled := cv.selectedContact == "" || cv.input.Text == ""
	if disabled {
		cv.sendBtn.Disable()
	} else {
		cv.sendBtn.Enable()
	}
}

func (cv *ChatView) notifyIncoming(msgs []messages.DecryptedMessage) {
	if cv.OnIncomingMessage == nil {
		return
	}
	seen := make(map[string]bool)
	for _, m := range msgs {
		if m.Sender != cv.username && !seen[m.Sender] {
			seen[m.Sender] = true
			cv.OnIncomingMessage(m.Sender)
		}
	}
}

func (cv *ChatView) refreshMessageList() {
	cv.mu.Lock()
	defer cv.mu.Unlock()
	cv.messageBox.RemoveAll()
	if len(cv.displayMsgs) == 0 {
		empty := widget.NewLabel("No messages yet. Type a message and press Enter to send.")
		empty.Alignment = fyne.TextAlignCenter
		cv.messageBox.Add(empty)
		return
	}
	for i := range cv.displayMsgs {
		cv.messageBox.Add(cv.newBubble(cv.displayMsgs[i]))
	}
	cv.messageBox.Refresh()
}

func (cv *ChatView) newBubble(msg messages.DecryptedMessage) fyne.CanvasObject {
	isSent := msg.Sender == cv.username
	ts := formatTimestamp(msg.Timestamp)

	contentLbl := widget.NewLabel(msg.Content)

	timeLbl := widget.NewLabel(ts)
	timeLbl.TextStyle = fyne.TextStyle{Italic: true}

	var header *fyne.Container
	if isSent {
		contentLbl.Alignment = fyne.TextAlignTrailing
		header = container.NewHBox(layout.NewSpacer(), timeLbl)
	} else {
		senderLbl := widget.NewLabel(msg.Sender)
		senderLbl.TextStyle = fyne.TextStyle{Bold: true}
		header = container.NewHBox(senderLbl, layout.NewSpacer(), timeLbl)
	}

	body := container.NewVBox(header, contentLbl)

	bg := canvas.NewRectangle(receivedColor)
	if isSent {
		bg = canvas.NewRectangle(sentColor)
	}

	bubble := container.NewStack(bg, container.NewPadded(body))

	if isSent {
		return container.NewHBox(layout.NewSpacer(), bubble)
	}
	return container.NewHBox(bubble, layout.NewSpacer())
}

func formatTimestamp(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		for _, layout := range []string{
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05Z",
		} {
			if t, err = time.Parse(layout, ts); err == nil {
				break
			}
		}
		if err != nil {
			return ""
		}
	}
	return fmt.Sprintf("[%s]", t.Format("15:04"))
}
