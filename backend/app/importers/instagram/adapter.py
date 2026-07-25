"""Instagram mesaj parse — conversation JSON'larını parse et."""

from __future__ import annotations

import json
import unicodedata
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path


@dataclass
class InstagramParticipant:
    name: str


@dataclass
class InstagramMessage:
    sender_name: str
    timestamp_ms: int
    content: str | None = None
    photos: list[str] = field(default_factory=list)
    videos: list[str] = field(default_factory=list)
    audio: list[str] = field(default_factory=list)
    share_link: str | None = None
    reactions: list[dict] = field(default_factory=list)
    source_file: str = ""
    sequence: int = 0

    @property
    def timestamp(self) -> datetime:
        return datetime.fromtimestamp(self.timestamp_ms / 1000, tz=timezone.utc)


@dataclass
class InstagramConversation:
    conversation_id: str
    title: str | None
    participants: list[InstagramParticipant]
    messages: list[InstagramMessage]
    source_path: str
    message_count: int = 0
    date_range: tuple[datetime, datetime] | None = None


def parse_conversation(conv_path: Path) -> InstagramConversation:
    """Tek bir konuşma dizinini parse et."""
    message_files = sorted(conv_path.glob("message_*.json"))
    participants: list[InstagramParticipant] = []
    messages: list[InstagramMessage] = []
    seq = 0

    for mf in message_files:
        try:
            data = json.loads(mf.read_text(encoding="utf-8"))
        except (json.JSONDecodeError, UnicodeDecodeError):
            continue

        if not participants:
            participants = [
                InstagramParticipant(name=_normalize_export_text(p.get("name")) or "Bilinmeyen katılımcı")
                for p in data.get("participants", [])
                if isinstance(p, dict)
            ]

        for msg in data.get("messages", []):
            seq += 1
            photos = []
            videos = []
            audio = []
            if "photos" in msg:
                photos = [p.get("uri", "") for p in msg["photos"]]
            if "videos" in msg:
                videos = [v.get("uri", "") for v in msg["videos"]]
            if "audio_files" in msg:
                audio = [a.get("uri", "") for a in msg["audio_files"]]

            share_link = None
            if "share" in msg:
                share_link = msg["share"].get("link")

            reactions = msg.get("reactions", [])

            messages.append(InstagramMessage(
                sender_name=_normalize_export_text(msg.get("sender_name")) or "Bilinmeyen katılımcı",
                timestamp_ms=int(msg.get("timestamp_ms", 0) or 0),
                content=_normalize_export_text(msg.get("content")),
                photos=photos,
                videos=videos,
                audio=audio,
                share_link=share_link,
                reactions=reactions,
                source_file=str(mf.relative_to(conv_path)),
                sequence=seq,
            ))

    conv_id = conv_path.name
    raw_title = conv_path.name.split("_")[0] if "_" in conv_path.name else conv_path.name
    conv_title = _normalize_export_text(raw_title) or raw_title

    date_range = None
    if messages:
        ordered_timestamps = [message.timestamp for message in messages if message.timestamp_ms > 0]
        if ordered_timestamps:
            date_range = (min(ordered_timestamps), max(ordered_timestamps))

    return InstagramConversation(
        conversation_id=conv_id,
        title=conv_title,
        participants=participants,
        messages=messages,
        source_path=str(conv_path),
        message_count=len(messages),
        date_range=date_range,
    )


def _normalize_export_text(value: object) -> str | None:
    if not isinstance(value, str):
        return None
    cleaned = unicodedata.normalize("NFC", value).strip()
    if any(marker in cleaned for marker in ("Ã", "Ä", "Â", "â")):
        for encoding in ("latin-1", "cp1252"):
            try:
                cleaned = cleaned.encode(encoding).decode("utf-8")
                break
            except (UnicodeEncodeError, UnicodeDecodeError):
                continue
    return cleaned or None
