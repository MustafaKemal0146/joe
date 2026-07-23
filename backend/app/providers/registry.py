from __future__ import annotations

from functools import lru_cache
from pathlib import Path
from typing import Any, Literal

import yaml
from pydantic import BaseModel, Field, HttpUrl


ProviderProtocol = Literal[
    "openai-chat",
    "anthropic",
    "gemini",
    "ollama",
    "azure-openai",
]


class ProviderCapabilities(BaseModel):
    structured_output: bool = False
    tools: bool = False
    vision: bool = False
    model_listing: bool = False
    local: bool = False


class ProviderProfile(BaseModel):
    id: str
    name: str
    protocol: ProviderProtocol
    default_base_url: str | None = None
    requires_api_key: bool = True
    api_key_label: str = "API anahtarı"
    model_hint: str | None = None
    docs_url: HttpUrl | None = None
    capabilities: ProviderCapabilities = Field(default_factory=ProviderCapabilities)
    aliases: list[str] = Field(default_factory=list)
    notes: str | None = None

    def public_dict(self) -> dict[str, Any]:
        data = self.model_dump(mode="json")
        data["docs_url"] = str(self.docs_url) if self.docs_url else None
        return data


class ProviderRegistry:
    def __init__(self, profile_dir: Path | None = None) -> None:
        self.profile_dir = profile_dir or Path(__file__).with_name("profiles")
        self._profiles: dict[str, ProviderProfile] = {}
        self.reload()

    def reload(self) -> None:
        loaded: dict[str, ProviderProfile] = {}
        for path in sorted(self.profile_dir.glob("*.yaml")):
            raw = yaml.safe_load(path.read_text(encoding="utf-8")) or []
            entries = raw if isinstance(raw, list) else [raw]
            for entry in entries:
                profile = ProviderProfile.model_validate(entry)
                if profile.id in loaded:
                    raise ValueError(f"Yinelenen sağlayıcı profili: {profile.id}")
                loaded[profile.id] = profile
        if not loaded:
            raise RuntimeError("Hiç sağlayıcı profili yüklenemedi.")
        self._profiles = loaded

    def all(self) -> list[ProviderProfile]:
        return sorted(self._profiles.values(), key=lambda item: item.name.casefold())

    def get(self, provider_id: str) -> ProviderProfile:
        normalized = provider_id.casefold().strip()
        if normalized in self._profiles:
            return self._profiles[normalized]
        for profile in self._profiles.values():
            if normalized in {alias.casefold() for alias in profile.aliases}:
                return profile
        raise KeyError(provider_id)


@lru_cache(maxsize=1)
def get_provider_registry() -> ProviderRegistry:
    return ProviderRegistry()

