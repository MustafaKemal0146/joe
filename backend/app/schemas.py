from __future__ import annotations

from datetime import datetime
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator


MAX_ANALYSIS_SOURCE_CHARS = 100_000


class ORMModel(BaseModel):
    model_config = ConfigDict(from_attributes=True)


class CaseCreate(BaseModel):
    name: str = Field(min_length=2, max_length=180)
    subject_label: str | None = Field(default=None, max_length=180)
    purpose: str | None = Field(default=None, max_length=3000)
    authorization_note: str | None = Field(default=None, max_length=3000)


class CaseRead(ORMModel):
    id: str
    name: str
    subject_label: str | None
    purpose: str | None
    authorization_note: str | None
    status: str
    created_at: datetime
    updated_at: datetime


class ProviderConnectionCreate(BaseModel):
    provider_id: str = Field(min_length=2, max_length=80)
    label: str = Field(min_length=2, max_length=120)
    model: str = Field(min_length=1, max_length=180)
    base_url: str | None = Field(default=None, max_length=500)
    api_key: str | None = Field(default=None, max_length=1000)
    extra_config: dict[str, Any] = Field(default_factory=dict)


class ProviderConnectionRead(ORMModel):
    id: str
    provider_id: str
    label: str
    model: str
    base_url: str | None
    enabled: bool
    has_api_key: bool = False
    created_at: datetime
    updated_at: datetime


class AnalysisSourceInput(BaseModel):
    """Analiz başlatılırken seçilen değişmez kaynak kesiti."""

    source_type: str = Field(min_length=2, max_length=40)
    source_ref: str = Field(min_length=1, max_length=700)
    source_label: str = Field(min_length=1, max_length=500)
    content: str = Field(min_length=1, max_length=MAX_ANALYSIS_SOURCE_CHARS)


class AnalysisCreate(BaseModel):
    model_config = ConfigDict(extra="forbid")

    title: str = Field(min_length=2, max_length=220)
    case_id: str = Field(min_length=2, max_length=36)
    source_text: str = Field(default="", max_length=MAX_ANALYSIS_SOURCE_CHARS)
    source_type: str = Field(default="text", max_length=50)
    source_items: list["AnalysisSourceInput"] = Field(default_factory=list, max_length=200)
    artifact_ids: list[str] = Field(default_factory=list, max_length=20)
    selected_personas: list[str] = Field(min_length=2, max_length=13)
    default_provider_connection_id: str | None = None
    synthesis_provider_connection_id: str | None = None
    provider_routes: dict[str, str] = Field(default_factory=dict)

    @field_validator("selected_personas")
    @classmethod
    def unique_personas(cls, value: list[str]) -> list[str]:
        return list(dict.fromkeys(value))


class CouncilTurnRead(ORMModel):
    id: str
    phase: str
    persona_id: str
    provider_connection_id: str | None
    payload: dict[str, Any]
    created_at: datetime


class AnalysisRead(ORMModel):
    id: str
    case_id: str | None
    title: str
    status: str
    source_type: str
    selected_personas: list[str]
    provider_routes: dict[str, str]
    default_provider_connection_id: str | None
    synthesis_provider_connection_id: str | None
    result: dict[str, Any] | None
    error_code: str | None
    error_message: str | None
    progress_phase: str | None
    heartbeat_at: datetime | None
    created_at: datetime
    updated_at: datetime
    turns: list[CouncilTurnRead] = Field(default_factory=list)


class AnalysisEstimate(BaseModel):
    """Analiz başlatılmadan önceki token ve maliyet tahmini."""

    total_chars: int
    persona_count: int
    estimated_prompts: int
    estimated_input_tokens: int
    estimated_output_tokens: int
    estimated_total_tokens: int
    estimated_cost_usd: float | None
    cost_note: str | None
    max_evidence_chars: int


class OsintPlanRequest(BaseModel):
    query: str = Field(min_length=2, max_length=500)
    query_type: Literal["username", "full_name"]


class OsintRunCreate(OsintPlanRequest):
    case_id: str = Field(min_length=2, max_length=36)


