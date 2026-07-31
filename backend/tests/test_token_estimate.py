from app.council.pricing import estimate_cost_usd
from app.api import _estimate_analysis_tokens


def test_estimate_analysis_tokens():
    result = _estimate_analysis_tokens(total_chars=1000, persona_count=2)
    assert result["estimated_prompts"] == 7
    assert result["estimated_input_tokens"] > 0
    assert result["estimated_output_tokens"] == 2 * 1200 + 2 * 1000 + 2 * 1200 + 1500
    assert result["estimated_total_tokens"] == result["estimated_input_tokens"] + result["estimated_output_tokens"]


def test_estimate_analysis_tokens_four_personas():
    result = _estimate_analysis_tokens(total_chars=100_000, persona_count=4)
    assert result["estimated_prompts"] == 13
    # Kanıt taşıyan promptlar: bağımsız görüş (4) + sentez (1)
    expected_evidence_tokens = (4 + 1) * (100_000 // 4)
    assert result["estimated_input_tokens"] == 13 * 1000 + expected_evidence_tokens


def test_deepseek_v4_pro_pricing():
    cost, note = estimate_cost_usd("deepseek", input_tokens=1_000_000, output_tokens=384_000)
    assert cost is not None
    assert note is None
    # 1M input * 0.435 + 384K output * 0.87 per 1M
    expected = round(0.435 + (384_000 * 0.87 / 1_000_000), 6)
    assert cost == expected


def test_unknown_provider_pricing_returns_none():
    cost, note = estimate_cost_usd("unknown-provider", input_tokens=1000, output_tokens=500)
    assert cost is None
    assert note is not None
    assert "fiyat" in note.casefold()
