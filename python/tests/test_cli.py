import hashlib
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from whatsapp_mcp import cli


class TestLauncherCLI(unittest.TestCase):
    def test_detect_platform_target(self) -> None:
        goos, goarch, ext = cli.detect_platform_target()
        self.assertIn(goos, ("darwin", "linux", "windows"))
        self.assertIn(goarch, ("amd64", "arm64"))
        self.assertEqual(ext, ".exe" if goos == "windows" else "")

    def test_verify_sha256_valid_and_mismatch(self) -> None:
        payload = b"test-binary-bytes"
        digest = hashlib.sha256(payload).hexdigest()
        checksums = f"{digest}  whatsapp-mcp-linux-amd64\n"
        cli._verify_sha256(payload, "whatsapp-mcp-linux-amd64", checksums)

        with self.assertRaises(RuntimeError):
            cli._verify_sha256(b"tampered", "whatsapp-mcp-linux-amd64", checksums)

    def test_resolve_explicit_binary_override(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            fake_bin = Path(tmp) / "whatsapp-mcp"
            fake_bin.write_bytes(b"#!/bin/sh\n")
            with mock.patch.dict(os.environ, {"WHATSAPP_MCP_BINARY": str(fake_bin)}):
                self.assertEqual(cli.resolve_binary(), fake_bin)


if __name__ == "__main__":
    unittest.main()
