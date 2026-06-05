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
