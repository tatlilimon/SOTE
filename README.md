![sote-logo](https://github.com/tatlilimon/SOTE/assets/43828285/4284dfcc-a4e7-4bf6-ae0e-b243a10439a0)

# SOTE
A messaging client with end-to-end encryption, no central server, self-hosted, communicates over the Tor network and can't be censored.

There is no server in the middle: every installation publishes its own Tor onion service, and peers connect to each other directly through Tor. Messages are encrypted to the receiver's GPG key and signed by the sender's.
SOTE is written in Go. It has a terminal interface (Bubble Tea) and an optional desktop interface (Fyne, `--gui`).
Only a username and password are needed to create an account. The GPG private key is stored encrypted with AES-256-GCM under a key derived from the password (PBKDF2-HMAC-SHA256, 600,000 iterations).
Users communicate by sharing their onion address (`.onion`), shown in Settings.
<hr>

## Dependencies
* Linux/amd64
* Tor >= 0.4.8 (`dnf install tor` / `apt install tor`)
* Go >= 1.25 to build
* For the desktop interface only, the X11/OpenGL development libraries:
  Fedora `libX11-devel libXcursor-devel libXrandr-devel libXinerama-devel libXi-devel libXxf86vm-devel mesa-libGL-devel`,
  Debian/Ubuntu `xorg-dev libgl1-mesa-dev`
<hr>

## How to Run
    go build -o sote ./cmd/sote               # terminal + desktop interface
    go build -tags nogui -o sote ./cmd/sote   # terminal only, no X11 libraries needed
    ./sote          # terminal interface
    ./sote --gui    # desktop interface

1. Create your account. SOTE supports one account per installation (the installation has one onion address).
2. Wait until the status bar shows Tor as connected, then open Settings to see your onion address and share it.
3. In Contacts, press `a` and paste a friend's onion address. SOTE fetches the identity served at that address, adds it, and sends your contact request.
4. Your friend sees the request (Contacts → `r`) and accepts it with `a`. SOTE checks, over Tor, that your onion address really serves the key the request carried.
5. Chat (`1`): `←`/`→` switch contact, Enter sends. A sent message shows `…` until it is delivered, then `✓`; `✗ not accepted` means the receiver refused it (for example, you are not in their contacts). A received message whose signature could not be checked is marked `⚠`.

Both sides do not need to be online at the same time to *write*: messages wait in an outbox and are retried until the receiver's onion service is reachable.
<hr>

## Files
Everything lives in `~/.sote` (owner-only):

| File | What it is |
|---|---|
| `config.yaml` | settings, including bridges |
| `data.db` | account (bcrypt hash, encrypted private key), contacts, encrypted messages, outbox |
| `onion_v3.key` | the onion service key. **Back it up**: your onion address is derived from it; losing it gives you a new address and your contacts can no longer reach you |
| `tor-data/`, `certs/` | Tor state, TLS certificate of the onion endpoint |
| `sote.log` | log (no message contents or passwords; it does record which addresses you talk to) |
<hr>

## Tor bridges
If Tor is blocked on your network, get bridges from https://bridges.torproject.org (or by e-mail/Telegram as described there) and add them in Settings (`b` in the terminal interface, "Tor Bridges" in the desktop interface), or in `config.yaml`:

```yaml
use_bridges: true
bridges:
  - obfs4 192.0.2.10:443 0123456789ABCDEF0123456789ABCDEF01234567 cert=... iat-mode=0
client_transport_plugins:
  - obfs4 exec /usr/bin/lyrebird
```

obfs4, webtunnel and snowflake bridges need a pluggable transport binary (`lyrebird` / `obfs4proxy`, `snowflake-client`, `webtunnel-client`) and a matching `client_transport_plugins` line. Lines are validated when saved; new settings apply the next time SOTE starts. `SOTE_USE_BRIDGES=0/1` overrides `use_bridges`.
<hr>

## Security notes
* The onion address is the identity: Tor only connects to the holder of the address's key, so the key fetched from an address is that address's key.
* Contact requests are signed by the key they introduce and checked against the requester's own onion identity before they can be accepted.
* Every message carries a detached signature over its ID, both onion addresses, its timestamp and its payload; it is checked against the stored contact key before anything is stored, and a message ID is stored only once (replays are acknowledged and dropped).
* The peer server listens on 127.0.0.1 only and serves nothing but `/identity`, `/receive-contact-request` and `/receive-message`, with size limits and timeouts.
* TLS inside Tor uses a self-signed certificate; the client skips certificate verification only for `.onion` hosts reached through Tor, where the address itself authenticates the peer.
* Not provided: forward secrecy (messages are GPG/RSA-2048), hiding the message metadata stored on your own disk, multiple devices per account.
<hr>

## Tests
    go test -tags nogui ./...        # everything except the desktop interface
    go test -race -tags nogui ./...
    go test ./...                    # including the desktop interface (needs the X11 libraries)
    SOTE_TOR_TESTS=1 go test ./internal/tor/   # starts a real Tor process; needs Internet, takes minutes

`internal/integration` runs two and three complete nodes against each other through a fake Tor (contact requests, acceptance, messages both ways, replays, forged senders, offline peers, password change).
<hr>

### TODOS

- [x] ~~Dynamic port allocation for Tor SOCKS/Control~~ — fixed (auto-increments if occupied)
- [x] ~~Tor process lifecycle~~ — fixed (cretz/bine with StopProcessOnClose, no orphaned processes)
- [x] ~~No daemon mode~~ — removed (single binary)
- [x] ~~Add tor bridges implementation for users who cannot access Tor without bridges in censored regions.~~ — `use_bridges`, `bridges`, `client_transport_plugins`

## Feel Free to Contribute This Project!
You can help me to developing this app by opening a pull request or issue.
> The project is licensed under GPL 3. This means that you can copy this software and use it anywhere you want, but there is only one condition; you must release it under the GPL 3 license. So if you are going to use this code elsewhere, that project must also be open source.
