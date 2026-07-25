from __future__ import annotations

import enum
import uuid
from datetime import UTC, datetime
from typing import Any

from sqlalchemy import JSON, Boolean, DateTime, ForeignKey, Index, String, Text, UniqueConstraint
from sqlalchemy.orm import Mapped, mapped_column, relationship

from .db import Base


def new_id() -> str:
    return str(uuid.uuid4())


def utcnow() -> datetime:
    return datetime.now(UTC)


class JobStatus(str, enum.Enum):
    queued = "queued"
    running = "running"
    completed = "completed"
    failed = "failed"
    cancelled = "cancelled"


class Case(Base):
    __tablename__ = "cases"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    name: Mapped[str] = mapped_column(String(180), nullable=False)
    subject_label: Mapped[str | None] = mapped_column(String(180))
    purpose: Mapped[str | None] = mapped_column(Text)
    authorization_note: Mapped[str | None] = mapped_column(Text)
    status: Mapped[str] = mapped_column(String(32), default="active", index=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), default=utcnow, onupdate=utcnow
    )

    evidence: Mapped[list["EvidenceItem"]] = relationship(
        back_populates="case", cascade="all, delete-orphan"
    )


class ProviderConnection(Base):
    __tablename__ = "provider_connections"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    provider_id: Mapped[str] = mapped_column(String(80), index=True)
    label: Mapped[str] = mapped_column(String(120))
    model: Mapped[str] = mapped_column(String(180))
    base_url: Mapped[str | None] = mapped_column(String(500))
    encrypted_api_key: Mapped[str | None] = mapped_column(Text)
    extra_config: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    enabled: Mapped[bool] = mapped_column(Boolean, default=True)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), default=utcnow, onupdate=utcnow
    )


class Artifact(Base):
    __tablename__ = "artifacts"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    case_id: Mapped[str | None] = mapped_column(ForeignKey("cases.id", ondelete="SET NULL"))
    original_name: Mapped[str] = mapped_column(String(300))
    media_type: Mapped[str] = mapped_column(String(160))
    storage_path: Mapped[str] = mapped_column(String(600), unique=True)
    sha256: Mapped[str] = mapped_column(String(64), index=True)
    size_bytes: Mapped[int] = mapped_column()
    extractor: Mapped[str] = mapped_column(String(80))
    extracted_text: Mapped[str | None] = mapped_column(Text)
    artifact_metadata: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)


class EvidenceItem(Base):
    __tablename__ = "evidence_items"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    case_id: Mapped[str | None] = mapped_column(ForeignKey("cases.id", ondelete="CASCADE"))
    analysis_session_id: Mapped[str | None] = mapped_column(
        ForeignKey("analysis_sessions.id", ondelete="CASCADE")
    )
    evidence_key: Mapped[str] = mapped_column(String(40), index=True)
    kind: Mapped[str] = mapped_column(String(60), default="text")
    content: Mapped[str] = mapped_column(Text)
    source_ref: Mapped[str | None] = mapped_column(String(700))
    content_hash: Mapped[str] = mapped_column(String(64), index=True)
    evidence_metadata: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    case: Mapped[Case | None] = relationship(back_populates="evidence")


class AnalysisSession(Base):
    __tablename__ = "analysis_sessions"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    case_id: Mapped[str | None] = mapped_column(ForeignKey("cases.id", ondelete="SET NULL"))
    title: Mapped[str] = mapped_column(String(220))
    # Eski veritabanlarıyla sütun uyumluluğu korunur; ürün tek bir tam konsey
    # protokolü çalıştırır ve kullanıcıya çalışma modu sunmaz.
    mode: Mapped[str] = mapped_column(String(40), default="council")
    status: Mapped[str] = mapped_column(String(24), default=JobStatus.queued.value, index=True)
    source_text: Mapped[str] = mapped_column(Text)
    source_type: Mapped[str] = mapped_column(String(50), default="text")
    selected_personas: Mapped[list[str]] = mapped_column(JSON, default=list)
    provider_routes: Mapped[dict[str, str]] = mapped_column(JSON, default=dict)
    default_provider_connection_id: Mapped[str | None] = mapped_column(
        ForeignKey("provider_connections.id", ondelete="SET NULL")
    )
    result: Mapped[dict[str, Any] | None] = mapped_column(JSON)
    error_code: Mapped[str | None] = mapped_column(String(80))
    error_message: Mapped[str | None] = mapped_column(Text)
    progress_phase: Mapped[str | None] = mapped_column(String(60))
    heartbeat_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), default=utcnow, onupdate=utcnow
    )

    turns: Mapped[list["CouncilTurn"]] = relationship(
        back_populates="session", cascade="all, delete-orphan", order_by="CouncilTurn.created_at"
    )
    source_packages: Mapped[list["AnalysisSourcePackage"]] = relationship(
        back_populates="session", cascade="all, delete-orphan", order_by="AnalysisSourcePackage.version"
    )


class CouncilTurn(Base):
    __tablename__ = "council_turns"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    session_id: Mapped[str] = mapped_column(
        ForeignKey("analysis_sessions.id", ondelete="CASCADE"), index=True
    )
    phase: Mapped[str] = mapped_column(String(60), index=True)
    persona_id: Mapped[str] = mapped_column(String(80), index=True)
    provider_connection_id: Mapped[str | None] = mapped_column(String(36))
    payload: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    session: Mapped[AnalysisSession] = relationship(back_populates="turns")


