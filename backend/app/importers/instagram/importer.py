"""Instagram import worker — arşivi tara, veritabanına yaz."""

from __future__ import annotations

import hashlib
from pathlib import Path

from sqlalchemy.orm import Session

from ...models import (
    ConversationParticipant,
    ImportBatch,
    ImportedConversation,
    ImportedMessage,
    ImportedProfile,
    JobStatus,
)
from .adapter import parse_conversation
from .discovery import discover_archive


def _message_source_id(sender_name: str, timestamp_ms: int, seq: int) -> str:
    raw = f"{sender_name}:{timestamp_ms}:{seq}"
    return hashlib.sha256(raw.encode()).hexdigest()[:40]


def import_instagram_archive(db: Session, batch_id: str) -> None:
    """Instagram arşivini tara ve veritabanına aktar."""
    batch = db.get(ImportBatch, batch_id)
    if not batch:
        raise RuntimeError("Import batch bulunamadı.")

    root = Path(batch.import_root)
    if not root.exists():
        batch.status = JobStatus.failed.value
        batch.error_code = "import_root_missing"
        batch.error_message = f"Dizin bulunamadı: {batch.import_root}"
        db.commit()
        return

    manifest = discover_archive(root)

    batch.archive_owner_username = manifest.profile_username
    batch.archive_owner_display_name = manifest.profile_display_name
    batch.total_conversations = manifest.conversation_count
    batch.status = JobStatus.running.value
    db.commit()

    # Profil kaydı
    if manifest.profile_username:
        profile = ImportedProfile(
            import_batch_id=batch_id,
            username=manifest.profile_username,
            display_name=manifest.profile_display_name,
            is_archive_owner=True,
        )
        db.add(profile)

    inbox = root / "your_instagram_activity" / "messages" / "inbox"
    total_messages = 0

    if inbox.exists():
        for conv_dir in sorted(inbox.iterdir()):
            if not conv_dir.is_dir():
                continue
            try:
                conv = parse_conversation(conv_dir)
            except Exception:
                continue

            db_conv = ImportedConversation(
                import_batch_id=batch_id,
                source_conversation_id=conv.conversation_id,
                title=conv.title,
                message_count=conv.message_count,
                participant_names=[p.name for p in conv.participants],
            )
            if conv.date_range:
                db_conv.earliest_message_at = conv.date_range[0]
                db_conv.latest_message_at = conv.date_range[1]

            db.add(db_conv)
            db.flush()

            for p in conv.participants:
                db.add(ConversationParticipant(
                    conversation_id=db_conv.id,
                    name=p.name,
                ))

            for msg in conv.messages:
                source_id = _message_source_id(msg.sender_name, msg.timestamp_ms, msg.sequence)
                all_media = msg.photos + msg.videos + msg.audio
                db.add(ImportedMessage(
                    conversation_id=db_conv.id,
                    sender_name=msg.sender_name,
                    timestamp_ms=msg.timestamp_ms,
                    content=msg.content,
                    share_link=msg.share_link,
                    has_media=bool(all_media),
                    media_refs=all_media,
                    reactions=msg.reactions or [],
                    source_file=msg.source_file,
                    sequence=msg.sequence,
                    source_message_id=source_id,
                    content_hash=hashlib.sha256(
                        (msg.content or "").encode()
                    ).hexdigest(),
                ))
                total_messages += 1

            # Her 500 mesajda bir commit
            if total_messages % 500 == 0:
                db.commit()

    batch.total_messages = total_messages
    batch.status = JobStatus.completed.value
    db.commit()
