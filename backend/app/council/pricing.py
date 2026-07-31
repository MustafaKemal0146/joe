from __future__ import annotations

# Fiyatlar sağlayıcı başına 1M token için USD cinsindendir.
# Giriş fiyatları cache-miss (en kötü durum) varsayımıyla tutulur; böylece
# kullanıcı gerçekleşebilecek en yüksek maliyeti önceden görür.
PRICE_PER_1M_TOKENS: dict[str, dict[str, float]] = {
    "deepseek": {
        "input": 0.435,
        "output": 0.87,
    },
    "openai": {
        "input": 2.50,
        "output": 10.00,
    },
    "openai-codex": {
        "input": 1.50,
        "output": 6.00,
    },
    "anthropic": {
        "input": 3.00,
        "output": 15.00,
    },
    "gemini": {
        "input": 0.50,
        "output": 1.50,
    },
    "openrouter": {
        # OpenRouter model başına farklılık gösterir; güvenli varsayılan.
        "input": 1.00,
        "output": 3.00,
    },
    "groq": {
        "input": 0.50,
        "output": 0.80,
    },
    "mistral": {
        "input": 0.50,
        "output": 1.50,
    },
    "xai": {
        "input": 2.00,
        "output": 10.00,
    },
    "together": {
        "input": 1.00,
        "output": 3.00,
    },
    "fireworks": {
        "input": 0.90,
        "output": 2.70,
    },
    "azure-openai": {
        "input": 2.50,
        "output": 10.00,
    },
}


def estimate_cost_usd(provider_id: str, input_tokens: int, output_tokens: int) -> tuple[float | None, str | None]:
    """Tahmini token sayılarına göre USD maliyet döndür.

    Sağlayıcı fiyatı bilinmiyorsa (None, açıklama) döner.
    """
    normalized = provider_id.casefold().strip()
    prices = PRICE_PER_1M_TOKENS.get(normalized)
    if not prices:
        return None, (
            f"'{provider_id}' sağlayıcısı için yerleşik fiyat bilgisi yok. "
            "Tahmini maliyeti görmek için sağlayıcının güncel fiyatlarını kontrol et."
        )
    input_cost = input_tokens * prices["input"] / 1_000_000
    output_cost = output_tokens * prices["output"] / 1_000_000
    return round(input_cost + output_cost, 6), None
