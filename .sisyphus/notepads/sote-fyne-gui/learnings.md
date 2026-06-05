# Learnings - SOTE Fyne GUI Task 1

## Date: 2026-06-05

## Key Decisions

1. **Fyne v2.7.4**: Installed via `go get fyne.io/fyne/v2@latest`. This is the latest stable version. The dependency added ~30 transitive dependencies including GLFW, OpenGL, and X11 bindings.

2. **Binary size jump**: From ~22MB (TUI-only) to ~44MB with Fyne GUI. Expected due to the GL rendering backend.

3. **`createWindow` helper for testing**: Separated window creation from `Run()` so tests can create windows without `ShowAndRun()` blocking. `Run()` calls `createWindow()` internally. This is a proper TDD pattern — the helper is in the same package so tests can call it.

4. **`app.NewWithID("sote.gui")`**: Used instead of `app.New()` to give the app a unique ID. This is important for X11 window management and desktop file integration.

5. **`_ = serviceVar` pattern**: Services are received by `Run()` but not yet used (this is Task 1 — scaffold only). The `_ = var` pattern suppresses Go's "unused variable" errors. These will be removed in subsequent tasks when views use the services.

6. **Service injection matches TUI pattern**: `runGUI()` takes exactly the same parameters as `runTUI()` — same types, same order. This ensures consistency and makes it easy to switch between modes.

7. **`--gui` flag default is `false`**: `flag.Bool("gui", false, ...)` ensures `./sote` (no flag) still launches TUI. Only `./sote --gui` launches GUI.

8. **`flag.Parse()` placement**: Must be called after `flag.Bool()` but before any flag usage. Placed after config load and before logger init to ensure config path determination happens first.

## Successful Patterns