class OsintRunRead(ORMModel):
    id: str
    case_id: str | None
    query: str
    query_type: str
    connectors: list[str]
    status: str
    result: dict[str, Any] | None
    error_code: str | None
    error_message: str | None
    created_at: datetime
    updated_at: datetime


class PageAnalysisCreate(BaseModel):
    provider_connection_id: str = Field(min_length=2, max_length=36)


class ArtifactRead(ORMModel):
    id: str
    original_name: str
    media_type: str
    sha256: str
    size_bytes: int
    extractor: str
    extracted_text: str | None
    artifact_metadata: dict[str, Any]
    created_at: datetime


class ImportStatusRead(BaseModel):
    configured: bool
    available: bool
    root_label: str


class ImportEntryRead(BaseModel):
    name: str
    relative_path: str
    kind: Literal["dizin", "dosya"]
    size_bytes: int | None = None


class ImportBrowseRead(BaseModel):
    path: str
    parent_path: str | None
    entries: list[ImportEntryRead]


class CorpusCreate(BaseModel):
    name: str = Field(min_length=2, max_length=220)
    relative_path: str = Field(default=".", max_length=1000)
    case_id: str = Field(min_length=2, max_length=36)


class CorpusRead(ORMModel):
    id: str
    case_id: str | None
    name: str
    relative_path: str
    status: str
    file_count: int
    indexed_file_count: int
    total_bytes: int
    error_count: int
    scan_summary: dict[str, Any]
    error_code: str | None
    error_message: str | None
    created_at: datetime
    updated_at: datetime


class CorpusSearchRequest(BaseModel):
    query: str = Field(min_length=2, max_length=1000)
    max_results: int = Field(default=20, ge=1, le=50)


class CorpusSearchHit(BaseModel):
    document_id: str
    relative_path: str
    extractor: str
    score: float
    excerpt: str


class CorpusSearchRead(BaseModel):
    corpus_id: str
    query: str
    hits: list[CorpusSearchHit]
    analysis_text: str
    truncated: bool


class DashboardSummary(BaseModel):
    case_count: int
    analysis_count: int
    osint_run_count: int
    provider_connection_count: int
    active_jobs: int


# ── Instagram / WhatsApp import ──

class ImportBatchCreate(BaseModel):
    import_root: str = Field(min_length=1, max_length=1000)
    case_id: str = Field(min_length=2, max_length=36)
    source_type: Literal["instagram"] = "instagram"


class ImportBatchRead(ORMModel):
    id: str
    case_id: str | None
    import_root: str
    source_type: str
    status: str
    archive_owner_username: str | None
    archive_owner_display_name: str | None
    archive_owner_confirmed: bool
    total_conversations: int
    total_messages: int
    error_code: str | None
    error_message: str | None
    created_at: datetime
    updated_at: datetime


class ImportedConversationRead(ORMModel):
    id: str
    source_conversation_id: str
    title: str | None
    message_count: int
    participant_names: list[str]
    earliest_message_at: datetime | None
    latest_message_at: datetime | None
    created_at: datetime


class ImportedMessageRead(ORMModel):
    id: str
    sender_name: str
    timestamp_ms: int
    content: str | None
    share_link: str | None
    has_media: bool
    source_file: str
    sequence: int
    created_at: datetime

class ImportedProfileRead(ORMModel):
    id: str
    username: str
    display_name: str | None
    is_archive_owner: bool
    created_at: datetime


# ── Vaka sohbeti ──

class CaseChatSessionCreate(BaseModel):
    scope_type: str = Field(default="evidence", max_length=40)
    scope_filters: dict[str, Any] = Field(default_factory=dict)
    title: str | None = None
    provider_connection_id: str | None = None


class CaseChatSessionRead(ORMModel):
    id: str
    case_id: str
    scope_type: str
    title: str | None
    created_at: datetime


class CaseChatMessageCreate(BaseModel):
    content: str = Field(min_length=1, max_length=20_000)


class CaseChatMessageRead(ORMModel):
    id: str
    role: str
    content: str
    cited_evidence: list[str]
    created_at: datetime