class OsintRun(Base):
    __tablename__ = "osint_runs"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    case_id: Mapped[str | None] = mapped_column(ForeignKey("cases.id", ondelete="SET NULL"))
    query: Mapped[str] = mapped_column(String(500))
    query_type: Mapped[str] = mapped_column(String(30))
    connectors: Mapped[list[str]] = mapped_column(JSON, default=list)
    status: Mapped[str] = mapped_column(String(24), default=JobStatus.queued.value, index=True)
    result: Mapped[dict[str, Any] | None] = mapped_column(JSON)
    error_code: Mapped[str | None] = mapped_column(String(80))
    error_message: Mapped[str | None] = mapped_column(Text)
    heartbeat_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), default=utcnow, onupdate=utcnow
    )


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

    __table_args__ = (UniqueConstraint("osint_run_id", "dedup_hash"),)


class OsintFindingSource(Base):
    __tablename__ = "osint_finding_sources"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    finding_id: Mapped[str] = mapped_column(
        ForeignKey("osint_findings.id", ondelete="CASCADE"), index=True
    )
    connector: Mapped[str] = mapped_column(String(80))
    site: Mapped[str] = mapped_column(String(200))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)


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
        UniqueConstraint("osint_run_id", "sequence"),
    )


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

    __table_args__ = (UniqueConstraint("analysis_session_id", "sequence"),)


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


class Corpus(Base):
    __tablename__ = "corpora"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    case_id: Mapped[str | None] = mapped_column(ForeignKey("cases.id", ondelete="SET NULL"))
    name: Mapped[str] = mapped_column(String(220), nullable=False)
    relative_path: Mapped[str] = mapped_column(String(1000), nullable=False)
    status: Mapped[str] = mapped_column(String(24), default=JobStatus.queued.value, index=True)
    file_count: Mapped[int] = mapped_column(default=0)
    indexed_file_count: Mapped[int] = mapped_column(default=0)
    total_bytes: Mapped[int] = mapped_column(default=0)
    error_count: Mapped[int] = mapped_column(default=0)
    scan_summary: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    error_code: Mapped[str | None] = mapped_column(String(80))
    error_message: Mapped[str | None] = mapped_column(Text)
    heartbeat_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), default=utcnow, onupdate=utcnow
    )

    documents: Mapped[list["CorpusDocument"]] = relationship(
        back_populates="corpus", cascade="all, delete-orphan"
    )


class CorpusDocument(Base):
    __tablename__ = "corpus_documents"
    __table_args__ = (UniqueConstraint("corpus_id", "relative_path"),)

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    corpus_id: Mapped[str] = mapped_column(
        ForeignKey("corpora.id", ondelete="CASCADE"), index=True
    )
    relative_path: Mapped[str] = mapped_column(String(1400), nullable=False)
    media_type: Mapped[str] = mapped_column(String(160))
    size_bytes: Mapped[int] = mapped_column()
    sha256: Mapped[str] = mapped_column(String(64), index=True)
    extractor: Mapped[str] = mapped_column(String(80))
    extracted_text: Mapped[str] = mapped_column(Text)
    document_metadata: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)

    corpus: Mapped[Corpus] = relationship(back_populates="documents")


# ── Instagram / WhatsApp import tabloları ──

class ImportBatch(Base):
    __tablename__ = "import_batches"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    case_id: Mapped[str | None] = mapped_column(ForeignKey("cases.id", ondelete="SET NULL"))
    import_root: Mapped[str] = mapped_column(String(1000))
    source_type: Mapped[str] = mapped_column(String(40))
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

    __table_args__ = (UniqueConstraint("import_batch_id", "username"),)


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

    __table_args__ = (UniqueConstraint("import_batch_id", "source_conversation_id"),)


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


# ── Analiz kaynak paketleri ──

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
    session: Mapped[AnalysisSession] = relationship(back_populates="source_packages")


class AnalysisSourceItem(Base):
    __tablename__ = "analysis_source_items"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    package_id: Mapped[str] = mapped_column(
        ForeignKey("analysis_source_packages.id", ondelete="CASCADE"), index=True
    )
    source_type: Mapped[str] = mapped_column(String(40))
    source_ref: Mapped[str] = mapped_column(String(700))
    source_label: Mapped[str] = mapped_column(String(500))
    content: Mapped[str] = mapped_column(Text)
    content_hash: Mapped[str] = mapped_column(String(64))
    sequence: Mapped[int] = mapped_column(default=0)

    package: Mapped[AnalysisSourcePackage] = relationship(back_populates="items")


# ── Vaka sohbeti ──

class CaseChatSession(Base):
    __tablename__ = "case_chat_sessions"

    id: Mapped[str] = mapped_column(String(36), primary_key=True, default=new_id)
    case_id: Mapped[str] = mapped_column(ForeignKey("cases.id", ondelete="CASCADE"), index=True)
    scope_type: Mapped[str] = mapped_column(String(40))
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
    role: Mapped[str] = mapped_column(String(20))
    content: Mapped[str] = mapped_column(Text)
    cited_evidence: Mapped[list[str]] = mapped_column(JSON, default=list)
    provider_metadata: Mapped[dict[str, Any]] = mapped_column(JSON, default=dict)
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), default=utcnow)


Index("ix_analysis_status_created", AnalysisSession.status, AnalysisSession.created_at)
Index("ix_osint_status_created", OsintRun.status, OsintRun.created_at)
Index("ix_corpus_status_created", Corpus.status, Corpus.created_at)
