# Joe P0 + Instagram İmplementasyon Planı

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** yapılacaklar.md P0 maddelerini ve Instagram arşiv içe aktarma altyapısını uygula.

**Architecture:** Alembic migration → normalize tablolar → route bazlı kalıcılık → canlı event altyapısı → UI güncellemeleri. Her adım test ve build ile doğrulanacak.

**Tech Stack:** Next.js 16 + React 19, FastAPI + SQLAlchemy, PostgreSQL, Redis, Docker Compose, Alembic

**Instagram veri kaynağı:** `C:\datas\server\c-arşiv\data` — 183 inbox konuşması, JSON formatında mesajlar, 1921 dosya toplam.

---

## Aşama 0: Alembic ve Veritabanı Hazırlığı

### Task 0.1: Alembic kurulumu ve ilk migration

**Objective:** Alembic'i projeye ekle, mevcut şemayı initial migration olarak al.

**Files:**
- Create: `backend/alembic.ini`
- Create: `backend/alembic/env.py`
- Create: `backend/alembic/script.py.mako`
- Create: `backend/alembic/versions/001_initial.py`
- Modify: `backend/pyproject.toml` veya `backend/requirements.txt`

**Step 1: Alembic kurulumu**

```bash
cd backend && pip install alembic
```

**Step 2: Alembic init**

```bash
cd backend && alembic init alembic
```

**Step 3: `alembic/env.py` yapılandır**

`Base.metadata` target olarak ayarla, `db.py`'den `engine` ve `Base` import et.

```python
# backend/alembic/env.py - önemli kısım
from app.db import Base, engine
from app import models  # noqa: F401 — tüm modelleri yükle
target_metadata = Base.metadata
```

**Step 4: İlk migration oluştur**

```bash
cd backend && alembic revision --autogenerate -m "initial_schema"
```

**Step 5: Migration dosyasını incele**

`backend/alembic/versions/` altındaki migration'da `create_all` ile aynı tabloları ürettiğinden emin ol.

**Step 6: Mevcut volume'ü yedekle**

```bash
docker compose down
docker volume ls | grep joe
# Volume'leri yedeklemek için not al, silme!
```

**Verification:**
- `alembic upgrade head` komutu hatasız çalışmalı
- Mevcut PostgreSQL volume'ü korunmalı

---

## Aşama 1: Veritabanı Normalizasyonu — Yeni Tablolar

### Task 1.1: `osint_findings` ve `osint_finding_sources` tabloları

**Objective:** JSON'daki bulguları normalize et, her bulgu ayrı satır.

**Files:**
- Modify: `backend/app/models.py`
- Create: `backend/alembic/versions/002_osint_findings.py`

**Step 1: Model sınıflarını ekle**

```python
# backend/app/models.py — ekle
class OsintFinding(Base):
    __tablename__ = "osint_findings"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    osint_run_id: Mapped[str] = mapped_column(
        ForeignKey("osint_runs.id", ondelete="CASCADE"), index=True
    )
    profile_url: Mapped[str] = mapped_column(String(2000))
    site: Mapped[str] = mapped_column(String(200))
    username: Mapped[str] = mapped_column(String(300))
    http_status: Mapped[str | None] = mapped_column(String(10))
    identity_status: Mapped[str] = mapped_column(String(40), default="doğrulanmadı")
    verification_status: Mapped[str] = mapped_column(String(40), default="doğrulanmadı")
    first_seen_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    last_checked_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))
    dedup_hash: Mapped[str] = mapped_column(String(64), index=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    __table_args__ = (
        UniqueConstraint("osint_run_id", "dedup_hash"),
    )


class OsintFindingSource(Base):
    __tablename__ = "osint_finding_sources"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    finding_id: Mapped[str] = mapped_column(
        ForeignKey("osint_findings.id", ondelete="CASCADE"), index=True
    )
    connector: Mapped[str] = mapped_column(String(80))
    site: Mapped[str] = mapped_column(String(200))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
```

**Step 2: Migration oluştur**

```bash
cd backend && alembic revision --autogenerate -m "add_osint_findings"
```

**Step 3: Migration'ı uygula**

```bash
cd backend && alembic upgrade head
```

**Verification:**
- `alembic upgrade head` başarılı
- `osint_findings` ve `osint_finding_sources` tabloları PostgreSQL'de mevcut

---

### Task 1.2: `osint_run_events` tablosu

**Objective:** Çalışma olaylarını sıralı kaydet.

**Files:**
- Modify: `backend/app/models.py`
- Create: `backend/alembic/versions/003_osint_run_events.py`

**Step 1: Model ekle**

```python
class OsintRunEvent(Base):
    __tablename__ = "osint_run_events"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    osint_run_id: Mapped[str] = mapped_column(
        ForeignKey("osint_runs.id", ondelete="CASCADE"), index=True
    )
    sequence: Mapped[int] = mapped_column()
    event_type: Mapped[str] = mapped_column(String(60), index=True)
    payload: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    __table_args__ = (
        Index("ix_osint_run_events_seq", "osint_run_id", "sequence"),
    )
```

**Step 2-3: Migration oluştur ve uygula**

```bash
cd backend && alembic revision --autogenerate -m "add_osint_run_events"
cd backend && alembic upgrade head
```

