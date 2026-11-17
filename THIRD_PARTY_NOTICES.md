# Third-Party Licenses & Legal Compliance Notices

## Combined Work License (`GPL-3.0-only`)

`whatsapp-mcp` statically links `go.mau.fi/whatsmeow` (`MPL-2.0`) together with its cryptographic Signal Protocol dependency `go.mau.fi/libsignal` (`GPL-3.0`).

Under Section 3.3 of the Mozilla Public License 2.0 (secondary license compatibility with GNU GPL v3.0) and the copyleft terms of the GNU General Public License v3.0:

1. **Source & Binary Redistribution**: Any compiled binary distribution of `whatsapp-mcp` constitutes a combined work governed by **GNU General Public License v3.0 (`GPL-3.0`)**. Closed-source or proprietary binary redistribution without corresponding source code is **not permitted**.
2. **Private / Internal Organizational Use**: Running `whatsapp-mcp` privately or commercially within your own infrastructure without distributing modified binaries to external third parties does not trigger external source disclosure obligations under GPL-3.0.

---

## Direct & Transitive Dependency License Inventory

| Package | License | Role |
|---|---|---|
| `go.mau.fi/whatsmeow` | `MPL-2.0` | WhatsApp Web Multi-Device client protocol |
| `go.mau.fi/util` | `MPL-2.0` | Database dialect & utility helpers |
| `go.mau.fi/libsignal` | `GPL-3.0` | Signal end-to-end encryption protocol |
| `github.com/modelcontextprotocol/go-sdk` | `Apache-2.0` / `MIT` | Official Go SDK for Model Context Protocol |
| `modernc.org/sqlite` | `BSD-3-Clause` | Pure-Go SQLite engine (CGO-free) |
| `github.com/skip2/go-qrcode` | `MIT` | QR code PNG & terminal rendering |
| `google.golang.org/protobuf` | `BSD-3-Clause` | Protocol Buffers runtime |
| `github.com/coder/websocket` | `ISC` | WebSocket transport |
| `github.com/rs/zerolog` | `MIT` | Structured logging |
| `github.com/beeper/argo-go` | `MIT` | Binary wire serialization |

---

## WhatsApp Terms of Service & Unofficial Protocol Disclaimer

This project is an independent integration built on `whatsmeow` (WhatsApp Web Multi-Device client protocol) and is **not affiliated with, endorsed by, or supported by WhatsApp LLC or Meta Platforms, Inc.**

- Use only for personal productivity or explicitly authorized human-in-the-loop workflows.
- Automated bulk messaging, unsolicited outreach, or high-frequency scraping violates WhatsApp Terms of Service and may result in permanent phone number suspension.
- For high-volume automated commercial messaging, use the official Meta WhatsApp Business Cloud API instead.
