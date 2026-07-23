from app.council.evidence import build_evidence_pack, normalize_text


def test_builds_stable_evidence_ids_without_inventing_content() -> None:
    source = "İlk ifade.\n\nİkinci ifade ve bağlamı."
    segments = build_evidence_pack(source)

    assert [item.key for item in segments] == ["K1", "K2"]
    assert [item.content for item in segments] == ["İlk ifade.", "İkinci ifade ve bağlamı."]
    assert all(len(item.content_hash) == 64 for item in segments)


def test_normalizes_line_endings_but_preserves_words() -> None:
    assert normalize_text("bir\r\n\r\niki   \r\n") == "bir\n\niki"

