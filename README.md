# whatsapp-mcp

Servidor **Model Context Protocol (MCP)** multiplataforma (**macOS, Linux e Windows**) para integração local com o **WhatsApp Multi-Device**, escrito em **Go puro (`CGO_ENABLED=0`)** com `whatsmeow`, `modernc.org/sqlite` e auto-daemon autenticado em background.

> **Vai instalar em outro computador (Windows, Mac ou Linux) sem complicação?**
> Siga o **[Manual Passo a Passo para Usuários Comuns (`docs/MANUAL_INSTALACAO.md`)](docs/MANUAL_INSTALACAO.md)** — pensado para uso direto sem precisar saber programar!

---

## Principais Recursos

- **Zero CGO & Multiplataforma Real**: Compila estaticamente para `darwin/arm64`, `darwin/amd64`, `linux/amd64`, `linux/arm64`, `windows/amd64` e `windows/arm64` sem exigir compilador C (GCC/MinGW) nem Docker.
- **Auto-Daemon Instantâneo e Seguro**: O processo MCP (`stdio`) inicia automaticamente um daemon local em background na primeira chamada, mantendo a conexão WebSocket do WhatsApp ativa entre chamadas sem timeouts de reconexão.
  - **macOS / Linux**: Unix Domain Socket (`0600`, `umask 0077`) + autenticação `Bearer` de 256 bits (`crypto/subtle`).
  - **Windows** (e caminhos longos): Loopback `127.0.0.1:<porta>` autenticado via token criptográfico em `daemon.json` (`0600`).
- **Pareamento Flexível (MCP Inline + CLI)**:
  - Retorna a imagem do QR Code **nativamente dentro do protocolo MCP (`ImageContent` PNG)** na ferramenta `wa_status(mode="qr")`, além de salvar `qr.png` localmente e renderizar QR em Unicode no terminal (`whatsapp-mcp qr`).
  - Suporta pareamento por código de 8 dígitos (`mode="pair_phone"` ou `whatsapp-mcp pair <telefone>`), sem necessidade de câmera.
- **Suporte Completo a Grupos `@lid`**: Resolução bidirecional de identificadores `@lid` ↔ `@s.whatsapp.net`, citações nativas (`reply_to_id`) e menções clicáveis automáticas.
- **Modo Somente Leitura (`WHATSAPP_MCP_READ_ONLY=true`)**: Remove `wa_send_message` da superfície MCP e bloqueia `/send` diretamente no daemon (`HTTP 403`).

---

## Configuração Padrão (`mcp_config.json`)

O servidor segue exatamente o padrão de configuração `command` / `args` / `env` de clientes MCP (Antigravity, Claude Desktop, Cursor, VS Code).

### Opção 1: Via Python / `uv` (Windows, macOS e Linux — igual ao `mcp-clickhouse`)

Usando um *wheel* pré-compilado da Release (funciona offline e em repositório privado) ou `GITHUB_TOKEN`:

```json
{
  "mcpServers": {
    "whatsapp": {
      "command": "uvx",
      "args": [
        "--from",
        "whatsapp-mcp",
        "whatsapp-mcp"
      ],
      "env": {
        "WHATSAPP_MCP_READ_ONLY": "false",
        "WHATSAPP_MCP_LOG_LEVEL": "INFO"
      }
    }
  }
}
```

> **Repositório privado (`fratuci-labs/whatsapp-mcp`)**: Enquanto o repositório estiver privado, instale o `.whl` correspondente à sua plataforma anexado na aba **Releases** (`uv tool install ./whatsapp_mcp-1.0.0-py3-none-macosx_11_0_arm64.whl` ou `win_amd64.whl`), ou exporte `GITHUB_TOKEN` no bloco `"env"` para que o launcher Python baixe o binário autenticado com verificação SHA-256.

### Opção 2: Executável Nativo Direto (macOS, Linux ou Windows `.exe`)

Baixe o binário da aba **Releases** (ou compile com `CGO_ENABLED=0 go build -o whatsapp-mcp .`):

```json
{
  "mcpServers": {
    "whatsapp": {
      "command": "/caminho/para/whatsapp-mcp",
      "env": {
        "WHATSAPP_MCP_HOME": "/caminho/opcional/dados-whatsapp",
        "WHATSAPP_MCP_READ_ONLY": "false",
        "WHATSAPP_MCP_LOG_LEVEL": "INFO",
        "WHATSAPP_MCP_REQUEST_TIMEOUT": "45"
      }
    }
  }
}
```