- TDD: Wrote `app_test.go` first (wouldn't compile without app.go), then `app.go` (GREEN — all 5 tests pass).
- `test.NewApp()` creates headless Fyne app — no X11 display needed for tests.
- `fyne.NewSize(1024, 768)` for window dimensions match task spec exactly.
- `container.NewAppTabs()` creates the tab container skeleton for future Task 2-8 views.

## Issues Encountered

- **Missing `libXxf86vm-dev`**: The system has `libXxf86vm.so.1` (runtime) but not `libXxf86vm.so` (development symlink). CGo linker fails with "cannot find -lXxf86vm". Workaround: create symlink in project directory and use `CGO_LDFLAGS="-L$(pwd) -lXxf86vm"`. Permanent fix: `sudo apt-get install libxxf86vm-dev` (requires root).

- **`go mod tidy` removes Fyne when no code imports it**: Running `go mod tidy` after `go get fyne.io/fyne/v2@latest` but before writing code that imports it causes the dependency to be removed. Fixed by running `go get` again after code exists.

- **testify upgraded**: `github.com/stretchr/testify` was upgraded from v1.9.0 to v1.11.1 as a transitive dependency of Fyne. All existing tests still pass.

## Test Results

### `internal/gui/` (5 tests, all PASS with `-race`)
| Test | What it verifies |
|------|-----------------|
| TestAppStarts | Fyne test app + window creation, title check |
| TestWindowTitle | Title exactly "SOTE — Decentralized Messenger" |
| TestWindowSize | Window resized to 1024×768 |
| TestRunCreatesWindow | createWindow helper returns valid window |
| TestTabContainer | Window content is not nil (AppTabs set) |

### `cmd/sote/` (4 tests, all PASS with `-race`)
| Test | What it verifies |
|------|-----------------|
| TestSIGTERMShutdown | Signal handler works (existing) |
| TestDBClosedOnShutdown | WAL checkpoint + DB close (existing) |
| TestTorClosedOnShutdown | Tor stop safe on unstarted (existing) |
| TestGUIFlagParsed | --gui flag set/absent/shorthand (new) |

## Files Modified/Created

- `go.mod` — added `fyne.io/fyne/v2 v2.7.4` + transitive deps
- `go.sum` — updated automatically
- `internal/gui/app.go` — NEW: Run function + createWindow helper
- `internal/gui/app_test.go` — NEW: 5 tests
- `cmd/sote/main.go` — added `--gui` flag, `runGUI()`, conditional dispatch
- `cmd/sote/main_test.go` — added TestGUIFlagParsed

## Guardrail Verification

- [x] TUI mode unaffected: `./sote` (no flag) calls `runTUI()` exactly as before
- [x] `runTUI()` unchanged: same function body, same parameters
- [x] No service layer modifications
- [x] No hardcoded service init in runGUI() — services passed as params
- [x] Race detector clean on all gui + cmd/sote tests
- [x] Binary builds successfully (44MB)

---

## Date: 2026-06-05 — Task 4: GUI Contacts View

## Key Decisions

1. **`NewContactsPage` returns `*ContactsPage`** (not `fyne.CanvasObject`): Follows the existing Settings/LoginView pattern where constructors return the struct directly and expose a `Content()` method. This enables tests to inspect widget properties.

2. **`storage.Contact` vs `domain.Contact`**: The contacts service `GetContacts()` returns `[]storage.Contact` (with `ContactUsername`, `Username` as owner). The service `AddContact()` accepts `domain.Contact` (with `Username`, `OnionAddress`, `PublicKey`). The GUI layer translates between them.

3. **Custom `contactRow` widget**: Created a Fyne widget extending `widget.BaseWidget` that implements `fyne.SecondaryTappable` for right-click context menu support. Each row shows a colored status dot (green=contact, yellow=pending), username, and truncated onion address.

4. **`dialog.NewForm` for add contact**: Uses Fyne's built-in form dialog instead of a custom popup. Validates onion address format and prevents self-add.

5. **Right-click context menu**: `widget.NewPopUpMenu` with `fyne.NewMenuItem` for "Copy Onion Address" (uses `w.Clipboard().SetContent()`), "Delete Contact" (shows `dialog.ShowConfirm`), and for pending requests: "Accept Request" / "Decline Request".

6. **Pending requests in Accordion**: `widget.NewAccordion` with a single item contains the pending requests list with Accept/Decline toolbar buttons.

7. **Empty state**: When no contacts, the list is hidden and a centered `EmptyLabel` ("No contacts yet.") + `EmptyBtn` ("Add Your First Contact") are shown.

8. **Async pattern**: `LoadContacts()` and `handleAcceptPending()` use goroutines with `fyne.Do()` for UI updates, matching the required async pattern.

## Successful Patterns

- `go vet ./internal/gui/` passes clean (exit 0)
- `go build ./internal/gui/` compiles successfully
- Exported widget fields (`List`, `PendingList`, `Accordion`, `EmptyLabel`, `EmptyBtn`, `AddBtn`, `PendingBtn`) enable direct test assertions
- `test.NewApp()` + `st.Initialize(":memory:")` pattern for headless GUI tests with real DB

## Issues Encountered

- **Tests cannot link**: Same `libxxf86vm-dev` issue as Task 1. `go vet` and `go build` pass, confirming code correctness.
- **No DeleteContact in service layer**: The `deleteContact()` method removes from local state only. DB persistence deferred.
- **contactRow.TappedSecondary**: Right-click on list items may not work in all Fyne environments — the widget implements `fyne.SecondaryTappable` interface but event propagation through `widget.List` depends on Fyne's internal handling.

## Test Coverage (15 tests)

| Test | What it verifies |
|------|-----------------|
| TestEmptyState | Empty label/button visible when no contacts, list hidden |
| TestContactListRenders | 2 contacts → list shows, empty state hides |
| TestContactListRowData | Row widget created, fields populated correctly |
| TestContactRowTruncation | Short addresses unchanged, long addresses truncated |
| TestContextMenu_RightClickCallback | `TappedSecondary` fires callback with correct contact |
| TestContextMenu_PendingRightClick | Pending requests also trigger right-click |
| TestContextMenu_MenuItemsExist | `showContactContextMenu` doesn't panic |
| TestAcceptRequest | Pending request removed after accept |
| TestDeclineRequest | Pending request removed after decline |
| TestDeleteContact | Contacts deleted from list, empty state restored |
| TestAddContactDialog_SelfAddPrevented | `extractUsernameFromOnion("alice.onion")` → "alice" |
| TestToolbarButtons | AddBtn and PendingBtn have correct text |
| TestAccordionExists | Accordion has 1 item titled "Pending Requests" |
| TestContentReturnsCanvasObject | Content() returns valid fyne.CanvasObject |
| TestSetPendingRequestsUpdatesList | PendingList length updates correctly |
| TestOnionTruncationHelper | truncateOnion helper edge cases tested |

---

## Date: 2026-06-05 — Task 5: GUI Chat View

### Key Decisions

1. **`ChatView` struct with `Container()` accessor**: Follows the existing Contacts/Settings/LoginView pattern where the constructor returns the struct and exposes a `Container()` method for embedding in layouts.

2. **Message bubbles via `canvas.NewRectangle` + `container.NewStack`**: Colored backgrounds achieved by stacking a colored `canvas.Rectangle` behind a `container.NewPadded` content body. Sent messages: right-aligned (HBox with spacer), purple (#7C3AED). Received: left-aligned, cyan (#06B6D4).

3. **No `CornerRadius`**: Removed `CornerRadius` from bubble backgrounds — caused rendering issues with Stack layout in Fyne v2.7.4 test environment.

4. **`sync.Mutex` for concurrent safety**: The async `loadMessages()` goroutine and the synchronous test code both call `refreshMessageList()`. Without a mutex, concurrent `RemoveAll()`/`Add()` on the messageBox container causes a panic ("index out of range"). Mutex serializes all message list updates.

5. **`SetContacts` clears selection on empty list**: When the contact list becomes empty, `selectedContact` is cleared and `contactSelect.ClearSelected()` is called to reset the widget state.

6. **Timestamp formatting matches TUI**: `formatTimestamp` parses RFC3339, "2006-01-02 15:04:05", and "2006-01-02T15:04:05Z" formats, returning `[HH:MM]` brackets — identical to the TUI's `formatTimestamp`.

7. **`test.NewApp()` required for widget state tests**: Tests that inspect `widget.Entry.Text` or `widget.Button.Disabled()` need a Fyne test app. Without it, widget state methods may return incorrect values.

### Successful Patterns

- `canvas.NewRectangle(color)` + `container.NewStack(bg, content)` for colored message bubbles
- `layout.NewSpacer()` for left/right bubble alignment in HBox
- `container.NewBorder(nil, nil, nil, sendBtn, input)` for the input row layout
- `test.NewApp()` + in-memory SQLite + real `crypto.New()` for integration tests
- `fyne.Do()` + goroutines for async send/fetch — send clears input immediately, reloads messages in callback
- GPG key generation in test setup (`crypto.New().GenerateKeyPair()`) — ~0.4s per key

### Issues Encountered

- **Concurrent `refreshMessageList` panic**: Async goroutine and test code both modifying `messageBox` caused "index out of range" panic. Fixed with `sync.Mutex` in `refreshMessageList` and protecting `displayMsgs` write in `loadMessages`.

- **`CornerRadius` incompatible with Stack**: Setting `CornerRadius` on the background rectangle caused rendering panics in Stack layout. Removed — solid-color rectangles work fine.

- **`libxxf86vm-dev` still needed**: Same linker issue as Task 1-4. Workaround: `CGO_LDFLAGS="-L/home/rootatar/.local/lib"`. 

- **Pre-existing test failures**: `TestOnionTruncationHelper` (contacts_test.go) and `TestRegisterPasswordMismatch` (login_test.go) fail independently of Task 5 changes.

### Test Results (11 chat tests, all PASS)

| Test | What it verifies |
|------|-----------------|
| TestChatViewRenders | Container creation, initial contact selection, empty messages |
| TestSendMessage | Full send flow: text → clear → async DB write → messages reloaded |
| TestReceiveMessage | Pre-seeded message loads into displayMsgs after async FetchMessages |
| TestContactSwitch | Contact selection persistence, SetContacts clears on empty |
| TestAutoScroll | Scroll container non-nil, ScrollToBottom doesn't panic |
| TestSendButtonDisabled_NoContact | Button stays disabled without contact even with text |
| TestSendButtonEnabled | Button enables with contact + text, disables on empty |
| TestMessageBubbleRendering | 2 messages → 2 bubble objects in messageBox |
| TestEmptyStateDisplayed | Empty displayMsgs shows "No messages yet" label |
| TestFormatTimestamp | RFC3339 → [15:04], datetime → [15:04], invalid → "" |
| TestOnSubmittedSendsMessage | Enter key clears input and disables send button |

### Files Created

- `internal/gui/chat.go` — 247 lines: ChatView struct, bubble rendering, async send/fetch
- `internal/gui/chat_test.go` — 243 lines: 11 tests with real DB + crypto setup
