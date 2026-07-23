"""Instagram arşiv keşif — klasör yapısını tara, profil bilgisini bul."""

from __future__ import annotations

import json
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
    file_hashes: dict[str, str] = field(default_factory=dict)


def discover_archive(root_path: Path) -> ArchiveManifest:
    """Instagram export klasörünü tara, manifest çıkar."""
    manifest = ArchiveManifest(root_path=root_path)

    # Profil bilgisi — personal_information/personal_information/personal_information.json
    personal_info = root_path / "personal_information" / "personal_information" / "personal_information.json"
    if personal_info.exists():
        try:
            data = json.loads(personal_info.read_text(encoding="utf-8"))
            for item in data.get("profile_user", []):
                smd = item.get("string_map_data", {})
                if "Kullanıcı Adı" in smd:
                    manifest.profile_username = smd["Kullanıcı Adı"]["value"]
                if "Ad" in smd:
                    manifest.profile_display_name = smd["Ad"]["value"]
        except (json.JSONDecodeError, KeyError, UnicodeDecodeError):
            pass

    # Konuşma sayısı
    inbox = root_path / "your_instagram_activity" / "messages" / "inbox"
    if inbox.exists():
        conversations = [d for d in inbox.iterdir() if d.is_dir()]
        manifest.conversation_count = len(conversations)

    return manifest
