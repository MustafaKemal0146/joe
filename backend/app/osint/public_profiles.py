from __future__ import annotations

import asyncio
from typing import Any
from urllib.parse import quote

import httpx

from .base import OsintConnectorError, validate_username


class PublicProfilesConnector:
    id = "public_profiles"
    name = "Açık profil dizinleri"
    query_types = {"username", "full_name"}

    async def run(self, query: str, query_type: str) -> dict[str, Any]:
        clean = validate_username(query) if query_type == "username" else query.strip()
        if query_type == "full_name" and (len(clean) < 2 or len(clean) > 180):
            raise OsintConnectorError("invalid_name", "Tam ad 2-180 karakter arasında olmalı.")
        async with httpx.AsyncClient(
            follow_redirects=True,
            timeout=20.0,
            headers={"User-Agent": "joe-osint/0.1", "Accept": "application/json"},
        ) as client:
            sources = ["GitHub", "GitLab", "Codeberg"]
            calls = [
                self._github(client, clean, query_type),
                self._gitlab(client, clean, query_type),
                self._codeberg(client, clean, query_type),
            ]
            if query_type == "full_name":
                sources.append("Stack Overflow")
                calls.append(self._stack_overflow(client, clean))
            results = await asyncio.gather(*calls, return_exceptions=True)

        findings: list[dict[str, Any]] = []
        errors: list[dict[str, str]] = []
        succeeded = 0
        for source, result in zip(sources, results, strict=True):
            if isinstance(result, Exception):
                errors.append({"source": source, "message": str(result)[:300]})
            else:
                succeeded += 1
                findings.extend(result)
        if not succeeded:
            raise OsintConnectorError(
                "public_directories_unavailable", "Açık profil dizinlerine ulaşılamadı."
            )
        return {
            "connector": self.id,
            "query": clean,
            "checked_sources": succeeded,
            "source_errors": errors,
            "finding_count": len(findings),
            "findings": findings,
            "notice": "Dizin sonuçları aday profillerdir; ad veya kullanıcı adı benzerliği sahiplik kanıtı değildir.",
        }

    async def _github(
        self, client: httpx.AsyncClient, query: str, query_type: str
    ) -> list[dict[str, Any]]:
        if query_type == "username":
            response = await client.get(f"https://api.github.com/users/{quote(query, safe='')}")
            if response.status_code == 404:
                return []
            response.raise_for_status()
            users = [response.json()]
        else:
            response = await client.get(
                "https://api.github.com/search/users",
                params={"q": f'"{query}" in:name type:user', "per_page": 20},
            )
            response.raise_for_status()
            users = response.json().get("items", [])
        return [
            self._finding("GitHub", item.get("login") or query, item.get("html_url"), "https://github.com")
            for item in users
            if item.get("html_url")
        ]

    async def _gitlab(
        self, client: httpx.AsyncClient, query: str, query_type: str
    ) -> list[dict[str, Any]]:
        params: dict[str, Any] = {"per_page": 20}
        params["username" if query_type == "username" else "search"] = query
        response = await client.get("https://gitlab.com/api/v4/users", params=params)
        response.raise_for_status()
        users = response.json()
        if query_type == "username":
            users = [item for item in users if str(item.get("username", "")).casefold() == query.casefold()]
        return [
            self._finding("GitLab", item.get("username") or query, item.get("web_url"), "https://gitlab.com")
            for item in users
            if item.get("web_url")
        ]

    async def _codeberg(
        self, client: httpx.AsyncClient, query: str, query_type: str
    ) -> list[dict[str, Any]]:
        if query_type == "username":
            response = await client.get(f"https://codeberg.org/api/v1/users/{quote(query, safe='')}")
            if response.status_code == 404:
                return []
            response.raise_for_status()
            users = [response.json()]
        else:
            response = await client.get(
                "https://codeberg.org/api/v1/users/search",
                params={"q": query, "limit": 20},
            )
            response.raise_for_status()
            users = response.json().get("data", [])
        return [
            self._finding("Codeberg", item.get("login") or query, item.get("html_url"), "https://codeberg.org")
            for item in users
            if item.get("html_url")
        ]

    async def _stack_overflow(
        self, client: httpx.AsyncClient, query: str
    ) -> list[dict[str, Any]]:
        response = await client.get(
            "https://api.stackexchange.com/2.3/users",
            params={"site": "stackoverflow", "inname": query, "pagesize": 20},
        )
        response.raise_for_status()
        users = response.json().get("items", [])
        return [
            self._finding(
                "Stack Overflow",
                item.get("display_name") or query,
                item.get("link"),
                "https://stackoverflow.com",
            )
            for item in users
            if item.get("link")
        ]

    @staticmethod
    def _finding(site: str, username: str, url: str, site_url: str) -> dict[str, Any]:
        return {
            "connector": "public_profiles",
            "sources": ["public_profiles"],
            "site": site,
            "username": username,
            "profile_url": url,
            "site_url": site_url,
            "http_status": 200,
            "response_time_seconds": None,
            "classification": "aday_profil",
            "identity_status": "doğrulanmadı",
        }