---

### Task 1.3: `analysis_events` ve `analysis_stage_runs` tabloları

**Objective:** Analiz olaylarını ve faz çıktılarını kalıcı kaydet.

**Files:**
- Modify: `backend/app/models.py`
- Create: `backend/alembic/versions/004_analysis_events.py`

**Step 1: Model ekle**

```python
class AnalysisEvent(Base):
    __tablename__ = "analysis_events"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    analysis_session_id: Mapped[str] = mapped_column(
        ForeignKey("analysis_sessions.id", ondelete="CASCADE"), index=True
    )
    sequence: Mapped[int] = mapped_column()
    event_type: Mapped[str] = mapped_column(String(60), index=True)
    payload: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)


class AnalysisStageRun(Base):
    __tablename__ = "analysis_stage_runs"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    analysis_session_id: Mapped[str] = mapped_column(
        ForeignKey("analysis_sessions.id", ondelete="CASCADE"), index=True
    )
    phase: Mapped[str] = mapped_column(String(60), index=True)
    persona_id: Mapped[str | None] = mapped_column(String(80))
    status: Mapped[str] = mapped_column(String(24), default="queued")
    provider_connection_id: Mapped[str | None] = mapped_column(String(36))
    payload: Mapped[dict[str, Any] | None] = mapped_column(JSON)
    error_code: Mapped[str | None] = mapped_column(String(80))
    error_message: Mapped[str | None] = mapped_column(Text)
    started_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))
    completed_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
```

**Step 2-3: Migration ve uygula**

---

### Task 1.4: `case_id` zorunlu foreign key yap

**Objective:** `osint_runs.case_id` ve `analysis_sessions.case_id` nullable'dan zorunluya çevir. Bu migration öncesi mevcut NULL kayıtları yok etme, `SET DEFAULT` ile varsayılan vaka oluştur (P0 ile çelişmemesi için; P0'da UI tarafında zorunlu vaka seçimi yapılacak).

**Files:**
- Create: `backend/alembic/versions/005_case_id_required.py`

**Step 1: Migration yaz**

```python
# backend/alembic/versions/005_case_id_required.py
def upgrade():
    # Mevcut NULL case_id satırlarını kontrol et
    # Varsa "Varsayılan Vaka" oluştur ve bağla
    op.execute("""
        INSERT INTO cases (id, name, status, created_at, updated_at)
        SELECT 'default-migration', 'Varsayılan Vaka', 'active', NOW(), NOW()
        WHERE NOT EXISTS (SELECT 1 FROM cases WHERE id = 'default-migration')
    """)
    op.execute("UPDATE osint_runs SET case_id = 'default-migration' WHERE case_id IS NULL")
    op.execute("UPDATE analysis_sessions SET case_id = 'default-migration' WHERE case_id IS NULL")
    # Nullable bırak, UI tarafında zorunlu yap (P0 sonrası gerçek ALTER)

def downgrade():
    pass  # nullable olduğu için geri alınacak bir şey yok
```

**Step 2: Uygula**

---

### Task 1.5: `page_snapshots` tablosu

**Objective:** Sayfa inceleme sonuçlarını kaydet.

```python
class PageSnapshot(Base):
    __tablename__ = "page_snapshots"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    finding_id: Mapped[str] = mapped_column(
        ForeignKey("osint_findings.id", ondelete="CASCADE"), index=True
    )
    url: Mapped[str] = mapped_column(String(2000))
    http_status: Mapped[int | None] = mapped_column()
    fetched_at: Mapped[datetime] = mapped_column(DateTime(timezone=True))
    content_hash: Mapped[str] = mapped_column(String(64), index=True)
    title: Mapped[str | None] = mapped_column(String(500))
    page_text: Mapped[str | None] = mapped_column(Text)
    structured_data: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    extractor: Mapped[str] = mapped_column(String(80))
    ai_analysis: Mapped[dict[str, Any] | None] = mapped_column(JSON)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
```

**Migration:** `006_page_snapshots.py`

---

## Aşama 2: Instagram İçe Aktarma Altyapısı

### Task 2.1: Instagram arşiv keşif ve şema adapteri

**Objective:** Instagram export zip/klasörünü tanı, profil bilgilerini çıkar, konuşmaları parse et.

**Files:**
- Create: `backend/app/importers/__init__.py`
- Create: `backend/app/importers/instagram/__init__.py`
- Create: `backend/app/importers/instagram/discovery.py`
- Create: `backend/app/importers/instagram/adapter.py`

**Step 1: Discovery modülü**

