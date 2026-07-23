from __future__ import annotations

import asyncio
import json
from datetime import UTC, datetime, timedelta
from typing import Any
from urllib.parse import quote, urlsplit

import httpx

from ..config import get_settings
from .base import OsintConnectorError, validate_username


DATASET_URL = "https://raw.githubusercontent.com/WebBreacher/WhatsMyName/main/wmn-data.json"


class WhatsMyNameConnector:
    id = "whatsmyname"
    name = "WhatsMyName"
    query_types = {"username"}

    def __init__(self) -> None:
        self.settings = get_settings()
        self.cache_path = self.settings.data_dir / "osint" / "wmn-data.json"

    async def run(self, username: str, _: str = "username") -> dict[str, Any]:
        clean = validate_username(username)
        sites, dataset_source = await self._load_sites()
        enabled_sites = [site for site in sites if site.get("valid", True) is not False]
        semaphore = asyncio.Semaphore(35)
        errors = 0

        async with httpx.AsyncClient(
            follow_redirects=True,
            timeout=httpx.Timeout(12.0),
            headers={"User-Agent": "joe-osint/0.1 (local research client)"},
        ) as client:

            async def check(site: dict[str, Any]) -> dict[str, Any] | None:
                nonlocal errors
                template = site.get("uri_check")
                if not isinstance(template, str) or "{account}" not in template:
                    return None
                url = template.replace("{account}", quote(clean, safe="._-"))
                try:
                    async with semaphore:
                        async with client.stream("GET", url) as response:
                            content = bytearray()
                            async for chunk in response.aiter_bytes():
                                content.extend(chunk)
                                if len(content) >= 768 * 1024:
                                    break
                            text = content.decode(response.encoding or "utf-8", errors="replace")
                            status_code = response.status_code
                except (httpx.HTTPError, UnicodeError):
                    errors += 1
                    return None

                exists_code = site.get("e_code")
                exists_marker = str(site.get("e_string") or "")
                missing_marker = str(site.get("m_string") or "")
                found = status_code == exists_code
                if exists_marker:
                    found = found and exists_marker in text
                if missing_marker:
                    found = found and missing_marker not in text
                if not found:
                    return None
                parsed = urlsplit(url)
                return {
                    "connector": self.id,
                    "sources": [self.id],
                    "site": site.get("name") or parsed.netloc,
                    "username": clean,
                    "profile_url": url,
                    "site_url": f"{parsed.scheme}://{parsed.netloc}",
                    "http_status": status_code,
                    "response_time_seconds": None,
                    "classification": "aday_hesap",
                    "identity_status": "doğrulanmadı",
                    "category": site.get("cat"),
                }

            checked = await asyncio.gather(*(check(site) for site in enabled_sites))

        findings = [item for item in checked if item]
        return {
            "connector": self.id,
            "query": clean,
            "dataset_source": dataset_source,
            "checked_site_count": len(enabled_sites),
            "request_error_count": errors,
            "finding_count": len(findings),
            "findings": findings,
            "notice": "WhatsMyName algılama kuralları aday profilleri gösterir; kimlik doğrulamaz.",
        }

    async def _load_sites(self) -> tuple[list[dict[str, Any]], str]:
        self.cache_path.parent.mkdir(parents=True, exist_ok=True)
        if self.cache_path.exists():
            modified = datetime.fromtimestamp(self.cache_path.stat().st_mtime, tz=UTC)
            if datetime.now(UTC) - modified < timedelta(hours=24):
                try:
                    return json.loads(self.cache_path.read_text(encoding="utf-8"))["sites"], "önbellek"
                except (OSError, json.JSONDecodeError, KeyError):
                    pass
        try:
            async with httpx.AsyncClient(follow_redirects=True, timeout=30.0) as client:
                response = await client.get(DATASET_URL)
                response.raise_for_status()
                payload = response.json()
            sites = payload.get("sites")
            if not isinstance(sites, list):
                raise ValueError("sites alanı yok")
            self.cache_path.write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")
            return sites, "github"
        except (httpx.HTTPError, ValueError, OSError, json.JSONDecodeError) as exc:
            if self.cache_path.exists():
                try:
                    return json.loads(self.cache_path.read_text(encoding="utf-8"))["sites"], "eski önbellek"
                except (OSError, json.JSONDecodeError, KeyError):
                    pass
            raise OsintConnectorError(
                "dataset_unavailable", "WhatsMyName kaynak verisi alınamadı."
            ) from exc
