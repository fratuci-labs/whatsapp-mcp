# Security & Data Privacy Policy

## Threat Model & Local Safeguards

`whatsapp-mcp` handles end-to-end encrypted WhatsApp sessions (`session.db`), synced message history (`messages.db`), and decrypted media attachments (`media/`).

1. **Authenticated Local IPC (`daemon.json` + `Bearer` Token)**:
   - Every daemon startup generates a fresh 256-bit random token (`crypto/rand`) persisted in `data/daemon.json` with `0600` permissions.
   - All HTTP requests between the `stdio` MCP process and the background daemon require `Authorization: Bearer <token>` verified via constant-time comparison (`crypto/subtle.ConstantTimeCompare`).
   - On macOS and Linux, IPC binds to a `0600` Unix Domain Socket (`data/daemon.sock` with `umask 0077`), falling back to `127.0.0.1` ephemeral loopback TCP on Windows or paths exceeding `SUN_LEN`.

2. **Read-Only Enforcement (`WHATSAPP_MCP_READ_ONLY=true`)**:
   - When enabled, `wa_send_message` is omitted from the MCP server's registered toolset **and** the daemon rejects `/send` calls with `HTTP 403 Forbidden`.

3. **Sensitive Files (Never Commit)**:
   - `session.db*` contains cryptographic device keys equivalent to an active WhatsApp session.
   - `messages.db*` and `media/*` contain personal communications (LGPD / GDPR sensitive personal data).
   - Keep `WHATSAPP_MCP_HOME` on encrypted local storage (FileVault / BitLocker / LUKS).

## Reporting Vulnerabilities

Report security issues privately to the `fratuci-labs` maintainers via GitHub Security Advisories on `fratuci-labs/whatsapp-mcp`.
