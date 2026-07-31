from __future__ import annotations

import json
import ipaddress
import socket
from dataclasses import dataclass
from typing import Any
from urllib.parse import quote, urlparse

import httpx

from .registry import ProviderProfile


class ProviderError(RuntimeError):
    def __init__(self, code: str, message: str, status_code: int | None = None) -> None:
        super().__init__(message)
        self.code = code
        self.status_code = status_code


@dataclass(slots=True)
class ProviderResponse:
    text: str
    model: str
    usage: dict[str, Any]
    raw_id: str | None = None


class ProviderClient:
    def __init__(
        self,
        profile: ProviderProfile,
        model: str,
        api_key: str | None,
        base_url: str | None = None,
        extra_config: dict[str, Any] | None = None,
        timeout: float = 90.0,
    ) -> None:
        self.profile = profile
        self.model = model
        self.api_key = api_key
        self.base_url = (base_url or profile.default_base_url or "").rstrip("/")
        self.extra_config = extra_config or {}
        self.timeout = timeout

        if profile.requires_api_key and not api_key:
            raise ProviderError("api_key_required", f"{profile.name} için API anahtarı gerekli.")
        if not self.base_url:
            raise ProviderError("base_url_required", "Bu sağlayıcı için bir temel URL gerekli.")
        self._validate_base_url()

    def _validate_base_url(self) -> None:
        """Reject malformed/SSRF-prone provider endpoints before any request."""
        parsed = urlparse(self.base_url)
        if parsed.scheme not in {"http", "https"} or not parsed.hostname:
            raise ProviderError("invalid_base_url", "Sağlayıcı temel URL'si http/https olmalıdır.")
        if parsed.username or parsed.password:
            raise ProviderError("invalid_base_url", "Sağlayıcı URL'sinde kullanıcı bilgisi kullanılamaz.")
        host = parsed.hostname.lower().rstrip(".")
        # Ollama is intentionally a local protocol; Docker users commonly point
        # it at the compose service name, which resolves to a private container IP.
        if self.profile.protocol == "ollama":
            return
        try:
            addresses = {
                item[4][0]
                for item in socket.getaddrinfo(host, parsed.port or (443 if parsed.scheme == "https" else 80), type=socket.SOCK_STREAM)
            }
        except OSError as exc:
            raise ProviderError("invalid_base_url", "Sağlayıcı adresi çözümlenemedi.") from exc
        for address in addresses:
            ip = ipaddress.ip_address(address)
            if ip.is_private or ip.is_loopback or ip.is_link_local or ip.is_reserved or ip.is_multicast:
                raise ProviderError("unsafe_base_url", "Özel veya yerel ağ sağlayıcı adreslerine izin verilmiyor.")

    async def chat(
        self,
        system: str,
        user: str,
        *,
        temperature: float = 0.2,
        structured: bool = True,
        attachments: list[dict[str, str]] | None = None,
        max_tokens: int | None = None,
    ) -> ProviderResponse:
        attachments = attachments or []
        if attachments and not self.profile.capabilities.vision:
            raise ProviderError("vision_unsupported", "Seçili sağlayıcı görsel analizi desteklemiyor.")
        protocol = self.profile.protocol
        if protocol == "openai-chat":
            return await self._openai_chat(system, user, temperature, structured, attachments, max_tokens)
        if protocol == "anthropic":
            return await self._anthropic(system, user, temperature, attachments, max_tokens)
        if protocol == "gemini":
            return await self._gemini(system, user, temperature, attachments, max_tokens)
        if protocol == "ollama":
            return await self._ollama(system, user, temperature, structured, attachments, max_tokens)
        if protocol == "azure-openai":
            return await self._azure_openai(system, user, temperature, structured, attachments, max_tokens)
        raise ProviderError("unsupported_protocol", f"Desteklenmeyen protokol: {protocol}")

    async def list_models(self) -> list[str]:
        protocol = self.profile.protocol
        async with httpx.AsyncClient(timeout=min(self.timeout, 30.0)) as client:
            try:
                if protocol == "openai-chat":
                    response = await client.get(
                        f"{self.base_url}/models",
                        headers={"Authorization": f"Bearer {self.api_key}"},
                    )
                    self._raise_for_status(response)
                    return sorted(
                        item["id"] for item in response.json().get("data", []) if item.get("id")
                    )
                if protocol == "ollama":
                    response = await client.get(f"{self.base_url}/api/tags")
                    self._raise_for_status(response)
                    return sorted(
                        item["name"] for item in response.json().get("models", []) if item.get("name")
                    )
                if protocol == "gemini":
                    response = await client.get(
                        f"{self.base_url}/models", headers={"x-goog-api-key": self.api_key or ""}
                    )
                    self._raise_for_status(response)
                    return sorted(
                        item["name"].removeprefix("models/")
                        for item in response.json().get("models", [])
                        if "generateContent" in item.get("supportedGenerationMethods", [])
                    )
            except httpx.RequestError as exc:
                raise ProviderError("connection_failed", "Sağlayıcıya bağlanılamadı.") from exc
        return []

    async def _openai_chat(
        self,
        system: str,
        user: str,
        temperature: float,
        structured: bool,
        attachments: list[dict[str, str]],
        max_tokens: int | None,
    ) -> ProviderResponse:
        payload: dict[str, Any] = {
            "model": self.model,
            "messages": [
                {"role": "system", "content": system},
                {"role": "user", "content": self._openai_content(user, attachments)},
            ],
            "temperature": temperature,
        }
        if max_tokens:
            payload["max_tokens"] = max_tokens
        if structured and self.profile.capabilities.structured_output:
            payload["response_format"] = {"type": "json_object"}
        data = await self._post_json(
            f"{self.base_url}/chat/completions",
            payload,
            {"Authorization": f"Bearer {self.api_key}", "Content-Type": "application/json"},
        )
        try:
            content = data["choices"][0]["message"]["content"]
        except (KeyError, IndexError, TypeError) as exc:
            raise ProviderError("invalid_response", "Sağlayıcı beklenen mesaj alanını döndürmedi.") from exc
        if isinstance(content, list):
            content = "\n".join(part.get("text", "") for part in content if isinstance(part, dict))
        return ProviderResponse(
            text=str(content),
            model=data.get("model", self.model),
            usage=data.get("usage", {}),
            raw_id=data.get("id"),
        )

    async def _anthropic(
        self,
        system: str,
        user: str,
        temperature: float,
        attachments: list[dict[str, str]],
        max_tokens: int | None,
    ) -> ProviderResponse:
        data = await self._post_json(
            f"{self.base_url}/messages",
            {
                "model": self.model,
                "system": system,
                "messages": [{"role": "user", "content": self._anthropic_content(user, attachments)}],
                "max_tokens": max_tokens or int(self.extra_config.get("max_tokens", 4096)),
                "temperature": temperature,
            },
            {
                "x-api-key": self.api_key or "",
                "anthropic-version": str(self.extra_config.get("anthropic_version", "2023-06-01")),
                "content-type": "application/json",
            },
        )
        blocks = data.get("content", [])
        text = "\n".join(block.get("text", "") for block in blocks if block.get("type") == "text")
        if not text:
            raise ProviderError("invalid_response", "Anthropic metin içeriği döndürmedi.")
        return ProviderResponse(
            text=text,
            model=data.get("model", self.model),
            usage=data.get("usage", {}),
            raw_id=data.get("id"),
        )

    async def _gemini(
        self,
        system: str,
        user: str,
        temperature: float,
        attachments: list[dict[str, str]],
        max_tokens: int | None,
    ) -> ProviderResponse:
        url = f"{self.base_url}/models/{quote(self.model, safe='-_.')}:generateContent"
        generation_config: dict[str, Any] = {
            "temperature": temperature,
            "responseMimeType": "application/json",
        }
        if max_tokens:
            generation_config["maxOutputTokens"] = max_tokens
        data = await self._post_json(
            url,
            {
                "systemInstruction": {"parts": [{"text": system}]},
                "contents": [{"role": "user", "parts": self._gemini_parts(user, attachments)}],
                "generationConfig": generation_config,
            },
            {"content-type": "application/json", "x-goog-api-key": self.api_key or ""},
        )
        try:
            parts = data["candidates"][0]["content"]["parts"]
            text = "\n".join(part.get("text", "") for part in parts)
        except (KeyError, IndexError, TypeError) as exc:
            raise ProviderError("invalid_response", "Gemini beklenen metin içeriğini döndürmedi.") from exc
        return ProviderResponse(
            text=text,
            model=self.model,
            usage=data.get("usageMetadata", {}),
        )

    async def _ollama(
        self,
        system: str,
        user: str,
        temperature: float,
        structured: bool,
        attachments: list[dict[str, str]],
        max_tokens: int | None,
    ) -> ProviderResponse:
        options: dict[str, Any] = {"temperature": temperature}
        if max_tokens:
            options["num_predict"] = max_tokens
        payload: dict[str, Any] = {
            "model": self.model,
            "messages": [
                {"role": "system", "content": system},
                {
                    "role": "user",
                    "content": user,
                    **({"images": [item["data"] for item in attachments]} if attachments else {}),
                },
            ],
            "stream": False,
            "options": options,
        }
        if structured:
            payload["format"] = "json"
        data = await self._post_json(
            f"{self.base_url}/api/chat", payload, {"content-type": "application/json"}
        )
        text = data.get("message", {}).get("content")
        if not text:
            raise ProviderError("invalid_response", "Ollama metin içeriği döndürmedi.")
        usage = {
            "prompt_tokens": data.get("prompt_eval_count"),
            "completion_tokens": data.get("eval_count"),
        }
        return ProviderResponse(text=text, model=data.get("model", self.model), usage=usage)

    async def _azure_openai(
        self,
        system: str,
        user: str,
        temperature: float,
        structured: bool,
        attachments: list[dict[str, str]],
        max_tokens: int | None,
    ) -> ProviderResponse:
        deployment = str(self.extra_config.get("deployment") or self.model)
        api_version = str(self.extra_config.get("api_version") or "2024-10-21")
        url = (
            f"{self.base_url}/openai/deployments/{quote(deployment, safe='-_.')}"
            f"/chat/completions?api-version={quote(api_version)}"
        )
        payload: dict[str, Any] = {
            "messages": [
                {"role": "system", "content": system},
                {"role": "user", "content": self._openai_content(user, attachments)},
            ],
            "temperature": temperature,
        }
        if max_tokens:
            payload["max_tokens"] = max_tokens
        if structured:
            payload["response_format"] = {"type": "json_object"}
        data = await self._post_json(
            url, payload, {"api-key": self.api_key or "", "content-type": "application/json"}
        )
        try:
            text = data["choices"][0]["message"]["content"]
        except (KeyError, IndexError, TypeError) as exc:
            raise ProviderError("invalid_response", "Azure OpenAI beklenen yanıtı döndürmedi.") from exc
        return ProviderResponse(
            text=text,
            model=data.get("model", deployment),
            usage=data.get("usage", {}),
            raw_id=data.get("id"),
        )

    async def _post_json(
        self,
        url: str,
        payload: dict[str, Any],
        headers: dict[str, str],
        params: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        try:
            async with httpx.AsyncClient(timeout=self.timeout) as client:
                response = await client.post(url, json=payload, headers=headers, params=params)
        except httpx.TimeoutException as exc:
            raise ProviderError("provider_timeout", "Sağlayıcı zaman aşımına uğradı.") from exc
        except httpx.RequestError as exc:
            raise ProviderError("connection_failed", "Sağlayıcıya bağlanılamadı.") from exc
        self._raise_for_status(response)
        try:
            return response.json()
        except json.JSONDecodeError as exc:
            raise ProviderError("invalid_json", "Sağlayıcı geçerli JSON döndürmedi.") from exc

    @staticmethod
    def _openai_content(user: str, attachments: list[dict[str, str]]) -> str | list[dict[str, Any]]:
        if not attachments:
            return user
        return [
            {"type": "text", "text": user},
            *[
                {
                    "type": "image_url",
                    "image_url": {
                        "url": f"data:{item['media_type']};base64,{item['data']}"
                    },
                }
                for item in attachments
            ],
        ]

    @staticmethod
    def _anthropic_content(user: str, attachments: list[dict[str, str]]) -> str | list[dict[str, Any]]:
        if not attachments:
            return user
        return [
            {"type": "text", "text": user},
            *[
                {
                    "type": "image",
                    "source": {
                        "type": "base64",
                        "media_type": item["media_type"],
                        "data": item["data"],
                    },
                }
                for item in attachments
            ],
        ]

    @staticmethod
    def _gemini_parts(user: str, attachments: list[dict[str, str]]) -> list[dict[str, Any]]:
        return [
            {"text": user},
            *[
                {"inline_data": {"mime_type": item["media_type"], "data": item["data"]}}
                for item in attachments
            ],
        ]

    @staticmethod
    def _raise_for_status(response: httpx.Response) -> None:
        if response.is_success:
            return
        message = "Sağlayıcı isteği reddetti."
        try:
            payload = response.json()
            remote = payload.get("error")
            if isinstance(remote, dict):
                message = str(remote.get("message") or message)
            elif remote:
                message = str(remote)
        except (ValueError, TypeError):
            pass
        if response.status_code in {401, 403}:
            message = "Sağlayıcı kimlik doğrulamasını reddetti. API anahtarını kontrol et."
        elif response.status_code == 402 or "insufficient balance" in message.casefold():
            raise ProviderError(
                "provider_balance_exhausted",
                "Sağlayıcı bakiyesi yetersiz. Bağlantının kredi/bakiye durumunu kontrol et.",
                response.status_code,
            )
        elif response.status_code == 429:
            message = "Sağlayıcı hız veya kota sınırına ulaştı."
        raise ProviderError("provider_http_error", message[:500], response.status_code)
