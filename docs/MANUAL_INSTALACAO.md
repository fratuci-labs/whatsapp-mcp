# Manual de Instalação para Usuários (Windows, Mac e Linux)

Este guia foi feito para que **qualquer pessoa** consiga instalar e usar o conector **WhatsApp MCP** em outro computador (**Windows**, **Mac** ou **Linux**) em poucos minutos — **sem precisar saber programar** e sem instalar ferramentas pesadas como Docker ou bancos de dados.

---

## Como funciona na prática?

Depois de instalado uma única vez:
1. O conector fica guardado em uma pasta do seu computador.
2. Sempre que você pedir algo sobre o WhatsApp para a sua IA (por exemplo, *"Resuma o que falaram hoje no grupo da equipe"*), ela inicia o conector automaticamente em segundo plano.
3. Suas mensagens e mídias ficam salvas **apenas no seu próprio computador**, nunca em servidores de terceiros.

---

## Passo 1 — Baixar o Conector no Novo Computador

Escolha **apenas uma** das formas abaixo (a **Forma A** é a mais simples para a maioria das pessoas).

### Forma A: Baixar o arquivo pronto pelo navegador (Recomendado)

1. No computador onde deseja instalar, faça login no GitHub (com sua conta que tem acesso à organização `fratuci-labs`) e abra a página de versões:
   - **https://github.com/fratuci-labs/whatsapp-mcp/releases/latest**
2. Em **Assets** (arquivos da versão), clique para baixar o arquivo correspondente ao seu computador:

| Seu Computador | Qual arquivo clicar para baixar |
|---|---|
| **Windows 10 ou 11** *(quase todos os PCs)* | `whatsapp-mcp-windows-amd64.exe` |
| **Mac recente (chips Apple M1, M2, M3 ou M4)** | `whatsapp-mcp-darwin-arm64` |
| **Mac mais antigo (processador Intel)** | `whatsapp-mcp-darwin-amd64` |
| **Linux (PC comum 64 bits)** | `whatsapp-mcp-linux-amd64` |

3. Mova o arquivo baixado para uma pasta fixa (evite deixá-lo na pasta *Downloads* para não apagá-lo por engano):

#### Se você está no Windows:
1. Abra o **Explorador de Arquivos**, entre em **Este Computador → Disco Local (C:)** e crie uma pasta chamada `whatsapp-mcp`.
2. Coloque o arquivo baixado dentro dela e renomeie para **`whatsapp-mcp.exe`**.
   - O caminho final ficará exatamente assim: `C:\whatsapp-mcp\whatsapp-mcp.exe`
3. *(Opcional — destravar aviso do Windows)*: Clique com o botão direito sobre `whatsapp-mcp.exe` → **Propriedades**. Se aparecer na parte de baixo a caixa **"Desbloquear"** (*Este arquivo veio de outro computador...*), marque **Desbloquear** e clique em **OK**.

#### Se você está no Mac:
1. Abra o aplicativo **Terminal** (aperte `Command + Espaço`, digite `Terminal` e pressione `Enter`).
2. Copie, cole o comando abaixo e aperte `Enter` (ele cria a pasta `~/whatsapp-mcp`, move o arquivo baixado e libera a permissão de segurança do macOS):
   ```bash
   mkdir -p ~/whatsapp-mcp && \
   mv ~/Downloads/whatsapp-mcp-darwin-* ~/whatsapp-mcp/whatsapp-mcp && \
   chmod +x ~/whatsapp-mcp/whatsapp-mcp && \
   xattr -d com.apple.quarantine ~/whatsapp-mcp/whatsapp-mcp 2>/dev/null || true
   ```

#### Se você está no Linux:
1. Abra o **Terminal**, cole o comando abaixo e aperte `Enter`:
   ```bash
   mkdir -p ~/whatsapp-mcp && \
   mv ~/Downloads/whatsapp-mcp-linux-* ~/whatsapp-mcp/whatsapp-mcp && \
   chmod +x ~/whatsapp-mcp/whatsapp-mcp
   ```

---

### Forma B: Instalação Automática de 1 Comando (Para quem já tem `gh` ou `Go` / `Token`)

Se você já tiver o repositório baixado na máquina (ou token do GitHub), pode rodar o instalador automático que já baixa/compila o conector no local certo e **imprime na tela o trecho exato para colar na configuração**:

- **No Mac ou Linux (Terminal)**:
  ```bash
  bash scripts/install.sh
  ```
- **No Windows (PowerShell)**:
  ```powershell
  powershell -ExecutionPolicy Bypass -File .\scripts\install.ps1
  ```

---

### Forma C: Instalação via Python / `uv` (Igual ao `mcp-clickhouse`)

Se você já usa `uv` para seus outros MCPs:
1. Baixe o arquivo `.whl` do seu sistema na página de **Releases** (ex.: `win_amd64.whl` no Windows ou `macosx_11_0_arm64.whl` no Mac Apple Silicon).
2. Rode no terminal:
   ```bash
   uv tool install ./whatsapp_mcp-*.whl
   ```

---

## Passo 2 — Cadastrar o Conector no seu Aplicativo de IA

Agora você só precisa avisar ao seu aplicativo de IA onde está o conector, editando o arquivo `mcp_config.json`.

### Onde fica esse arquivo?

- **Google Antigravity**:
  - **Windows**: `C:\Users\SEU_USUARIO\.gemini\config\mcp_config.json`
  - **Mac / Linux**: `~/.gemini/config/mcp_config.json`
