from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any
from urllib.parse import quote

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

    async def chat(
        self,
        system: str,
        user: str,
        *,
        temperature: float = 0.2,
        structured: bool = True,
    ) -> ProviderResponse:
        protocol = self.profile.protocol
        if protocol == "openai-chat":
            return await self._openai_chat(system, user, temperature, structured)
        if protocol == "anthropic":
            return await self._anthropic(system, user, temperature)
        if protocol == "gemini":
            return await self._gemini(system, user, temperature)
        if protocol == "ollama":
            return await self._ollama(system, user, temperature, structured)
        if protocol == "azure-openai":
            return await self._azure_openai(system, user, temperature, structured)
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
                        f"{self.base_url}/models", params={"key": self.api_key}
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
        self, system: str, user: str, temperature: float, structured: bool
    ) -> ProviderResponse:
        payload: dict[str, Any] = {
            "model": self.model,
            "messages": [
                {"role": "system", "content": system},
                {"role": "user", "content": user},
            ],
            "temperature": temperature,
        }
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
        self, system: str, user: str, temperature: float
    ) -> ProviderResponse:
        data = await self._post_json(
            f"{self.base_url}/messages",
            {
                "model": self.model,
                "system": system,
                "messages": [{"role": "user", "content": user}],
                "max_tokens": int(self.extra_config.get("max_tokens", 4096)),
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

    async def _gemini(self, system: str, user: str, temperature: float) -> ProviderResponse:
        url = f"{self.base_url}/models/{quote(self.model, safe='-_.')}:generateContent"
        data = await self._post_json(
            url,
            {
                "systemInstruction": {"parts": [{"text": system}]},
                "contents": [{"role": "user", "parts": [{"text": user}]}],
                "generationConfig": {
                    "temperature": temperature,
                    "responseMimeType": "application/json",
                },
            },
            {"content-type": "application/json"},
            params={"key": self.api_key},
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
        self, system: str, user: str, temperature: float, structured: bool
    ) -> ProviderResponse:
        payload: dict[str, Any] = {
            "model": self.model,
            "messages": [
                {"role": "system", "content": system},
                {"role": "user", "content": user},
            ],
            "stream": False,
            "options": {"temperature": temperature},
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
        self, system: str, user: str, temperature: float, structured: bool
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
                {"role": "user", "content": user},
            ],
            "temperature": temperature,
        }
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
        elif response.status_code == 429:
            message = "Sağlayıcı hız veya kota sınırına ulaştı."
        raise ProviderError("provider_http_error", message[:500], response.status_code)