```python
# backend/app/importers/instagram/discovery.py
from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass, field
from pathlib import Path


@dataclass
class ArchiveManifest:
    root_path: Path
    version: str | None = None
    language: str | None = None
    date_range: tuple[int, int] | None = None  # (min_ts_ms, max_ts_ms)
    profile_username: str | None = None
    profile_display_name: str | None = None
    conversation_count: int = 0
    total_message_count: int = 0
    file_hashes: dict[str, str] = field(default_factory=dict)


def discover_archive(root_path: Path) -> ArchiveManifest:
    """Instagram export klasörünü tara, manifest çıkar."""
    manifest = ArchiveManifest(root_path=root_path)
    activity_path = root_path / "your_instagram_activity"

    # Profil bilgisi
    profile_info = activity_path / "personal_information" / "personal_information" / "personal_information.json"
    if profile_info.exists():  # Hata: yanlış yol, düzelt
        pass
    
    # Gerçek yol
    personal_info = root_path / "personal_information" / "personal_information" / "personal_information.json"
    if personal_info.exists():
        data = json.loads(personal_info.read_text(encoding="utf-8"))
        for item in data.get("profile_user", []):
            smd = item.get("string_map_data", {})
            if "Kullanıcı Adı" in smd:
                manifest.profile_username = smd["Kullanıcı Adı"]["value"]
            if "Ad" in smd:
                manifest.profile_display_name = smd["Ad"]["value"]

    # Konuşma sayısı
    inbox = root_path / "your_instagram_activity" / "messages" / "inbox"
    if inbox.exists():
        conversations = [d for d in inbox.iterdir() if d.is_dir()]
        manifest.conversation_count = len(conversations)

    return manifest
```

**Step 2: Adapter — mesaj parse**

```python
# backend/app/importers/instagram/adapter.py
from __future__ import annotations

from dataclasses import dataclass
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
    photos: list[str] = None  # field(default_factory=list)
    videos: list[str] = None
    audio: list[str] = None
    share_link: str | None = None
    reactions: list[dict] = None
    is_unsent: bool = False
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
    participants = []
    messages = []
    seq = 0

    for mf in message_files:
        data = json.loads(mf.read_text(encoding="utf-8"))
        if not participants:
            participants = [
                InstagramParticipant(name=p["name"])
                for p in data.get("participants", [])
            ]
        for msg in data.get("messages", []):
            seq += 1
            messages.append(InstagramMessage(
                sender_name=msg.get("sender_name", ""),
                timestamp_ms=msg.get("timestamp_ms", 0),
                content=msg.get("content"),
                share_link=msg.get("share", {}).get("link"),
                reactions=msg.get("reactions"),
                source_file=str(mf.relative_to(conv_path)),
                sequence=seq,
            ))

    conv_id = conv_path.name
    conv_title = conv_path.name.split("_")[0] if "_" in conv_path.name else conv_path.name

    return InstagramConversation(
        conversation_id=conv_id,
        title=conv_title,
        participants=participants,
        messages=messages,
        source_path=str(conv_path),
        message_count=len(messages),
    )
```

**Verification:**
- Örnek konuşma parse edilip mesaj sayısı doğrulanmalı
- Profil bilgisi çıkarılabilmeli

---

### Task 2.2: Instagram veritabanı tabloları

**Objective:** Instagram verilerini saklayacak normalize tablolar.

**Files:**
- Modify: `backend/app/models.py`
- Create: `backend/alembic/versions/007_instagram_tables.py`

**Step 1: Modeller**

```python
class ImportBatch(Base):
    __tablename__ = "import_batches"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    case_id: Mapped[str | None] = mapped_column(ForeignKey("cases.id", ondelete="SET NULL"))
    import_root: Mapped[str] = mapped_column(String(1000))
    source_type: Mapped[str] = mapped_column(String(40))  # "instagram", "whatsapp"
    status: Mapped[str] = mapped_column(String(24), default=JobStatus.queued.value, index=True)
    archive_owner_username: Mapped[str | None] = mapped_column(String(200))
    archive_owner_display_name: Mapped[str | None] = mapped_column(String(300))
    archive_owner_confirmed: Mapped[bool] = mapped_column(default=False)
    total_conversations: Mapped[int] = mapped_column(default=0)
    total_messages: Mapped[int] = mapped_column(default=0)
    import_summary: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    error_code: Mapped[str | None] = mapped_column(String(80))
    error_message: Mapped[str | None] = mapped_column(Text)
    heartbeat_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), default=utcnow, onupdate=utcnow
    )


class ImportedProfile(Base):
    __tablename__ = "imported_profiles"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    import_batch_id: Mapped[str] = mapped_column(
        ForeignKey("import_batches.id", ondelete="CASCADE"), index=True
    )
    username: Mapped[str] = mapped_column(String(200))
    display_name: Mapped[str | None] = mapped_column(String(300))
    is_archive_owner: Mapped[bool] = mapped_column(default=False)
    follower_count: Mapped[int | None] = mapped_column()
    following_count: Mapped[int | None] = mapped_column()
    profile_metadata: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)


class ImportedConversation(Base):
    __tablename__ = "imported_conversations"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    import_batch_id: Mapped[str] = mapped_column(
        ForeignKey("import_batches.id", ondelete="CASCADE"), index=True
    )
    source_conversation_id: Mapped[str] = mapped_column(String(200))
    title: Mapped[str | None] = mapped_column(String(500))
    message_count: Mapped[int] = mapped_column(default=0)
    earliest_message_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))
    latest_message_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))
    participant_names: Mapped[list[str]] = mapped_column(JSON, default=list)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    messages: Mapped[list["ImportedMessage"]] = relationship(
        back_populates="conversation", cascade="all, delete-orphan"
    )
    participants: Mapped[list["ConversationParticipant"]] = relationship(
        back_populates="conversation", cascade="all, delete-orphan"
    )


class ConversationParticipant(Base):
    __tablename__ = "conversation_participants"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    conversation_id: Mapped[str] = mapped_column(
        ForeignKey("imported_conversations.id", ondelete="CASCADE"), index=True
    )
    name: Mapped[str] = mapped_column(String(300), index=True)
    profile_id: Mapped[str | None] = mapped_column(
        ForeignKey("imported_profiles.id", ondelete="SET NULL")
    )

    conversation: Mapped[ImportedConversation] = relationship(back_populates="participants")


class ImportedMessage(Base):
    __tablename__ = "imported_messages"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    conversation_id: Mapped[str] = mapped_column(
        ForeignKey("imported_conversations.id", ondelete="CASCADE"), index=True
    )
    sender_name: Mapped[str] = mapped_column(String(300), index=True)
    timestamp_ms: Mapped[int] = mapped_column(index=True)
    content: Mapped[str | None] = mapped_column(Text)
    content_normalized: Mapped[str | None] = mapped_column(Text)
    share_link: Mapped[str | None] = mapped_column(String(2000))
    has_media: Mapped[bool] = mapped_column(default=False)
    media_refs: Mapped[list[str]] = mapped_column(JSON, default=list)
    reactions: Mapped[list[dict[str, Any]]] = mapped_column(JSON, default=list)
    source_file: Mapped[str] = mapped_column(String(500))
    sequence: Mapped[int] = mapped_column()
    content_hash: Mapped[str] = mapped_column(String(64), index=True)
    source_message_id: Mapped[str] = mapped_column(String(200))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    conversation: Mapped[ImportedConversation] = relationship(back_populates="messages")

    __table_args__ = (
        UniqueConstraint("conversation_id", "source_message_id"),
    )
```

