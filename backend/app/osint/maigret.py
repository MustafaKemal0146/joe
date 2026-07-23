from __future__ import annotations

import asyncio
import json
import os
import shutil
import sys
import tempfile
from pathlib import Path
from typing import Any

from ..config import get_settings
from .base import OsintConnectorError, validate_username


class MaigretConnector:
    id = "maigret"
    name = "Maigret"
    query_types = {"username"}

    def __init__(self) -> None:
        self.settings = get_settings()

    def command(self) -> list[str]:
        executable = shutil.which("maigret")
        if executable:
            return [executable]
        try:
            __import__("maigret")
        except ImportError as exc:
            raise OsintConnectorError(
                "connector_unavailable", "Maigret bağlayıcısı bu kurulumda bulunamadı."
            ) from exc
        return [sys.executable, "-m", "maigret"]

    async def run(self, username: str, _: str = "username") -> dict[str, Any]:
        clean = validate_username(username)
        with tempfile.TemporaryDirectory(prefix="joe-maigret-") as temp:
            output_dir = Path(temp)
            args = [
                *self.command(),
                clean,
                "--all-sites",
                "--timeout",
                "12",
                "--max-connections",
                "50",
                "--no-recursion",
                "--no-extracting",
                "--no-autoupdate",
                "--no-color",
                "--no-progressbar",
                "--json",
                "simple",
                "--folderoutput",
                str(output_dir),
            ]
            environment = {**os.environ, "PYTHONUTF8": "1", "PYTHONIOENCODING": "utf-8"}
            process = await asyncio.create_subprocess_exec(
                *args,
                cwd=output_dir,
                env=environment,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
            )
            try:
                stdout, stderr = await asyncio.wait_for(
                    process.communicate(), timeout=self.settings.osint_timeout_seconds
                )
            except TimeoutError as exc:
                if process.returncode is None:
                    process.kill()
                    await process.wait()
                raise OsintConnectorError(
                    "connector_timeout", "Maigret taraması zaman aşımına uğradı."
                ) from exc

            reports = list(output_dir.glob("*_simple.json"))
            if not reports:
                detail = stderr.decode("utf-8", errors="replace").strip()
                if not detail:
                    detail = stdout.decode("utf-8", errors="replace").strip()
                raise OsintConnectorError(
                    "connector_failed",
                    f"Maigret sonuç üretemedi: {detail[-500:] or 'bilinmeyen hata'}",
                )
            try:
                payload = json.loads(reports[0].read_text(encoding="utf-8"))
            except (OSError, json.JSONDecodeError) as exc:
                raise OsintConnectorError(
                    "invalid_output", "Maigret sonuç dosyası ayrıştırılamadı."
                ) from exc

        findings: list[dict[str, Any]] = []
        for site_name, item in payload.items():
            status = item.get("status") or {}
            if str(status.get("status", "")).casefold() != "claimed":
                continue
            profile_url = status.get("url") or item.get("url_user")
            if not profile_url:
                continue
            findings.append(
                {
                    "connector": self.id,
                    "sources": [self.id],
                    "site": status.get("site_name") or site_name,
                    "username": status.get("username") or clean,
                    "profile_url": profile_url,
                    "site_url": item.get("url_main"),
                    "http_status": item.get("http_status"),
                    "response_time_seconds": None,
                    "classification": "aday_hesap",
                    "identity_status": "doğrulanmadı",
                    "tags": status.get("tags") or [],
                }
            )
        return {
            "connector": self.id,
            "query": clean,
            "scope": "Maigret etkin siteler veritabanının tamamı",
            "finding_count": len(findings),
            "findings": findings,
            "notice": "Eşleşmeler kullanıcı adı adaylarıdır; kişi sahipliği doğrulanmış değildir.",
        }
