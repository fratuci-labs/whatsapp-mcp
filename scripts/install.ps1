$ErrorActionPreference = "Stop"

$Repo = if ($env:WHATSAPP_MCP_REPO) { $env:WHATSAPP_MCP_REPO } else { "fratuci-labs/whatsapp-mcp" }
$InstallDir = if ($env:WHATSAPP_MCP_INSTALL_DIR) { $env:WHATSAPP_MCP_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\whatsapp-mcp" }
$BinPath = Join-Path $InstallDir "whatsapp-mcp.exe"

$Arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLower() -eq "arm64") { "arm64" } else { "amd64" }
$AssetName = "whatsapp-mcp-windows-$Arch.exe"

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("whatsapp-mcp-" + [guid]::NewGuid().ToString())
New-Item -ItemType Directory -Force -Path $TmpDir | Out-Null

try {
    $Downloaded = $false

    if (Get-Command gh -ErrorAction SilentlyContinue) {
        try {
            gh release download --repo $Repo --pattern $AssetName --pattern "checksums.txt" --dir $TmpDir 2>$null
            if (Test-Path (Join-Path $TmpDir $AssetName)) {
                $Downloaded = $true
            }
        } catch {}
    }

    $Token = if ($env:WHATSAPP_MCP_GITHUB_TOKEN) { $env:WHATSAPP_MCP_GITHUB_TOKEN } elseif ($env:GITHUB_TOKEN) { $env:GITHUB_TOKEN } else { $env:GH_TOKEN }
    if (-not $Downloaded -and $Token) {
        Write-Host "==> Baixando $AssetName via GitHub API autenticada..."
        $Headers = @{
            "Authorization" = "Bearer $Token"
            "Accept"        = "application/vnd.github+json"
            "User-Agent"    = "whatsapp-mcp-installer"
        }
        $Release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers $Headers
        $Asset = $Release.assets | Where-Object { $_.name -eq $AssetName } | Select-Object -First 1
        if ($Asset) {
            $DlHeaders = @{
                "Authorization" = "Bearer $Token"
                "Accept"        = "application/octet-stream"
                "User-Agent"    = "whatsapp-mcp-installer"
            }
            Invoke-WebRequest -Uri $Asset.url -Headers $DlHeaders -OutFile (Join-Path $TmpDir $AssetName)
            $ChkAsset = $Release.assets | Where-Object { $_.name -eq "checksums.txt" } | Select-Object -First 1
            if ($ChkAsset) {
                Invoke-WebRequest -Uri $ChkAsset.url -Headers $DlHeaders -OutFile (Join-Path $TmpDir "checksums.txt")
            }
            $Downloaded = $true
        }
    }

    if ($Downloaded -and (Test-Path (Join-Path $TmpDir $AssetName))) {
        $ChkFile = Join-Path $TmpDir "checksums.txt"
        if (Test-Path $ChkFile) {
            $Line = Get-Content $ChkFile | Where-Object { $_ -match [regex]::Escape($AssetName) } | Select-Object -First 1
            if ($Line) {
                $Expected = ($Line -split "\s+")[0].ToLower()
                $Actual = (Get-FileHash -Algorithm SHA256 (Join-Path $TmpDir $AssetName)).Hash.ToLower()
                if ($Expected -ne $Actual) {
                    throw "Falha na verificação SHA-256 do executável baixado!"
                }
                Write-Host "==> Checksum SHA-256 verificado com sucesso."
            }
        }
        Move-Item -Force (Join-Path $TmpDir $AssetName) $BinPath
        Unblock-File -Path $BinPath -ErrorAction SilentlyContinue
    } elseif ((Test-Path "go.mod") -and (Get-Command go -ErrorAction SilentlyContinue)) {
        Write-Host "==> Compilando whatsapp-mcp.exe localmente (CGO_ENABLED=0)..."
        $env:CGO_ENABLED = "0"
        go build -trimpath -ldflags="-s -w" -o $BinPath .
    } else {
        throw "Baixe '$AssetName' diretamente pelo navegador em https://github.com/$Repo/releases/latest"
    }

    $EscapedPath = $BinPath.Replace("\", "\\")
    Write-Host ""
    Write-Host "Instalação concluída em: $BinPath"
    Write-Host ""
    Write-Host 'Cole este bloco dentro de "mcpServers" no seu mcp_config.json:'
    Write-Host @"
    "whatsapp": {
      "command": "$EscapedPath",
      "env": {
        "WHATSAPP_MCP_READ_ONLY": "false"
      }
    }
"@
} finally {
    Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue
}
