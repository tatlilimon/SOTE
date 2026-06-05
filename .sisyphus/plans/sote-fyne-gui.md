# SOTE Fyne GUI — Native Desktop Interface

## TL;DR

> **Quick Summary**: Add a native desktop GUI to SOTE using the Fyne toolkit. Zero changes to the service layer — auth, crypto, storage, tor, transport, contacts, and messages remain untouched. The existing Bubble Tea TUI stays as the default mode; `./sote --gui` launches the Fyne window.
>
> **Deliverables**:
> - `internal/gui/` package with 4 views: Login/Register, Contacts, Chat, Settings
> - `--gui` flag in `cmd/sote/main.go` — same binary, two interfaces
> - System tray icon with connection status + notifications on incoming messages
> - Right-click context menus, native file dialogs for future file sharing
> - Fyne unit tests with headless test driver + testify
> - Custom SOTE theme matching the existing TUI color scheme
>
> **Estimated Effort**: Medium (services are done — view layer only)
> **Parallel Execution**: YES — 3 waves, up to 4 parallel tasks in Wave 2
> **Critical Path**: Task 1 → Task 2 → Task 5 → Task 6 → Task 7 → Task 8 → F1-F4

---

## Context

### Original Request
Add a native GUI to SOTE using Fyne. The TUI remains the default. `./sote --gui` opens a desktop window. Same functionality as the TUI (login, contacts, chat, settings) plus enhancements: system tray, right-click menus, native dialogs.

### Interview Summary

**Key Discussions**:
- **Framework**: Fyne (fyne.io) — most mature Go GUI toolkit, 26K+ stars, native widgets, cross-platform
- **Mode Selection**: `./sote` = TUI (existing), `./sote --gui` = Fyne window (new)
- **Scope**: Enhanced GUI — system tray with notifications, right-click context menus, native dialogs, plus all TUI-equivalent views
- **Test Strategy**: TDD with Fyne test driver (`fyne.io/fyne/v2/test`) + `testify` assertions — headless GUI tests
- **Service Layer**: Completely unchanged — the GUI calls the same `AuthService`, `CryptoService`, `StorageService`, `TorService`, `TransportService`, `ContactService`, `MessageService` interfaces from `internal/`

**Research Findings**:
- **Fyne async**: `fyne.Do()` is the goroutine-safe UI update function — equivalent to Bubble Tea's `p.Send()`. Background Tor/HTTP listeners call `fyne.Do()` to update widgets.
- **Fyne widgets**: `widget.Entry` (text input, password mode), `widget.List`, `widget.Label`, `widget.Button`, `container.AppTabs` (tab navigation), `container.Border`, `container.Split` — all needed for our 4 views
- **System tray**: `desktop.App` interface with `SetSystemTrayMenu()`, `SetSystemTrayIcon()` — Fyne's desktop extension
- **Notifications**: `app.SendNotification(fyne.NewNotification(title, body))` — native desktop notifications
- **Fyne test driver**: `test.Tap(canvas, obj)`, `test.Type(obj, text)`, `test.NewApp()` — headless, no display needed
- **Dependencies**: `fyne.io/fyne/v2` (core), `github.com/fyne-io/desktop` (system tray)

