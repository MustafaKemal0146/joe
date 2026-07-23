from app.providers import get_provider_registry


def test_provider_registry_covers_cloud_local_and_custom_routes() -> None:
    registry = get_provider_registry()
    profiles = registry.all()
    ids = {item.id for item in profiles}

    assert len(profiles) >= 20
    assert {"openai", "anthropic", "gemini", "openrouter", "ollama", "custom-openai"} <= ids
    assert registry.get("claude").id == "anthropic"
    assert registry.get("ollama").capabilities.local is True

