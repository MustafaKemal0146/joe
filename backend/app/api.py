from __future__ import annotations

import asyncio
import hashlib
import json
import re
from datetime import UTC, datetime

from fastapi import APIRouter, Depends, File, Form, HTTPException, Query, UploadFile, status
from fastapi.responses import Response, StreamingResponse
from sqlalchemy import func, select, text
from sqlalchemy.orm import Session, selectinload

from .artifacts import ArtifactError, ArtifactService
from .config import get_settings
from .corpus import CorpusError, CorpusService
from .db import SessionLocal, get_db
from .models import (
    AnalysisEvent,
    AnalysisSession,
    AnalysisStageRun,
    AnalysisSourceItem,
    AnalysisSourcePackage,
    Artifact,
    Case,
    CaseChatMessage,
    CaseChatSession,
    Corpus,
    EvidenceItem,
    ImportBatch,
    ImportedConversation,
    ImportedMessage,
    ImportedProfile,
    JobStatus,
    OsintRun,
    OsintRunEvent,
    OsintFinding,
    PageSnapshot,
    ProviderConnection,
)
from .osint.planner import build_manual_plan
from .osint.service import automatic_connector_ids
from .personas import get_persona, list_personas
from .providers import get_provider_registry
from .providers.client import ProviderClient, ProviderError
from .schemas import (
    AnalysisCreate,
    AnalysisRead,
    AnalysisSourceInput,
    ArtifactRead,
    CaseCreate,
    CaseRead,
    CorpusCreate,
    CorpusRead,
    CorpusSearchRead,
    CorpusSearchRequest,
    DashboardSummary,
    CaseChatMessageCreate,
    CaseChatMessageRead,
    CaseChatSessionCreate,
    CaseChatSessionRead,
    ImportBatchCreate,
    ImportBatchRead,
    ImportBrowseRead,
    ImportedConversationRead,
    ImportedMessageRead,
    ImportedProfileRead,
    ImportStatusRead,
    OsintPlanRequest,
    OsintRunCreate,
    OsintRunRead,
    PageAnalysisCreate,
    ProviderConnectionCreate,
    ProviderConnectionRead,
)
from .security.vault import SecretVault


router = APIRouter()
settings = get_settings()
registry = get_provider_registry()
vault = SecretVault()
artifact_service = ArtifactService()
corpus_service = CorpusService()


def _connection_read(connection: ProviderConnection) -> ProviderConnectionRead:
    payload = ProviderConnectionRead.model_validate(connection)
    return payload.model_copy(update={"has_api_key": bool(connection.encrypted_api_key)})


def _get_connection(db: Session, connection_id: str) -> ProviderConnection:
    connection = db.get(ProviderConnection, connection_id)
    if not connection:
        raise HTTPException(status_code=404, detail="Sağlayıcı bağlantısı bulunamadı.")
    return connection


def _get_case(db: Session, case_id: str) -> Case:
    case = db.get(Case, case_id)
    if not case:
        raise HTTPException(status_code=404, detail="Vaka bulunamadı.")
    return case


def _provider_client(connection: ProviderConnection) -> ProviderClient:
    try:
        profile = registry.get(connection.provider_id)
    except KeyError as exc:
        raise HTTPException(status_code=422, detail="Sağlayıcı profili bulunamadı.") from exc
    return ProviderClient(
        profile=profile,
        model=connection.model,
        api_key=vault.decrypt(connection.encrypted_api_key),
        base_url=connection.base_url,
        extra_config=connection.extra_config,
        timeout=settings.provider_timeout_seconds,
    )


@router.get("/health")
def health(db: Session = Depends(get_db)) -> dict[str, object]:
    db.execute(text("SELECT 1"))
    return {
        "status": "sağlıklı",
        "service": "joe-api",
        "environment": settings.environment,
        "time": datetime.now(UTC).isoformat(),
    }


