import httpx
import pytest

from app.providers import get_provider_registry
from app.providers.client import ProviderClient, ProviderError


def test_provider_registry_covers_cloud_local_and_custom_routes() -> None:
    registry = get_provider_registry()
    profiles = registry.all()
    ids = {item.id for item in profiles}

    assert len(profiles) >= 20
    assert {"openai", "anthropic", "gemini", "openrouter", "ollama", "custom-openai"} <= ids
    assert registry.get("claude").id == "anthropic"
    assert registry.get("ollama").capabilities.local is True


def test_insufficient_balance_is_reported_in_turkish() -> None:
    response = httpx.Response(
        400,
        json={"error": {"message": "Insufficient Balance"}},
        request=httpx.Request("POST", "https://example.com/chat"),
    )

    with pytest.raises(ProviderError) as caught:
        ProviderClient._raise_for_status(response)

    assert caught.value.code == "provider_balance_exhausted"
    assert "bakiyesi yetersiz" in str(caught.value)
