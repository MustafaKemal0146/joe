from __future__ import annotations

import asyncio
import csv
import shutil
import sys
import tempfile
from pathlib import Path
from typing import Any

from ..config import get_settings
from .base import OsintConnectorError, validate_username


class SherlockError(OsintConnectorError):
    pass


class SherlockConnector:
    id = "sherlock"
    name = "Sherlock"
    query_types = {"username"}

    def __init__(self) -> None:
        self.settings = get_settings()

    def command(self) -> list[str]:
        executable = shutil.which("sherlock")
        if executable:
            return [executable]
        try:
            __import__("sherlock_project")
        except ImportError as exc:
            raise SherlockError(
                "connector_unavailable",
                "Sherlock bağlayıcısı bu kurulumda bulunamadı.",
            ) from exc
        return [sys.executable, "-m", "sherlock_project"]

    async def run(self, username: str, _: str = "username") -> dict[str, Any]:
        try:
            clean = validate_username(username)
        except OsintConnectorError as exc:
            raise SherlockError(exc.code, str(exc)) from exc

        command = self.command()
        with tempfile.TemporaryDirectory(prefix="joe-sherlock-") as temp:
            output_dir = Path(temp)
            args = [
                *command,
                clean,
                "--csv",
                "--print-found",
                "--no-color",
                "--timeout",
                "15",
            ]
            try:
                process = await asyncio.create_subprocess_exec(
                    *args,
                    cwd=output_dir,
                    stdout=asyncio.subprocess.PIPE,
                    stderr=asyncio.subprocess.PIPE,
                )
                stdout, stderr = await asyncio.wait_for(
                    process.communicate(), timeout=self.settings.osint_timeout_seconds
                )
            except TimeoutError as exc:
                if "process" in locals() and process.returncode is None:
                    process.kill()
                    await process.wait()
                raise SherlockError("connector_timeout", "Sherlock taraması zaman aşımına uğradı.") from exc

            csv_path = output_dir / f"{clean}.csv"
            if process.returncode != 0 and not csv_path.exists():
                detail = stderr.decode("utf-8", errors="replace").strip()
                if not detail:
                    detail = stdout.decode("utf-8", errors="replace").strip()
                raise SherlockError(
                    "connector_failed",
                    f"Sherlock çalıştırılamadı: {detail[:500] or 'bilinmeyen hata'}",
                )
            if not csv_path.exists():
                raise SherlockError("missing_output", "Sherlock sonuç dosyası üretmedi.")

            findings: list[dict[str, Any]] = []
            with csv_path.open("r", encoding="utf-8", newline="") as handle:
                for row in csv.DictReader(handle):
                    if not row.get("url_user"):
                        continue
                    findings.append(
                        {
                            "connector": self.id,
                            "sources": [self.id],
                            "site": row.get("name"),
                            "username": row.get("username", clean),
                            "profile_url": row.get("url_user"),
                            "site_url": row.get("url_main"),
                            "http_status": row.get("http_status") or None,
                            "response_time_seconds": row.get("response_time_s") or None,
                            "classification": "aday_hesap",
                            "identity_status": "doğrulanmadı",
                        }
                    )

        return {
            "connector": self.id,
            "query": clean,
            "finding_count": len(findings),
            "findings": findings,
            "notice": (
                "Kullanıcı adının bir sitede bulunması, hesabın araştırılan kişiye ait "
                "olduğunu tek başına kanıtlamaz. Her eşleşme ayrıca doğrulanmalıdır."
            ),
        }