@router.get("/summary", response_model=DashboardSummary)
def summary(db: Session = Depends(get_db)) -> DashboardSummary:
    def count(model: type) -> int:
        return int(db.scalar(select(func.count()).select_from(model)) or 0)

    active_analyses = int(
        db.scalar(
            select(func.count())
            .select_from(AnalysisSession)
            .where(AnalysisSession.status.in_([JobStatus.queued.value, JobStatus.running.value]))
        )
        or 0
    )
    active_osint = int(
        db.scalar(
            select(func.count())
            .select_from(OsintRun)
            .where(OsintRun.status.in_([JobStatus.queued.value, JobStatus.running.value]))
        )
        or 0
    )
    active_corpora = int(
        db.scalar(
            select(func.count())
            .select_from(Corpus)
            .where(Corpus.status.in_([JobStatus.queued.value, JobStatus.running.value]))
        )
        or 0
    )
    return DashboardSummary(
        case_count=count(Case),
        analysis_count=count(AnalysisSession),
        osint_run_count=count(OsintRun),
        provider_connection_count=count(ProviderConnection),
        active_jobs=active_analyses + active_osint + active_corpora,
    )


@router.get("/personas")
def personas() -> list[dict[str, object]]:
    return [persona.public_dict() for persona in list_personas()]


@router.get("/providers/profiles")
def provider_profiles() -> list[dict[str, object]]:
    return [profile.public_dict() for profile in registry.all()]


@router.get("/providers/connections", response_model=list[ProviderConnectionRead])
def provider_connections(db: Session = Depends(get_db)) -> list[ProviderConnectionRead]:
    connections = db.scalars(
        select(ProviderConnection).order_by(ProviderConnection.created_at.desc())
    ).all()
    return [_connection_read(item) for item in connections]


@router.post(
    "/providers/connections",
    response_model=ProviderConnectionRead,
    status_code=status.HTTP_201_CREATED,
)
def create_provider_connection(
    body: ProviderConnectionCreate, db: Session = Depends(get_db)
) -> ProviderConnectionRead:
    try:
        profile = registry.get(body.provider_id)
    except KeyError as exc:
        raise HTTPException(status_code=422, detail="Bilinmeyen sağlayıcı profili.") from exc
    if profile.requires_api_key and not body.api_key:
        raise HTTPException(status_code=422, detail=f"{profile.name} için API anahtarı gerekli.")
    if not body.base_url and not profile.default_base_url:
        raise HTTPException(status_code=422, detail="Bu sağlayıcı için temel URL gerekli.")
    connection = ProviderConnection(
        provider_id=profile.id,
        label=body.label,
        model=body.model,
        base_url=body.base_url,
        encrypted_api_key=vault.encrypt(body.api_key),
        extra_config=body.extra_config,
    )
    db.add(connection)
    db.commit()
    db.refresh(connection)
    return _connection_read(connection)


@router.delete("/providers/connections/{connection_id}", status_code=status.HTTP_204_NO_CONTENT)
def delete_provider_connection(connection_id: str, db: Session = Depends(get_db)) -> None:
    connection = _get_connection(db, connection_id)
    db.delete(connection)
    db.commit()


@router.post("/providers/connections/{connection_id}/test")
async def test_provider_connection(
    connection_id: str, db: Session = Depends(get_db)
) -> dict[str, object]:
    connection = _get_connection(db, connection_id)
    try:
        response = await _provider_client(connection).chat(
            "Kısa bir bağlantı denetimi yapıyorsun.",
            'Yalnızca {"durum":"tamam"} JSON nesnesini döndür.',
            temperature=0,
            structured=True,
        )
    except ProviderError as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc
    return {"status": "bağlandı", "model": response.model, "usage": response.usage}


@router.get("/providers/connections/{connection_id}/models")
async def list_provider_models(
    connection_id: str, db: Session = Depends(get_db)
) -> dict[str, object]:
    connection = _get_connection(db, connection_id)
    try:
        models = await _provider_client(connection).list_models()
    except ProviderError as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc
    return {"models": models}


@router.get("/cases", response_model=list[CaseRead])
def list_cases(db: Session = Depends(get_db)) -> list[Case]:
    return list(db.scalars(select(Case).order_by(Case.updated_at.desc())).all())


@router.post("/cases", response_model=CaseRead, status_code=status.HTTP_201_CREATED)
def create_case(body: CaseCreate, db: Session = Depends(get_db)) -> Case:
    case = Case(**body.model_dump())
    db.add(case)
    db.commit()
    db.refresh(case)
    return case


