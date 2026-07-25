from __future__ import annotations

import json
from pathlib import Path

from app.importers.instagram.adapter import parse_conversation
from app.importers.instagram.discovery import discover_archive


def _write_json(path: Path, payload: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")


def test_discovers_current_meta_profile_schema_and_counts_messages(tmp_path: Path) -> None:
    _write_json(
        tmp_path / "personal_information" / "personal_information" / "instagram_profile_information.json",
        {"label_values": [
            {"label": "Kullanıcı Adı", "value": "ornek_kullanici"},
            {"label": "Ad", "value": "Örnek Kişi"},
        ]},
    )
    _write_json(
        tmp_path / "your_instagram_activity" / "messages" / "inbox" / "sohbet_1" / "message_1.json",
        {"messages": [{"timestamp_ms": 1_735_572_578_562}, {"timestamp_ms": 1_735_572_578_563}]},
    )

    manifest = discover_archive(tmp_path)

    assert manifest.profile_username == "ornek_kullanici"
    assert manifest.profile_display_name == "Örnek Kişi"
    assert manifest.conversation_count == 1
    assert manifest.message_file_count == 1
    assert manifest.total_message_count == 2


def test_parser_preserves_millisecond_timestamp_and_orders_date_range(tmp_path: Path) -> None:
    conversation = tmp_path / "sohbet_1"
    _write_json(
        conversation / "message_1.json",
        {
            "participants": [{"name": chr(0xC3) + chr(0x96) + "zne"}, {"name": "Bağlam"}],
            "messages": [
                {"sender_name": "Özne", "timestamp_ms": 1_735_572_578_562, "content": "son mesaj"},
                {"sender_name": "Bağlam", "timestamp_ms": 1_735_572_578_000, "content": "ilk mesaj"},
            ],
        },
    )

    parsed = parse_conversation(conversation)

    assert parsed.messages[0].timestamp_ms == 1_735_572_578_562
    assert parsed.participants[0].name == "Özne"
    assert parsed.date_range is not None
    assert parsed.date_range[0] < parsed.date_range[1]