**Step 2: Migration oluştur ve uygula**

---

### Task 2.3: Instagram import worker görevi

**Objective:** Worker'da Instagram import işlemini gerçekleştir, `discover_archive` + `parse_conversation` kullanarak veritabanını doldur.

**Files:**
- Create: `backend/app/importers/instagram/importer.py`
- Modify: `backend/app/worker.py` — yeni iş türü ekle
- Modify: `backend/app/models.py` — `ImportBatch`'i kaydet

**Step 1: Importer servisi**

```python
# backend/app/importers/instagram/importer.py
import hashlib
from pathlib import Path

from sqlalchemy.orm import Session

from ...db import SessionLocal
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
    return hashlib.sha256(
        f"{sender_name}:{timestamp_ms}:{seq}".encode()
    ).hexdigest()[:40]


def import_instagram_archive(db: Session, batch_id: str) -> None:
    batch = db.get(ImportBatch, batch_id)
    if not batch:
        raise RuntimeError("Import batch not found")

    root = Path(batch.import_root)
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
            if conv.messages:
                db_conv.latest_message_at = conv.messages[0].timestamp
                db_conv.earliest_message_at = conv.messages[-1].timestamp
            db.add(db_conv)
            db.flush()

            for p in conv.participants:
                db.add(ConversationParticipant(
                    conversation_id=db_conv.id,
                    name=p.name,
                ))

            for msg in conv.messages:
                source_id = _message_source_id(msg.sender_name, msg.timestamp_ms, msg.sequence)
                db.add(ImportedMessage(
                    conversation_id=db_conv.id,
                    sender_name=msg.sender_name,
                    timestamp_ms=msg.timestamp_ms,
                    content=msg.content,
                    share_link=msg.share_link,
                    has_media=bool(msg.photos or msg.videos),
                    media_refs=(msg.photos or []) + (msg.videos or []),
                    reactions=msg.reactions or [],
                    source_file=msg.source_file,
                    sequence=msg.sequence,
                    source_message_id=source_id,
                    content_hash=hashlib.sha256(
                        (msg.content or "").encode()
                    ).hexdigest()[:64],
                ))
                total_messages += 1

            if total_messages % 500 == 0:
                db.commit()

    batch.total_messages = total_messages
    batch.status = JobStatus.completed.value
    db.commit()
```

**Step 2: Worker'da iş türü ekle**

`worker.py` içinde `_process_import` fonksiyonu ekle, ana döngüde `ImportBatch` kontrolü yap.

---

### Task 2.4: Instagram API endpoint'leri

**Objective:** Import başlatma, durum sorgulama, konuşma ve mesaj listeleme endpoint'leri.

**Files:**
- Modify: `backend/app/api.py`
- Create: `backend/app/schemas.py` değişiklikler

**Step 1: Schemas ekle**

```python
class ImportBatchCreate(BaseModel):
    import_root: str = Field(min_length=1, max_length=1000)
    case_id: str | None = None
    source_type: Literal["instagram"] = "instagram"

class ImportBatchRead(ORMModel):
    id: str
    case_id: str | None
    status: str
    archive_owner_username: str | None
    archive_owner_display_name: str | None
    archive_owner_confirmed: bool
    total_conversations: int
    total_messages: int
    created_at: datetime

class ConversationRead(ORMModel):
    id: str
    source_conversation_id: str
    title: str | None
    message_count: int
    participant_names: list[str]
    earliest_message_at: datetime | None
    latest_message_at: datetime | None
```

**Step 2: API endpoint'leri**