@router.delete("/cases/{case_id}", status_code=status.HTTP_204_NO_CONTENT)
def delete_case(case_id: str, db: Session = Depends(get_db)) -> Response:
    """Vaka ile ona ait kanıt/oturum kayıtlarını geri döndürülemez biçimde kaldırır."""
    case = _get_case(db, case_id)
    db.delete(case)
    db.commit()
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.get("/cases/{case_id}/workspace")
def case_workspace(case_id: str, db: Session = Depends(get_db)) -> dict[str, object]:
    """Vaka dosyasının bütün çalışma kayıtlarını tek kalıcı noktadan döndürür."""
    case = _get_case(db, case_id)
    analyses = list(
        db.scalars(
            select(AnalysisSession)
            .where(AnalysisSession.case_id == case_id)
            .options(selectinload(AnalysisSession.turns))
            .order_by(AnalysisSession.updated_at.desc())
            .limit(100)
        ).all()
    )
    runs = list(
        db.scalars(
            select(OsintRun)
            .where(OsintRun.case_id == case_id)
            .order_by(OsintRun.updated_at.desc())
            .limit(100)
        ).all()
    )
    corpora = list(
        db.scalars(
            select(Corpus)
            .where(Corpus.case_id == case_id)
            .order_by(Corpus.updated_at.desc())
            .limit(100)
        ).all()
    )
    imports = list(
        db.scalars(
            select(ImportBatch)
            .where(ImportBatch.case_id == case_id)
            .order_by(ImportBatch.updated_at.desc())
            .limit(100)
        ).all()
    )
    return {
        "case": CaseRead.model_validate(case).model_dump(mode="json"),
        "analyses": [AnalysisRead.model_validate(item).model_dump(mode="json") for item in analyses],
        "osint_runs": [OsintRunRead.model_validate(item).model_dump(mode="json") for item in runs],
        "corpora": [CorpusRead.model_validate(item).model_dump(mode="json") for item in corpora],
        "imports": [ImportBatchRead.model_validate(item).model_dump(mode="json") for item in imports],
    }


@router.get("/analyses", response_model=list[AnalysisRead])
def list_analyses(db: Session = Depends(get_db)) -> list[AnalysisSession]:
    return list(
        db.scalars(
            select(AnalysisSession)
            .options(selectinload(AnalysisSession.turns))
            .order_by(AnalysisSession.created_at.desc())
            .limit(100)
        ).all()
    )


