# SOTE Rewrite: Single Binary with Bubble Tea TUI

## TL;DR

> **Quick Summary**: Complete rewrite of the SOTE decentralized messaging app into a single Go binary (`sote`) with a polished Bubble Tea TUI and optional daemon mode. Fixes all security flaws, process management bugs, and architectural anti-patterns from the original proof-of-concept while preserving the core concepts: GPG end-to-end encryption, Tor hidden services for P2P, and SQLite local storage.
>
> **Deliverables**:
> - Single binary `sote` (TUI mode default, `--daemon` for headless)
> - Bubble Tea TUI with 5 views: Login/Register, Chat, Contacts, Settings, Main Menu
> - Fixed security: bcrypt/argon2 passwords, TLS cert pinning, PBKDF2 key derivation
> - Fixed architecture: proper Tor lifecycle via cretz/bine, WAL mode SQLite, graceful shutdown, structured logging
> - Full test suite (TDD): unit + integration + TUI tests with `go test -race`
> - Configuration via `~/.sote/config.yaml`
>
> **Estimated Effort**: Large
> **Parallel Execution**: YES — 5 waves, up to 7 parallel tasks in Wave 4
> **Critical Path**: Task 1 → Task 5 → Task 6 → Task 11 → Task 12 → Task 20 → Task 23 → F1-F4

---

## Context

### Original Request
Merge the current two-binary architecture (sote-node HTTP daemon + sote-client CLI) into a single binary with a fancy Bubble Tea TUI. Fix all security and architecture issues identified in the codebase review. Keep existing DB schema and crypto logic (GPG, AES-256). No backward compatibility needed — fresh start.

### Interview Summary

**Key Discussions**:
- **TUI Framework**: Bubble Tea (charmbracelet) + Lipgloss v2 + Bubbles v2 — the Go ecosystem standard
- **Operation Mode**: Single binary, `--daemon` flag for headless mode (uses `tea.WithoutRenderer()`)
- **P2P Transport**: Keep HTTPS over Tor hidden services, but with proper TLS cert pinning (no `InsecureSkipVerify`)
- **Test Strategy**: TDD workflow with `go test -race -count=1 ./...` + testify assertions
- **Scope**: Keep DB schema (encrypted keys stored in SQLite) + crypto logic (GPG + AES-256-GCM). Rewrite everything else.
- **Multi-user**: Single-user only (one account, one Tor hidden service at a time)
- **Backward Compat**: None needed — fresh start, no migration code
- **Docker**: Out of scope — native binary only
- **TUI Quality**: Polished & usable — Lipgloss-styled components, keyboard navigation, color scheme, responsive layout. No animations.

**Research Findings**:
- **Bubble Tea Concurrency**: `p.Send()` from background goroutines is the primary pattern for Tor message dispatch into the TUI event loop. Update() is single-threaded — no mutexes needed in model.
- **TUI + Daemon**: `tui-daemon-combo` example provides the exact template — `tea.WithoutRenderer()` + `term.IsTerminal()` check for daemon mode.
- **HTTP Integration**: Start HTTP server in goroutine before `p.Run()`, forward incoming requests as messages via `p.Send()`.
- **Project Layout**: `cmd/sote/main.go` (thin wire-up) + `internal/` (all logic) per Go conventions.
- **SQLite**: `modernc.org/sqlite` (pure Go, no CGo) with WAL mode + `_txlock=immediate` for concurrent safety.

### Metis Review

**Identified Gaps** (resolved with user):
- **Multi-account vs Single-user**: Confirmed single-user only
- **Backward compatibility**: Confirmed no migration needed — fresh start
- **DB schema changes**: Confirmed keep current schema (encrypted keys in DB)
- **Docker**: Out of scope
- **HTTPS vs HTTP over Tor**: Confirmed keep HTTPS with cert pinning
- **TUI scope**: Confirmed polished Lipgloss components, no animations
- **Config approach**: `~/.sote/config.yaml` with env var overrides

**Guardrails Applied**:
- S1–S6: Non-negotiable security rules (no InsecureSkipVerify, bcrypt/argon2 only, PBKDF2 for AES, no log.Fatal, structured logging)
- A1–A5: Architecture rules (cretz/bine for Tor, WAL mode, graceful shutdown, cmd/internal layout, non-blocking Update)
- T1–T3: TUI rules (80×24 minimum, terminal restore on exit, state-flag view switching)

**Scope Creep Locked Down**:
- NO: animations, Docker, Tor bridges, file transfer, group chat, delivery receipts, QR codes, data shredding, multi-account

---

## Work Objectives

### Core Objective
Rewrite SOTE as a single Go binary with a polished Bubble Tea TUI, eliminating the client-server split while preserving GPG encryption and SQLite storage. Fix all 15+ security and architecture issues from the original proof-of-concept.

### Concrete Deliverables
- `cmd/sote/main.go` — single binary entry point with `--daemon` flag
- `internal/domain/` — User, Contact, Message types + interfaces
- `internal/auth/` — bcrypt/argon2 password hashing, PBKDF2 key derivation, login/register
- `internal/crypto/` — GPG encrypt/decrypt (migrated from user.go), AES-256-GCM
- `internal/storage/` — SQLite with WAL mode, migrations, CRUD operations
- `internal/tor/` — cretz/bine Tor lifecycle, hidden service management
- `internal/transport/` — HTTPS server (TLS pinning), Tor SOCKS HTTP client
- `internal/tui/` — Bubble Tea model, 5 views, Lipgloss theme, keyboard navigation
- `internal/config/` — YAML config loading, env var overrides, validation
- `~/.sote/config.yaml` — default config written on first run

### Definition of Done
- [ ] `go build -o sote ./cmd/sote` succeeds with zero warnings
- [ ] `go test -race -count=1 ./...` passes all tests (0 failures, 0 race conditions)
- [ ] `grep -r "InsecureSkipVerify" internal/ cmd/` returns zero results
- [ ] `grep -r "sha256.Sum256" internal/ | grep -v _test.go | grep -v migrate` returns zero results
- [ ] `grep -r "log.Fatal" internal/` returns zero results
- [ ] `./sote --daemon` starts headless, accepts HTTP requests, clean shutdown on SIGTERM
- [ ] TUI renders correctly at 80×24, 120×40, handles resize without crash
- [ ] Integration test: two instances, full message roundtrip over Tor (or mock Tor)

### Must Have
- bcrypt (cost ≥ 12) or argon2id for password hashing
- PBKDF2-HMAC-SHA256 (≥ 100k iterations) for AES key derivation
- TLS certificate fingerprint pinning — NO InsecureSkipVerify
- cretz/bine for Tor lifecycle (StopProcessOnClose, defer Close, context cancellation)
- SQLite WAL mode with busy_timeout and _txlock=immediate
- signal.NotifyContext for graceful shutdown (drain HTTP, close Tor, checkpoint WAL)
- log/slog structured logging — NO bare fmt.Println in production code
- Bubble Tea TUI with keyboard navigation, Ctrl+C/Ctrl+D/q exit, terminal restore

### Must NOT Have (Guardrails)
- NO `InsecureSkipVerify` anywhere in production code
- NO SHA-256 for password hashing (bcrypt/argon2id only)
- NO single SHA-256 for AES key derivation (PBKDF2 only)
- NO `log.Fatal()` in library code (return errors)
- NO bare `fmt.Println` debug prints in production (use slog)
- NO `exec.Command("tor")` (use cretz/bine)
- NO blocking operations in Bubble Tea Update() loop
- NO Docker support (out of scope)
- NO backward compatibility / migration code
- NO animations, custom themes, QR codes, file transfer, group chat, multi-account, bridges, data shredding

---

## Verification Strategy (MANDATORY)

> **ZERO HUMAN INTERVENTION** — ALL verification is agent-executed. No exceptions.

### Test Decision
- **Infrastructure exists**: NO (will be set up in Task 1)
- **Automated tests**: TDD (RED → GREEN → REFACTOR)
- **Framework**: Go standard `testing` + `github.com/stretchr/testify` (assertions)
- **Race detector**: `-race` flag on ALL test runs

### QA Policy
Every task MUST include agent-executed QA scenarios. Evidence saved to `.sisyphus/evidence/task-{N}-{scenario-slug}.{ext}`.

- **TUI tests**: `tea.WithInput()` + `tea.WithOutput(io.Discard)` + assertions on model state
- **HTTP handler tests**: `httptest.NewServer()` + curl assertions
- **DB tests**: In-memory SQLite, verify migrations, WAL mode, CRUD correctness
- **Crypto tests**: Roundtrip: encrypt → decrypt == original, wrong password → error
- **Integration tests**: Two full app instances, message roundtrip, contact request flow

---

## Execution Strategy

### Parallel Execution Waves

```
Wave 1 (Start Immediately — foundation + scaffolding):
├── Task 1: Project scaffolding + test infra [quick]
├── Task 2: Configuration system [quick]
├── Task 3: Structured logging setup [quick]
├── Task 4: Database layer [quick]
└── Task 5: Domain types + interfaces [quick]

Wave 2 (After Wave 1 — core services, MAX PARALLEL):
├── Task 6: Crypto service [quick]
├── Task 7: Auth service [quick]
├── Task 8: Tor service [deep]
├── Task 9: Contact service [quick]
└── Task 10: Message service [quick]

Wave 3 (After Waves 1-2 — transport layer):
├── Task 11: TLS certificate management [quick]
├── Task 12: HTTP server + handlers [deep]
└── Task 13: HTTP client (Tor SOCKS proxy) [quick]

Wave 4 (After Wave 1 — TUI views, MAX PARALLEL):
├── Task 14: TUI theme + styles [visual-engineering]
├── Task 15: Login/Register views [visual-engineering]
├── Task 16: Main menu + navigation [visual-engineering]
├── Task 17: Contacts view [visual-engineering]
├── Task 18: Chat view [visual-engineering]
└── Task 19: Settings view [visual-engineering]

Wave 5 (After Waves 2+3+4 — integration):
├── Task 20: TUI + services wire-up [deep]
├── Task 21: Daemon mode [quick]
├── Task 22: Graceful shutdown [deep]
└── Task 23: Integration tests [deep]

Wave FINAL (After ALL tasks — parallel review agents):
├── F1: Plan Compliance Audit (oracle)
├── F2: Code Quality Review (unspecified-high)
├── F3: Real Manual QA (unspecified-high + playwright)
└── F4: Scope Fidelity Check (deep)
→ Present consolidated results → wait for explicit user okay
```

**Critical Path**: Task 1 → Task 5 → Task 6 → Task 11 → Task 12 → Task 20 → Task 23 → F1-F4
**Parallel Speedup**: ~65% faster than sequential (Waves 1, 2, and 4 are highly parallel)
**Max Concurrent**: 6 (Wave 4 — all TUI views)

### Agent Dispatch Summary

- **Wave 1**: **5** — T1-T5 → `quick` (boilerplate, config, simple setup)
- **Wave 2**: **5** — T6-T10 → `quick` (crypto, auth, contacts, messages), T8 → `deep` (Tor lifecycle)
- **Wave 3**: **3** — T11 → `quick`, T12 → `deep`, T13 → `quick`
- **Wave 4**: **6** — T14-T19 → `visual-engineering` (all TUI views with Lipgloss)
- **Wave 5**: **4** — T20 → `deep`, T21 → `quick`, T22 → `deep`, T23 → `deep`
- **FINAL**: **4** — F1 → `oracle`, F2 → `unspecified-high`, F3 → `unspecified-high`, F4 → `deep`

---

## TODOs

