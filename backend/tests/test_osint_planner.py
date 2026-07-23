from app.osint.planner import build_manual_plan


def test_username_plan_marks_every_link_as_a_lead_not_evidence() -> None:
    plan = build_manual_plan("araştırmacı", "username")

    assert plan["query"] == "araştırmacı"
    assert plan["direct_profile_leads"]
    assert all(item["classification"] == "araştırma_adayı" for item in plan["searches"])
    assert "kanıt değil" in plan["notice"]


def test_full_name_plan_uses_exact_phrase_queries() -> None:
    plan = build_manual_plan("Ada Lovelace", "full_name")
    queries = {item["query"] for item in plan["searches"]}
    assert '"Ada Lovelace"' in queries
    assert any("site:linkedin.com/in" in item for item in queries)