@router.post("/analyses", response_model=AnalysisRead, status_code=status.HTTP_202_ACCEPTED)
def create_analysis(body: AnalysisCreate, db: Session = Depends(get_db)) -> AnalysisSession:
    for persona_id in body.selected_personas:
        try:
            get_persona(persona_id)
        except KeyError as exc:
            raise HTTPException(status_code=422, detail=f"Bilinmeyen persona: {persona_id}") from exc
    if body.default_provider_connection_id:
        _get_connection(db, body.default_provider_connection_id)
    if body.synthesis_provider_connection_id:
        _get_connection(db, body.synthesis_provider_connection_id)
    for persona_id, connection_id in body.provider_routes.items():
        if persona_id not in body.selected_personas:
            raise HTTPException(
                status_code=422,
                detail=f"Sağlayıcı rotası seçilmemiş personaya ait: {persona_id}",
            )
        _get_connection(db, connection_id)
    if not body.default_provider_connection_id and not body.provider_routes:
        raise HTTPException(status_code=422, detail="En az bir AI sağlayıcı bağlantısı seçilmeli.")
    if body.case_id:
        _get_case(db, body.case_id)
    source_items = list(body.source_items)
    if body.artifact_ids:
        artifacts = list(
            db.scalars(select(Artifact).where(Artifact.id.in_(body.artifact_ids))).all()
        )
        found_ids = {artifact.id for artifact in artifacts}
        missing = [artifact_id for artifact_id in body.artifact_ids if artifact_id not in found_ids]
        if missing:
            raise HTTPException(status_code=404, detail="Seçilen dosyalardan biri bulunamadı.")
        for artifact in artifacts:
            if artifact.case_id not in {None, body.case_id}:
                raise HTTPException(status_code=422, detail="Dosya seçilen vakaya bağlı değil.")
            content = artifact.extracted_text or (
                f"Görsel kaynak: {artifact.original_name}. OCR metni bulunamadı; "
                "görsel, görsel destekli sağlayıcıya aktarılmalıdır."
            )
            source_items.append(
                AnalysisSourceInput(
                    source_type="visual" if artifact.media_type.startswith("image/") else artifact.extractor,
                    source_ref=artifact.id,
                    source_label=artifact.original_name,
                    content=content,
                )
            )
    if not source_items and body.source_text.strip():
        source_items = [
            AnalysisSourceInput(
                source_type=body.source_type,
                source_ref="inline",
                source_label="Kullanıcının seçtiği içerik",
                content=body.source_text.strip(),
            )
        ]
    if not source_items:
        raise HTTPException(
            status_code=422,
            detail="Analiz için metin, dosya, görsel, arşiv veya indekslenmiş kaynak seçilmeli.",
        )

    rendered_sources = []
    prepared_items: list[tuple[AnalysisSourceInput, str]] = []
    total_chars = 0
    for item in source_items:
        content = item.content.strip()
        if not content.strip():
            continue
        if total_chars + len(content) > settings.max_evidence_chars:
            raise HTTPException(
                status_code=422,
                detail=(
                    f"Seçilen kaynaklar toplam {settings.max_evidence_chars:,} karakter sınırını aşıyor. "
                    "Joe içeriği sessizce kesmez; konuşmayı ayrı analiz oturumlarına böl veya daha dar bir kaynak seç."
                ).replace(",", "."),
            )
        rendered_sources.append(f"### {item.source_label}\n{content}")
        prepared_items.append((item, content))
        total_chars += len(content)
    if not rendered_sources:
        raise HTTPException(status_code=422, detail="Seçilen kaynaklardan analiz içeriği çıkarılamadı.")

    payload = body.model_dump(exclude={"source_items", "artifact_ids"})
    payload["source_text"] = "\n\n".join(rendered_sources)
    if len(source_items) > 1:
        payload["source_type"] = "mixed"
    session = AnalysisSession(**payload, mode="council")
    db.add(session)
    db.flush()
    package = AnalysisSourcePackage(analysis_session_id=session.id, version=1)
    db.add(package)
    db.flush()
    for sequence, (item, content) in enumerate(prepared_items):
        db.add(
            AnalysisSourceItem(
                package_id=package.id,
                source_type=item.source_type,
                source_ref=item.source_ref,
                source_label=item.source_label,
                content=content,
                content_hash=hashlib.sha256(content.encode("utf-8")).hexdigest(),
                sequence=sequence,
            )
        )
    db.commit()
    db.refresh(session)
    return session


@router.get("/analyses/{analysis_id}", response_model=AnalysisRead)
def get_analysis(analysis_id: str, db: Session = Depends(get_db)) -> AnalysisSession:
    session = db.scalar(
        select(AnalysisSession)
        .where(AnalysisSession.id == analysis_id)
        .options(selectinload(AnalysisSession.turns))
    )
    if not session:
        raise HTTPException(status_code=404, detail="Analiz oturumu bulunamadı.")
    return session


@router.post("/analyses/{analysis_id}/cancel", response_model=AnalysisRead)
def cancel_analysis(analysis_id: str, db: Session = Depends(get_db)) -> AnalysisSession:
    session = db.scalar(
        select(AnalysisSession)
        .where(AnalysisSession.id == analysis_id)
        .options(selectinload(AnalysisSession.turns))
    )
    if not session:
        raise HTTPException(status_code=404, detail="Analiz oturumu bulunamadı.")
    if session.status in {JobStatus.queued.value, JobStatus.running.value}:
        session.status = JobStatus.cancelled.value
        session.progress_phase = "iptal edildi"
        db.commit()
        db.refresh(session)
    return session


@router.post("/osint/plan")
def osint_plan(body: OsintPlanRequest) -> dict[str, object]:
    return build_manual_plan(body.query, body.query_type)


@router.get("/osint/runs", response_model=list[OsintRunRead])
def list_osint_runs(db: Session = Depends(get_db)) -> list[OsintRun]:
    return list(
        db.scalars(select(OsintRun).order_by(OsintRun.created_at.desc()).limit(100)).all()
    )


