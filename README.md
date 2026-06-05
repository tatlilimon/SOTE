# SOTE
A messaging client with end-to-end encryption, no central server, self-hosted, communicates over the Tor network and can't be censored. 

Unlike other messaging apps, the app doesn't need any centralized server and users simply connect and communicate with each other. This connection takes place entirely on the tor network and is protected by end-to-end GPG asymmetric encryption.
The application is written entirely in Go with a Bubble Tea terminal UI (TUI). No graphical interface required — everything runs in the terminal.
Only a username and password are needed to create an account. Private keys are encrypted with AES-256-GCM using a PBKDF2-derived key.
Users communicate by sharing their Tor hidden service address (`.onion` domain), displayed in the settings view.
<hr>

## Dependencies
* Linux/amd64
* Tor >= 0.4.8.11
* Golang >= 1.21.10
<hr>

## How to Run
  go build -o sote ./cmd/sote
  ./sote
<hr>

### TODOS

- [x] ~~Dynamic port allocation for Tor SOCKS/Control~~ — fixed (auto-increments if occupied)
- [x] ~~Tor process lifecycle~~ — fixed (cretz/bine with StopProcessOnClose, no orphaned processes)
- [x] ~~No daemon mode~~ — removed (TUI-only single binary)

- [ ] Add tor bridges implementation for users who cannot access Tor without bridges in censored regions.

## Feel Free to Contribute This Project!
You can help me to developing this app by opening a pull request or issue.
> The project is licensed under GPL 3. This means that you can copy this software and use it anywhere you want, but there is only one condition; you must release it under the GPL 3 license. So if you are going to use this code elsewhere, that project must also be open source.