```python
@router.post("/imports", response_model=ImportBatchRead, status_code=202)
def create_import(body: ImportBatchCreate, db: Session = Depends(get_db)):
    # root_path izin denetimi
    ...

@router.get("/imports/{batch_id}", response_model=ImportBatchRead)
def get_import(batch_id: str, db: Session = Depends(get_db)):
    ...

@router.get("/imports/{batch_id}/conversations")
def list_conversations(batch_id: str, db: Session = Depends(get_db)):
    ...

@router.get("/imports/{batch_id}/conversations/{conv_id}/messages")
def list_messages(batch_id: str, conv_id: str, db: Session = Depends(get_db)):
    ...

@router.post("/imports/{batch_id}/confirm-owner")
def confirm_owner(batch_id: str, db: Session = Depends(get_db)):
    ...
```

---

## Aşama 3: OSINT Kalıcı Bulgu Kaydı ve Anlık Event

### Task 3.1: OsintService'e kademeli bulgu commit'leme

**Objective:** Her bağlayıcı tamamlandığında bulguları `osint_findings` tablosuna anında commit et.

**Files:**
- Modify: `backend/app/osint/service.py`

**Step 1: `_commit_findings` yardımcısı ekle**

```python
# backend/app/osint/service.py — ekle
def _commit_findings(db: Session, run_id: str, connector_id: str, findings: list[dict]) -> int:
    count = 0
    for f in findings:
        profile_url = f.get("profile_url", "")
        dedup_hash = hashlib.sha256(profile_url.encode()).hexdigest()[:64]
        existing = db.scalar(
            select(OsintFinding).where(
                OsintFinding.osint_run_id == run_id,
                OsintFinding.dedup_hash == dedup_hash,
            )
        )
        if existing:
            # Yeni kaynak ekle
            source = OsintFindingSource(
                finding_id=existing.id,
                connector=connector_id,
                site=f.get("site", ""),
            )
            db.add(source)
        else:
            finding = OsintFinding(
                osint_run_id=run_id,
                profile_url=profile_url,
                site=f.get("site", ""),
                username=f.get("username", ""),
                http_status=str(f.get("http_status")) if f.get("http_status") else None,
                dedup_hash=dedup_hash,
            )
            db.add(finding)
            db.flush()
            db.add(OsintFindingSource(
                finding_id=finding.id,
                connector=connector_id,
                site=f.get("site", ""),
            ))
        count += 1
    db.commit()
    return count
```

**Step 2: `OsintService.run` içinde her bağlayıcı bitince `_commit_findings` çağır ve `osint_run_events` kaydı at.**

---

### Task 3.2: SSE endpoint ve canlı event akışı

**Objective:** Frontend'in canlı güncelleme alabilmesi için SSE endpoint.

**Files:**
- Modify: `backend/app/api.py`
- Create: `backend/app/events/__init__.py`
- Create: `backend/app/events/stream.py`

**Step 1: Event stream yardımcısı**

```python
# backend/app/events/stream.py
import asyncio
import json
from collections import defaultdict
from datetime import UTC, datetime

from ..db import SessionLocal
from ..models import OsintRunEvent


class EventStream:
    def __init__(self):
        self._subscribers: dict[str, list[asyncio.Queue]] = defaultdict(list)

    async def subscribe(self, run_id: str) -> asyncio.Queue:
        queue: asyncio.Queue = asyncio.Queue()
        self._subscribers[run_id].append(queue)
        return queue

    def unsubscribe(self, run_id: str, queue: asyncio.Queue) -> None:
        if run_id in self._subscribers:
            try:
                self._subscribers[run_id].remove(queue)
            except ValueError:
                pass

    async def publish(self, run_id: str, event_type: str, payload: dict) -> None:
        data = json.dumps({"event_type": event_type, "payload": payload, "timestamp": datetime.now(UTC).isoformat()})
        for q in self._subscribers.get(run_id, []):
            await q.put(data)
```

**Step 2: SSE endpoint**

```python
from fastapi.responses import StreamingResponse

stream = EventStream()

@router.get("/osint/runs/{run_id}/events")
async def osint_run_events(run_id: str, since: int = Query(default=0)):
    async def event_generator():
        queue = await stream.subscribe(run_id)
        try:
            # Önce kaçırılan event'leri gönder (since > sequence)
            with SessionLocal() as db:
                missed = db.scalars(
                    select(OsintRunEvent)
                    .where(OsintRunEvent.osint_run_id == run_id, OsintRunEvent.sequence > since)
                    .order_by(OsintRunEvent.sequence)
                ).all()
                for evt in missed:
                    yield f"data: {json.dumps({'event_type': evt.event_type, 'payload': evt.payload, 'sequence': evt.sequence})}\n\n"
            # Canlı event'leri gönder
            while True:
                try:
                    data = await asyncio.wait_for(queue.get(), timeout=25)
                    yield f"data: {data}\n\n"
                except asyncio.TimeoutError:
                    yield ": heartbeat\n\n"
        except asyncio.CancelledError:
            pass
        finally:
            stream.unsubscribe(run_id, queue)

    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "Connection": "keep-alive"},
    )
```

---

## Aşama 4: Route Bazlı OSINT ve Analiz Kalıcılığı