- [x] 1. **Project Scaffolding + Test Infrastructure**

  **What to do**:
  - Create `cmd/sote/main.go` — thin entry point that parses `--daemon` flag, loads config, initializes logger, starts TUI or daemon
  - Create `internal/` directory structure: `domain/`, `auth/`, `crypto/`, `storage/`, `tor/`, `transport/`, `tui/`, `config/`
  - Update `go.mod`: rename module to `sote`, bump Go to 1.22 (for `net/http` pattern routing), add dependencies: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `charm.land/bubbles/v2`, `modernc.org/sqlite`, `golang.org/x/crypto/bcrypt`, `github.com/stretchr/testify`, `gopkg.in/yaml.v3`
  - Delete old files: `node/main.go`, `client/main.go`, `tor/tor.go`, `sote-node` binary, `unneccesary/` directory, `curls/` directory (clean slate)
  - Copy `user/user.go` crypto functions into `internal/crypto/` and `db/db.go` schema into `internal/storage/` as reference
  - Create `.sisyphus/evidence/` directory
  - Write `go test ./...` that passes (empty, proves test infra works)
  - Run `go mod tidy` to clean up go.sum

  **Must NOT do**:
  - Do NOT implement any business logic — just scaffolding and empty packages
  - Do NOT delete `user/user.go` or `db/db.go` yet (reference for Tasks 4-10)
  - Do NOT add CGo dependencies (use `modernc.org/sqlite` not `mattn/go-sqlite3`)

  **TDD**: Write `cmd/sote/main_test.go` with `TestMainParsesFlags` before implementing main.go

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Scaffolding and boilerplate — single straightforward task
  - **Skills**: [`git-master`]
    - `git-master`: For proper atomic commits of the new project structure

  **Parallelization**:
  - **Can Run In Parallel**: NO (first task, everything depends on it)
  - **Parallel Group**: Wave 1 (sequential within wave)
  - **Blocks**: Tasks 2, 3, 4, 5, 14, 15, 16, 17, 18, 19
  - **Blocked By**: None

  **References**:
  - `go.mod:1-12` — Current module name and dependencies to migrate from
  - `db/db.go:25-67` — Current SQLite schema (3 tables) to preserve as reference
  - `user/user.go:16-31` — Current User and Contact structs to preserve as reference
  - `user/user.go:81-98` — Current AES-256-GCM encrypt (migrate to new package)
  - `user/user.go:116-132` — Current GPG encrypt (migrate to new package)
  - `user/user.go:134-168` — Current GPG decrypt (migrate to new package)
  - `Dockerfile:1-19` — Reference for build commands and exposed ports
  - Bubble Tea v2 docs: `https://charm.land/bubbletea/v2` — Import path and API reference
  - Lipgloss v2 docs: `https://charm.land/lipgloss/v2` — Styling API reference

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `cmd/sote/main_test.go` — `TestMainParsesFlags`: verifies `--daemon` flag is parsed correctly
  - [ ] `cmd/sote/main_test.go` — `TestMainHelpFlag`: verifies `--help` outputs usage

  **QA Scenarios**:

  ```
  Scenario: Project builds from scratch
    Tool: Bash
    Preconditions: Clean working directory, all old .go files deleted per task instructions
    Steps:
      1. Run: go build -o sote ./cmd/sote
      2. Run: ./sote --help
    Expected Result: Binary compiles with zero errors. --help shows usage with --daemon flag.
    Failure Indicators: Compilation errors, missing imports, undefined symbols
    Evidence: .sisyphus/evidence/task-1-build.txt

  Scenario: Test infrastructure works
    Tool: Bash
    Preconditions: Project scaffolding complete
    Steps:
      1. Run: go test -race -count=1 ./...
    Expected Result: All tests pass (including the placeholder and flag tests), zero race conditions
    Failure Indicators: Test failures, race detector warnings, "no test files" for any package
    Evidence: .sisyphus/evidence/task-1-test.txt
  ```

  **Commit**: YES
  - Message: `scaffold: project structure with cmd/internal layout and test infra`
  - Files: `cmd/sote/main.go`, `internal/*/`, `go.mod`, `go.sum`

