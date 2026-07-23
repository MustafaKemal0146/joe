"""Instagram mesaj parse — conversation JSON'larını parse et."""

from __future__ import annotations

import json
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
                InstagramParticipant(name=p["name"])
                for p in data.get("participants", [])
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
                sender_name=msg.get("sender_name", ""),
                timestamp_ms=msg.get("timestamp_ms", 0),
                content=msg.get("content"),
                photos=photos,
                videos=videos,
                audio=audio,
                share_link=share_link,
                reactions=reactions,
                source_file=str(mf.relative_to(conv_path)),
                sequence=seq,
            ))

    conv_id = conv_path.name
    conv_title = conv_path.name.split("_")[0] if "_" in conv_path.name else conv_path.name

    date_range = None
    if messages:
        date_range = (messages[-1].timestamp, messages[0].timestamp)

    return InstagramConversation(
        conversation_id=conv_id,
        title=conv_title,
        participants=participants,
        messages=messages,
        source_path=str(conv_path),
        message_count=len(messages),
        date_range=date_range,
    )