### Task 4.1: OSINT route'ları (`/osint/calisma/{run_id}`)

**Objective:** OSINT ekranı sayfa değişiminde kaybolmasın, URL'den hydrate edilsin.

**Files:**
- Create: `app/osint/calisma/[id]/page.tsx`
- Modify: `components/views/osint-view.tsx`
- Modify: `components/joe-app.tsx` (routing)

**Step 1: Next.js App Router sayfası**

```tsx
// app/osint/calisma/[id]/page.tsx
import { OsintCalismaView } from "@/components/views/osint-calisma-view";

export default async function OsintCalismaPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <OsintCalismaView runId={id} />;
}
```

**Step 2: Yeni bileşen — run_id ile hydrate**

```tsx
// components/views/osint-calisma-view.tsx
"use client";

export function OsintCalismaView({ runId }: { runId: string }) {
  const [run, setRun] = useState<OsintRun | null>(null);
  
  useEffect(() => {
    joeApi.osintRun(runId).then(setRun);
  }, [runId]);
  
  // SSE bağlantısı + polling fallback
  // ...
}
```

**Step 3: OsintView'den run başlatınca navigate**

```tsx
// OsintView içinde
const nextRun = await joeApi.createOsintRun({...});
setRun(nextRun);
router.push(`/osint/calisma/${nextRun.id}`);
```

---

### Task 4.2: Analiz route'u (`/analiz/oturum/{analysis_id}`) ve analiz geçmişi

**Objective:** Analiz sayfasında geçmiş oturumlar listelensin, tamamlanan analizler kalıcı görünsün.

**Files:**
- Create: `app/analiz/oturum/[id]/page.tsx`
- Modify: `components/views/analysis-view.tsx`

**Step 1: Analiz geçmişi listesi — API ve UI**

```python
# API'de zaten GET /analyses var, UI tarafında kullan
@router.get("/analyses/{analysis_id}", response_model=AnalysisRead)
def get_analysis(analysis_id: str, db: Session = Depends(get_db)):
    ...
```

**Step 2: AnalysisView güncelleme**

```tsx
// components/views/analysis-view.tsx
// Yeni: "Devam Eden", "Tamamlanan", "Başarısız" sekmeleri
// Mevcut: serbest metin kutusu yerine kaynak seçici (Aşama 5'te detaylandırılacak)
```

---

### Task 4.3: Global aktif işler çekmecesi

**Objective:** Üst çubukta aktif OSINT/Analiz/Import işleri görünsün.

**Files:**
- Create: `components/views/active-jobs.tsx`
- Modify: `components/joe-app.tsx`

**Step 1: ActiveJobs bileşeni**

```tsx
// components/views/active-jobs.tsx
export function ActiveJobs() {
  // GET /summary → active_jobs
  // GET /osint/runs (status=queued,running)
  // GET /analyses (status=queued,running)
  // GET /imports (status=queued,running)
  // Sağ çekmece veya dropdown
}
```

---

## Aşama 5: Analiz Kaynak Seçici (Metinsiz Analiz)

### Task 5.1: `analysis_source_packages` ve `analysis_source_items` tabloları

**Objective:** Analiz için değişmez kaynak paketi modeli.

**Files:**
- Modify: `backend/app/models.py`
- Create: `backend/alembic/versions/008_analysis_source_packages.py`

```python
class AnalysisSourcePackage(Base):
    __tablename__ = "analysis_source_packages"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    analysis_session_id: Mapped[str] = mapped_column(
        ForeignKey("analysis_sessions.id", ondelete="CASCADE"), index=True
    )
    version: Mapped[int] = mapped_column(default=1)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    
    items: Mapped[list["AnalysisSourceItem"]] = relationship(
        back_populates="package", cascade="all, delete-orphan"
    )


class AnalysisSourceItem(Base):
    __tablename__ = "analysis_source_items"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    package_id: Mapped[str] = mapped_column(
        ForeignKey("analysis_source_packages.id", ondelete="CASCADE"), index=True
    )
    source_type: Mapped[str] = mapped_column(String(40))  # text, corpus, instagram_message, document, screenshot
    source_ref: Mapped[str] = mapped_column(String(700))
    source_label: Mapped[str] = mapped_column(String(500))
    content: Mapped[str] = mapped_column(Text)
    content_hash: Mapped[str] = mapped_column(String(64))
    sequence: Mapped[int] = mapped_column(default=0)

    package: Mapped[AnalysisSourcePackage] = relationship(back_populates="items")
```

---

### Task 5.2: Analiz kaynak seçici UI

**Objective:** Serbest metin zorunlu olmasın, dizin/corpus/Instagram/kanıt/dosya seçilebilsin.

**Files:**
- Create: `components/views/analysis-source-picker.tsx`
- Modify: `components/views/analysis-view.tsx`

**Step 1: Kaynak seçici bileşen**

```tsx
// components/views/analysis-source-picker.tsx
type SourceType = "text" | "corpus" | "instagram" | "evidence" | "document" | "screenshot";

export function AnalysisSourcePicker({
  corpora, conversations, evidenceItems, onSelect
}: {
  corpora: CorpusRecord[];
  conversations: ConversationRead[];
  evidenceItems: EvidenceItem[];
  onSelect: (sources: SourceSelection[]) => void;
}) {
  // Sekmeli seçici
  // Her kaynak türü için önizleme
  // "Analiz kaynağına ekle" butonu
  // Değişmez paket oluşturma
}
```