- [ ] 2. **Configuration System**

  **What to do**:
  - Create `internal/config/config.go` with `Config` struct containing all settings
  - Support YAML config file at `~/.sote/config.yaml` with defaults for all fields
  - Support env var overrides (e.g., `SOTE_DB_PATH`, `SOTE_TOR_SOCKS_PORT`, `SOTE_LISTEN_PORT`)
  - Default values: DB path `~/.sote/data.db`, listen port 18080, Tor SOCKS port 9060, Tor control port 9061, log level "info"
  - `LoadConfig(path string) (*Config, error)` — loads from file, applies env overrides, validates
  - `SaveDefaultConfig(path string) error` — writes default config file on first run
  - Write config to `~/.sote/config.yaml` on first run if it doesn't exist
  - Validation: port ranges (1024-65535), log level must be one of debug/info/warn/error, paths must be writable

  **Must NOT do**:
  - Do NOT hardcode paths — use `os.UserHomeDir()` for `~/.sote/`
  - Do NOT log secrets (the config struct should have `yaml:"-" json:"-"` on sensitive fields)

  **TDD**: Write `TestLoadConfigDefaults`, `TestLoadConfigFromFile`, `TestEnvVarOverrides`, `TestConfigValidation` before implementing

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Straightforward config loading with well-known patterns

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Task 3, after Task 1)
  - **Parallel Group**: Wave 1
  - **Blocks**: Tasks 4, 6, 7, 8, 10, 12, 20
  - **Blocked By**: Task 1

  **References**:
  - `tor/tor.go:33-39` — Current hardcoded ports to move into config
  - `node/main.go:24` — Current hardcoded port `18080` to make configurable
  - `client/main.go:59` — Current hardcoded `TOR_SOCKS_PORT` to make configurable
  - YAML library: `gopkg.in/yaml.v3` — Standard Go YAML parsing

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/config/config_test.go` — `TestLoadConfigDefaults`: all fields have sensible defaults
  - [ ] `internal/config/config_test.go` — `TestLoadConfigFromFile`: YAML file correctly parsed
  - [ ] `internal/config/config_test.go` — `TestEnvVarOverrides`: `SOTE_DB_PATH` env var overrides config file
  - [ ] `internal/config/config_test.go` — `TestConfigValidation`: invalid port (<1024) returns error

  **QA Scenarios**:

  ```
  Scenario: Default config is created on first run
    Tool: Bash
    Preconditions: ~/.sote/ directory does not exist
    Steps:
      1. Run: go test -run TestSaveDefaultConfig -v ./internal/config/
      2. OR manually: build and run ./sote once, then check ~/.sote/config.yaml exists
    Expected Result: config.yaml created with all default values, YAML is valid
    Failure Indicators: File not created, invalid YAML, missing required fields
    Evidence: .sisyphus/evidence/task-2-config.yaml

  Scenario: Env var overrides config file
    Tool: Bash
    Preconditions: config.yaml with listen_port: 18080
    Steps:
      1. Run: SOTE_LISTEN_PORT=9999 go test -run TestEnvVarOverrides -v ./internal/config/
    Expected Result: Config.Port == 9999 (env var wins over file)
    Failure Indicators: Port stays at 18080, env var ignored
    Evidence: .sisyphus/evidence/task-2-env-override.txt
  ```

  **Commit**: YES
  - Message: `feat(config): YAML config system with env var overrides`
  - Files: `internal/config/config.go`, `internal/config/config_test.go`

- [ ] 3. **Structured Logging Setup**

  **What to do**:
  - Create `internal/logging/logging.go` with `log/slog` initialization
  - `InitLogger(level string, format string) *slog.Logger` — supports "text" and "json" formats
  - Log levels: debug, info, warn, error — mapped to slog levels
  - Write logs to `~/.sote/sote.log` by default, with stdout fallback
  - Helper functions: `Info(msg, args...)`, `Debug(...)`, `Warn(...)`, `Error(...)`
  - Ensure no sensitive data (passwords, private keys, message bodies) in log output
  - Replace ALL existing `fmt.Println` debug prints with structured logging calls throughout the codebase

  **Must NOT do**:
  - Do NOT log passwords, private keys, or full message bodies
  - Do NOT use `fmt.Println` for logging — use the structured logger

  **TDD**: Write `TestInitLoggerLevels`, `TestLoggerFormats` before implementing

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Standard slog setup — well-documented Go stdlib feature

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Task 2, after Task 1)
  - **Parallel Group**: Wave 1
  - **Blocks**: All other tasks (logging is used everywhere)
  - **Blocked By**: Task 1

  **References**:
  - `node/main.go` — ~30 `fmt.Println` debug prints to replace (`grep -n "fmt.Println" node/main.go`)
  - `client/main.go` — ~15 `fmt.Println` debug prints to replace (`grep -n "fmt.Println" client/main.go`)
  - Go slog docs: `https://pkg.go.dev/log/slog` — Official slog API

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/logging/logging_test.go` — `TestInitLoggerLevels`: debug level shows debug, info hides debug
  - [ ] `internal/logging/logging_test.go` — `TestLoggerFormats`: json format outputs valid JSON, text format is human-readable
  - [ ] `internal/logging/logging_test.go` — `TestNoSecretLeakage`: logger does not output password field

  **QA Scenarios**:

  ```
  Scenario: Logger outputs to file
    Tool: Bash
    Preconditions: ~/.sote/ directory exists
    Steps:
      1. Run: go test -run TestLogToFile -v ./internal/logging/
      2. Check: cat ~/.sote/sote.log | grep "test message"
    Expected Result: Log entry contains "test message" with timestamp and level
    Failure Indicators: File not created, empty file, no timestamp/level
    Evidence: .sisyphus/evidence/task-3-log-output.txt

  Scenario: Debug messages hidden at info level
    Tool: Bash
    Preconditions: Logger initialized at info level
    Steps:
      1. Call logger.Debug("should not appear") and logger.Info("should appear")
      2. Assert: debug message NOT in output, info message IS in output
    Expected Result: Only info-level message visible
    Failure Indicators: Debug message visible when level is info
    Evidence: .sisyphus/evidence/task-3-log-levels.txt
  ```

  **Commit**: YES
  - Message: `feat(logging): structured slog logger with file output and levels`
  - Files: `internal/logging/logging.go`, `internal/logging/logging_test.go`

- [ ] 4. **Database Layer**

  **What to do**:
  - Create `internal/storage/db.go` — SQLite initialization with `modernc.org/sqlite` (pure Go, no CGo)
  - Implement WAL mode: connection string with `_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate`
  - Keep existing 3-table schema: `user`, `contacts`, `messages` (from `db/db.go:25-67`)
  - Add migration system: `schema_version` table, version-based `UP` migrations
  - Implement CRUD: `SaveUser`, `GetUser`, `SaveContact`, `GetContactByUsername`, `GetAllContacts`, `SaveMessage`, `GetMessages`
  - Fix `SaveContact` bug: current code silently returns nil for new contacts (line 114-121 in db.go) — fix to actually save
  - Fix `GetAllContacts`: add owner filter (current code returns ALL contacts from ALL users — only relevant for single-user but correct design)
  - Set `MaxOpenConns(1)` for writer, separate read-only connection pool
  - Use `database/sql` with prepared statements for all queries
  - `Initialize(dbPath string) (*sql.DB, error)` — returns initialized DB handle

  **Must NOT do**:
  - Do NOT use CGo-based `mattn/go-sqlite3` — use `modernc.org/sqlite`
  - Do NOT open DB in non-WAL mode
  - Do NOT skip the `SaveContact` bug fix
  - Do NOT change the table schema (keep user, contacts, messages tables as-is)

  **TDD**: Write tests using in-memory SQLite (`:memory:`), verify WAL mode, CRUD operations, migration

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Straightforward DB layer — migrate existing schema, add WAL mode, fix known bugs

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Tasks 2, 3, after Task 1)
  - **Parallel Group**: Wave 1
  - **Blocks**: Tasks 6, 7, 9, 10, 20
  - **Blocked By**: Task 1

  **References**:
  - `db/db.go:1-221` — Full current DB implementation to migrate and fix
  - `db/db.go:27-51` — CREATE TABLE statements to preserve exactly
  - `db/db.go:111-132` — `SaveContact` with the silent-return bug to fix
  - `db/db.go:135-164` — `GetAllContacts` missing owner filter to fix
  - `db/db.go:69-75` — Message struct to preserve
  - `modernc.org/sqlite` docs: `https://pkg.go.dev/modernc.org/sqlite` — Pure-Go SQLite driver

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/storage/db_test.go` — `TestWALMode`: verify `PRAGMA journal_mode` returns `wal`
  - [ ] `internal/storage/db_test.go` — `TestSaveAndGetUser`: save user, retrieve, fields match
  - [ ] `internal/storage/db_test.go` — `TestSaveContact`: save contact, verify saved, verify duplicate returns error (not silent nil)
  - [ ] `internal/storage/db_test.go` — `TestSaveAndGetMessages`: save 3 messages between two users, retrieve bidirectionally
  - [ ] `internal/storage/db_test.go` — `TestMigration`: run migrations twice, verify idempotent

  **QA Scenarios**:

  ```
  Scenario: WAL mode is enabled
    Tool: Bash
    Preconditions: DB initialized
    Steps:
      1. Run: go test -run TestWALMode -v ./internal/storage/
      2. OR: sqlite3 /tmp/test.db "PRAGMA journal_mode;"
    Expected Result: Output is "wal"
    Failure Indicators: Output is "delete" or "truncate" or "memory"
    Evidence: .sisyphus/evidence/task-4-wal-mode.txt

  Scenario: SaveContact actually saves (bug fix verified)
    Tool: Bash
    Preconditions: Empty contacts table
    Steps:
      1. Save contact: SaveContact("alice", "bob", "bob.onion", []byte("key"))
      2. Retrieve: GetContactByUsername("bob")
    Expected Result: Contact returned with username "bob", onionAddress "bob.onion", publicKey matches
    Failure Indicators: Error returned, empty contact, contact not found
    Evidence: .sisyphus/evidence/task-4-contact-save.txt
  ```

  **Commit**: YES
  - Message: `feat(storage): SQLite with WAL mode, migrations, fix SaveContact bug`
  - Files: `internal/storage/db.go`, `internal/storage/db_test.go`, `internal/storage/migrations.go`

- [ ] 5. **Domain Types + Interfaces**

  **What to do**:
  - Create `internal/domain/types.go` — core domain types:
    - `User` struct (Username, PasswordHash, EncryptedPrivateKey, PublicKey, OnionAddress, TorrcPath)
    - `Contact` struct (Username, OnionAddress, PublicKey)
    - `Message` struct (Sender, Receiver, EncryptedContent, Timestamp)
  - Create `internal/domain/interfaces.go` — service interfaces for dependency injection:
    - `AuthService` interface: `Register(ctx, username, password) (*User, error)`, `Login(ctx, username, password) (*User, error)`
    - `CryptoService` interface: `GenerateKeys() (privateKey, publicKey []byte, error)`, `EncryptMessage(plaintext, publicKey []byte) ([]byte, error)`, `DecryptMessage(ciphertext, privateKey, password []byte) (string, error)`, `EncryptAES(data, passphrase []byte) ([]byte, error)`, `DecryptAES(data, passphrase []byte) ([]byte, error)`
    - `StorageService` interface: `SaveUser(...)`, `GetUser(...)`, `SaveContact(...)`, `GetContactByUsername(...)`, `GetAllContacts() ([]Contact, error)`, `SaveMessage(...)`, `GetMessages(sender, receiver string) ([]Message, error)`
    - `TorService` interface: `Start(ctx) error`, `Stop() error`, `GetOnionAddress() string`
    - `TransportService` interface: `SendMessage(ctx, onionAddr string, msg Message) error`, `StartServer(ctx, handler http.Handler) error`
  - Migrate `user.Contact` and `db.Message` structs into `domain` package
  - All interfaces use `context.Context` as first parameter

  **Must NOT do**:
  - Do NOT define interfaces that aren't used by at least 2 consumers
  - Do NOT put implementation in domain package — only types and interfaces

  **TDD**: Write tests that verify types are JSON-serializable, interfaces compile

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Type definitions — no logic, just structure

  **Parallelization**:
  - **Can Run In Parallel**: YES (after Task 1, parallel with Tasks 2,3,4)
  - **Parallel Group**: Wave 1
  - **Blocks**: Tasks 6, 7, 8, 9, 10, 12, 13, 20
  - **Blocked By**: Task 1

  **References**:
  - `user/user.go:16-31` — Current User and Contact structs to migrate
  - `db/db.go:69-75` — Current Message struct to migrate
  - Go interfaces convention: `https://go.dev/doc/effective_go#interfaces` — Keep interfaces small, consumer-side

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/domain/types_test.go` — `TestUserJSONRoundtrip`: User → JSON → User, fields match
  - [ ] `internal/domain/types_test.go` — `TestMessageJSONRoundtrip`: Message → JSON → Message, fields match
  - [ ] `internal/domain/types_test.go` — `TestInterfacesCompile`: verify interface satisfaction (compile-time check)

  **QA Scenarios**:

  ```
  Scenario: Types are JSON serializable
    Tool: Bash
    Preconditions: Types defined
    Steps:
      1. Run: go test -run TestUserJSONRoundtrip -v ./internal/domain/
      2. Run: go test -run TestMessageJSONRoundtrip -v ./internal/domain/
    Expected Result: Both tests pass, JSON roundtrip preserves all fields
    Failure Indicators: Test failure, missing fields after deserialization
    Evidence: .sisyphus/evidence/task-5-json-roundtrip.txt

  Scenario: Interfaces compile correctly
    Tool: Bash
    Preconditions: Interfaces defined in domain package
    Steps:
      1. Run: go build ./internal/domain/
    Expected Result: Zero compilation errors
    Failure Indicators: Compilation errors from interface definition issues
    Evidence: .sisyphus/evidence/task-5-compile.txt
  ```

  **Commit**: YES
  - Message: `feat(domain): core types and service interfaces`
  - Files: `internal/domain/types.go`, `internal/domain/interfaces.go`, `internal/domain/types_test.go`

- [ ] 6. **Crypto Service** (SECURITY-CRITICAL)

  **What to do**:
  - Create `internal/crypto/crypto.go` implementing `CryptoService` interface from domain
  - **Password hashing**: Use `golang.org/x/crypto/bcrypt` with cost ≥ 12 (REPLACES SHA-256×21)
  - **AES key derivation**: Use `golang.org/x/crypto/pbkdf2` with HMAC-SHA256, ≥ 100k iterations, 32-byte random salt (REPLACES single SHA-256)
  - **AES-256-GCM encrypt/decrypt**: Migrate from `user/user.go:81-98` and `user/user.go:170-190` — keep same GCM pattern but use PBKDF2-derived key
  - **GPG key generation**: Migrate from `user/user.go:34-79` (`GenerateKey` with RSA 2048, armor, encrypt private key with AES)
  - **GPG message encrypt/decrypt**: Migrate from `user/user.go:116-132` and `user/user.go:134-168` (OpenPGP via ProtonMail gopenpgp/v2)
  - All functions take `context.Context` and return errors properly (no `log.Fatal`)
  - Salt generation: `crypto/rand` for all random values

  **Guardrails**:
  - S2: MUST use bcrypt (cost ≥ 12) — `grep -r "sha256" internal/crypto/crypto.go | grep -v "_test.go"` returns zero
  - S3: MUST use PBKDF2 for AES key derivation — single SHA-256 is FORBIDDEN
  - S5: MUST NOT log password or key material

  **Must NOT do**:
  - Do NOT use `crypto/sha256` directly for password hashing or key derivation
  - Do NOT hardcode salts or use predictable RNG — always `crypto/rand`
  - Do NOT use `log.Fatal` — return errors to caller

  **TDD**: Write ALL tests BEFORE implementation: `TestHashPassword`, `TestVerifyPassword`, `TestAESRoundtrip`, `TestAESWrongPassword`, `TestGPGEncryptDecrypt`, `TestGPGBadKey`

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Well-defined crypto migration — copy patterns, replace weak primitives

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Tasks 7, 8, 9, 10 after deps)
  - **Parallel Group**: Wave 2
  - **Blocks**: Task 7, Task 10
  - **Blocked By**: Tasks 1, 5

  **References**:
  - `user/user.go:81-98` — Current AES encrypt (good GCM pattern, needs PBKDF2)
  - `user/user.go:170-190` — Current AES decrypt (good GCM pattern, needs PBKDF2)
  - `user/user.go:100-104` — Current `createHash` (REPLACE entirely with PBKDF2)
  - `user/user.go:106-114` — Current `HashPassword` (REPLACE entirely with bcrypt)
  - `user/user.go:34-79` — Current GPG key generation (keep, migrate)
  - `user/user.go:116-132` — Current GPG encrypt (keep, migrate)
  - `user/user.go:134-168` — Current GPG decrypt (keep, migrate)
  - `golang.org/x/crypto/bcrypt`: `https://pkg.go.dev/golang.org/x/crypto/bcrypt`

  **Acceptance Criteria**:

  **TDD Tests** (RED → GREEN):
  - [ ] `internal/crypto/crypto_test.go` — `TestHashPassword`: bcrypt hash starts with `$2a$`, cost ≥ 12
  - [ ] `internal/crypto/crypto_test.go` — `TestVerifyPasswordWrongPassword`: error returned
  - [ ] `internal/crypto/crypto_test.go` — `TestAESRoundtrip`: encrypt → decrypt → original
  - [ ] `internal/crypto/crypto_test.go` — `TestAESWrongPassword`: wrong password → error
  - [ ] `internal/crypto/crypto_test.go` — `TestGPGEncryptDecrypt`: roundtrip with real keys
  - [ ] `internal/crypto/crypto_test.go` — `TestGPGBadKey`: wrong key → error

  **QA Scenarios**:

  ```
  Scenario: Password uses bcrypt (NOT SHA-256)
    Tool: Bash
    Preconditions: crypto service implemented
    Steps:
      1. Run: grep -n "sha256" internal/crypto/crypto.go | grep -v "_test.go" | grep -v "pbkdf2"
      2. Run: go test -run TestHashPassword -v ./internal/crypto/
    Expected Result: Step 1 returns ZERO matches. Step 2 passes with bcrypt output.
    Failure Indicators: SHA-256 found in production code, test fails
    Evidence: .sisyphus/evidence/task-6-bcrypt.txt

  Scenario: AES roundtrip with PBKDF2 key
    Tool: Bash
    Steps: go test -run TestAESRoundtrip -v ./internal/crypto/
    Expected Result: encrypt("hello", "password") → ciphertext; decrypt(ciphertext, "password") → "hello"
    Failure Indicators: Decryption fails, returns wrong text, returns nil error with garbage
    Evidence: .sisyphus/evidence/task-6-aes.txt
  ```

  **Commit**: YES
  - Message: `feat(crypto): bcrypt hashing, PBKDF2 key derivation, GPG encryption`
  - Files: `internal/crypto/crypto.go`, `internal/crypto/crypto_test.go`