- **Claude Desktop**:
  - **Windows**: `%APPDATA%\Claude\claude_desktop_config.json` *(cole esse caminho na barra do Explorador de Arquivos e aperte Enter)*
  - **Mac**: `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Cursor**:
  - Abra *Settings → MCP → Add new global MCP server* (arquivo `~/.cursor/mcp.json`).

### O que colar dentro do arquivo?

#### Exemplo para Windows (usando `C:\whatsapp-mcp\whatsapp-mcp.exe`):
> **Atenção no Windows**: dentro de arquivos `.json`, sempre use **duas barras invertidas** (`\\`) nos caminhos!

```json
{
  "mcpServers": {
    "whatsapp": {
      "command": "C:\\whatsapp-mcp\\whatsapp-mcp.exe",
      "env": {
        "WHATSAPP_MCP_READ_ONLY": "false"
      }
    }
  }
}
```

#### Exemplo para Mac ou Linux (substitua `SEU_USUARIO` pelo nome da sua pasta de usuário):

```json
{
  "mcpServers": {
    "whatsapp": {
      "command": "/Users/SEU_USUARIO/whatsapp-mcp/whatsapp-mcp",
      "env": {
        "WHATSAPP_MCP_READ_ONLY": "false"
      }
    }
  }
}
```
*(No Linux, o caminho geralmente é `/home/SEU_USUARIO/whatsapp-mcp/whatsapp-mcp`.)*

#### Exemplo para quem instalou via `uv` (Forma C — igual em qualquer sistema):

```json
{
  "mcpServers": {
    "whatsapp": {
      "command": "whatsapp-mcp",
      "env": {
        "WHATSAPP_MCP_READ_ONLY": "false"
      }
    }
  }
}
```

> **Dica se você já tiver outros MCPs nesse arquivo**: adicione apenas o bloco `"whatsapp": { ... }` dentro do `"mcpServers"` que já existe, lembrando de colocar uma **vírgula `,`** após o `}` do MCP anterior!

Salve o arquivo e **feche e abra novamente** o seu aplicativo de IA para ele carregar o novo conector.

---

## Passo 3 — Conectar seu WhatsApp (30 segundos)

Você não precisa usar o terminal para conectar seu celular. Faça tudo direto na conversa com a IA:

### Opção 1: Por QR Code (Direto na tela do chat)
1. Abra uma nova conversa na sua IA e escreva:
   > **"Verifique o status do meu WhatsApp e mostre o QR Code para eu conectar."**
2. A IA vai exibir a imagem do QR Code na tela.
3. Pegue seu celular e abra o **WhatsApp**:
   - **No iPhone**: toque em **Configurações** (canto inferior direito) → **Aparelhos conectados** → **Conectar aparelho**.
   - **No Android**: toque nos **três pontinhos (⋮)** no canto superior direito → **Aparelhos conectados** → **Conectar aparelho**.
4. Aponte a câmera do celular para o QR Code na tela do computador. Pronto! Aguarde cerca de 10 a 20 segundos para ele sincronizar suas conversas iniciais.

### Opção 2: Por Código de 8 Dígitos (Sem usar a câmera)
Se a câmera do celular não funcionar ou preferir digitar um código:
1. Escreva para a IA (trocando pelo seu número com `55` + DDD, apenas números):
   > **"Conecte meu WhatsApp usando código pelo telefone 5511999999999"**
2. A IA vai te informar um código de 8 letras/números (ex.: `ABCD-1234`).
3. No WhatsApp do celular, vá em **Aparelhos conectados → Conectar aparelho → Conectar com número de telefone** (na parte inferior da tela) e digite o código!

---

## Passo 4 — Dica de Segurança: Modo "Somente Leitura"

Quer ter **100% de certeza** de que a IA nunca enviará nenhuma mensagem por engano, funcionando apenas para ler, pesquisar histórico, ouvir áudios e resumir grupos?

Basta trocar `"WHATSAPP_MCP_READ_ONLY"` para `"true"` no seu `mcp_config.json`:

```json
"env": {
  "WHATSAPP_MCP_READ_ONLY": "true"
}
```

Com isso ativado, a ferramenta de envio desaparece para a IA e o próprio conector bloqueia qualquer tentativa de envio.

---

## Perguntas Frequentes (FAQ)

### 1. Posso usar este MCP em dois ou mais computadores ao mesmo tempo?
**Sim!** O WhatsApp suporta até 4 aparelhos conectados simultaneamente. Basta seguir este manual no segundo computador e fazer o pareamento normalmente — você **não precisa copiar arquivos de banco de dados** de um computador para o outro.

### 2. Preciso abrir algum programa toda vez que eu reiniciar o computador?
**Não.** Quando você liga o computador e faz uma pergunta sobre WhatsApp no chat da IA, o conector inicia sozinho em segundo plano em menos de 1 segundo.

### 3. O que eu posso pedir para a IA depois de conectado?
Exemplos reais do dia a dia:
- *"Liste meus grupos do WhatsApp."*
- *"Resuma o que discutiram hoje cedo no grupo Financeiro."*
- *"Procure nas minhas mensagens quem me mandou o boleto ou chave Pix ontem."*
- *"Baixe e escute o último áudio que fulano me mandou e me diga o que ele pediu."*

### 4. Como faço para desconectar minha conta deste computador?
Pelo próprio celular: abra **WhatsApp → Aparelhos conectados**, toque no computador listado e escolha **Desconectar**.
Se quiser também apagar o histórico salvo no computador, basta excluir a pasta de dados:
- **Windows**: `%LOCALAPPDATA%\whatsapp-mcp`
- **Mac**: `~/Library/Application Support/whatsapp-mcp`
- **Linux**: `~/.local/share/whatsapp-mcp`
