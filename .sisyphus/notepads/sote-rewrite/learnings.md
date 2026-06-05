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