- [ ] 7. **Auth Service**

  **What to do**:
  - Create `internal/auth/auth.go` implementing `AuthService` interface
  - `Register(ctx, username, password)`: validate input, hash password with bcrypt, generate GPG keys, generate Tor hidden service placeholder, encrypt private key with AES PBKDF2, save to storage
  - `Login(ctx, username, password)`: retrieve user, verify bcrypt hash, return user
  - Input validation: username 3-32 chars (alphanumeric + underscore), password ≥ 8 chars
  - Rate limiting stub: track failed logins in memory (full rate limit deferred to v2)

  **Must NOT do**:
  - Do NOT store raw passwords anywhere
  - Do NOT return private key in login response unless explicitly decrypting
  - Do NOT log passwords

  **TDD**: `TestRegisterValidUser`, `TestRegisterDuplicate`, `TestRegisterWeakPassword`, `TestLoginValid`, `TestLoginWrongPassword`, `TestLoginNonExistent`

  **Recommended Agent Profile**:
  - **Category**: `quick` — Simple orchestration of crypto + storage

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Tasks 6, 8, 9, 10)
  - **Parallel Group**: Wave 2
  - **Blocks**: Tasks 12, 15, 20
  - **Blocked By**: Tasks 1, 4, 5, 6

  **References**:
  - `node/main.go:78-110` — Register handler logic to migrate
  - `node/main.go:112-156` — Login handler logic to migrate
  - `internal/domain/interfaces.go` — AuthService contract
  - `internal/crypto/crypto.go` — CryptoService dependency
  - `internal/storage/db.go` — StorageService dependency

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/auth/auth_test.go` — `TestRegisterValidUser`: user saved, bcrypt hash, keys generated
  - [ ] `internal/auth/auth_test.go` — `TestRegisterDuplicate`: second register → error
  - [ ] `internal/auth/auth_test.go` — `TestRegisterWeakPassword`: < 8 chars → error
  - [ ] `internal/auth/auth_test.go` — `TestLoginValid`: register → login → success
  - [ ] `internal/auth/auth_test.go` — `TestLoginWrongPassword`: error returned
  - [ ] `internal/auth/auth_test.go` — `TestLoginNonExistent`: unregistered user → error

  **QA Scenarios**:

  ```
  Scenario: Full register + login flow
    Tool: Bash
    Steps:
      1. go test -run TestRegisterValidUser -v ./internal/auth/
      2. go test -run TestLoginValid -v ./internal/auth/
    Expected Result: Both tests pass, user has username "alice", non-empty public key
    Failure Indicators: Register fails, login returns wrong user, keys empty
    Evidence: .sisyphus/evidence/task-7-register-login.txt

  Scenario: Wrong password rejected
    Tool: Bash
    Steps: go test -run TestLoginWrongPassword -v ./internal/auth/
    Expected Result: Error returned (not nil), no user data in error message
    Failure Indicators: Login succeeds with wrong password, error leaks user info
    Evidence: .sisyphus/evidence/task-7-wrong-password.txt
  ```

  **Commit**: YES
  - Message: `feat(auth): register and login with bcrypt, input validation`
  - Files: `internal/auth/auth.go`, `internal/auth/auth_test.go`

---

- [ ] 8. **Tor Service**

  **What to do**:
  - Create `internal/tor/tor.go` implementing `TorService` interface
  - Use `cretz/bine` (not `os/exec`) for Tor lifecycle
  - `Start(ctx)`: start Tor process via bine, configure hidden service on port from config, wait for onion address, set `StopProcessOnClose: true`
  - `Stop()`: call `tor.Close()` — bine handles graceful shutdown when `StopProcessOnClose` is true
  - `GetOnionAddress()`: return the .onion address
  - Dynamic port allocation: if configured SOCKS port is in use, auto-increment (9060 → 9061 → ...)
  - Error handling: if Tor binary not found, return clear error message "Tor not found. Install: apt install tor"
  - Context propagation: pass context to `tor.Start()`, cancel on shutdown

  **Must NOT do**:
  - A1: Do NOT use `exec.Command("tor")` — use cretz/bine only
  - Do NOT hardcode paths — use config for data directory
  - Do NOT leave orphaned Tor processes (bine handles this with StopProcessOnClose)

  **TDD**: Use mock Tor or test with `tor.Start(nil, &tor.StartConf{NoAutoSocksPort: true})` for unit tests

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: Tor lifecycle is complex — process management, context handling, port allocation

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Tasks 6, 7, 9, 10)
  - **Parallel Group**: Wave 2
  - **Blocks**: Tasks 12, 13, 20
  - **Blocked By**: Tasks 1, 2, 5

  **References**:
  - `tor/tor.go:22-81` — Current `GenerateOnionAddress` (keep the flow, replace `exec.Command("tor")` with bine)
  - `tor/tor.go:83-94` — Current `StartTorWithConfig` (REPLACE entirely with bine)
  - `client/main.go:62-68` — Current bine `tor.Start(context.TODO(), nil)` usage to extend
  - `cretz/bine` docs: `https://pkg.go.dev/github.com/cretz/bine` — Tor control library
  - `cretz/bine/tor` docs: `https://pkg.go.dev/github.com/cretz/bine/tor` — Start, Dialer, Listen

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/tor/tor_test.go` — `TestStartStop`: start → onion address set → stop → clean exit
  - [ ] `internal/tor/tor_test.go` — `TestDynamicPortAllocation`: two instances → different SOCKS ports
  - [ ] `internal/tor/tor_test.go` — `TestContextCancellation`: cancel context → Tor stops gracefully

  **QA Scenarios**:

  ```
  Scenario: Tor starts and generates onion address
    Tool: Bash (requires tor installed)
    Steps:
      1. go test -run TestStartStop -v -timeout 120s ./internal/tor/
    Expected Result: Test passes, onion address ends with .onion, stop is clean
    Failure Indicators: Timeout (>120s), no .onion address, Tor process left running
    Evidence: .sisyphus/evidence/task-8-tor-start.txt

  Scenario: Missing Tor binary gives clear error
    Tool: Bash
    Steps: Temporarily remove tor from PATH, run test
    Expected Result: Error message "Tor not found. Install: apt install tor"
    Failure Indicators: Panic, obscure error, silent failure
    Evidence: .sisyphus/evidence/task-8-no-tor.txt
  ```

  **Commit**: YES
  - Message: `feat(tor): cretz/bine lifecycle with dynamic port allocation`
  - Files: `internal/tor/tor.go`, `internal/tor/tor_test.go`

- [ ] 9. **Contact Service**

  **What to do**:
  - Create `internal/contacts/contacts.go` implementing contact management
  - `AddContact(ctx, myUsername, contactUsername, onionAddr, publicKey)`: validate, save to DB
  - `GetContacts(ctx)`: list all contacts for current user
  - `GetContactByUsername(ctx, username)`: find specific contact
  - `AcceptContactRequest(ctx, myUsername, theirUsername, theirOnion, theirKey)`: save contact + send own info back via HTTP (delegates to Transport service)
  - Fix the silent-return bug from old `SaveContact`: return error for duplicates, don't silently skip

  **Must NOT do**:
  - Do NOT allow adding self as contact
  - Do NOT save duplicate contacts

  **TDD**: `TestAddContact`, `TestAddDuplicateContact`, `TestAddSelfAsContact`, `TestGetContacts`, `TestGetContactByUsername`

  **Recommended Agent Profile**:
  - **Category**: `quick` — Simple CRUD orchestration

  **Parallelization**:
  - **Can Run In Parallel**: YES (with 6, 7, 8, 10)
  - **Parallel Group**: Wave 2
  - **Blocks**: Tasks 12, 17, 20
  - **Blocked By**: Tasks 1, 4, 5

  **References**:
  - `db/db.go:111-132` — `SaveContact` with bug to fix
  - `db/db.go:135-164` — `GetAllContacts` to migrate
  - `db/db.go:167-190` — `GetContactByUsername` to migrate
  - `node/main.go:184-205` — `addContactHandler` logic
  - `node/main.go:207-297` — `receiveContactRequestHandler` logic (remove fmt.Scanln!)

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/contacts/contacts_test.go` — `TestAddContact`: save → retrieve → fields match
  - [ ] `internal/contacts/contacts_test.go` — `TestAddDuplicateContact`: second save → error
  - [ ] `internal/contacts/contacts_test.go` — `TestAddSelfAsContact`: self-add → error
  - [ ] `internal/contacts/contacts_test.go` — `TestGetContacts`: 3 contacts → all 3 returned

  **QA Scenarios**:

  ```
  Scenario: Add and retrieve contacts
    Tool: Bash
    Steps: go test -run TestAddContact -v ./internal/contacts/
    Expected Result: Contact saved with correct username, onion address, public key
    Failure Indicators: Silent nil return (old bug), wrong fields, contact not found
    Evidence: .sisyphus/evidence/task-9-contacts.txt
  ```

  **Commit**: YES
  - Message: `feat(contacts): contact management with duplicate prevention`
  - Files: `internal/contacts/contacts.go`, `internal/contacts/contacts_test.go`