@router.post("/osint/runs", response_model=OsintRunRead, status_code=status.HTTP_202_ACCEPTED)
def create_osint_run(body: OsintRunCreate, db: Session = Depends(get_db)) -> OsintRun:
    case_id = body.case_id
    if case_id:
        _get_case(db, case_id)
    else:
        query_kind = "kullanıcı adı" if body.query_type == "username" else "tam ad"
        case = Case(
            name=f"{body.query} araştırması",
            subject_label=body.query,
            purpose=f"{query_kind.capitalize()} üzerinden açık kaynak araştırması",
            authorization_note="Vaka, OSINT araştırması başlatılırken otomatik oluşturuldu.",
        )
        db.add(case)
        db.flush()
        case_id = case.id
    run = OsintRun(
        **body.model_dump(exclude={"case_id"}),
        case_id=case_id,
        connectors=automatic_connector_ids(body.query_type),
    )
    db.add(run)
    db.commit()
    db.refresh(run)
    return run


@router.get("/osint/runs/{run_id}", response_model=OsintRunRead)
def get_osint_run(run_id: str, db: Session = Depends(get_db)) -> OsintRun:
    run = db.get(OsintRun, run_id)
    if not run:
        raise HTTPException(status_code=404, detail="OSINT çalışması bulunamadı.")
    return run


@router.post("/osint/findings/{finding_id}/page-analysis")
async def analyze_osint_page(
    finding_id: str,
    body: PageAnalysisCreate,
    db: Session = Depends(get_db),
) -> dict[str, object]:
    finding = db.get(OsintFinding, finding_id)
    if not finding:
        raise HTTPException(status_code=404, detail="OSINT bulgusu bulunamadı.")
    snapshot = db.scalar(
        select(PageSnapshot)
        .where(PageSnapshot.finding_id == finding_id)
        .order_by(PageSnapshot.created_at.desc())
        .limit(1)
    )
    if not snapshot:
        raise HTTPException(status_code=422, detail="Bu bağlantı için sayfa gözlemi bulunamadı.")
    connection = _get_connection(db, body.provider_connection_id)
    prompt = (
        "Aşağıdaki açık kaynak sayfa gözlemini incele. Yalnız gözlenen metadata ve "
        "metne dayan; hesabın kesin olarak aynı kişiye ait olduğunu iddia etme. "
        "Yanıtı Türkçe, nötr ve kanıt sınırlarını belirterek ver.\n\n"
        f"URL: {snapshot.url}\nBAŞLIK: {snapshot.title or 'yok'}\n"
        f"SAYFA METNİ:\n{(snapshot.page_text or '')[:settings.max_evidence_chars]}"
    )
    try:
        response = await _provider_client(connection).chat(
            "Sen Joe OSINT sayfa gözlem yardımcısısın; teşhis ve kesin kimlik kararı üretme.",
            prompt,
            temperature=0.1,
            structured=False,
        )
    except ProviderError as exc:
        raise HTTPException(status_code=502, detail=str(exc)) from exc
    snapshot.ai_analysis = {
        "text": response.text,
        "model": response.model,
        "usage": response.usage,
        "analyzed_at": datetime.now(UTC).isoformat(),
    }
    db.commit()
    return {"finding_id": finding_id, "snapshot_id": snapshot.id, "analysis": snapshot.ai_analysis}


@router.get("/analyses/{analysis_id}/stages")
def get_analysis_stages(analysis_id: str, db: Session = Depends(get_db)) -> list[dict[str, Any]]:
    session = db.get(AnalysisSession, analysis_id)
    if not session:
        raise HTTPException(status_code=404, detail="Analiz oturumu bulunamadı.")
    stages = db.scalars(
        select(AnalysisStageRun)
        .where(AnalysisStageRun.analysis_session_id == analysis_id)
        .order_by(AnalysisStageRun.created_at)
    ).all()
    return [
        {
            "phase": s.phase,
            "persona_id": s.persona_id,
            "status": s.status,
            "error_message": s.error_message,
            "started_at": s.started_at.isoformat() if s.started_at else None,
            "completed_at": s.completed_at.isoformat() if s.completed_at else None,
        }
        for s in stages
    ]


