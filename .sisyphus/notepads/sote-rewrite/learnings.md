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