---

### Task 5.3: Council engine'i kaynak paketi ile güncelle

**Objective:** `AnalysisCreate.source_text` yerine kaynak paketi kullan.

**Files:**
- Modify: `backend/app/schemas.py` — AnalysisCreate güncelle
- Modify: `backend/app/api.py` — create_analysis güncelle
- Modify: `backend/app/council/engine.py` — source_text yerine source_items

---

## Aşama 6: Vaka Detay Sayfası

### Task 6.1: Vaka kartı tıklanabilir — `/vakalar/{case_id}` route

**Objective:** Vaka kartları tıklanınca detay sayfasına gitsin.

**Files:**
- Create: `app/vakalar/[id]/page.tsx`
- Modify: `components/views/cases-view.tsx`
- Modify: `components/joe-app.tsx`

**Step 1: Next.js route**

```tsx
// app/vakalar/[id]/page.tsx
import { CaseDetailView } from "@/components/views/case-detail/case-detail-view";

export default async function CaseDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <CaseDetailView caseId={id} />;
}
```

---

### Task 6.2: Vaka detay bileşeni (8 sekme)

**Objective:** Vaka dosyası gibi çalışan detay ekranı.

**Files:**
- Create: `components/views/case-detail/case-detail-view.tsx`
- Create: `components/views/case-detail/overview-tab.tsx`
- Create: `components/views/case-detail/osint-tab.tsx`
- Create: `components/views/case-detail/archives-tab.tsx`
- Create: `components/views/case-detail/analyses-tab.tsx`
- Create: `components/views/case-detail/evidence-tab.tsx`
- Create: `components/views/case-detail/timeline-tab.tsx`
- Create: `components/views/case-detail/chat-tab.tsx`
- Create: `components/views/case-detail/audit-tab.tsx`

**Step 1: Ana detay bileşeni**

```tsx
// 8 sekmeli: Genel Bakış, OSINT, Arşivler, Analizler, Kanıtlar, Zaman Çizelgesi, Sohbet, Denetim
// Her sekme kendi API çağrısını yapar
// OSINT sekmesinde aktif run'ların canlı event'leri
```

---

### Task 6.3: Vaka detay API endpoint'leri

**Objective:** Vaka detayı için gerekli tüm veri endpoint'lerini ekle.

**Files:**
- Modify: `backend/app/api.py`

```python
@router.get("/cases/{case_id}/osint-runs")
def case_osint_runs(case_id: str, db: Session = Depends(get_db)):
    ...

@router.get("/cases/{case_id}/findings")
def case_findings(case_id: str, db: Session = Depends(get_db)):
    ...

@router.get("/cases/{case_id}/analyses")
def case_analyses(case_id: str, db: Session = Depends(get_db)):
    ...

@router.get("/cases/{case_id}/evidence")
def case_evidence(case_id: str, db: Session = Depends(get_db)):
    ...

@router.get("/cases/{case_id}/timeline")
def case_timeline(case_id: str, db: Session = Depends(get_db)):
    ...
```

---

## Aşama 7: OSINT'te Vaka Zorunluluğu ve Analog Panel Kaldırma

### Task 7.1: OSINT başlatırken vaka seçimi zorunlu

**Objective:** Sessiz otomatik vaka yok. Vaka seçilmezse buton pasif.

**Files:**
- Modify: `components/views/osint-view.tsx`
- Modify: `backend/app/schemas.py` (OsintRunCreate)
- Modify: `backend/app/api.py` (create_osint_run)

**Step 1: OsintRunCreate şema güncelleme**

```python
class OsintRunCreate(BaseModel):
    query: str = Field(min_length=2, max_length=500)
    query_type: Literal["username", "full_name"]
    case_id: str = Field(...)  # zorunlu
```

**Step 2: OsintView güncelleme**

```tsx
// caseId boşsa "Araştırmayı başlat" butonu pasif
// "Lütfen bir vaka seçin veya yeni vaka oluşturun" uyarısı
```

---

### Task 7.2: Analog araştırma panelini kaldır

**Objective:** `manual_plan` render edilmesin, arama motoru sorgu kartları gösterilmesin.

**Files:**
- Modify: `components/views/osint-view.tsx`

**Step 1: manual_plan bölümünü kaldır**

```tsx
// Şu anki "ANALOG ARAŞTIRMA — Doğrulama sorguları" kartlarını gösteren kodu kaldır
// plan state'ini hala backend'den al fakat UI'da gösterme
```

---

## Aşama 8: Canlı Konsey Faz Gösterimi

### Task 8.1: Council engine'de anlık faz kaydı

**Objective:** Her persona çıktısı tamamlandığında `analysis_stage_runs` ve `analysis_events` tablolarına yaz.

**Files:**
- Modify: `backend/app/council/engine.py`

**Step 1: `_record_stage` yardımcısı**