Quando `WHATSAPP_MCP_HOME` não é informado, os dados são salvos automaticamente no diretório padrão do usuário:
- **macOS**: `~/Library/Application Support/whatsapp-mcp`
- **Windows**: `%LOCALAPPDATA%\whatsapp-mcp`
- **Linux**: `~/.local/share/whatsapp-mcp` (ou `$XDG_DATA_HOME/whatsapp-mcp`)

---

## Variáveis de Ambiente Suportadas

| Variável | Padrão | Descrição |
|---|---|---|
| `WHATSAPP_MCP_HOME` | *(AppData do SO)* | Diretório-base para `data/` e `media/` |
| `WHATSAPP_MCP_DATA_DIR` | `<HOME>/data` | Caminho explícito para `session.db`, `messages.db` e `daemon.json` |
| `WHATSAPP_MCP_MEDIA_DIR` | `<HOME>/media` | Caminho explícito para mídias descriptografadas |
| `WHATSAPP_MCP_READ_ONLY` | `false` | Se `true`, desabilita o envio de mensagens no MCP e no daemon |
| `WHATSAPP_MCP_LOG_LEVEL` | `INFO` | Nível de log (`DEBUG`, `INFO`, `WARN`, `ERROR`) |
| `WHATSAPP_MCP_REQUEST_TIMEOUT` | `45` | Timeout HTTP (segundos) entre cliente MCP e daemon (`5`–`300`) |
| `WHATSAPP_MCP_DEVICE_NAME` | *(Auto por SO)* | Nome exibido em *Aparelhos Conectados* no WhatsApp |

---

## Como Parear o WhatsApp

1. **Direto pelo Agente IA (MCP)**:
   Peça ao agente para verificar o status ou gerar o QR Code (`wa_status` com `mode: "qr"`). O servidor retorna a imagem PNG inline no chat para leitura imediata, ou use `mode: "pair_phone"` informando o número com DDI (`5511999999999`) para receber o código de 8 caracteres.

2. **Via Terminal (CLI)**:
   ```bash
   # Exibe o QR Code diretamente no terminal e salva data/qr.png
   whatsapp-mcp qr

   # Ou gera código de pareamento de 8 dígitos para digitar no celular
   whatsapp-mcp pair 5511999999999

   # Verifica conexão e contagens sincronizadas
   whatsapp-mcp status

   # Encerra o daemon em background
   whatsapp-mcp stop
   ```

---

## Ferramentas MCP Expostas

| Ferramenta | Descrição |
|---|---|
| `wa_status` | Verifica status da conexão e métricas do SQLite, ou inicia pareamento via `qr` (com `ImageContent` PNG) / `pair_phone` |
| `wa_list_chats` | Lista ou filtra conversas individuais e grupos sincronizados |
| `wa_read_messages` | Lê mensagens recentes em ordem cronológica com resolução automática de nomes e `@lid` |
| `wa_search_messages` | Busca mensagens por texto ou nome do remetente em todas as conversas ou em um chat específico |
| `wa_find_contacts` | Busca contatos locais ou consulta participantes ao vivo de um grupo (`group_jid`) com `@lid` exato |
| `wa_download_media` | Descriptografa e baixa áudios (`.ogg`), imagens, vídeos, documentos e figurinhas |
| `wa_send_message` | Envia mensagens com suporte a quote nativo (`reply_to_id`) e `@menções` clicáveis *(desativada se `WHATSAPP_MCP_READ_ONLY=true`)* |

---

## Build & Testes a partir do Código-Fonte

```bash
# Validar código e rodar suíte de testes Go (sem CGO)
CGO_ENABLED=0 go vet ./...
CGO_ENABLED=0 go test -v ./...

# Testar launcher Python
python3 -m unittest discover -s python/tests -v

# Compilar binário local
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o whatsapp-mcp .

# Cross-compile para Windows (64 bits) a partir do macOS/Linux
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o whatsapp-mcp.exe .
```

---

## Licença e Avisos Legais

Distribuído sob a licença **GNU General Public License v3.0 (`GPL-3.0`)** em conformidade com a cadeia de dependências (`go.mau.fi/whatsmeow` sob `MPL-2.0` e `go.mau.fi/libsignal` sob `GPL-3.0`). Consulte [LICENSE](LICENSE) e [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) para o inventário completo de licenças e avisos de uso do protocolo WhatsApp Multi-Device.
