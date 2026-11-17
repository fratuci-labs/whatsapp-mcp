"""Cross-platform launcher for the native Go whatsapp-mcp binary.

Resolution order:
1. WHATSAPP_MCP_BINARY environment variable (explicit local override).
2. Embedded binary inside whatsapp_mcp/bin/ (platform wheels built in CI).
3. Cached/downloaded release asset from GitHub Releases (supports GITHUB_TOKEN /
   GH_TOKEN for private repositories and verifies SHA-256 against checksums.txt).
"""

from __future__ import annotations

import hashlib
import json
import os
import platform
import stat
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request
from pathlib import Path

from . import __version__

DEFAULT_REPO = "fratuci-labs/whatsapp-mcp"


def detect_platform_target() -> tuple[str, str, str]:
    """Return (goos, goarch, binary_extension) for the current host."""
    sys_name = platform.system().lower()
    if sys_name == "darwin":
        goos = "darwin"
    elif sys_name == "windows":
        goos = "windows"
    elif sys_name == "linux":
        goos = "linux"
    else:
        raise RuntimeError(f"Unsupported operating system: {platform.system()}")

    machine = platform.machine().lower()
    if machine in ("x86_64", "amd64"):
        goarch = "amd64"
    elif machine in ("arm64", "aarch64"):
        goarch = "arm64"
    else:
        raise RuntimeError(f"Unsupported CPU architecture: {platform.machine()}")

    ext = ".exe" if goos == "windows" else ""
    return goos, goarch, ext


def default_cache_dir() -> Path:
    if custom := os.environ.get("WHATSAPP_MCP_CACHE_DIR", "").strip():
        return Path(custom)
    if platform.system().lower() == "windows":
        base = os.environ.get("LOCALAPPDATA") or str(Path.home() / "AppData" / "Local")
        return Path(base) / "whatsapp-mcp" / "bin-cache"
    if platform.system().lower() == "darwin":
        return Path.home() / "Library" / "Caches" / "whatsapp-mcp" / "bin"
    xdg_cache = os.environ.get("XDG_CACHE_HOME") or str(Path.home() / ".cache")
    return Path(xdg_cache) / "whatsapp-mcp" / "bin"


def _github_headers(accept: str = "application/vnd.github+json") -> dict[str, str]:
    headers = {
        "Accept": accept,
        "User-Agent": f"whatsapp-mcp-python/{__version__}",
    }
    token = (
        os.environ.get("WHATSAPP_MCP_GITHUB_TOKEN", "").strip()
        or os.environ.get("GITHUB_TOKEN", "").strip()
        or os.environ.get("GH_TOKEN", "").strip()
    )
    if token:
        headers["Authorization"] = f"Bearer {token}"
    return headers


def _http_get_bytes(url: str, accept: str = "application/vnd.github+json") -> bytes:
    req = urllib.request.Request(url, headers=_github_headers(accept=accept))
    with urllib.request.urlopen(req, timeout=60) as resp:
        return resp.read()


def _verify_sha256(data: bytes, asset_name: str, checksums_text: str) -> None:
    expected = None
    for line in checksums_text.splitlines():
        parts = line.strip().split()
        if len(parts) >= 2 and parts[1].lstrip("*") == asset_name:
            expected = parts[0].lower()
            break
    if not expected:
        raise RuntimeError(f"Checksum entry for {asset_name} not found in checksums.txt")
    actual = hashlib.sha256(data).hexdigest().lower()
    if actual != expected:
        raise RuntimeError(
            f"SHA-256 mismatch for {asset_name}: expected {expected}, got {actual}"
        )


def download_release_binary(version: str, dest: Path) -> Path:
    repo = os.environ.get("WHATSAPP_MCP_REPO", DEFAULT_REPO).strip()
    goos, goarch, ext = detect_platform_target()
    asset_name = f"whatsapp-mcp-{goos}-{goarch}{ext}"
    tag = f"v{version}" if not version.startswith("v") else version

    api_url = f"https://api.github.com/repos/{repo}/releases/tags/{tag}"
    try:
        release_raw = _http_get_bytes(api_url)
    except urllib.error.HTTPError as exc:
        hint = ""
        if exc.code in (401, 403, 404):
            hint = (
                " (if the repository is private, export GITHUB_TOKEN or install a "
                "platform wheel with the bundled binary)"
            )
        raise RuntimeError(
            f"Failed to query GitHub release {repo}@{tag}: HTTP {exc.code}{hint}"
        ) from exc

    release = json.loads(release_raw.decode("utf-8"))
    assets = {a["name"]: a["url"] for a in release.get("assets", [])}
    if asset_name not in assets:
        raise RuntimeError(f"Release {tag} does not contain asset '{asset_name}'")

    binary_bytes = _http_get_bytes(assets[asset_name], accept="application/octet-stream")
    if "checksums.txt" in assets:
        checksums_bytes = _http_get_bytes(
            assets["checksums.txt"], accept="application/octet-stream"
        )
        _verify_sha256(binary_bytes, asset_name, checksums_bytes.decode("utf-8"))

    dest.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=str(dest.parent), delete=False) as tmp:
        tmp.write(binary_bytes)
        tmp_path = Path(tmp.name)

    if goos != "windows":
        tmp_path.chmod(stat.S_IRWXU)
    tmp_path.replace(dest)
    return dest


def resolve_binary() -> Path:
    if explicit := os.environ.get("WHATSAPP_MCP_BINARY", "").strip():
        p = Path(explicit)
        if not p.is_file():
            raise RuntimeError(f"WHATSAPP_MCP_BINARY does not exist: {p}")
        return p

    _, _, ext = detect_platform_target()
    bundled = Path(__file__).resolve().parent / "bin" / f"whatsapp-mcp{ext}"
    if bundled.is_file():
        if ext == "" and not os.access(bundled, os.X_OK):
            bundled.chmod(stat.S_IRWXU)
        return bundled

    cached = default_cache_dir() / f"v{__version__}" / f"whatsapp-mcp{ext}"
    if cached.is_file():
        return cached

    return download_release_binary(__version__, cached)


def main() -> None:
    try:
        bin_path = resolve_binary()
    except Exception as exc:
        print(f"whatsapp-mcp launcher error: {exc}", file=sys.stderr)
        sys.exit(1)

    args = [str(bin_path), *sys.argv[1:]]
    if platform.system().lower() != "windows":
        os.execv(str(bin_path), args)
    else:
        proc = subprocess.run(args)
        sys.exit(proc.returncode)


if __name__ == "__main__":
    main()