### Guardrails Applied
- **G1**: Service layer MUST NOT be modified — GUI reads same packages, calls same interfaces
- **G2**: TUI MUST remain functional — `--gui` flag selects mode, default mode is TUI
- **G3**: All widget state updates MUST use `fyne.Do()` from goroutines — Fyne is single-threaded like Bubble Tea
- **G4**: GUI tests MUST use `test.NewApp()` — no real display required
- **G5**: Color scheme MUST match existing TUI styles (primary #7C3AED purple, secondary #06B6D4 cyan, etc.)

---

## Work Objectives

### Core Objective
Add a native Fyne desktop GUI to SOTE without modifying ANY existing service code. The GUI mirrors all TUI functionality plus adds system tray, notifications, and native interactions.

### Concrete Deliverables
- `internal/gui/app.go` — Fyne app setup, window management, tab container, `--gui` entry point
- `internal/gui/theme.go` — Custom Fyne theme matching SOTE color scheme
- `internal/gui/login.go` — Login/Register view with form validation
- `internal/gui/contacts.go` — Contact list, add contact dialog, right-click context menu
- `internal/gui/chat.go` — Chat view with message list, input, send button
- `internal/gui/settings.go` — Settings view with user info, password change, connection status
- `internal/gui/tray.go` — System tray with connection indicator + notifications
- `cmd/sote/main.go` — Added `--gui` flag + `runGUI()` function
- `internal/gui/*_test.go` — Fyne headless tests for all views

### Definition of Done
- [ ] `go build -o sote ./cmd/sote` succeeds with `fyne.io/fyne/v2` dependency
- [ ] `go test -race -count=1 ./internal/gui/` passes all GUI tests (headless)
- [ ] `./sote` launches TUI (unchanged)
- [ ] `./sote --gui` launches Fyne window with tabbed views
- [ ] `go test -race -count=1 ./...` passes — all existing tests unaffected

### Must Have
- 4 functional views: Login/Register, Contacts, Chat, Settings — matching TUI feature parity
- `--gui` flag in `cmd/sote/main.go`
- System tray icon with Tor connection status indicator
- Desktop notifications on incoming messages
- Right-click context menus (delete contact, copy onion address)
- Fyne test driver + testify for all view tests
- Custom SOTE theme (colors match TUI)

### Must NOT Have (Guardrails)
- NO modifications to `internal/auth/`, `internal/crypto/`, `internal/storage/`, `internal/tor/`, `internal/transport/`, `internal/contacts/`, `internal/messages/`, `internal/tui/`
- NO modifications to TUI behavior — `./sote` must work exactly as before
- NO web view, Electron, or browser-based rendering
- NO new database schema
- NO blocking the Fyne main loop — all long ops in goroutines with `fyne.Do()`

---

## Verification Strategy (MANDATORY)

### Test Decision
- **Infrastructure exists**: YES (testify already in go.mod)
- **Automated tests**: TDD with Fyne test driver + testify
- **Framework**: `fyne.io/fyne/v2/test` for widget interaction tests, `github.com/stretchr/testify` for assertions
- **Headless**: `test.NewApp()` creates an in-memory Fyne app — no display server needed, works in CI

### QA Policy
Every task MUST include agent-executed QA scenarios. Evidence saved to `.sisyphus/evidence/gui-task-{N}-{scenario-slug}.{ext}`.

- **GUI tests**: `test.Tap()`, `test.Type()`, `test.NewApp()` — headless widget verification
- **Integration**: `go build` with `--gui` flag, run on actual display (manual QA step)
- **Regression**: `go test -race -count=1 ./...` passes for all existing packages

---

## Execution Strategy

### Parallel Execution Waves

```
Wave 1 (Start Immediately — Fyne foundation):
├── Task 1: Fyne app scaffold + --gui flag in main.go [quick]
└── Task 2: Custom SOTE theme [quick]

Wave 2 (After Wave 1 — views, MAX PARALLEL):
├── Task 3: Login/Register view [visual-engineering]
├── Task 4: Contacts view [visual-engineering]
├── Task 5: Chat view [visual-engineering]
└── Task 6: Settings view [visual-engineering]

Wave 3 (After Wave 2 — system integrations):
├── Task 7: System tray + desktop notifications [deep]
└── Task 8: GUI wire-up + integration tests [deep]

Wave FINAL (After ALL tasks — parallel review agents):
├── G1: Plan Compliance Audit (oracle)
├── G2: Code Quality Review (unspecified-high)
├── G3: Real Manual QA (unspecified-high)
└── G4: Scope Fidelity Check (deep)
→ Present consolidated results → wait for explicit user okay
```

**Critical Path**: Task 1 → Task 5 → Task 7 → Task 8 → G1-G4
**Parallel Speedup**: ~50% faster than sequential (Wave 2 — all 4 views in parallel)
**Max Concurrent**: 4 (Wave 2)

### Agent Dispatch Summary

- **Wave 1**: **2** — T1,T2 → `quick` (scaffold, theme)
- **Wave 2**: **4** — T3-T6 → `visual-engineering` (all views with Fyne widgets)
- **Wave 3**: **2** — T7 → `deep` (system tray/notifications), T8 → `deep` (integration)
- **FINAL**: **4** — G1 → `oracle`, G2 → `unspecified-high`, G3 → `unspecified-high`, G4 → `deep`

---

## TODOs

- [x] 1. **Fyne App Scaffold + --gui Flag**

  **What to do**:
  - Add `fyne.io/fyne/v2` dependency to `go.mod` (`go get fyne.io/fyne/v2`)
  - Add `--gui` flag to `cmd/sote/main.go` — when set, calls `runGUI()` instead of `runTUI()`
  - Create `internal/gui/app.go` — `Run(cfg, db, authSvc, cryptoSvc, contactSvc, msgSvc, torSvc, srv, client)` function that creates Fyne app, sets title "SOTE — Decentralized Messenger", sets window size (1024×768 minimum 800×600), creates tab container
  - Create `internal/gui/app_test.go` — `TestAppStarts`, `TestWindowTitle`, `TestGUIFlag` using `test.NewApp()`
  - Verify `go build -o sote ./cmd/sote` compiles with Fyne
  - Verify `./sote --gui` opens a window (even if empty)

  **Must NOT do**:
  - Do NOT modify TUI mode — `./sote` (no flag) must still launch TUI
  - Do NOT put view logic in app.go — only window creation and tab container
  - Do NOT use CGo if avoidable (Fyne uses CGo for graphics drivers — this is expected and acceptable)

  **TDD**: Write `TestAppStarts` and `TestGUIFlag` before implementing

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Dependency addition, flag parsing, window scaffold — straightforward

  **Parallelization**:
  - **Can Run In Parallel**: NO (first task)
  - **Parallel Group**: Wave 1
  - **Blocks**: Tasks 2-8
  - **Blocked By**: None

  **References**:
  - `cmd/sote/main.go:86` — Current `runTUI()` call to branch from
  - `internal/tui/model.go` — Model struct to mirror for GUI services injection
  - `fyne.io/fyne/v2` docs: `https://docs.fyne.io/` — App, Window, Container API

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/gui/app_test.go` — `TestAppStarts`: `test.NewApp()` creates app, window exists
  - [ ] `internal/gui/app_test.go` — `TestWindowTitle`: window title is "SOTE — Decentralized Messenger"
  - [ ] `internal/gui/app_test.go` — `TestGUIFlag`: `--gui` flag correctly routes to GUI path
  - [ ] `cmd/sote/main_test.go` — `TestGUIFlagParsed`: verify `--gui` flag is parsed

  **QA Scenarios**:

  ```
  Scenario: GUI binary compiles and runs
    Tool: Bash
    Steps:
      1. go build -o sote ./cmd/sote
      2. ./sote --gui & (background, kill after 3s)
    Expected Result: Window opens with title "SOTE — Decentralized Messenger", size ≥ 800×600
    Failure Indicators: Compilation error, crash on startup, no window, wrong title
    Evidence: .sisyphus/evidence/gui-task-1-build.txt

  Scenario: TUI still works (no regression)
    Tool: Bash
    Steps:
      1. ./sote 2>&1 | head -5
    Expected Result: Shows TUI startup (SOTE - Decentralized Messenger, menu)
    Failure Indicators: TUI fails to start, unexpected GUI mode without --gui flag
    Evidence: .sisyphus/evidence/gui-task-1-tui-regression.txt
  ```

  **Commit**: YES
  - Message: `scaffold(gui): Fyne app window, --gui flag, tab container`
  - Files: `internal/gui/app.go`, `cmd/sote/main.go`, `go.mod`, `go.sum`

- [ ] 2. **Custom SOTE Theme**

  **What to do**:
  - Create `internal/gui/theme.go` — implement `fyne.Theme` interface
  - Colors: primary (#7C3AED purple), secondary (#06B6D4 cyan), success (#10B981 green), error (#EF4444 red), warning (#F59E0B amber), background (#1A1B26 dark), text (#A9B1D6 light), muted (#565F89 dim)
  - Override: `Color(fyne.ThemeColorName, fyne.ThemeVariant)`, `Font(fyne.TextStyle)`, `Size(fyne.ThemeSizeName)`, `Icon(fyne.ThemeIconName)`
  - Custom button styling matching TUI ButtonStyle
  - Custom input styling matching TUI InputStyle (focused border = cyan, blurred = dim)
  - Create `internal/gui/theme_test.go` — `TestThemeColors`, `TestThemeFonts`, `TestThemeSizes`, `TestCustomIcons`

  **Must NOT do**:
  - Do NOT hardcode color values in individual view files — use theme
  - G5: Colors must match TUI styles.go scheme

  **TDD**: Write `TestThemeColors` before implementing Theme interface

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Theme implementation — well-defined Fyne interface, color constants already defined in TUI

  **Parallelization**:
  - **Can Run In Parallel**: NO (after Task 1)
  - **Parallel Group**: Wave 1
  - **Blocks**: Tasks 3-8 (all views use theme)
  - **Blocked By**: Task 1

  **References**:
  - `internal/tui/styles.go` — Existing TUI color scheme to match
  - `fyne.io/fyne/v2/theme` — Theme interface, ThemeColorName constants

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/gui/theme_test.go` — `TestThemeColors`: all custom colors return correct hex values
  - [ ] `internal/gui/theme_test.go` — `TestThemeFonts`: custom fonts applied
  - [ ] `internal/gui/theme_test.go` — `TestThemeSizes`: custom sizing applied
  - [ ] `internal/gui/theme_test.go` — `TestThemeMatchesTUI`: primary color resolves to #7C3AED

  **QA Scenarios**:

  ```
  Scenario: Theme colors match TUI scheme
    Tool: Bash
    Steps: go test -run TestThemeMatchesTUI -v ./internal/gui/
    Expected Result: All color assertions pass — primary=#7C3AED, secondary=#06B6D4, etc.
    Failure Indicators: Color mismatch, theme not applied
    Evidence: .sisyphus/evidence/gui-task-2-theme.txt
  ```

  **Commit**: YES
  - Message: `feat(gui): custom Fyne theme matching SOTE color scheme`
  - Files: `internal/gui/theme.go`, `internal/gui/theme_test.go`

- [ ] 3. **Login/Register View**

  **What to do**:
  - Create `internal/gui/login.go` — login/register form with inline validation
  - Layout: title, username `Entry`, password `Entry` (Password=true), login `Button`, register link
  - Register: adds confirm password field and register button
  - Inline error labels (red, hidden until validation fails)
  - Loading: disable buttons + `ProgressBarInfinite` during auth
  - Async: auth in goroutine, UI updates via `fyne.Do()`
  - On success: callback to switch to main tab container

  **Must NOT do**: Do NOT echo password (Entry.Password=true), Do NOT block UI during auth

  **TDD**: TestLoginFormRenders, TestLoginValidation, TestLoginSuccess, TestRegisterPasswordMismatch

  **Recommended Agent Profile**: `visual-engineering` with `frontend-ui-ux`

  **Parallelization**: YES — Wave 2 with Tasks 4-6. Blocks Task 8. Blocked by Tasks 1,2.

  **References**: `internal/tui/login.go`, `internal/auth/auth.go`, `fyne.io/fyne/v2/widget`, `fyne.io/fyne/v2/dialog`

  **TDD Tests**:
  - [ ] `internal/gui/login_test.go` — `TestLoginFormRenders`: form has username, password, login button
  - [ ] `internal/gui/login_test.go` — `TestLoginSuccess`: valid input → callback fires
  - [ ] `internal/gui/login_test.go` — `TestRegisterPasswordMismatch`: different → error label
  - [ ] `internal/gui/login_test.go` — `TestSwitchToRegister`: register link switches view

  **QA**:
  ```
  Scenario: Full login flow
    Steps: test.Tap(username) → test.Type("alice") → test.Tap(password) → test.Type("secret123") → test.Tap(loginButton)
    Expected: Callback called with correct credentials
    Evidence: .sisyphus/evidence/gui-task-3-login.txt
  ```

  **Commit**: YES — `feat(gui): login/register form with validation` — internal/gui/login.go, login_test.go

- [ ] 4. **Contacts View**

  **What to do**:
  - Create `internal/gui/contacts.go` — contact list with add dialog, context menu, pending requests
  - List: `widget.NewList` with username, truncated onion, colored status dot per row
  - Add Contact: popup dialog with onion address entry + "Add" button
  - Right-click context: "Copy Onion Address" (clipboard), "Delete Contact" (confirm dialog)
  - Pending requests: accordion section with Accept/Decline buttons
  - Empty state: "No contacts yet. Click Add to start."
  - Async: `fyne.Do()` for UI updates from goroutines

  **Must NOT do**: Do NOT block UI, Do NOT allow self-add

  **TDD**: TestContactListRenders, TestAddContactDialog, TestContextMenu, TestAcceptRequest, TestEmptyState

  **Recommended Agent Profile**: `visual-engineering` with `frontend-ui-ux`

  **Parallelization**: YES — Wave 2. Blocks Task 8. Blocked by Tasks 1,2.

  **References**: `internal/tui/contacts.go`, `internal/contacts/contacts.go`, `fyne.io/fyne/v2/widget`, `fyne.io/fyne/v2/dialog`, `fyne.io/fyne/v2/clipboard`

  **Commit**: YES — `feat(gui): contacts view with list, add dialog, context menu` — internal/gui/contacts.go, contacts_test.go

- [ ] 5. **Chat View**

  **What to do**:
  - Create `internal/gui/chat.go` — message list + input + contact selector
  - Top: `widget.NewSelect` for contact switching
  - Center: scrollable message list — sent (right, purple bg), received (left, cyan bg), each with timestamp
  - Bottom: `widget.NewEntry` with `OnSubmitted` for Enter-to-send
  - Send button disabled when no contact or empty input
  - Auto-scroll to bottom on new message
  - Async: send/fetch in goroutines, `fyne.Do()` for UI updates

  **Must NOT do**: Do NOT block UI, Do NOT log message bodies

  **TDD**: TestChatViewRenders, TestSendMessage, TestReceiveMessage, TestContactSwitch, TestAutoScroll

  **Recommended Agent Profile**: `visual-engineering` with `frontend-ui-ux`

  **Parallelization**: YES — Wave 2. Blocks Task 8. Blocked by Tasks 1,2.

  **References**: `internal/tui/chat.go`, `internal/messages/messages.go`, `fyne.io/fyne/v2/widget`, `fyne.io/fyne/v2/container`

  **Commit**: YES — `feat(gui): chat view with message bubbles and contact switching` — internal/gui/chat.go, chat_test.go

- [ ] 6. **Settings View**

  **What to do**:
  - Create `internal/gui/settings.go` — settings with accordion sections
  - Account: username label, onion address (selectable), copy button
  - Password: current/new/confirm entries + "Change Password" button. Validate ≥8 chars, match, differs from current. Re-encrypt key on success.
  - Connection: Tor status indicator, SOCKS port, uptime
  - About: version "SOTE_Alpha_v1.0", GPLv3, Fyne credit
  - Password fields: Entry.Password = true

  **Must NOT do**: Do NOT display private key, Do NOT log passwords

  **TDD**: TestSettingsViewRenders, TestChangePassword, TestNoPrivateKeyLeak, TestConnectionStatus

  **Recommended Agent Profile**: `visual-engineering` with `frontend-ui-ux`

  **Parallelization**: YES — Wave 2. Blocks Task 8. Blocked by Tasks 1,2.

  **References**: `internal/tui/settings.go`, `internal/auth/auth.go`, `fyne.io/fyne/v2/widget/Accordion`

  **Commit**: YES — `feat(gui): settings view with password change` — internal/gui/settings.go, settings_test.go

- [ ] 7. **System Tray + Desktop Notifications**

  **What to do**:
  - Create `internal/gui/tray.go` — system tray integration
  - Add `github.com/fyne-io/desktop` dependency
  - Check if app supports desktop features: `desk, ok := app.(desktop.App)` — if not available, skip gracefully (no crash)
  - System tray menu: "Show SOTE" (restore window), "Connection: Connected/Disconnected" (status, non-clickable), "Quit" (graceful shutdown)
  - Tray icon: use SOTE logo or colored dot (green=connected, red=disconnected)
  - Desktop notifications: `app.SendNotification(fyne.NewNotification("SOTE", "New message from " + sender))` on incoming Tor messages
  - Wire notifications into the message receive handler in `internal/transport/server.go` via callback

  **Must NOT do**:
  - Do NOT crash if desktop features unavailable (headless/test environments)
  - G1: Do NOT modify service layer — add notification callback interface if needed

  **TDD**: `TestTrayMenuItems`, `TestTrayRestoreWindow`, `TestNotificationSent`, `TestTrayWithoutDesktop` (graceful fallback)

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: System integration — desktop app features, notification API, graceful fallback

  **Parallelization**:
  - **Can Run In Parallel**: NO (after Waves 1-2)
  - **Parallel Group**: Wave 3
  - **Blocks**: Task 8
  - **Blocked By**: Tasks 1-6

  **References**:
  - `fyne.io/fyne/v2` — App.SendNotification
  - `github.com/fyne-io/desktop` — desktop.App interface, SetSystemTrayMenu

  **Commit**: YES — `feat(gui): system tray with connection status and desktop notifications` — internal/gui/tray.go, tray_test.go

- [ ] 8. **GUI Wire-Up + Integration Tests**

  **What to do**:
  - Wire all 4 views into app.go's tab container
  - Implement `runGUI()` in `cmd/sote/main.go`: init services (same as TUI), create Fyne app, show window, run
  - Wire incoming Tor messages: `p.Send()` equivalent via callback → `fyne.Do()` → update chat/notification
  - Tab switching: login on start, after login switch to main tabs (Chat, Contacts, Settings)
  - Graceful shutdown on window close: stop Tor, checkpoint WAL, close DB
  - Create `internal/gui/integration_test.go` with end-to-end test: login → add contact → send message → receive → verify in UI
  - Verify `./sote --gui` launches complete working app

  **Must NOT do**:
  - G1: Do NOT modify service layer code
  - G2: Do NOT break TUI mode
  - Do NOT create new global variables

  **TDD**: `TestFullLoginFlow`, `TestTabSwitching`, `TestIncomingMessageNotification`

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: Complex integration across all views and services

  **Parallelization**:
  - **Can Run In Parallel**: NO (depends on ALL previous)
  - **Parallel Group**: Wave 3
  - **Blocks**: Final Verification
  - **Blocked By**: Tasks 1-7

  **Commit**: YES — `feat(gui): wire all views, runGUI entry, integration tests` — internal/gui/app.go, cmd/sote/main.go, internal/gui/integration_test.go

---

## Final Verification Wave

> 4 review agents run in PARALLEL. ALL must APPROVE. Present consolidated results and get explicit user "okay".

- [ ] G1. **Plan Compliance Audit** — `oracle`
  Verify: all 4 views exist, --gui flag works, system tray present, theme matches TUI colors, service layer UNMODIFIED, TUI regression-free. Output: `Must Have [N/N] | Must NOT Have [N/N] | Views [4/4] | VERDICT`

- [ ] G2. **Code Quality Review** — `unspecified-high`
  Run `go vet ./...`, `go test -race ./...`, `go build`. Check: no unused imports, no CGo where avoidable, no Fyne UI leaks. Output: `Build [PASS/FAIL] | Vet [PASS/FAIL] | Tests [N pass/N fail] | VERDICT`

- [ ] G3. **Real Manual QA** — `unspecified-high`
  Build and run `./sote --gui` on an actual display. Verify: window opens, login works, contacts view works, chat sends/receives, settings change password, system tray appears, notifications fire. Output: `Views [N/N functional] | Tray [OK/FAIL] | Notifications [OK/FAIL] | TUI Regression [OK/FAIL] | VERDICT`

- [ ] G4. **Scope Fidelity Check** — `deep`
  Verify: zero changes to `internal/{auth,crypto,storage,tor,transport,contacts,messages,tui}/`. Only `internal/gui/` + `cmd/sote/main.go` + `go.mod` changed. Output: `Service Layer [UNCHANGED] | New Files [N] | Contamination [CLEAN/N issues] | VERDICT`

---

## Commit Strategy

- **Wave 1**: Commit after EACH task (Tasks 1-2) — foundational
- **Wave 2**: Commit after EACH task (Tasks 3-6) — each view independently testable
- **Wave 3**: Commit after EACH task (Tasks 7-8) — integration
- **Final**: Commit after ALL G1-G4 approval

## Success Criteria

### Verification Commands
```bash
go build -o sote ./cmd/sote                        # Must succeed
go test -race -count=1 ./internal/gui/              # All GUI tests pass
go test -race -count=1 ./...                        # All existing tests unaffected
./sote                                               # TUI launches (unchanged)
./sote --gui                                         # GUI window opens
grep -rn "fyne\.Do" internal/gui/ | wc -l           # > 0 (async updates used)
grep -rn "fyne.io/fyne/v2" internal/auth/ internal/crypto/ internal/storage/ internal/tor/ internal/transport/ internal/contacts/ internal/messages/ internal/tui/
                                                      # MUST be ZERO (service layer unchanged)
```

### Final Checklist
- [ ] All 8 tasks completed with passing tests
- [ ] All 4 views functional: Login, Contacts, Chat, Settings
- [ ] System tray works (graceful fallback if unavailable)
- [ ] Desktop notifications fire on incoming messages
- [ ] Service layer completely unchanged
- [ ] TUI mode completely unaffected
- [ ] All 4 Final Verification agents (G1-G4) return APPROVE