@router.get("/analyses/{analysis_id}/events")
async def analysis_events_endpoint(
    analysis_id: str,
    since: int = Query(default=0, ge=0),
    db: Session = Depends(get_db),
):
    session = db.get(AnalysisSession, analysis_id)
    if not session:
        raise HTTPException(status_code=404, detail="Analiz oturumu bulunamadı.")

    async def event_generator():
        last_seq = since
        while True:
            await asyncio.sleep(settings.worker_poll_seconds)
            with SessionLocal() as stream_db:
                new_events = stream_db.scalars(
                    select(AnalysisEvent)
                    .where(
                        AnalysisEvent.analysis_session_id == analysis_id,
                        AnalysisEvent.sequence > last_seq,
                    )
                    .order_by(AnalysisEvent.sequence)
                ).all()
                payloads = [
                    {
                        "event_type": evt.event_type,
                        "payload": evt.payload,
                        "sequence": evt.sequence,
                    }
                    for evt in new_events
                ]
            if not payloads:
                yield ": heartbeat\n\n"
                continue
            for payload in payloads:
                yield f"data: {json.dumps(payload, ensure_ascii=False)}\n\n"
                last_seq = payload["sequence"]

    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "Connection": "keep-alive", "X-Accel-Buffering": "no"},
    )


@router.get("/osint/runs/{run_id}/events")
async def osint_run_events_endpoint(
    run_id: str,
    since: int = Query(default=0, ge=0),
    db: Session = Depends(get_db),
):
    if not db.get(OsintRun, run_id):
        raise HTTPException(status_code=404, detail="OSINT çalışması bulunamadı.")

    async def event_generator():
        last_seq = since
        while True:
            await asyncio.sleep(settings.worker_poll_seconds)
            with SessionLocal() as stream_db:
                new_events = stream_db.scalars(
                    select(OsintRunEvent)
                    .where(
                        OsintRunEvent.osint_run_id == run_id,
                        OsintRunEvent.sequence > last_seq,
                    )
                    .order_by(OsintRunEvent.sequence)
                ).all()
                payloads = [
                    {
                        "event_type": evt.event_type,
                        "payload": evt.payload,
                        "sequence": evt.sequence,
                    }
                    for evt in new_events
                ]
            if not payloads:
                yield ": heartbeat\n\n"
                continue
            for payload in payloads:
                yield f"data: {json.dumps(payload, ensure_ascii=False)}\n\n"
                last_seq = payload["sequence"]

    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "Connection": "keep-alive", "X-Accel-Buffering": "no"},
    )


@router.post("/artifacts", response_model=ArtifactRead, status_code=status.HTTP_201_CREATED)
async def upload_artifact(
    file: UploadFile = File(...),
    case_id: str | None = Form(default=None),
    db: Session = Depends(get_db),
) -> object:
    if case_id:
        _get_case(db, case_id)
    try:
        return await artifact_service.ingest(file, case_id)
    except ArtifactError as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc


@router.get("/imports/status", response_model=ImportStatusRead)
def import_status() -> dict[str, object]:
    return corpus_service.status()


@router.get("/imports/browse", response_model=ImportBrowseRead)
def browse_imports(path: str = Query(default=".", max_length=1000)) -> dict[str, object]:
    try:
        return corpus_service.browse(path)
    except CorpusError as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc


@router.get("/corpora", response_model=list[CorpusRead])
def list_corpora(db: Session = Depends(get_db)) -> list[Corpus]:
    return list(db.scalars(select(Corpus).order_by(Corpus.created_at.desc()).limit(100)).all())


@router.post("/corpora", response_model=CorpusRead, status_code=status.HTTP_202_ACCEPTED)
def create_corpus(body: CorpusCreate, db: Session = Depends(get_db)) -> Corpus:
    if body.case_id:
        _get_case(db, body.case_id)
    try:
        browse_result = corpus_service.browse(body.relative_path)
    except CorpusError as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc
    corpus = Corpus(
        name=body.name,
        relative_path=str(browse_result["path"]),
        case_id=body.case_id,
    )
    db.add(corpus)
    db.commit()
    db.refresh(corpus)
    return corpus


@router.get("/corpora/{corpus_id}", response_model=CorpusRead)
def get_corpus(corpus_id: str, db: Session = Depends(get_db)) -> Corpus:
    corpus = db.get(Corpus, corpus_id)
    if not corpus:
        raise HTTPException(status_code=404, detail="Dizin indeksi bulunamadı.")
    return corpus


