"""Instagram arşiv keşif — klasör yapısını tara, profil bilgisini bul."""

from __future__ import annotations

import json
import unicodedata
from dataclasses import dataclass, field
from pathlib import Path


@dataclass
class ArchiveManifest:
    root_path: Path
    version: str | None = None
    language: str | None = None
    date_range: tuple[int, int] | None = None
    profile_username: str | None = None
    profile_display_name: str | None = None
    conversation_count: int = 0
    total_message_count: int = 0
    message_file_count: int = 0
    unreadable_message_files: int = 0
    file_hashes: dict[str, str] = field(default_factory=dict)


def discover_archive(root_path: Path) -> ArchiveManifest:
    """Instagram export klasörünü tara, manifest çıkar."""
    manifest = ArchiveManifest(root_path=root_path)

    # Meta dışa aktarmaları sürüme göre iki profil şeması kullanıyor. Alanları
    # yalnızca sahip bilgisini göstermek için okuruz; konuşma içeriğini burada
    # keşfetmeyiz.
    profile_dir = root_path / "personal_information" / "personal_information"
    for personal_info in (
        profile_dir / "personal_information.json",
        profile_dir / "instagram_profile_information.json",
    ):
        if not personal_info.exists():
            continue
        try:
            data = json.loads(personal_info.read_text(encoding="utf-8"))
            for item in data.get("profile_user", []):
                smd = item.get("string_map_data", {})
                for label, value in smd.items():
                    _assign_profile_field(manifest, label, value.get("value"))
            for item in data.get("label_values", []):
                if isinstance(item, dict):
                    _assign_profile_field(manifest, item.get("label"), item.get("value"))
        except (json.JSONDecodeError, KeyError, UnicodeDecodeError):
            continue

    # Konuşma sayısı
    inbox = root_path / "your_instagram_activity" / "messages" / "inbox"
    if inbox.exists():
        conversations = [d for d in inbox.iterdir() if d.is_dir()]
        manifest.conversation_count = len(conversations)
        for conversation in conversations:
            for message_file in conversation.glob("message_*.json"):
                manifest.message_file_count += 1
                try:
                    payload = json.loads(message_file.read_text(encoding="utf-8"))
                    messages = payload.get("messages", [])
                    if isinstance(messages, list):
                        manifest.total_message_count += len(messages)
                    else:
                        manifest.unreadable_message_files += 1
                except (OSError, json.JSONDecodeError, UnicodeDecodeError):
                    manifest.unreadable_message_files += 1

    return manifest


def _assign_profile_field(manifest: ArchiveManifest, raw_label: object, raw_value: object) -> None:
    """Şema farklarından gelen profil alanlarını güvenli ve ihtiyatlı eşleştir."""
    if not isinstance(raw_label, str) or not isinstance(raw_value, str):
        return
    label = unicodedata.normalize("NFKD", _repair_text(raw_label)).casefold()
    value = _repair_text(raw_value)
    if not value:
        return
    if any(token in label for token in ("kullanici adi", "kullanıcı adı", "username")):
        manifest.profile_username = value
    elif label.strip() in {"ad", "isim", "name", "full name", "tam ad"}:
        manifest.profile_display_name = value


def _repair_text(value: str) -> str:
    """Meta'nın bazı exportlarında görülen UTF-8/Latin-1 bozulmasını düzelt."""
    cleaned = unicodedata.normalize("NFC", value).strip()
    if any(marker in cleaned for marker in ("Ã", "Ä", "Â", "â")):
        for encoding in ("latin-1", "cp1252"):
            try:
                return cleaned.encode(encoding).decode("utf-8").strip()
            except (UnicodeEncodeError, UnicodeDecodeError):
                continue
    return cleaned
