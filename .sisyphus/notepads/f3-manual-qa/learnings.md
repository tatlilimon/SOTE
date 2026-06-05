# F3: Real Manual QA — Learnings

## Test Results
- **Unit tests**: 12/12 packages PASS (including `internal/tor` which ran successfully without real Tor)
- **Integration tests**: PASS (30.2s)
- **Binary**: Builds (22MB, ELF x86-64), `--help` works, `--daemon` starts within 3s on port 18080
- **Race detector**: Clean — no race conditions detected across all tests

## Guardrails
- `log.Fatal` in `internal/`: CLEAN (no hits)
- `sha256` in `crypto.go`: Only used via `pbkdf2.Key()` for KDF (correct) + standard library import line (false positive of grep)
- `InsecureSkipVerify: true` in `internal/transport/client.go:164`: intentional for Tor fingerprint-based trust anchor with `//nolint:gosec` justification