@router.post("/corpora/{corpus_id}/search", response_model=CorpusSearchRead)
def search_corpus(corpus_id: str, body: CorpusSearchRequest) -> dict[str, object]:
    try:
        return corpus_service.search(corpus_id, body.query, body.max_results)
    except CorpusError as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc


# ── Instagram / WhatsApp import ──

@router.post("/imports", response_model=ImportBatchRead, status_code=status.HTTP_202_ACCEPTED)
def create_import(body: ImportBatchCreate, db: Session = Depends(get_db)) -> ImportBatch:
    _get_case(db, body.case_id)
    try:
        import_root = str(corpus_service.resolve_directory(body.import_root))
    except CorpusError as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc
    batch = ImportBatch(
        import_root=import_root,
        source_type=body.source_type,
        case_id=body.case_id,
    )
    db.add(batch)
    db.commit()
    db.refresh(batch)
    return batch


@router.get("/imports", response_model=list[ImportBatchRead])
def list_imports(db: Session = Depends(get_db)) -> list[ImportBatch]:
    return list(db.scalars(
        select(ImportBatch).order_by(ImportBatch.created_at.desc()).limit(100)
    ).all())


@router.get("/imports/{batch_id}", response_model=ImportBatchRead)
def get_import(batch_id: str, db: Session = Depends(get_db)) -> ImportBatch:
    batch = db.get(ImportBatch, batch_id)
    if not batch:
        raise HTTPException(status_code=404, detail="İçe aktarma bulunamadı.")
    return batch


@router.post("/imports/{batch_id}/confirm-owner", response_model=ImportBatchRead)
def confirm_import_owner(batch_id: str, db: Session = Depends(get_db)) -> ImportBatch:
    batch = db.get(ImportBatch, batch_id)
    if not batch:
        raise HTTPException(status_code=404, detail="İçe aktarma bulunamadı.")
    batch.archive_owner_confirmed = True
    db.commit()
    db.refresh(batch)
    return batch


@router.get("/imports/{batch_id}/conversations", response_model=list[ImportedConversationRead])
def list_import_conversations(
    batch_id: str,
    db: Session = Depends(get_db),
) -> list[ImportedConversation]:
    batch = db.get(ImportBatch, batch_id)
    if not batch:
        raise HTTPException(status_code=404, detail="İçe aktarma bulunamadı.")
    return list(db.scalars(
        select(ImportedConversation)
        .where(ImportedConversation.import_batch_id == batch_id)
        .order_by(ImportedConversation.message_count.desc())
    ).all())


@router.get(
    "/imports/{batch_id}/conversations/{conv_id}/messages",
    response_model=list[ImportedMessageRead],
)
def list_import_messages(
    batch_id: str,
    conv_id: str,
    limit: int = Query(default=200, ge=1, le=1000),
    offset: int = Query(default=0, ge=0),
    db: Session = Depends(get_db),
) -> list[ImportedMessage]:
    batch = db.get(ImportBatch, batch_id)
    if not batch:
        raise HTTPException(status_code=404, detail="İçe aktarma bulunamadı.")
    return list(db.scalars(
        select(ImportedMessage)
        .where(ImportedMessage.conversation_id == conv_id)
        .order_by(ImportedMessage.timestamp_ms.desc())
        .offset(offset)
        .limit(limit)
    ).all())


@router.get("/imports/{batch_id}/profiles", response_model=list[ImportedProfileRead])
def list_import_profiles(batch_id: str, db: Session = Depends(get_db)) -> list[ImportedProfile]:
    batch = db.get(ImportBatch, batch_id)
    if not batch:
        raise HTTPException(status_code=404, detail="İçe aktarma bulunamadı.")
    return list(db.scalars(
        select(ImportedProfile)
        .where(ImportedProfile.import_batch_id == batch_id)
        .order_by(ImportedProfile.is_archive_owner.desc(), ImportedProfile.username)
    ).all())


# ── Vaka sohbeti ──

@router.post("/cases/{case_id}/chat/sessions", response_model=CaseChatSessionRead, status_code=status.HTTP_201_CREATED)
def create_chat_session(case_id: str, body: CaseChatSessionCreate, db: Session = Depends(get_db)) -> CaseChatSession:
    _get_case(db, case_id)
    if body.provider_connection_id:
        _get_connection(db, body.provider_connection_id)
    session = CaseChatSession(
        case_id=case_id,
        scope_type=body.scope_type,
        scope_filters=body.scope_filters,
        title=body.title,
        provider_connection_id=body.provider_connection_id,
    )
    db.add(session)
    db.commit()
    db.refresh(session)
    return session


