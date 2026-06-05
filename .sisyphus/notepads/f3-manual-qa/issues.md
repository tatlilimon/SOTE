# F3: Real Manual QA — Issues

## Guardrail: InsecureSkipVerify
- `internal/transport/client.go:164`: `InsecureSkipVerify: true, //nolint:gosec // fingerprint is trust anchor`
- Strictly FAILS the guardrail grep check (should be empty)
- Design justification: Tor hidden services use onion address fingerprints as trust anchors instead of traditional certificate chain verification. This is the standard approach for Tor .onion services.
- Mitigation: The `//nolint:gosec` comment and TLS fingerprint verification provide compensating controls.
