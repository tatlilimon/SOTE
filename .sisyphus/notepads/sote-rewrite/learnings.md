# Learnings - SOTE Rewrite Task 1

## Date: 2026-06-05

## Key Decisions
1. **Go version**: System has Go 1.25.10, go.mod tracks minimum 1.25.0 (auto-bumped by toolchain). Task spec called for 1.22 but `go mod tidy` enforces minimum from deps.
2. **charm.land packages**: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `charm.land/bubbles/v2` all resolve correctly (v2.0.7, v2.0.3, v2.1.0). `go mod tidy` removes them since no code imports them yet — will be re-added in Task 20-22.
3. **Reference files**: `user/user.go` and `db/db.go` preserved but modified to remove broken imports (`sote/tor`, `mattn/go-sqlite3`). `user.go` had `GenerateOnionAddress()` call replaced with placeholders since `tor/tor.go` was deleted.
4. **modernc.org/sqlite**: v1.51.0 resolved, pure Go (no CGo). Import switched in `db/db.go` from `mattn/go-sqlite3`.
5. **TDD workflow**: Test written first (RED — wouldn't compile without main.go), then main.go created (GREEN). Three tests: daemon flag parsing, help flag, default daemon=false.

## Successful Patterns
- `flag.Bool()` with separate FlagSet in tests works for testing flag parsing
- `go mod tidy` is the authority — don't fight it, let it determine used dependencies
- Statically linked binary (2.4MB) — good for deployment

## Issues Encountered
- TestMainHelpFlag initially expected `NoError` but `flag.ContinueOnError` + `--help` returns `flag.ErrHelp`. Fixed with `assert.ErrorIs`.
- `go mod tidy` repeatedly bumped Go version to 1.25.0. `go mod edit -go=1.22` overridden by tidy. Decision: accept toolchain version.

## Task 2: Configuration System (2026-06-05)

### Key Decisions
1. **yaml.v3 was already transitive**: `gopkg.in/yaml.v3` existed as indirect via gopenpgp. `go mod tidy` promoted it to direct after import.
2. **TDD order**: Wrote all tests first (RED — 8 test functions, build failure). Then implemented in one pass (GREEN — all 8 passed first try).
3. **validateConfig is unexported**: Test file is in `package config` so can call `validateConfig(cfg)` directly. No need for exported Validate().
4. **Env overrides use LookupEnv**: `os.LookupEnv()` distinguishes "unset" from "set to empty" — important so we don't zero-out ports on empty string.
5. **Partial YAML files work**: yaml.Unmarshal only overwrites fields present in the file. Start from DefaultConfig() so missing fields keep defaults.

### Successful Patterns
- `os.LookupEnv()` with strconv.Atoi for port env vars (don't overwrite on invalid env values)
- Defaults from `DefaultConfig()`, YAML file layers on top, env vars layer on top of that
- TempDir in tests via `t.TempDir()` — automatic cleanup, no risk of writing to real ~/.sote
- `unsetSoteEnv()` helper with `defer` in tests to avoid cross-test pollution

### Issues Encountered
- None. Implementation passed 8/8 tests with race detector on first run.

## Task 3: Structured Logging Setup (2026-06-05)

### Key Decisions
1. **Redaction via custom slog.Handler**: Wrapped the base handler in a `redactingHandler` that iterates `r.Attrs()` and replaces values for sensitive keys with `[REDACTED]`. This is transparent — callers just pass `"password", value` and redaction happens automatically. No custom `SanitizeArgs()` needed.
2. **Sensitive keys**: Case-insensitive match on: `password`, `passwd`, `secret`, `private_key`, `privkey`, `token`, `api_key`, `apikey`, `auth`, `authorization`.
3. **File writer with stderr fallback**: If `logPath` is empty → stdout. If file can't be opened → stderr. Parent directories created via `os.MkdirAll`. File opened with `O_CREATE|O_WRONLY|O_APPEND` for append mode.
4. **Level mapping**: `parseLevel()` maps strings (debug/info/warn/error) to `slog.LevelDebug/Info/Warn/Error`. Unknown defaults to `slog.LevelInfo`.
5. **Format default**: Unknown format strings default to "text" (key=value style).

### Successful Patterns
- **Test isolation**: `saveAndRestoreLogger(t)` saves/restores both the package-level `logger` var and `slog.Default()` via `t.Cleanup`. Every test gets a fresh logger.
- **Temp files**: `t.TempDir()` + file-read assertions — no writes to real filesystem paths.
- **18 tests cover**: 5 level tests, 3 format tests, 3 file I/O tests, 6 redaction tests, 2 edge case tests.
- **Redaction covers text AND JSON**: Both formats tested for secret leakage.

### Issues Encountered
- None. Implementation passed 18/18 tests with `-race` on first run.
- `gopls` not installed on this system — `go test -race` provides stronger verification anyway (compilation + race detection).

## Task 5: Domain Types + Interfaces (2026-06-05)

### Key Decisions
1. **Domain package is leaf-level**: No imports from other `sote/internal/*` packages. Keeps domain pure.
2. **JSON tags follow snake_case**: `json:"username"`, `json:"password_hash"`, `json:"encrypted_private_key"`, etc. Consistent with Go conventions for JSON APIs.
3. **RawPassword uses `json:"-"`**: Prevents accidental serialization of cleartext passwords.
4. **Message.EncryptedContent is `[]byte`**: Binary-safe; JSON marshals to base64 automatically.
5. **Compile-time interface checks**: `var _ AuthService = (*authServiceStub)(nil)` in test verifies stubs satisfy interfaces without runtime cost.

### Successful Patterns
- TDD flow: wrote `types_test.go` first (RED — wouldn't compile), then `types.go` and `interfaces.go` (GREEN — all 4 tests pass).
- `TestUserJSONRoundtrip`: marshals User, verifies RawPassword absent from JSON via `json:"-"`, then roundtrips back and asserts field equality.
- `TestInterfacesCompile`: compile-time assertions that stub types satisfy all 5 interfaces.
- Interface methods all follow `context.Context` as first param, returning `error` last.

### Issues Encountered
- `gopls` not installed on system — LSP diagnostics unavailable. Build+test verification used instead (`go test -race -count=1 ./internal/domain/`).

## Task 4: Database Layer (2026-06-05)

### Key Decisions
1. **Standalone functions**: Functions take `*sql.DB` as first param (not methods on a Store struct). Simplifies test injection with `:memory:` DBs and follows `Initialize(dbPath string) (*sql.DB, error)` signature as specified.
2. **WAL mode via URI DSN**: `file:path?cache=shared&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate`. In-memory databases use `memory` journal mode regardless — WAL only applies to file-based DBs.
3. **Migration system**: `schema_version` table with version column + applied_at timestamp. `migrate()` checks MAX(version), runs v1 if < 1. Extensible for future versions.
4. **SaveContact fix**: Old code returned `nil` on `sql.ErrNoRows` (line 113), meaning new contacts were NEVER saved. Fix: explicit `contactExists()` check returns `ErrContactExists`. Self-add returns `ErrSelfAdd`.
5. **GetAllContacts owner filter**: Old code did `SELECT ... FROM contacts` without WHERE. Fix: prepared statement with `WHERE username = ?` parameter.
6. **MaxOpenConns(1)**: Set for writer safety as specified.
7. **Prepared statements**: All queries use `db.Prepare()` with `defer stmt.Close()`. No global cached statements — each call prepares anew for thread safety with single-conn pool.

### Successful Patterns
- TDD: wrote 11 test functions first (build failure), then implementation (all 11 GREEN first run except WAL mode).
- `t.TempDir()` + file-based DB for WAL test since in-memory SQLite uses "memory" journal mode.
- `t.Cleanup(func() { db.Close() })` ensures test DBs are always closed.
- `assert.ErrorIs(t, err, sql.ErrNoRows)` for not-found checks.

### Issues Encountered
- In-memory SQLite uses `memory` journal mode, not `wal`. Fixed TestWALMode to use file-based DB.
- One WAL test fix needed; all other tests passed first try with `-race`.

## Task 8: Tor Service (2026-06-05)

### Key Decisions
1. **cretz/bine v0.2.0**: Added via `go get`. Uses bine's `tor.Start` for process lifecycle and `tor.Listen` for v3 hidden services. No os/exec calls.
2. **ControlPort auto-management**: Bine has a race condition when ControlPort is explicitly specified in StartConf — it dials immediately without retry and the Tor process may not be listening yet. Fix: let bine auto-pick the control port (ControlPort=0 in StartConf). Bine creates a temp file, passes `--ControlPort auto --ControlPortWriteToFile <file>`, and retries reading the file 10 times with 200ms delays.
3. **Dynamic SOCKS port allocation**: `findAvailablePort()` tries the configured port, then increments up to 100 times. Uses `net.Listen("tcp", ...)` as a probe — there's a TOCTOU window but it's sufficient in practice. Verified by TestDynamicPortAllocation (occupied port 9070, Tor auto-incremented).
4. **Onion service close order**: Must close the `OnionService` before calling `tor.Close()`. The onion service sends a DEL_ONION command to the controller, which requires the control connection to still be alive.
5. **New() signature**: Takes `(dataDir, listenPort, socksPort int)` — no controlPort parameter since bine manages it internally.

### Successful Patterns
- Test pattern: use `torAvailable()` check → `t.Skip()` if Tor not installed
- Test pattern: use `t.TempDir()` for Tor data directory (auto-cleanup)
- Test pattern: use `context.WithTimeout(..., 3*time.Minute)` to prevent hanging
- **7/8 tests pass**, 1 skipped (TorNotFound — only runs when Tor is not installed)
- Race detector clean on all tests (57.58s total)

### Issues Encountered
- **Bine ControlPort race condition**: When `StartConf.ControlPort` is non-zero, bine skips retry logic and immediately dials. Tor process may not be ready → "connection refused". Workaround: don't set ControlPort in StartConf, let bine auto-manage.
- **gopls not installed** — verification via `go build` + `go vet` + `go test -race` instead.

## Task 10: Message Service (2026-06-05)

### Key Decisions
1. **1-byte encryption prefix**: Stored message blobs prefixed with `0x01` (GPG) or `0x02` (AES) so `FetchMessages` can select the correct decryption path without a schema change.
2. **Dual-row SendMessage**: Sender saves both a GPG-encrypted copy (for receiver) and an AES-encrypted copy (for own sent-messages view). Both rows use same sender/receiver but different prefixes.
3. **`storage.GetUser` in FetchMessages**: Needed to retrieve the caller's encrypted private key for GPG decryption of received messages. `storage.GetContactByUsername` used in SendMessage for receiver's public key.
4. **64KB size limit**: Checked with `len([]byte(plaintext)) > MaxMessageSize` — returns `ErrMessageTooLarge`.
5. **Transport deferred**: `TODO(Task 13)` comment marks where transport delivery will go. Current `_ = gpgCipher` to avoid unused variable.
6. **Mock crypto in tests**: Real crypto primitives (AES-256-GCM, GPG via gopenpgp) used in test mock — enables true roundtrip verification of encrypt/decrypt.

### Successful Patterns
- TDD: wrote test file first, then implementation. 8 test functions covering send, receive, fetch, size limit, roundtrip, empty messages, invalid prefix, retry.
- `assert.ErrorIs(t, err, ErrMessageTooLarge)` for sentinel error checking.
- `t.Helper()` on `setupTestDB`, `seedUser`, `seedContact` — clean test stack traces.
- All tests pass with `-race -count=1`.

### Issues Encountered
- `seedUser` called twice for same username in `TestMessageTooLarge` — duplicate UNIQUE constraint. Fixed by reusing the return value.
- `assert.Equal` type mismatch: `encTypeGPG` (int constant) vs `row.Message[0]` (uint8). Fixed with `uint8(encTypeGPG)` cast.

## Task 9: Contact Service (2026-06-05)

### Key Decisions
1. **Service structure**: `Service` struct holds `*sql.DB` — no additional state needed. All operations delegate directly to `storage.*` functions.
2. **AcceptContactRequest returns `*storage.User`**: Returns the accepting user's identity (public key + onion address) so the transport layer can send it back to the requester for mutual handshake. Not `*domain.User` — storage.User has the full DB row which may be useful for transport.
3. **Error wrapping convention**: All storage errors wrapped with `fmt.Errorf("operation: %w", err)` — preserves error chain for `errors.Is()` checks (e.g., `storage.ErrContactExists`, `storage.ErrSelfAdd`).
4. **Test setup**: `setupService(t)` helper creates in-memory SQLite DB, initializes schema, saves a test user "alice", returns Service + cleanup. Tests never touch real filesystem.

### Successful Patterns
- `storage.Initialize(":memory:")` + `storage.SaveUser()` in test helper — one setup, reused across all tests.
- `require.NoError` for setup assertions (fatal), `assert.*` for test assertions (non-fatal).
- `assert.ErrorIs` verifies specific sentinel errors through wrapping chain.

### Issues Encountered
- None. All 6 tests passed first run with `-race`.

## Task 6: Crypto Service (2026-06-05)

### Key Decisions
1. **bcrypt cost 12**: Minimum required by spec. Each HashPassword call takes ~4.8s — acceptable for registration/login since they're infrequent operations.
2. **PBKDF2 binary format**: salt(32 bytes) | nonce(12 bytes) | ciphertext. Salt prepended to ciphertext so DecryptAES is self-contained — no separate salt storage needed.
3. **GPG key name**: `sote-user` — same as original `crypto.GenerateKey(username, "", "rsa", 2048)` but with fixed name since GenerateKeyPair doesn't take a username param.
4. **Local CryptoService interface**: Defined in `crypto.go` as a local alias of `domain.CryptoService` to avoid circular imports between `internal/crypto` and `internal/domain`. Compile-time check via `var _ CryptoService = (*Service)(nil)`.
5. **`golang.org/x/crypto` promoted**: Was indirect via gopenpgp. `go mod tidy` promoted to direct after bcrypt/pbkdf2 imports added.
6. **Service struct with no fields**: Stateless service — all cryptographic state is carried in the parameters (salts, keys, ciphertexts). No mutable fields needed.

### Successful Patterns
- TDD: wrote 12 tests first (build failure — no implementation), then implemented in one pass (all 12 GREEN first run with `-race`).
- Test coverage: HashPassword (unique salts, no raw password leakage), VerifyPassword (correct passes, wrong fails), AES roundtrip (encrypt-then-decrypt), AES wrong password (must fail), AES empty data (edge case), AES too-short input (defensive), GPG full flow (generate → encrypt → decrypt via AES-encrypted private key), GPG bad key input (fail fast), GPG wrong passphrase (fail fast), GenerateKeyPair uniqueness, interface compliance.
- `TestGPGEncryptDecrypt` simulates the full storage flow: generate keys → AES-encrypt private key → encrypt message with public key → decrypt message using AES-decrypted private key.
- bcrypt tests verify hash uniqueness (random salts) and that hashes don't contain the raw password.

### Issues Encountered
- None. Implementation passed 12/12 tests with `-race` on first run.
- `gopls` still not installed — `go test -race` provides build + race verification.

## Task 7: Auth Service (2026-06-05)

### Key Decisions
1. **Crypto inline, not separate service**: The task spec said "Implement crypto inline or import Task 6's implementation." Since `internal/crypto/crypto.go` only contains reference comments (no real code), all crypto primitives were implemented directly in the auth package: bcrypt hashing, AES-256-GCM encryption with PBKDF2 key derivation, and GPG key generation via ProtonMail/gopenpgp.
2. **AES wire format**: `salt (16 bytes) || nonce (12 bytes) || ciphertext+tag`. Salt is random, PBKDF2 with 100K iterations derives a 32-byte AES-256 key, and GCM provides authenticated encryption.
3. **Storage functions take `*sql.DB` not `context.Context`**: `storage.SaveUser` and `storage.GetUser` use `*sql.DB` directly, so `AuthService` stores a `*sql.DB` handle. The `context.Context` parameter in the interface is accepted but unused for now — future use for request-scoped cancellation.
4. **Duplicate detection via string matching**: `storage.SaveUser` wraps SQLite errors. Duplicate username triggers "UNIQUE constraint failed" — checked with `strings.Contains`. Not ideal but pragmatic given no structured error from modernc.org/sqlite.
5. **GPG key generation**: `crypto.GenerateKey("sote", username+"@sote.local", "rsa", 2048)` — armored keys stored as `[]byte`.
6. **Tor onion placeholder**: Empty string (`""`) for both onion address and torrc path. Real Tor integration in Task 8.

### Successful Patterns
- In-memory SQLite (`:memory:`) with `storage.Initialize` for test isolation
- `t.Cleanup(func() { db.Close() })` for DB cleanup
- `decryptAES` added for testing key roundtrip verification (not required by the auth service interface)
- `assert.ErrorIs` for sentinel error checking
- Compile-time interface check: `var _ domain.AuthService = (*AuthService)(nil)`

### Test Coverage (10 tests, all GREEN)
- TestRegisterValid: full registration + key roundtrip verification
- TestRegisterDuplicate: double registration → ErrDuplicateUser
- TestRegisterWeakPassword: 7-char password → ErrWeakPassword
- TestRegisterInvalidUsername: 7 sub-cases (short, long, dash, space, dot, empty, special chars)
- TestLoginValid: register + login, verify all fields match
- TestLoginWrongPassword: wrong password → ErrInvalidCredentials
- TestLoginNonExistent: non-existent user → ErrInvalidCredentials
- TestLoginCaseSensitiveUsername: "Frank" vs "frank" → ErrInvalidCredentials
- TestAESRoundtrip: encrypt then decrypt → original data
- TestAESWrongPassphrase: wrong passphrase → decrypt error

### Issues Encountered
- None. Implementation passed 10/10 tests with `-race` on first run.

### Dependencies Added
- `golang.org/x/crypto/bcrypt` — bcrypt password hashing
- `golang.org/x/crypto/pbkdf2` — AES key derivation
- `golang.org/x/crypto` already existed as indirect dependency
- `github.com/ProtonMail/gopenpgp/v2` already existed as direct dependency