@router.get("/cases/{case_id}/chat/sessions", response_model=list[CaseChatSessionRead])
def list_chat_sessions(case_id: str, db: Session = Depends(get_db)) -> list[CaseChatSession]:
    _get_case(db, case_id)
    return list(db.scalars(
        select(CaseChatSession)
        .where(CaseChatSession.case_id == case_id)
        .order_by(CaseChatSession.created_at.desc())
    ).all())


@router.post("/cases/{case_id}/chat/sessions/{session_id}/messages", response_model=CaseChatMessageRead, status_code=status.HTTP_201_CREATED)
async def send_chat_message(
    case_id: str,
    session_id: str,
    body: CaseChatMessageCreate,
    db: Session = Depends(get_db),
) -> CaseChatMessage:
    session = db.get(CaseChatSession, session_id)
    if not session or session.case_id != case_id:
        raise HTTPException(status_code=404, detail="Sohbet oturumu bulunamadı.")

    connection_id = session.provider_connection_id
    if not connection_id:
        raise HTTPException(status_code=422, detail="Vaka sohbeti için bir AI sağlayıcısı seçilmeli.")
    connection = _get_connection(db, connection_id)

    evidence_rows = list(
        db.scalars(
            select(EvidenceItem)
            .where(EvidenceItem.case_id == case_id)
            .order_by(EvidenceItem.created_at)
            .limit(200)
        ).all()
    )
    if not evidence_rows:
        raise HTTPException(status_code=422, detail="Bu vaka için sohbet edilecek K* kanıtı bulunamadı.")
    evidence_text = "\n\n".join(
        f"[{item.evidence_key}] {item.content}" for item in evidence_rows
    )[: settings.max_evidence_chars]

    # Kullanıcı mesajını kaydet; provider başarısız olursa bu kayıt korunur,
    # fakat sahte assistant cevabı yazılmaz.
    user_msg = CaseChatMessage(
        session_id=session_id,
        role="user",
        content=body.content,
    )
    db.add(user_msg)
    db.commit()
    system_prompt = (
        "Sen Joe vaka sohbeti asistanısın. Yalnızca verilen K* kanıtlarını kullan. "
        "Her maddi iddianın sonuna gerçek bir [K1] benzeri kanıt kimliği ekle. "
        "Kanıt yetersizse bunu açıkça söyle; klinik tanı, tehlikelilik veya sosyal "
        "puan üretme. Tarihsel personayı taklit etme. Türkçe, nötr ve kısa yaz."
    )
    user_prompt = f"KANITLAR\n{evidence_text}\n\nKULLANICI MESAJI\n{body.content}"
    try:
        response = await _provider_client(connection).chat(
            system_prompt,
            user_prompt,
            temperature=0.1,
            structured=False,
        )
    except ProviderError as exc:
        db.commit()
        raise HTTPException(status_code=502, detail=str(exc)) from exc

    assistant_content = response.text.strip()
    valid_keys = {item.evidence_key for item in evidence_rows}
    cited = sorted(
        {match for match in re.findall(r"\[(K\d+)\]", assistant_content) if match in valid_keys}
    )
    assistant_msg = CaseChatMessage(
        session_id=session_id,
        role="assistant",
        content=assistant_content,
        cited_evidence=cited,
        provider_metadata={"model": response.model, "usage": response.usage},
    )
    db.add(assistant_msg)
    db.commit()
    db.refresh(assistant_msg)
    return assistant_msg


@router.get("/cases/{case_id}/chat/sessions/{session_id}/messages", response_model=list[CaseChatMessageRead])
def list_chat_messages(
    case_id: str,
    session_id: str,
    db: Session = Depends(get_db),
) -> list[CaseChatMessage]:
    session = db.get(CaseChatSession, session_id)
    if not session or session.case_id != case_id:
        raise HTTPException(status_code=404, detail="Sohbet oturumu bulunamadı.")
    return list(db.scalars(
        select(CaseChatMessage)
        .where(CaseChatMessage.session_id == session_id)
        .order_by(CaseChatMessage.created_at)
    ).all())