- [ ] 10. **Message Service**

  **What to do**:
  - Create `internal/messages/messages.go` implementing message send/receive/fetch
  - `SendMessage(ctx, sender, receiver, plaintext)`: look up receiver's public key, GPG encrypt, save to local DB (AES-encrypted for own copy), send to receiver via Transport service
  - `ReceiveMessage(ctx, sender, receiver, encryptedContent)`: save incoming encrypted message to DB
  - `FetchMessages(ctx, user1, user2)`: get bidirectional messages, decrypt with appropriate key
  - Message size limit: max 64KB (reject with clear error)
  - Retry logic: 3 attempts with exponential backoff (1s, 2s, 4s) for outbound sends

  **Must NOT do**:
  - Do NOT log message bodies
  - Do NOT block indefinitely on send — use context timeout (30s)

  **TDD**: `TestSendMessage`, `TestReceiveMessage`, `TestFetchMessages`, `TestMessageTooLarge`, `TestEncryptDecryptRoundtrip`

  **Recommended Agent Profile**:
  - **Category**: `quick` — Straightforward message orchestration

  **Parallelization**:
  - **Can Run In Parallel**: YES (with 6, 7, 8, 9)
  - **Parallel Group**: Wave 2
  - **Blocks**: Tasks 12, 18, 20
  - **Blocked By**: Tasks 1, 4, 5, 6

  **References**:
  - `node/main.go:299-380` — `sendMessageHandler` (encrypt, proxy, send, save)
  - `node/main.go:382-403` — `receiveMessageHandler` (save incoming)
  - `node/main.go:405-433` — `fetchMessagesHandler` (bidirectional query)
  - `client/main.go:518-531` — Message decryption logic (own messages vs received)
  - `internal/crypto/crypto.go` — CryptoService for GPG encrypt/decrypt

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/messages/messages_test.go` — `TestSendMessage`: sends, saves locally, returns success
  - [ ] `internal/messages/messages_test.go` — `TestReceiveMessage`: saves incoming, DB has record
  - [ ] `internal/messages/messages_test.go` — `TestFetchMessages`: 3 messages between A↔B, all returned
  - [ ] `internal/messages/messages_test.go` — `TestMessageTooLarge`: > 64KB → error
  - [ ] `internal/messages/messages_test.go` — `TestEncryptDecryptRoundtrip`: encrypt → save → decrypt → original

  **QA Scenarios**:

  ```
  Scenario: Full message send → receive → fetch flow
    Tool: Bash
    Steps: go test -run TestFetchMessages -v ./internal/messages/
    Expected Result: All 3 messages retrieved, both directions present
    Failure Indicators: Missing messages, wrong sender/receiver, decryption failure
    Evidence: .sisyphus/evidence/task-10-messages.txt
  ```

  **Commit**: YES
  - Message: `feat(messages): send, receive, fetch with encryption and retry`
  - Files: `internal/messages/messages.go`, `internal/messages/messages_test.go`

- [ ] 11. **TLS Certificate Management**

  **What to do**:
  - Create `internal/transport/tls.go` — TLS certificate management
  - Generate self-signed CA certificate on first run (stored in `~/.sote/certs/`)
  - Generate server certificate signed by CA for the node's hidden service
  - Implement certificate fingerprint pinning: store expected cert SHA-256 fingerprint in config, verify on connection
  - `VerifyConnection` callback in `tls.Config` that checks fingerprint matches expected value
  - Auto-regenerate certificates 30 days before expiry (365-day validity)
  - Store CA cert, server cert, server key in `~/.sote/certs/` with restricted permissions (0600 for key)

  **Must NOT do**:
  - S1: Do NOT use `InsecureSkipVerify` ANYWHERE — verify with `grep -r "InsecureSkipVerify" internal/ cmd/`
  - Do NOT hardcode certificate paths

  **TDD**: `TestCAGeneration`, `TestServerCertGeneration`, `TestFingerprintVerification`, `TestFingerprintMismatch`

  **Recommended Agent Profile**:
  - **Category**: `quick` — Standard Go TLS patterns

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Tasks 12, 13)
  - **Parallel Group**: Wave 3
  - **Blocks**: Task 12
  - **Blocked By**: Tasks 1, 2

  **References**:
  - `node/main.go:27-36` — Current cert generation (replace openssl with Go `crypto/x509`)
  - `node/main.go:274-277, 331-334` — Current `InsecureSkipVerify: true` (REPLACE with fingerprint pinning)
  - `client/main.go:30-33` — Another `InsecureSkipVerify: true` (REPLACE)
  - Go crypto/tls: `https://pkg.go.dev/crypto/tls#Config` — VerifyConnection, RootCAs

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/transport/tls_test.go` — `TestCAGeneration`: CA cert created, valid x509
  - [ ] `internal/transport/tls_test.go` — `TestFingerprintVerification`: correct fingerprint → accepted
  - [ ] `internal/transport/tls_test.go` — `TestFingerprintMismatch`: wrong fingerprint → rejected

  **QA Scenarios**:

  ```
  Scenario: No InsecureSkipVerify anywhere in production code
    Tool: Bash
    Steps: grep -rn "InsecureSkipVerify" internal/ cmd/ | grep -v "_test.go"
    Expected Result: ZERO matches (empty output)
    Failure Indicators: Any match — MUST fix before proceeding
    Evidence: .sisyphus/evidence/task-11-no-insecure.txt

  Scenario: Fingerprint mismatch is rejected
    Tool: Bash
    Steps: go test -run TestFingerprintMismatch -v ./internal/transport/
    Expected Result: Connection fails with TLS error, NOT silently accepted
    Failure Indicators: Connection succeeds with wrong fingerprint
    Evidence: .sisyphus/evidence/task-11-fingerprint.txt
  ```

  **Commit**: YES
  - Message: `feat(transport): TLS cert management with fingerprint pinning`
  - Files: `internal/transport/tls.go`, `internal/transport/tls_test.go`

- [ ] 12. **HTTP Server + Handlers**

  **What to do**:
  - Create `internal/transport/server.go` — HTTP server with TLS
  - Use `net/http` with Go 1.22+ pattern routing: `mux.HandleFunc("POST /register", h)`
  - Endpoints: `POST /register`, `POST /login`, `GET /onion-address`, `POST /add-contact`, `POST /receive-contact-request`, `POST /send-message`, `POST /receive-message`, `POST /fetch-messages`
  - Middleware: structured logging (request ID, method, path, status, duration), recover from panics, CORS for localhost
  - Each handler: decode JSON, call appropriate service, encode response, handle errors
  - **CRITICAL**: Remove the `fmt.Scanln` blocking pattern from `receiveContactRequestHandler` — use async contact request queue instead: incoming requests stored in DB, TUI polls and prompts user
  - Server starts on configured port with TLS cert from Task 11
  - Use `httptest.NewServer()` for handler tests

  **Must NOT do**:
  - Do NOT use `fmt.Scanln` or any blocking stdin reads in HTTP handlers
  - Do NOT use `InsecureSkipVerify` on server side
  - Do NOT use `log.Fatal` in handlers — return HTTP errors

  **TDD**: Write handler tests with `httptest.NewServer()`: `TestRegisterHandler`, `TestLoginHandler`, `TestInvalidJSON`, `TestMissingFields`

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: Complex — 8 endpoints, middleware, async contact request pattern, TLS integration

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Tasks 11, 13)
  - **Parallel Group**: Wave 3
  - **Blocks**: Task 20
  - **Blocked By**: Tasks 1, 2, 6, 7, 8, 9, 10, 11

  **References**:
  - `node/main.go:40-48` — Current endpoint registration to migrate
  - `node/main.go:54-67` — `setCurrentUserHandler` (merge into TUI model, not HTTP)
  - `node/main.go:78-110` — `registerHandler` to migrate to new handler style
  - `node/main.go:112-156` — `loginHandler` to migrate
  - `node/main.go:207-297` — `receiveContactRequestHandler` — REMOVE fmt.Scanln, use async queue
  - `node/main.go:299-380` — `sendMessageHandler` to migrate
  - `node/main.go:382-433` — `receiveMessageHandler` + `fetchMessagesHandler` to migrate

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/transport/server_test.go` — `TestRegisterHandler`: POST valid user → 201, correct JSON
  - [ ] `internal/transport/server_test.go` — `TestLoginHandler`: POST valid credentials → 200, user JSON
  - [ ] `internal/transport/server_test.go` — `TestLoginWrongPassword`: wrong password → 401
  - [ ] `internal/transport/server_test.go` — `TestInvalidJSON`: malformed body → 400
  - [ ] `internal/transport/server_test.go` — `TestMissingFields`: missing username → 400
  - [ ] `internal/transport/server_test.go` — `TestContactRequestQueue`: POST contact request → saved to queue, NO blocking

  **QA Scenarios**:

  ```
  Scenario: All 8 endpoints respond correctly
    Tool: Bash (curl)
    Preconditions: Server running on localhost:18080
    Steps:
      1. curl -sk -X POST https://localhost:18080/register -d '{"username":"test","password":"secret123"}'
      2. curl -sk -X POST https://localhost:18080/login -d '{"username":"test","password":"secret123"}'
    Expected Result: Register → 201 with user JSON. Login → 200 with user JSON.
    Failure Indicators: 500 errors, connection refused, wrong status codes
    Evidence: .sisyphus/evidence/task-12-endpoints.txt

  Scenario: No blocking stdin reads in handlers
    Tool: Bash
    Steps: grep -n "Scanln\|ReadString" internal/transport/*.go | grep -v "_test.go"
    Expected Result: ZERO matches
    Failure Indicators: Any fmt.Scanln or bufio.ReadString in handler code
    Evidence: .sisyphus/evidence/task-12-no-scanln.txt
  ```

  **Commit**: YES
  - Message: `feat(transport): HTTPS server with 8 endpoints, async contact queue`
  - Files: `internal/transport/server.go`, `internal/transport/handlers.go`, `internal/transport/server_test.go`