```python
def _record_stage(db, session_id, phase, persona_id, status, payload=None, error=None):
    stage = AnalysisStageRun(
        analysis_session_id=session_id,
        phase=phase,
        persona_id=persona_id,
        status=status,
        payload=payload,
        started_at=datetime.now(UTC) if status == "running" else None,
        completed_at=datetime.now(UTC) if status == "completed" else None,
    )
    db.add(stage)
    db.commit()
```

**Step 2: Mevcut `run` metodunda her persona/faz sonrası `_record_stage` çağır.**

---

### Task 8.2: UI'da canlı konsey faz gösterimi

**Objective:** Analiz ekranında dönen yükleniyor yerine gerçek faz/persona ilerlemesi.

**Files:**
- Modify: `components/views/analysis-view.tsx`

**Step 1: SSE ile canlı faz gösterimi**

```tsx
// GET /analyses/{id}/events SSE endpoint'inden
// "Freud bağımsız görüşünü oluşturuyor"
// "Jung tamamlandı"
// "Çapraz itirazlar yürütülüyor"
// gibi canlı durum satırı
```

---

## Aşama 9: Vaka İçi AI Sohbeti (P3'ten öne çekildi — temel seviye)

### Task 9.1: `case_chat_sessions` ve `case_chat_messages` tabloları

**Objective:** Vaka bazlı AI sohbet tabloları.

**Files:**
- Modify: `backend/app/models.py`
- Create: `backend/alembic/versions/009_case_chat.py`

```python
class CaseChatSession(Base):
    __tablename__ = "case_chat_sessions"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    case_id: Mapped[str] = mapped_column(ForeignKey("cases.id", ondelete="CASCADE"), index=True)
    scope_type: Mapped[str] = mapped_column(String(40))  # osint, snapshots, archive, council, evidence
    scope_filters: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    provider_connection_id: Mapped[str | None] = mapped_column(String(36))
    title: Mapped[str | None] = mapped_column(String(300))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)


class CaseChatMessage(Base):
    __tablename__ = "case_chat_messages"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    session_id: Mapped[str] = mapped_column(
        ForeignKey("case_chat_sessions.id", ondelete="CASCADE"), index=True
    )
    role: Mapped[str] = mapped_column(String(20))  # user, assistant
    content: Mapped[str] = mapped_column(Text)
    cited_evidence: Mapped[list[str]] = mapped_column(JSON, default=list)
    provider_metadata: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
```

---

### Task 9.2: Case chat API endpoint'leri

**Files:**
- Modify: `backend/app/api.py`

```python
@router.post("/cases/{case_id}/chat/sessions", status_code=201)
def create_chat_session(case_id, body, db):
    ...

@router.post("/cases/{case_id}/chat/sessions/{session_id}/messages")
def send_chat_message(case_id, session_id, body, db):
    # AI çağrısı yap, kanıt referanslarını kontrol et
    ...

@router.get("/cases/{case_id}/chat/sessions/{session_id}/messages")
def list_chat_messages(case_id, session_id, db):
    ...
```

---

## Aşama 10: Test ve Doğrulama

### Task 10.1: Frontend testleri güncelle

```bash
npm test
```

Tüm testler geçmeli. Yeni route'lar test edilmeli.

### Task 10.2: Backend testleri güncelle

```bash
cd backend && python -m pytest
```

Yeni tablolar ve endpoint'ler için test ekle.

### Task 10.3: Build doğrulama

```bash
npm run build
```

Next.js build hatasız tamamlanmalı.

### Task 10.4: Docker Compose doğrulama

```bash
docker compose down
docker compose up -d --build
curl http://localhost:8000/api/v1/health
```

### Task 10.5: Instagram import end-to-end test

```bash
# API üzerinden import başlat
curl -X POST http://localhost:8000/api/v1/imports \
  -H "Content-Type: application/json" \
  -d '{"import_root": "/imports/data", "source_type": "instagram"}'
```

---

## Öncelik Sırası

1. **Aşama 0**: Alembic (temel, her şeyden önce)
2. **Aşama 1**: Normalize tablolar (veri modeli temeli)
3. **Aşama 2**: Instagram import (örnek veri var, hemen test edilebilir)
4. **Aşama 3**: OSINT event + SSE (canlılık)
5. **Aşama 4**: Route bazlı kalıcılık
6. **Aşama 5**: Analiz kaynak seçici
7. **Aşama 6**: Vaka detay sayfası
8. **Aşama 7**: OSINT vaka zorunluluğu + analog panel kaldırma
9. **Aşama 8**: Canlı konsey faz gösterimi
10. **Aşama 9**: Vaka sohbeti (temel)
11. **Aşama 10**: Toplu test ve doğrulama

---

## Riskler ve Açık Konular

1. **Docker volume kaybı**: Migration sırasında volume'ler asla silinmemeli. Her migration öncesi yedek alınmalı.
2. **SSE bağlantı kopması**: polling fallback implementasyonu şart.
3. **Instagram Unicode**: JSON parse sırasında mojibake sorunu olabilir, `encoding="utf-8"` zorunlu.
4. **Worker ayrımı**: Mevcut tek worker, Instagram import sırasında uzun sürebilir (183 konuşma). İleride ayrı import worker'ına geçilmeli.
5. **Mevcut veri uyumu**: JSON result'taki mevcut bulgular backfill ile normalize tablolara taşınmalı.
