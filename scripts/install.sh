#!/usr/bin/env bash
set -euo pipefail

REPO="${WHATSAPP_MCP_REPO:-fratuci-labs/whatsapp-mcp}"
INSTALL_DIR="${WHATSAPP_MCP_INSTALL_DIR:-$HOME/.local/bin}"
BIN_PATH="$INSTALL_DIR/whatsapp-mcp"

OS_RAW="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH_RAW="$(uname -m | tr '[:upper:]' '[:lower:]')"

case "$OS_RAW" in
  darwin) GOOS="darwin" ;;
  linux)  GOOS="linux" ;;
  *)
    echo "Sistema operacional não suportado por install.sh: $OS_RAW (no Windows use scripts/install.ps1)" >&2
    exit 1
    ;;
esac

case "$ARCH_RAW" in
  x86_64|amd64)  GOARCH="amd64" ;;
  arm64|aarch64) GOARCH="arm64" ;;
  *)
    echo "Arquitetura de CPU não suportada: $ARCH_RAW" >&2
    exit 1
    ;;
esac

ASSET_NAME="whatsapp-mcp-${GOOS}-${GOARCH}"
mkdir -p "$INSTALL_DIR"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

DOWNLOADED=0
TOKEN="${WHATSAPP_MCP_GITHUB_TOKEN:-${GITHUB_TOKEN:-${GH_TOKEN:-}}}"

# 1. Public direct download from GitHub Releases (no token/login required when repo is public)
if [ -z "$TOKEN" ] && command -v curl >/dev/null 2>&1; then
  BASE_DL="https://github.com/${REPO}/releases/latest/download"
  if curl -fsSL "${BASE_DL}/${ASSET_NAME}" -o "$TMP_DIR/$ASSET_NAME" 2>/dev/null; then
    echo "==> Baixado ${ASSET_NAME} da Release oficial..."
    curl -fsSL "${BASE_DL}/checksums.txt" -o "$TMP_DIR/checksums.txt" 2>/dev/null || true
    DOWNLOADED=1
  fi
fi

# 2. Authenticated fallback via GitHub CLI (gh)
if [ "$DOWNLOADED" -eq 0 ] && command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
  echo "==> Baixando ${ASSET_NAME} via GitHub CLI (gh)..."
  if gh release download --repo "$REPO" --pattern "$ASSET_NAME" --pattern "checksums.txt" --dir "$TMP_DIR" 2>/dev/null; then
    DOWNLOADED=1
  fi
fi

# 3. Authenticated fallback via GitHub API token
if [ "$DOWNLOADED" -eq 0 ] && [ -n "$TOKEN" ]; then
  echo "==> Baixando ${ASSET_NAME} via GitHub API autenticada..."
  RELEASE_JSON="$(curl -fsSL -H "Authorization: Bearer $TOKEN" -H "Accept: application/vnd.github+json" "https://api.github.com/repos/${REPO}/releases/latest" || true)"
  if [ -n "$RELEASE_JSON" ] && command -v python3 >/dev/null 2>&1; then
    python3 - "$RELEASE_JSON" "$ASSET_NAME" "$TMP_DIR" "$TOKEN" <<'PY' && DOWNLOADED=1 || true
import json, sys, urllib.request
from pathlib import Path

release = json.loads(sys.argv[1])
target_name = sys.argv[2]
out_dir = Path(sys.argv[3])
token = sys.argv[4]
assets = {a["name"]: a["url"] for a in release.get("assets", [])}
if target_name not in assets:
    sys.exit(1)

for name in (target_name, "checksums.txt"):
    if name not in assets:
        continue
    req = urllib.request.Request(
        assets[name],
        headers={"Authorization": f"Bearer {token}", "Accept": "application/octet-stream"},
    )
    with urllib.request.urlopen(req, timeout=60) as resp:
        (out_dir / name).write_bytes(resp.read())
PY
  fi
fi

if [ "$DOWNLOADED" -eq 1 ] && [ -f "$TMP_DIR/$ASSET_NAME" ]; then
  if [ -f "$TMP_DIR/checksums.txt" ]; then
    EXPECTED="$(awk -v f="$ASSET_NAME" '$2 == f {print $1}' "$TMP_DIR/checksums.txt")"
    if [ -n "$EXPECTED" ]; then
      if command -v shasum >/dev/null 2>&1; then
        ACTUAL="$(shasum -a 256 "$TMP_DIR/$ASSET_NAME" | awk '{print $1}')"
      else
        ACTUAL="$(sha256sum "$TMP_DIR/$ASSET_NAME" | awk '{print $1}')"
      fi
      if [ "$EXPECTED" != "$ACTUAL" ]; then
        echo "Erro: falha na verificação SHA-256 do binário baixado!" >&2
        exit 1
      fi
      echo "==> Checksum SHA-256 verificado com sucesso."
    fi
  fi
  mv "$TMP_DIR/$ASSET_NAME" "$BIN_PATH"
elif [ -f "go.mod" ] && command -v go >/dev/null 2>&1; then
  echo "==> Compilando whatsapp-mcp localmente com Go puro (CGO_ENABLED=0)..."
  CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$BIN_PATH" .
else
  echo "Não foi possível baixar automaticamente nem compilar localmente." >&2
  echo "Baixe o arquivo '${ASSET_NAME}' pelo navegador em: https://github.com/${REPO}/releases/latest" >&2
  exit 1
fi

chmod 755 "$BIN_PATH"
if [ "$GOOS" = "darwin" ]; then
  xattr -d com.apple.quarantine "$BIN_PATH" 2>/dev/null || true
fi

echo ""
echo "Instalação concluída em: $BIN_PATH"
echo "Versão: $("$BIN_PATH" version)"
echo ""
echo "Cole este bloco dentro de \"mcpServers\" no seu mcp_config.json:"
cat <<EOF
    "whatsapp": {
      "command": "$BIN_PATH",
      "env": {
        "WHATSAPP_MCP_READ_ONLY": "false"
      }
    }
EOF