- [ ] 13. **HTTP Client (Tor SOCKS Proxy)**

  **What to do**:
  - Create `internal/transport/client.go` — HTTP client that routes through Tor SOCKS proxy
  - Use cretz/bine dialer for Tor SOCKS connections
  - TLS fingerprint verification on all outbound connections (from Task 11)
  - Retry logic: 3 attempts with exponential backoff (1s, 2s, 4s)
  - Context-aware: respect context deadline/timeout
  - `SendPost(ctx, onionAddr, path string, body interface{}) (*http.Response, error)` — convenience method
  - Circuit failure handling: detect Tor circuit failures, return descriptive errors

  **Must NOT do**:
  - S1: Do NOT use `InsecureSkipVerify` — always verify fingerprint
  - Do NOT hardcode the proxy address — use config

  **TDD**: `TestSendPost`, `TestRetryOnFailure`, `TestContextTimeout`, `TestFingerprintVerification`

  **Recommended Agent Profile**:
  - **Category**: `quick` — Standard HTTP client with Tor proxy

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Tasks 11, 12)
  - **Parallel Group**: Wave 3
  - **Blocks**: Task 20
  - **Blocked By**: Tasks 1, 2, 8, 11

  **References**:
  - `node/main.go:264-277` — Tor proxy HTTP client pattern (migrate, replace InsecureSkipVerify)
  - `client/main.go:292-307` — bine dialer + HTTP client pattern (keep, integrate)
  - `node/main.go:329-335` — Second copy of same pattern (deduplicate!)

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/transport/client_test.go` — `TestSendPost`: POST to test server → 200
  - [ ] `internal/transport/client_test.go` — `TestRetryOnFailure`: server fails twice, succeeds third → client retries
  - [ ] `internal/transport/client_test.go` — `TestContextTimeout`: context deadline exceeded → error

  **QA Scenarios**:

  ```
  Scenario: Client routes through Tor SOCKS proxy
    Tool: Bash
    Steps: go test -run TestSendPost -v ./internal/transport/
    Expected Result: Client successfully connects through proxy, TLS fingerprint verified
    Failure Indicators: Connection refused, TLS error, proxy error
    Evidence: .sisyphus/evidence/task-13-tor-client.txt
  ```

  **Commit**: YES
  - Message: `feat(transport): Tor SOCKS HTTP client with retry and fingerprint verification`
  - Files: `internal/transport/client.go`, `internal/transport/client_test.go`

- [ ] 14. **TUI Theme + Styles**

  **What to do**:
  - Create `internal/tui/styles.go` — centralized Lipgloss style definitions
  - Define color scheme: primary (#7C3AED purple), secondary (#06B6D4 cyan), success (#10B981 green), error (#EF4444 red), warning (#F59E0B amber), background (#1A1B26 dark), text (#A9B1D6 light), muted (#565F89 dim)
  - Define reusable styles:
    - `HeaderStyle` — bold, centered, colored background, full width
    - `StatusBarStyle` — dim text, colored background, full width
    - `InputStyle` — focused (cyan border) / blurred (dim border)
    - `ListStyle` — selected (highlighted) / unselected (dim)
    - `ButtonStyle` — primary (purple bg) / danger (red bg) / default (dim)
    - `CardStyle` — bordered, padded container
    - `ErrorStyle` — red text, bold
    - `SuccessStyle` — green text
    - `HelpStyle` — dim, italic (if supported)
  - All styles responsive: use percentage-based widths where possible
  - Support light/dark terminal detection via `lipgloss.HasDarkBackground()`
  - Export style variables so all TUI views use consistent theme

  **Must NOT do**:
  - Do NOT hardcode color values in individual views — use styles from this file
  - T1: Must render correctly at 80×24 minimum

  **TDD**: `TestStyleConstants`, `TestColorSchemeContrast` (verify contrast ratios)

  **Recommended Agent Profile**:
  - **Category**: `visual-engineering`
    - Reason: Pure visual design — color theory, Lipgloss styling, terminal rendering
  - **Skills**: [`frontend-ui-ux`]
    - `frontend-ui-ux`: Terminal color scheme and visual hierarchy design

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Tasks 15-19, after Task 1)
  - **Parallel Group**: Wave 4 (all TUI tasks can start after Wave 1)
  - **Blocks**: Tasks 15, 16, 17, 18, 19
  - **Blocked By**: Tasks 1, 3

  **References**:
  - Lipgloss v2 styles: `https://charm.land/lipgloss/v2` — Style, Color, Border, Padding
  - Lipgloss adaptive colors: `https://charm.land/lipgloss/v2#adaptive-colors` — HasDarkBackground
  - `client/main.go:225-266` — Current menu UI structure (reference for what views need)

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/tui/styles_test.go` — `TestStyleConstants`: all exported styles are non-nil
  - [ ] `internal/tui/styles_test.go` — `TestColorSchemeContrast`: primary text on dark background has sufficient visibility
  - [ ] `internal/tui/styles_test.go` — `TestResponsiveWidths`: styles use math for width calculations, not magic numbers

  **QA Scenarios**:

  ```
  Scenario: Theme renders at 80×24
    Tool: Bash (go test with tea.WithWindowSize(80,24))
    Preconditions: Styles defined
    Steps:
      1. go test -run TestStyleConstants -v ./internal/tui/
    Expected Result: Tests pass, styles allocate space within 80×24
    Failure Indicators: Styles exceed width, panic on narrow terminal
    Evidence: .sisyphus/evidence/task-14-styles.txt

  Scenario: Color scheme is consistent
    Tool: Bash
    Steps: grep -rn "lipgloss.Color" internal/tui/styles.go | sort | uniq
    Expected Result: Colors defined once in styles.go, referenced by name elsewhere
    Failure Indicators: Duplicate color literals in multiple files
    Evidence: .sisyphus/evidence/task-14-colors.txt
  ```

  **Commit**: YES
  - Message: `feat(tui): centralized theme with Lipgloss styles and color scheme`
  - Files: `internal/tui/styles.go`, `internal/tui/styles_test.go`

- [ ] 15. **Login + Register Views**

  **What to do**:
  - Create `internal/tui/login.go` — login view using Bubble Tea + Bubbles `textinput`
  - Create `internal/tui/register.go` — register view with password confirmation
  - Login view: username field, password field (masked), login button, error display area, link to register
  - Register view: username, password, confirm password fields, validation errors inline, register button, link back to login
  - Keyboard navigation: Tab between fields, Enter to submit, Esc to go back
  - Validation: show inline error messages (red) for empty fields, short password, mismatched confirmation
  - Loading state: show spinner while login/register request is in flight
  - Error state: show server errors (e.g., "Username taken", "Wrong password")
  - On success: transition to main menu view

  **Must NOT do**:
  - Do NOT echo password in the TUI (use textinput.EchoModePassword)
  - T2: Must restore terminal on Ctrl+C exit

  **TDD**: `TestLoginViewRenders`, `TestLoginTabNavigation`, `TestLoginSubmitEmpty`, `TestRegisterViewRenders`, `TestRegisterPasswordMismatch`

  **Recommended Agent Profile**:
  - **Category**: `visual-engineering`
    - Reason: Interactive form UI with validation states, keyboard navigation
  - **Skills**: [`frontend-ui-ux`]
    - `frontend-ui-ux`: Form UX with inline validation, focus management, error states

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Tasks 14, 16-19)
  - **Parallel Group**: Wave 4
  - **Blocks**: Task 20
  - **Blocked By**: Tasks 1, 7, 14

  **References**:
  - `client/main.go:106-157` — Current register flow (username/password/confirm)
  - `client/main.go:159-203` — Current login flow (username/password → server → response)
  - Bubbles textinput: `https://charm.land/bubbles/v2/textinput` — EchoMode, Placeholder, Focus/Blur
  - Bubbles spinner: `https://charm.land/bubbles/v2/spinner` — Loading states

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/tui/login_test.go` — `TestLoginViewRenders`: view() returns non-empty string with "Username" and "Password"
  - [ ] `internal/tui/login_test.go` — `TestLoginTabNavigation`: Tab key moves focus from username→password→button
  - [ ] `internal/tui/login_test.go` — `TestLoginSubmitEmpty`: submit with empty fields → error message displayed
  - [ ] `internal/tui/register_test.go` — `TestRegisterPasswordMismatch`: different passwords → "Passwords do not match" error
  - [ ] `internal/tui/register_test.go` — `TestRegisterSuccess`: valid register → view transitions to main menu

  **QA Scenarios**:

  ```
  Scenario: Full login flow with keyboard navigation
    Tool: Bash (go test with tea.WithInput)
    Preconditions: TUI model initialized, auth service mocked
    Steps:
      1. Simulate: Tab (focus password), type "secret123", Tab (focus button), Enter
      2. Assert: login called with correct credentials, view transitions to main menu
    Expected Result: Login succeeds, transition to main menu view
    Failure Indicators: Wrong credentials sent, view doesn't change, crash
    Evidence: .sisyphus/evidence/task-15-login-flow.txt

  Scenario: Ctrl+C exits cleanly from login
    Tool: Bash
    Steps: go test -run TestCtrlCExit -v ./internal/tui/
    Expected Result: Program exits without error, terminal restored
    Failure Indicators: Terminal in raw mode after exit, error code non-zero
    Evidence: .sisyphus/evidence/task-15-ctrl-c.txt
  ```

  **Commit**: YES
  - Message: `feat(tui): login and register views with validation and keyboard nav`
  - Files: `internal/tui/login.go`, `internal/tui/register.go`, `internal/tui/login_test.go`

- [ ] 16. **Main Menu + Navigation**

  **What to do**:
  - Create `internal/tui/nav.go` — navigation system and main menu view
  - State-based view switching: enum with `loginView`, `registerView`, `mainMenuView`, `chatView`, `contactsView`, `settingsView`
  - Main menu view: list of options (Send Message, Contacts, Show Onion Address, Settings, Exit) with keyboard shortcuts (1-5)
  - Status bar at bottom: shows connection status, current user, Tor status
  - Header at top: "SOTE — Decentralized Messenger" with primary color
  - Tab-like navigation: keyboard numbers to select option, `q` to quit, `Esc` to go back
  - Handle `tea.WindowSizeMsg` for responsive layout recalculations
  - Pass WindowSizeMsg to ALL child models (forward to active view)

  **Must NOT do**:
  - T1: Must support 80×24 minimum, handle resize gracefully
  - Do NOT block Update() with long operations

  **TDD**: `TestMainMenuRenders`, `TestViewSwitching`, `TestResizeHandling`, `TestKeyboardShortcuts`

  **Recommended Agent Profile**:
  - **Category**: `visual-engineering`
    - Reason: Navigation architecture, layout composition, responsive design
  - **Skills**: [`frontend-ui-ux`]
    - `frontend-ui-ux`: Navigation UX patterns (tabs, shortcuts, status indicators)

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Tasks 14, 15, 17-19)
  - **Parallel Group**: Wave 4
  - **Blocks**: Task 20
  - **Blocked By**: Tasks 1, 14

  **References**:
  - `client/main.go:222-266` — Current main menu to translate into TUI
  - Bubble Tea tabs example: GitHub `charmbracelet/bubbletea/examples/tabs/main.go` — View switching pattern
  - Bubble Tea composable-views: `examples/composable-views/main.go` — Focus management
  - Bubble Tea window-size: `examples/window-size/main.go` — Resize handling

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/tui/nav_test.go` — `TestViewSwitching`: "1" key → chatView, "2" → contactsView
  - [ ] `internal/tui/nav_test.go` — `TestResizeHandling`: WindowSizeMsg → width/height updated in model
  - [ ] `internal/tui/nav_test.go` — `TestKeyboardShortcuts`: "q" → tea.Quit, "5" → exit
  - [ ] `internal/tui/nav_test.go` — `TestMainMenuRenders`: view string contains all 5 menu options

  **QA Scenarios**:

  ```
  Scenario: Resize from 80×24 to 120×40 doesn't crash
    Tool: Bash (go test)
    Steps:
      1. Create program at 80×24
      2. Send WindowSizeMsg{120, 40}
      3. Send WindowSizeMsg{80, 24}
      4. Verify model not nil, no panic
    Expected Result: Model adapts to both sizes without crash
    Failure Indicators: Panic, nil pointer, layout overflow
    Evidence: .sisyphus/evidence/task-16-resize.txt
  ```

  **Commit**: YES
  - Message: `feat(tui): main menu with view switching and responsive layout`
  - Files: `internal/tui/nav.go`, `internal/tui/nav_test.go`

