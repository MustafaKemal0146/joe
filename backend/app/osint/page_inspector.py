from __future__ import annotations

import hashlib
import ipaddress
import re
import socket
from dataclasses import dataclass
from urllib.parse import urljoin, urlsplit

import httpx


class PageInspectionError(RuntimeError):
    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


@dataclass(slots=True)
class PageObservation:
    url: str
    status_code: int
    title: str | None
    text: str
    content_hash: str
    content_type: str


def _validate_public_url(url: str) -> None:
    parsed = urlsplit(url)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        raise PageInspectionError("invalid_url", "Yalnız HTTP/HTTPS kaynakları incelenebilir.")
    host = parsed.hostname.casefold().rstrip(".")
    if host in {"localhost", "localhost.localdomain"} or host.endswith(".local"):
        raise PageInspectionError("private_host", "Yerel ağ kaynakları incelenemez.")
    try:
        addresses = {item[4][0] for item in socket.getaddrinfo(host, None)}
    except OSError as exc:
        raise PageInspectionError("dns_failed", "Kaynağın adresi çözülemedi.") from exc
    for address in addresses:
        ip = ipaddress.ip_address(address)
        if ip.is_private or ip.is_loopback or ip.is_link_local or ip.is_reserved or ip.is_multicast:
            raise PageInspectionError("private_host", "Özel veya yerel ağ adresleri incelenemez.")


def _visible_text(html: str) -> str:
    without_script = re.sub(r"<(script|style|noscript)\b[^>]*>.*?</\1>", " ", html, flags=re.I | re.S)
    text = re.sub(r"<[^>]+>", " ", without_script)
    return re.sub(r"\s+", " ", text).strip()


def _title(html: str) -> str | None:
    match = re.search(r"<title[^>]*>(.*?)</title>", html, flags=re.I | re.S)
    if not match:
        return None
    return re.sub(r"\s+", " ", re.sub(r"<[^>]+>", " ", match.group(1))).strip()[:500] or None


async def inspect_url(url: str, *, max_bytes: int = 2_000_000, max_redirects: int = 4) -> PageObservation:
    current = url
    async with httpx.AsyncClient(
        timeout=httpx.Timeout(20.0, connect=8.0),
        follow_redirects=False,
        headers={"User-Agent": "JoeResearch/0.1 (+local-source-observation)"},
    ) as client:
        for _ in range(max_redirects + 1):
            _validate_public_url(current)
            try:
                response = await client.get(current)
            except httpx.TimeoutException as exc:
                raise PageInspectionError("page_timeout", "Kaynak zaman aşımına uğradı.") from exc
            except httpx.RequestError as exc:
                raise PageInspectionError("page_fetch_failed", "Kaynak okunamadı.") from exc
            if response.is_redirect:
                location = response.headers.get("location")
                if not location:
                    break
                current = urljoin(current, location)
                continue
            if response.status_code >= 400:
                raise PageInspectionError("page_http_error", f"Kaynak HTTP {response.status_code} döndürdü.")
            content_type = response.headers.get("content-type", "")
            raw = response.content[:max_bytes]
            encoding = response.encoding or "utf-8"
            html = raw.decode(encoding, errors="replace")
            text = _visible_text(html)[:500_000]
            return PageObservation(
                url=str(response.url),
                status_code=response.status_code,
                title=_title(html),
                text=text,
                content_hash=hashlib.sha256(raw).hexdigest(),
                content_type=content_type[:160],
            )
    raise PageInspectionError("too_many_redirects", "Kaynak yönlendirme sınırını aştı.")