- [ ] 17. **Contacts View**

  **What to do**:
  - Create `internal/tui/contacts.go` — contacts management view
  - Contact list: Bubbles `list.Model` showing all contacts with username and .onion address
  - "Add Contact" form: textinput for .onion address, sends contact request via Tor
  - "Pending Requests" section: incoming contact requests with Accept/Decline (keyboard: `a`/`d`)
  - Keyboard: ↑↓ to navigate list, `a` to add contact, `Enter` to view contact details, `Esc` to go back
  - Status indicators: online (green dot), offline (red dot), pending (yellow dot) — based on last known state
  - Empty state: "No contacts yet. Press 'a' to add your first contact." with muted styling

  **Must NOT do**:
  - Do NOT block on network requests — use `tea.Cmd` for async operations
  - Do NOT allow adding self as contact

  **TDD**: `TestContactsViewRenders`, `TestAddContactForm`, `TestAcceptContactRequest`, `TestDeclineContactRequest`, `TestEmptyState`

  **Recommended Agent Profile**:
  - **Category**: `visual-engineering`
    - Reason: List-based UI with forms, keyboard shortcuts, status indicators
  - **Skills**: [`frontend-ui-ux`]
    - `frontend-ui-ux`: List UX with inline forms, empty states, status badges

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Tasks 14-16, 18-19)
  - **Parallel Group**: Wave 4
  - **Blocks**: Task 20
  - **Blocked By**: Tasks 1, 9, 14

  **References**:
  - `client/main.go:281-349` — Current `addContact` flow to translate into TUI
  - `client/main.go:268-279` — Current `getContacts` to integrate
  - `node/main.go:207-297` — Current contact request accept/decline flow
  - Bubbles list: `https://charm.land/bubbles/v2/list` — ItemDelegate, Title, Description
  - Bubbles textinput: `https://charm.land/bubbles/v2/textinput` — For add contact form

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/tui/contacts_test.go` — `TestContactsViewRenders`: view shows contact list
  - [ ] `internal/tui/contacts_test.go` — `TestAddContactForm`: "a" key opens form, Enter submits
  - [ ] `internal/tui/contacts_test.go` — `TestAcceptContactRequest`: "a" on pending → contact accepted
  - [ ] `internal/tui/contacts_test.go` — `TestEmptyState`: no contacts → shows empty state message

  **QA Scenarios**:

  ```
  Scenario: Add contact via .onion address
    Tool: Bash (go test with tea.WithInput)
    Steps:
      1. Press "a" to open add contact form
      2. Type "xyz.onion" + Enter
      3. Assert: contact saved to DB, contact appears in list
    Expected Result: Contact added, visible in list
    Failure Indicators: Contact not saved, list not updated, panic
    Evidence: .sisyphus/evidence/task-17-add-contact.txt

  Scenario: Empty state shown when no contacts
    Tool: Bash
    Steps: Initialize contacts view with empty list, render view
    Expected Result: "No contacts yet. Press 'a' to add your first contact." visible
    Failure Indicators: Blank screen, crash, misleading UI
    Evidence: .sisyphus/evidence/task-17-empty-state.txt
  ```

  **Commit**: YES
  - Message: `feat(tui): contacts view with list, add form, and request management`
  - Files: `internal/tui/contacts.go`, `internal/tui/contacts_test.go`

- [ ] 18. **Chat View**

  **What to do**:
  - Create `internal/tui/chat.go` — the main chat view
  - Message display: Bubbles `viewport.Model` showing messages with sender, timestamp, content
  - Message input: Bubbles `textarea.Model` at bottom for composing messages
  - Send on Enter (Shift+Enter for newline), Ctrl+S to send
  - Message styling: sent messages (right-aligned, primary color), received messages (left-aligned, secondary color)
  - Timestamps: `[HH:MM]` prefix on each message
  - Auto-scroll: viewport scrolls to bottom on new message, unless user has scrolled up
  - Contact selector: show current contact name at top, ←→ to switch between contacts
  - Decrypt messages: own sent messages use AES decrypt, received use GPG decrypt (per existing logic in `client/main.go:518-531`)
  - Incoming message notification: when `p.Send(IncomingMessageMsg)` fires, prepend to message list

  **Must NOT do**:
  - Do NOT block on decryption in Update() — use `tea.Cmd` for heavy crypto
  - Do NOT log message bodies

  **TDD**: `TestChatViewRenders`, `TestSendMessage`, `TestReceiveMessage`, `TestAutoScroll`, `TestSwitchContact`

  **Recommended Agent Profile**:
  - **Category**: `visual-engineering`
    - Reason: Complex interactive chat UI — viewport, textarea, real-time updates
  - **Skills**: [`frontend-ui-ux`]
    - `frontend-ui-ux`: Chat UX patterns (message bubbles, auto-scroll, typing indicator)

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Tasks 14-17, 19)
  - **Parallel Group**: Wave 4
  - **Blocks**: Task 20
  - **Blocked By**: Tasks 1, 10, 14

  **References**:
  - `client/main.go:402-456` — Current `sendMessage` flow to integrate
  - `client/main.go:458-535` — Current `fetchMessages` with decryption logic (lines 518-531)
  - Bubble Tea chat example: `examples/chat/main.go` — Viewport + textarea pattern
  - Bubbles viewport: `https://charm.land/bubbles/v2/viewport` — Model, SetContent, ScrollToBottom
  - Bubbles textarea: `https://charm.land/bubbles/v2/textarea` — Multi-line input

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/tui/chat_test.go` — `TestSendMessage`: type message + Enter → sends, clears input, message appears
  - [ ] `internal/tui/chat_test.go` — `TestReceiveMessage`: simulate incoming msg → appears in viewport
  - [ ] `internal/tui/chat_test.go` — `TestAutoScroll`: new message → viewport at bottom (unless scrolled up)
  - [ ] `internal/tui/chat_test.go` — `TestSwitchContact`: left/right arrow → different contact messages loaded
  - [ ] `internal/tui/chat_test.go` — `TestMessageDecryption`: received GPG msg → decrypt → display plaintext
  - [ ] `internal/tui/chat_test.go` — `TestOwnMessageDecryption`: own sent msg → AES decrypt → display plaintext

  **QA Scenarios**:

  ```
  Scenario: Full chat flow — send and receive messages
    Tool: Bash (go test with tea.WithInput)
    Steps:
      1. Select contact "bob"
      2. Type "Hello Bob!" + Enter
      3. Assert: message sent, appears in viewport with timestamp [HH:MM]
      4. Simulate incoming: "Hey Alice!" from bob
      5. Assert: message appears, viewport scrolls to bottom, correct color (secondary)
    Expected Result: Both sent and received messages visible with correct styling
    Failure Indicators: Messages not appearing, wrong colors, viewport stuck
    Evidence: .sisyphus/evidence/task-18-chat-flow.txt

  Scenario: Contact switching preserves message history
    Tool: Bash
    Steps:
      1. Chat with "bob" — send 2 messages
      2. Press right arrow → switch to "carol"
      3. Assert: viewport shows carol's messages (not bob's)
      4. Press left arrow → switch back to "bob"
      5. Assert: viewport shows bob's 2 messages
    Expected Result: Each contact has independent message history in view
    Failure Indicators: Messages mixed, history lost, crash on switch
    Evidence: .sisyphus/evidence/task-18-contact-switch.txt
  ```

  **Commit**: YES
  - Message: `feat(tui): chat view with viewport, textarea, and message decryption`
  - Files: `internal/tui/chat.go`, `internal/tui/chat_test.go`

- [ ] 19. **Settings View**

  **What to do**:
  - Create `internal/tui/settings.go` — settings and account view
  - Display current user info: username, .onion address (copyable — show full address)
  - "Change Password" section: current password, new password, confirm new password
  - Password change flow: verify current password (bcrypt), re-encrypt private key with new PBKDF2 key, update DB
  - "Connection Info": Tor status (connected/disconnected), hidden service address, uptime
  - "About": version number, license info (GPLv3)
  - Keyboard: Tab between fields, Enter to submit, Esc to go back

  **Must NOT do**:
  - Do NOT display private key
  - Do NOT log old or new passwords

  **TDD**: `TestSettingsViewRenders`, `TestChangePassword`, `TestChangePasswordWrongCurrent`, `TestOnionAddressDisplay`

  **Recommended Agent Profile**:
  - **Category**: `visual-engineering`
    - Reason: Settings form with password change, connection status display
  - **Skills**: [`frontend-ui-ux`]
    - `frontend-ui-ux`: Settings UX (sections, info display, sensitive data handling)

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Tasks 14-18)
  - **Parallel Group**: Wave 4
  - **Blocks**: Task 20
  - **Blocked By**: Tasks 1, 7, 8, 14

  **References**:
  - `client/main.go:351-400` — Current `getOnionAddress` and `showQR` (keep address display, drop QR)
  - `client/main.go:27` — `version string` to display in About
  - `LICENSE` — GPLv3 reference for About section

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/tui/settings_test.go` — `TestSettingsViewRenders`: shows username, onion address, version
  - [ ] `internal/tui/settings_test.go` — `TestChangePassword`: change password → re-login with new password works
  - [ ] `internal/tui/settings_test.go` — `TestChangePasswordWrongCurrent`: wrong current password → error, password unchanged
  - [ ] `internal/tui/settings_test.go` — `TestOnionAddressDisplay`: .onion address visible, ends with .onion

  **QA Scenarios**:

  ```
  Scenario: Change password successfully
    Tool: Bash (go test)
    Steps:
      1. Enter current password "oldpass", new password "newpass123", confirm "newpass123"
      2. Submit
      3. Assert: password updated in DB (bcrypt hash of "newpass123")
      4. Re-login with "newpass123" → success
    Expected Result: Password changed, old password no longer works, new password works
    Failure Indicators: Old password still works, new password doesn't, DB corruption
    Evidence: .sisyphus/evidence/task-19-password-change.txt

  Scenario: Private key NOT displayed
    Tool: Bash
    Steps: Render settings view, search view string for private key patterns
    Expected Result: View does NOT contain "BEGIN PGP PRIVATE KEY" or similar
    Failure Indicators: Private key visible in TUI output
    Evidence: .sisyphus/evidence/task-19-no-key-leak.txt
  ```

  **Commit**: YES
  - Message: `feat(tui): settings view with password change and connection info`
  - Files: `internal/tui/settings.go`, `internal/tui/settings_test.go`

- [ ] 20. **TUI + Services Wire-Up**

  **What to do**:
  - Create `internal/tui/model.go` — the root Bubble Tea model that composes all views and services
  - Wire all services (auth, crypto, storage, tor, transport, contacts, messages) into the TUI model
  - Implement `Init()`: return `tea.Batch` with Tor startup, message listener subscription
  - Implement `Update()`: route messages to active view, handle view switching, handle incoming Tor messages via `p.Send()`
  - Implement `View()`: compose header + active view + status bar using Lipgloss `JoinVertical`/`JoinHorizontal`
  - Handle `tea.WindowSizeMsg`: recalculate all sub-component sizes
  - Forward events to child models (textarea, viewport, textinput, list) and collect their commands
  - Handle graceful shutdown: `ctrl+c` → `tea.Quit`, but first save unsent messages, close connections
  - Wire `cmd/sote/main.go` to create model with all services, start HTTP server in background goroutine, start Tor message listener, run `tea.NewProgram(m, opts...)`

  **Must NOT do**:
  - A5: Do NOT block in Update() — all heavy operations must be `tea.Cmd`
  - Do NOT create global variables — all state in model struct

  **TDD**: `TestFullLoginFlow`, `TestIncomingMessageNotification`, `TestViewSwitchingIntegration`, `TestGracefulShutdown`

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: Complex integration — wiring 6 services into Bubble Tea event loop, requires careful orchestration

  **Parallelization**:
  - **Can Run In Parallel**: NO (depends on ALL previous tasks)
  - **Parallel Group**: Wave 5
  - **Blocks**: Tasks 21, 23
  - **Blocked By**: Tasks 1-19 (all)

  **References**:
  - `cmd/sote/main.go` — Entry point to complete
  - `internal/domain/interfaces.go` — All service interfaces to wire
  - `internal/tui/nav.go` — View switching in navigation system
  - `internal/tui/chat.go` — Chat view receives incoming messages
  - Bubble Tea send-msg example: `examples/send-msg/main.go` — `p.Send()` from goroutines
  - Bubble Tea tui-daemon-combo: `examples/tui-daemon-combo/main.go` — Dual mode pattern

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/tui/model_test.go` — `TestFullLoginFlow`: start → login screen → login → main menu
  - [ ] `internal/tui/model_test.go` — `TestIncomingMessageNotification`: `p.Send(IncomingMessageMsg{...})` → message appears in chat view
  - [ ] `internal/tui/model_test.go` — `TestViewSwitchingIntegration`: login → menu → contacts → chat → settings → menu
  - [ ] `internal/tui/model_test.go` — `TestGracefulShutdown`: Ctrl+C → tea.Quit, services closed, no goroutine leaks

  **QA Scenarios**:

  ```
  Scenario: End-to-end TUI flow
    Tool: Bash (go test with tea.WithInput)
    Steps:
      1. Start at login screen
      2. Register new user "alice" with password "secret123"
      3. Auto-redirect to main menu
      4. Navigate to Contacts (press 2)
      5. Navigate to Chat (press 1)
      6. Navigate to Settings (press 3)
      7. Back to Main Menu (Esc)
      8. Exit (press 5)
    Expected Result: All views render, no crashes, transitions smooth
    Failure Indicators: Any crash, nil pointer, view doesn't render, state corruption
    Evidence: .sisyphus/evidence/task-20-e2e-flow.txt
  ```

  **Commit**: YES
  - Message: `feat(tui): wire all services into Bubble Tea model with full event loop`
  - Files: `internal/tui/model.go`, `cmd/sote/main.go`, `internal/tui/model_test.go`

- [ ] 21. **Daemon Mode**

  **What to do**:
  - Implement `--daemon` flag in `cmd/sote/main.go`
  - When `--daemon` or `!term.IsTerminal(os.Stdout.Fd())`: use `tea.WithoutRenderer()` program option
  - In daemon mode: start HTTP server only (no TUI rendering), log to file, respond to API requests
  - Same HTTP endpoints as server (Task 12) work in daemon mode
  - Contact request handling: in daemon mode, auto-accept contact requests (or queue for later TUI review)
  - Print startup message: "SOTE daemon running on port {port}. Onion address: {addr}. PID: {pid}"

  **Must NOT do**:
  - Do NOT attempt to render TUI in daemon mode
  - Do NOT crash if no TTY attached

  **TDD**: `TestDaemonModeFlag`, `TestDaemonModeHTTPEndpoints`, `TestDaemonModeNoTUI`

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Simple mode switch — well-documented Bubble Tea pattern

  **Parallelization**:
  - **Can Run In Parallel**: NO (depends on Task 20)
  - **Parallel Group**: Wave 5
  - **Blocks**: Task 23
  - **Blocked By**: Tasks 12, 20

  **References**:
  - Bubble Tea tui-daemon-combo: `examples/tui-daemon-combo/main.go` — `tea.WithoutRenderer()`, `term.IsTerminal()`
  - `cmd/sote/main.go` — Entry point to modify for flag

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `cmd/sote/main_test.go` — `TestDaemonModeFlag`: `--daemon` sets daemon mode to true
  - [ ] `cmd/sote/main_test.go` — `TestDaemonModeHTTPEndpoints`: daemon mode → HTTP endpoints respond
  - [ ] `cmd/sote/main_test.go` — `TestDaemonModeNoTUI`: daemon mode → no terminal output (UI)

  **QA Scenarios**:

  ```
  Scenario: Daemon mode starts and responds to HTTP
    Tool: Bash
    Steps:
      1. Run: ./sote --daemon &
      2. sleep 2; curl -sk https://localhost:18080/register -d '{"username":"test","password":"secret123"}'
    Expected Result: Daemon starts, register endpoint returns 201
    Failure Indicators: Daemon crashes, port not listening, endpoint 404
    Evidence: .sisyphus/evidence/task-21-daemon.txt
  ```

  **Commit**: YES
  - Message: `feat(daemon): --daemon flag with headless mode and HTTP API`
  - Files: `cmd/sote/main.go`, `cmd/sote/main_test.go`

- [ ] 22. **Graceful Shutdown**

  **What to do**:
  - Implement `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` in `cmd/sote/main.go`
  - Pass shutdown context to all services via `tea.WithContext(ctx)`
  - On SIGTERM/SIGINT:
    1. Stop accepting new HTTP connections
    2. Drain existing HTTP connections (30s timeout via `srv.Shutdown(ctx)`)
    3. Close Tor hidden service (`tor.Close()`)
    4. Flush logs to file
    5. Checkpoint SQLite WAL (`PRAGMA wal_checkpoint(TRUNCATE)`)
    6. Close database connection
    7. Restore terminal (if TUI mode)
  - Print shutdown message: "SOTE stopped gracefully."
  - Exit code 0 on clean shutdown, non-zero on error

  **Must NOT do**:
  - Do NOT force-kill processes — always graceful shutdown
  - Do NOT leave orphaned Tor processes or DB locks

  **TDD**: `TestSIGTERMShutdown`, `TestDBClosedOnShutdown`, `TestTorClosedOnShutdown`

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: Coordinated shutdown across HTTP server, Tor, SQLite, TUI — multiple subsystems, ordering matters

  **Parallelization**:
  - **Can Run In Parallel**: NO (depends on Task 20)
  - **Parallel Group**: Wave 5
  - **Blocks**: Task 23
  - **Blocked By**: Tasks 8, 12, 20

  **References**:
  - Go signal: `https://pkg.go.dev/os/signal#NotifyContext` — Signal handling with context
  - Bubble Tea WithContext: `https://charm.land/bubbletea/v2#WithContext` — Context propagation
  - `net/http.Server.Shutdown`: `https://pkg.go.dev/net/http#Server.Shutdown` — Graceful HTTP shutdown
  - SQLite WAL checkpoint: `PRAGMA wal_checkpoint(TRUNCATE)` — Clean WAL on shutdown

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `cmd/sote/main_test.go` — `TestSIGTERMShutdown`: SIGTERM → clean exit code 0, all services closed
  - [ ] `cmd/sote/main_test.go` — `TestDBClosedOnShutdown`: after shutdown → DB file has no WAL files (.db-wal, .db-shm deleted)
  - [ ] `cmd/sote/main_test.go` — `TestTorClosedOnShutdown`: after shutdown → no running Tor processes

  **QA Scenarios**:

  ```
  Scenario: SIGTERM triggers clean shutdown
    Tool: Bash
    Steps:
      1. Run: ./sote --daemon &
      2. PID=$!
      3. sleep 2; kill -TERM $PID
      4. wait $PID; echo "Exit: $?"
    Expected Result: Exit code 0, "SOTE stopped gracefully." in logs, no orphaned processes
    Failure Indicators: Non-zero exit, Tor processes still running, DB WAL files remain
    Evidence: .sisyphus/evidence/task-22-graceful-shutdown.txt

  Scenario: Ctrl+C in TUI mode exits cleanly
    Tool: Bash (go test)
    Steps: Simulate Ctrl+C in TUI, verify terminal restored
    Expected Result: Terminal in cooked mode, cursor visible, no raw mode artifacts
    Failure Indicators: Terminal garbled, cursor hidden, need `reset` command
    Evidence: .sisyphus/evidence/task-22-ctrl-c-tui.txt
  ```

  **Commit**: YES
  - Message: `feat(shutdown): graceful shutdown with signal handling and resource cleanup`
  - Files: `cmd/sote/main.go`, `cmd/sote/main_test.go`

- [ ] 23. **Integration Tests**

  **What to do**:
  - Create `internal/integration/` package with end-to-end tests
  - `TestFullMessageRoundtrip`: two app instances, register both, add contacts, send message, receive, decrypt
  - `TestContactRequestFlow`: A sends contact request → B receives and accepts → both have each other as contacts
  - `TestDaemonMessageFlow`: daemon mode A sends message → daemon mode B receives it
  - `TestConcurrentMessages`: 10 concurrent sends, verify all received, no DB corruption
  - `TestPasswordChangeAndRelogin`: change password → re-login → decrypt old messages with new key
  - Use mock Tor or real Tor (with build tag `//go:build integration`)
  - Use in-memory SQLite for speed, real crypto
  - Run with `go test -race -count=1 -tags=integration ./internal/integration/`

  **Must NOT do**:
  - Do NOT skip race detector — MUST run with `-race`
  - Do NOT use real network for unit CI — mock Tor for fast tests

  **TDD**: Write tests that describe expected behavior, then verify against implementation

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: Complex multi-instance testing — requires careful test harness, service orchestration

  **Parallelization**:
  - **Can Run In Parallel**: NO (depends on ALL previous tasks)
  - **Parallel Group**: Wave 5
  - **Blocks**: Final Verification
  - **Blocked By**: Tasks 1-22 (all)

  **References**:
  - All `internal/*/` packages — Test against real implementations
  - `httptest.NewServer` — For HTTP handler testing without network
  - Go build tags: `https://pkg.go.dev/go/build#hdr-Build_Constraints` — For integration test isolation

  **Acceptance Criteria**:

  **TDD Tests**:
  - [ ] `internal/integration/integration_test.go` — `TestFullMessageRoundtrip`: A sends "Hello" → B receives → decrypt → "Hello"
  - [ ] `internal/integration/integration_test.go` — `TestContactRequestFlow`: full add + accept contact cycle
  - [ ] `internal/integration/integration_test.go` — `TestConcurrentMessages`: 10 concurrent sends → all 10 received
  - [ ] `internal/integration/integration_test.go` — `TestPasswordChangeAndRelogin`: change pass → login → decrypt old msgs
  - [ ] `internal/integration/integration_test.go` — `TestDaemonMessageFlow`: two daemon instances communicate

  **QA Scenarios**:

  ```
  Scenario: Full integration — two users complete message roundtrip
    Tool: Bash
    Steps:
      1. go test -race -count=1 -run TestFullMessageRoundtrip -v ./internal/integration/
    Expected Result: Test PASS, zero race conditions. Message "Hello World" survives encrypt→send→receive→decrypt
    Failure Indicators: Test failure, race detector warning, decryption error
    Evidence: .sisyphus/evidence/task-23-integration.txt
  ```

  **Commit**: YES
  - Message: `test(integration): end-to-end message roundtrip and contact flow tests`
  - Files: `internal/integration/integration_test.go`

---

## Final Verification Wave

> 4 review agents run in PARALLEL. ALL must APPROVE. Present consolidated results to user and get explicit "okay" before completing. Do NOT auto-proceed after verification.

- [ ] F1. **Plan Compliance Audit** — `oracle`
  Read the plan end-to-end. For each "Must Have": verify implementation exists. For each "Must NOT Have": search codebase for forbidden patterns — reject with file:line if found. Check evidence files exist in `.sisyphus/evidence/`. Compare deliverables against plan.
  Output: `Must Have [N/N] | Must NOT Have [N/N] | Tasks [N/N] | VERDICT: APPROVE/REJECT`

- [ ] F2. **Code Quality Review** — `unspecified-high`
  Run `go vet ./...` + `gofmt -d .` + `go test -race -count=1 ./...`. Review all changed files for: `as any`/`@ts-ignore` equivalents, empty catches, `fmt.Println` in prod, commented-out code, unused imports. Check AI slop: excessive comments, over-abstraction, generic names (data/result/item/temp). Check for `log.Fatal` in `internal/`.
  Output: `Build [PASS/FAIL] | Vet [PASS/FAIL] | Tests [N pass/N fail] | Files [N clean/N issues] | VERDICT`

- [ ] F3. **Real Manual QA** — `unspecified-high`
  Start from clean state. Execute EVERY QA scenario from EVERY task — follow exact steps, capture evidence. Test cross-task integration (TUI → HTTP → DB → Tor). Test edge cases: empty state, invalid input, rapid actions, resize. Save to `.sisyphus/evidence/final-qa/`.
  Output: `Scenarios [N/N pass] | Integration [N/N] | Edge Cases [N tested] | VERDICT`

- [ ] F4. **Scope Fidelity Check** — `deep`
  For each task: read "What to do", read actual diff. Verify 1:1 — everything in spec was built, nothing beyond spec was built. Check "Must NOT do" compliance. Detect cross-task contamination. Flag unaccounted changes.
  Output: `Tasks [N/N compliant] | Contamination [CLEAN/N issues] | Unaccounted [CLEAN/N files] | VERDICT`

---

## Commit Strategy

- **Wave 1**: Commit after EACH task (Tasks 1-5) — foundational, each independently verifiable
- **Wave 2**: Commit after EACH task (Tasks 6-10) — core services, each independently testable
- **Wave 3**: Commit after EACH task (Tasks 11-13) — transport layer
- **Wave 4**: Commit after EACH task (Tasks 14-19) — TUI views, each independently renderable
- **Wave 5**: Commit after EACH task (Tasks 20-23) — integration, each is a milestone
- **Final**: Commit after ALL F1-F4 approval (squash if needed)

**Pre-commit checks**: `go test -race -count=1 ./...` must pass before every commit.

---

## Success Criteria

### Verification Commands
```bash
# Build
go build -o sote ./cmd/sote

# All tests with race detector
go test -race -count=1 ./...

# No InsecureSkipVerify anywhere (Guardrail S1)
grep -r "InsecureSkipVerify" internal/ cmd/ && echo "FAIL: InsecureSkipVerify found" || echo "PASS"

# No SHA-256 in password hashing (Guardrail S2)
grep -r "sha256" internal/crypto/ internal/auth/ | grep -v "_test.go" | grep -v "pbkdf2" && echo "FAIL" || echo "PASS"

# No log.Fatal in library code (Guardrail S6)
grep -r "log\.Fatal" internal/ && echo "FAIL: log.Fatal in internal/" || echo "PASS"

# No fmt.Println in production code
grep -r "fmt\.Println\|fmt\.Printf" internal/ | grep -v "_test.go" && echo "WARN: debug prints" || echo "PASS"

# WAL mode check (if DB exists)
sqlite3 ~/.sote/data.db "PRAGMA journal_mode;" 2>/dev/null | grep -q "wal" && echo "WAL: PASS" || echo "WAL: N/A"
```

### Final Checklist
- [ ] All 23 tasks completed with passing tests
- [ ] All "Must Have" (bcrypt, PBKDF2, TLS pinning, cretz/bine, WAL, slog, graceful shutdown, Bubble Tea TUI) present
- [ ] All "Must NOT Have" (InsecureSkipVerify, SHA-256 hashing, log.Fatal, fmt.Println, exec.Command("tor"), animations, Docker, QR codes, multi-account) absent
- [ ] All 4 Final Verification agents (F1-F4) return APPROVE
- [ ] Evidence files exist in `.sisyphus/evidence/` for all QA scenarios
- [ ] User explicitly approves completion
