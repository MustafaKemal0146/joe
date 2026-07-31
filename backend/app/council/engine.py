from __future__ import annotations

import asyncio
import base64
import mimetypes
from pathlib import Path
from datetime import UTC, datetime
from typing import Any

from sqlalchemy import delete, func, select
from sqlalchemy.orm import selectinload

from ..config import get_settings
from ..db import SessionLocal
from ..models import (
    AnalysisEvent,
    AnalysisSession,
    AnalysisSourcePackage,
    AnalysisStageRun,
    CouncilTurn,
    Artifact,
    EvidenceItem,
    JobStatus,
    ProviderConnection,
)
from ..personas import get_persona
from ..providers import get_provider_registry
from ..providers.client import ProviderClient, ProviderError
from ..security.vault import SecretVault
from .contracts import CouncilSynthesis, PersonaAnalysis, PersonaChallenge, PersonaRevision
from .evidence import build_evidence_pack, render_evidence_pack
from .parsing import StructuredOutputError, parse_structured
from .prompts import (
    MODERATOR_SYSTEM,
    analysis_prompt,
    challenge_prompt,
    revision_prompt,
    synthesis_prompt,
)

# Konsey fazları için çıktı token sınırları. Bu sınırlar maliyeti kontrol altında
# tutar ve aşırı uzun, gereksiz yere ayrıntılı çıktıları önler.
PHASE_MAX_TOKENS = {
    "bağımsız_görüşler": 1200,
    "çapraz_sorgu": 1000,
    "görüş_revizyonu": 1200,
    "ortak_sentez": 1500,
}
from .validation import audit_payload


class CouncilRunError(RuntimeError):
    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


class CouncilEngine:
    def __init__(self) -> None:
        self.settings = get_settings()
        self.registry = get_provider_registry()
        self.vault = SecretVault()
        self._semaphore = asyncio.Semaphore(4)
    def _record_stage(
        self,
        session_id: str,
        phase: str,
        persona_id: str | None,
        status: str,
        payload: dict[str, Any] | None = None,
        error: str | None = None,
        connection_id: str | None = None,
    ) -> None:
        with SessionLocal.begin() as db:
            now = datetime.now(UTC)
            stage = None
            if status != "running":
                stage = db.scalar(
                    select(AnalysisStageRun)
                    .where(
                        AnalysisStageRun.analysis_session_id == session_id,
                        AnalysisStageRun.phase == phase,
                        AnalysisStageRun.persona_id == persona_id,
                        AnalysisStageRun.status == "running",
                    )
                    .order_by(AnalysisStageRun.created_at.desc())
                    .limit(1)
                )
            if stage:
                stage.status = status
                stage.payload = payload
                stage.error_message = error
                stage.provider_connection_id = connection_id or stage.provider_connection_id
                stage.completed_at = now
            else:
                db.add(AnalysisStageRun(
                    analysis_session_id=session_id,
                    phase=phase,
                    persona_id=persona_id,
                    status=status,
                    provider_connection_id=connection_id,
                    payload=payload,
                    error_message=error,
                    started_at=now,
                    completed_at=now if status in ("completed", "failed") else None,
                ))
            session = db.get(AnalysisSession, session_id)
            if session:
                session.heartbeat_at = now

    def _record_event(self, session_id: str, event_type: str, payload: dict[str, Any]) -> None:
        with SessionLocal.begin() as db:
            session = db.get(AnalysisSession, session_id, with_for_update=True)
            next_seq = int(
                db.scalar(
                    select(func.coalesce(func.max(AnalysisEvent.sequence), 0) + 1).where(
                        AnalysisEvent.analysis_session_id == session_id
                    )
                )
                or 1
            )
            db.add(AnalysisEvent(
                analysis_session_id=session_id,
                sequence=next_seq,
                event_type=event_type,
                payload=payload,
            ))
            if session:
                session.heartbeat_at = datetime.now(UTC)

    async def run(self, session_id: str) -> dict[str, Any]:
        self._prepare_recovered_run(session_id)
        session = self._load_session(session_id)
        if not session.selected_personas:
            raise CouncilRunError("persona_required", "En az iki kuramsal persona seçilmeli.")

        segments = build_evidence_pack(session.source_text)
        if not segments:
            raise CouncilRunError("evidence_required", "Analiz edilecek kanıt üretilemedi.")
        evidence_text = render_evidence_pack(segments)
        attachments = self._load_attachments(session)
        valid_evidence_ids = {segment.key for segment in segments}
        self._replace_evidence(session, segments)

        self._set_progress(session_id, "bağımsız_görüşler")
        self._record_event(session_id, "phase_started", {"phase": "bağımsız_görüşler", "persona_count": len(session.selected_personas)})
        independent = await self._run_independent(session, evidence_text, attachments)
        if self._is_cancelled(session_id):
            return {}
        successes = {key: value for key, value in independent.items() if value.get("ok")}
        if len(successes) < 2:
            errors = [value.get("error", "Bilinmeyen hata") for value in independent.values()]
            raise CouncilRunError(
                "insufficient_persona_results",
                "Konsey için en az iki geçerli persona yanıtı gerekli. " + " | ".join(errors[:3]),
            )

        self._set_progress(session_id, "çapraz_sorgu")
        self._record_event(session_id, "phase_started", {"phase": "çapraz_sorgu"})
        challenges = await self._run_challenges(session, evidence_text, successes, attachments)
        if self._is_cancelled(session_id):
            return {}
        self._raise_if_phase_fully_failed("çapraz_sorgu", challenges)
        self._set_progress(session_id, "görüş_revizyonu")
        self._record_event(session_id, "phase_started", {"phase": "görüş_revizyonu"})
        revisions = await self._run_revisions(session, evidence_text, successes, challenges, attachments)
        if self._is_cancelled(session_id):
            return {}
        self._raise_if_phase_fully_failed("görüş_revizyonu", revisions)
        final_persona_outputs: dict[str, dict[str, Any]] = {}
        for persona_id, original in successes.items():
            revision = revisions.get(persona_id)
            final_persona_outputs[persona_id] = (
                revision["payload"] if revision and revision.get("ok") else original["payload"]
            )

        self._set_progress(session_id, "ortak_sentez")
        self._record_event(session_id, "phase_started", {"phase": "ortak_sentez"})
        synthesis, synthesis_meta = await self._run_synthesis(
            session, evidence_text, final_persona_outputs, attachments
        )
        if self._is_cancelled(session_id):
            return {}
        audit = audit_payload(synthesis, valid_evidence_ids)

        result = {
            "version": "1.0",
            "protocol": "tam_konsey",
            "evidence": [
                {"id": segment.key, "content": segment.content, "sha256": segment.content_hash}
                for segment in segments
            ],
            "independent_analyses": independent,
            "challenges": challenges,
            "final_persona_outputs": final_persona_outputs,
            "synthesis": synthesis,
            "epistemic_audit": audit,
            "run_metadata": {
                "completed_at": datetime.now(UTC).isoformat(),
                "persona_count": len(session.selected_personas),
                "successful_persona_count": len(successes),
                "synthesis_provider": synthesis_meta,
            },
        }
        self._record_event(session_id, "analysis_completed", {"persona_count": len(session.selected_personas), "successful_persona_count": len(successes)})
        self._complete(session_id, result)
        return result

    @staticmethod
    def _is_cancelled(session_id: str) -> bool:
        with SessionLocal() as db:
            session = db.get(AnalysisSession, session_id)
            return bool(session and session.status == JobStatus.cancelled.value)

    @staticmethod
    def _raise_if_phase_fully_failed(phase: str, outputs: dict[str, dict[str, Any]]) -> None:
        if not outputs or any(value.get("ok") for value in outputs.values()):
            return
        errors = [str(value.get("error") or "Sağlayıcı yanıtı alınamadı.") for value in outputs.values()]
        phase_label = {
            "çapraz_sorgu": "Çapraz sorgu",
            "görüş_revizyonu": "Görüş revizyonu",
        }.get(phase, phase)
        raise CouncilRunError(
            "provider_phase_failed",
            f"{phase_label} aşamasında hiçbir geçerli sağlayıcı yanıtı alınamadı. {errors[0]}",
        )

    def _prepare_recovered_run(self, session_id: str) -> None:
        """Kesilen worker çalışmasını temiz bir protokol koşusu olarak yeniden başlat."""
        with SessionLocal.begin() as db:
            session = db.get(AnalysisSession, session_id)
            if not session or session.error_code != "worker_interrupted":
                return
            db.execute(delete(CouncilTurn).where(CouncilTurn.session_id == session_id))
            db.execute(delete(AnalysisStageRun).where(AnalysisStageRun.analysis_session_id == session_id))
            db.execute(delete(AnalysisEvent).where(AnalysisEvent.analysis_session_id == session_id))
            session.result = None
            session.error_code = None
            session.error_message = None
            session.heartbeat_at = datetime.now(UTC)

    async def _run_independent(
        self,
        session: AnalysisSession,
        evidence_text: str,
        attachments: list[dict[str, str]],
    ) -> dict[str, dict[str, Any]]:
        async def one(persona_id: str) -> tuple[str, dict[str, Any]]:
            persona = get_persona(persona_id)
            connection_id = session.provider_routes.get(persona_id) or session.default_provider_connection_id
            if not connection_id:
                return persona_id, {"ok": False, "error": "Bu persona için sağlayıcı seçilmedi."}
            try:
                client = self._client(connection_id)
                async with self._semaphore:
                    self._record_stage(session.id, "bağımsız_görüşler", persona_id, "running", connection_id=connection_id)
                    self._record_event(session.id, "persona_started", {"phase": "bağımsız_görüşler", "persona_id": persona_id})
                    response = await client.chat(
                        persona.system_prompt,
                        analysis_prompt(persona, evidence_text),
                        temperature=0.25,
                        attachments=attachments,
                        max_tokens=PHASE_MAX_TOKENS["bağımsız_görüşler"],
                    )
                parsed = parse_structured(response.text, PersonaAnalysis).model_dump()
                meta = {"model": response.model, "usage": response.usage, "connection_id": connection_id}
                self._save_turn(session.id, "bağımsız_görüş", persona_id, connection_id, parsed, meta)
                self._record_stage(session.id, "bağımsız_görüşler", persona_id, "completed", payload=parsed, connection_id=connection_id)
                self._record_event(session.id, "persona_completed", {"phase": "bağımsız_görüşler", "persona_id": persona_id})
                return persona_id, {"ok": True, "payload": parsed, "meta": meta}
            except (ProviderError, StructuredOutputError, KeyError) as exc:
                self._record_stage(session.id, "bağımsız_görüşler", persona_id, "failed", error=str(exc))
                self._record_event(session.id, "persona_failed", {"phase": "bağımsız_görüşler", "persona_id": persona_id, "message": str(exc)[:300]})
                return persona_id, {"ok": False, "error": str(exc)}

        pairs = await asyncio.gather(*(one(pid) for pid in session.selected_personas))
        return dict(pairs)

    async def _run_challenges(
        self,
        session: AnalysisSession,
        evidence_text: str,
        analyses: dict[str, dict[str, Any]],
        attachments: list[dict[str, str]],
    ) -> dict[str, dict[str, Any]]:
        async def one(persona_id: str) -> tuple[str, dict[str, Any]]:
            persona = get_persona(persona_id)
            connection_id = session.provider_routes.get(persona_id) or session.default_provider_connection_id
            peer_payloads = {
                key: value["payload"] for key, value in analyses.items() if key != persona_id
            }
            try:
                client = self._client(connection_id or "")
                async with self._semaphore:
                    self._record_stage(session.id, "çapraz_sorgu", persona_id, "running", connection_id=connection_id)
                    self._record_event(session.id, "persona_started", {"phase": "çapraz_sorgu", "persona_id": persona_id})
                    response = await client.chat(
                        persona.system_prompt,
                        challenge_prompt(persona, peer_payloads),
                        temperature=0.15,
                        attachments=attachments,
                        max_tokens=PHASE_MAX_TOKENS["çapraz_sorgu"],
                    )
                parsed = parse_structured(response.text, PersonaChallenge).model_dump()
                meta = {"model": response.model, "usage": response.usage, "connection_id": connection_id}
                self._save_turn(session.id, "çapraz_sorgu", persona_id, connection_id, parsed, meta)
                self._record_stage(session.id, "çapraz_sorgu", persona_id, "completed", payload=parsed, connection_id=connection_id)
                self._record_event(session.id, "persona_completed", {"phase": "çapraz_sorgu", "persona_id": persona_id})
                return persona_id, {"ok": True, "payload": parsed, "meta": meta}
            except (ProviderError, StructuredOutputError, KeyError) as exc:
                self._record_stage(session.id, "çapraz_sorgu", persona_id, "failed", error=str(exc))
                self._record_event(session.id, "persona_failed", {"phase": "çapraz_sorgu", "persona_id": persona_id, "message": str(exc)[:300]})
                return persona_id, {"ok": False, "error": str(exc)}

        pairs = await asyncio.gather(*(one(pid) for pid in analyses))
        return dict(pairs)

    async def _run_revisions(
        self,
        session: AnalysisSession,
        evidence_text: str,
        analyses: dict[str, dict[str, Any]],
        challenges: dict[str, dict[str, Any]],
        attachments: list[dict[str, str]],
    ) -> dict[str, dict[str, Any]]:
        async def one(persona_id: str) -> tuple[str, dict[str, Any]]:
            persona = get_persona(persona_id)
            connection_id = session.provider_routes.get(persona_id) or session.default_provider_connection_id
            incoming: list[dict[str, Any]] = []
            for challenger_id, challenge in challenges.items():
                if not challenge.get("ok"):
                    continue
                for item in challenge["payload"].get("challenges", []):
                    if item.get("target_persona_id") == persona_id:
                        incoming.append({"challenger": challenger_id, **item})
            try:
                client = self._client(connection_id or "")
                async with self._semaphore:
                    self._record_stage(session.id, "görüş_revizyonu", persona_id, "running", connection_id=connection_id)
                    self._record_event(session.id, "persona_started", {"phase": "görüş_revizyonu", "persona_id": persona_id})
                    response = await client.chat(
                        persona.system_prompt,
                        revision_prompt(
                            persona,
                            analyses[persona_id]["payload"],
                            incoming,
                        ),
                        temperature=0.2,
                        attachments=attachments,
                        max_tokens=PHASE_MAX_TOKENS["görüş_revizyonu"],
                    )
                parsed = parse_structured(response.text, PersonaRevision).model_dump()
                meta = {"model": response.model, "usage": response.usage, "connection_id": connection_id}
                self._save_turn(session.id, "görüş_revizyonu", persona_id, connection_id, parsed, meta)
                self._record_stage(session.id, "görüş_revizyonu", persona_id, "completed", payload=parsed, connection_id=connection_id)
                self._record_event(session.id, "persona_completed", {"phase": "görüş_revizyonu", "persona_id": persona_id})
                return persona_id, {"ok": True, "payload": parsed, "meta": meta}
            except (ProviderError, StructuredOutputError, KeyError) as exc:
                self._record_stage(session.id, "görüş_revizyonu", persona_id, "failed", error=str(exc))
                self._record_event(session.id, "persona_failed", {"phase": "görüş_revizyonu", "persona_id": persona_id, "message": str(exc)[:300]})
                return persona_id, {"ok": False, "error": str(exc)}

        pairs = await asyncio.gather(*(one(pid) for pid in analyses))
        return dict(pairs)

    async def _run_synthesis(
        self,
        session: AnalysisSession,
        evidence_text: str,
        outputs: dict[str, dict[str, Any]],
        attachments: list[dict[str, str]],
    ) -> tuple[dict[str, Any], dict[str, Any]]:
        # Konsey moderatörü ayrı seçilebilir; seçilmezse varsayılan rota korunur.
        connection_id = session.synthesis_provider_connection_id or session.default_provider_connection_id
        if not connection_id:
            connection_id = next(iter(session.provider_routes.values()), None)
        if not connection_id:
            raise CouncilRunError("provider_required", "Sentez için sağlayıcı seçilmedi.")
        self._record_stage(session.id, "ortak_sentez", "moderator", "running", connection_id=connection_id)
        self._record_event(session.id, "persona_started", {"phase": "ortak_sentez", "persona_id": "moderator"})
        try:
            client = self._client(connection_id)
            response = await client.chat(
                MODERATOR_SYSTEM,
                synthesis_prompt(evidence_text, outputs),
                temperature=0.1,
                attachments=attachments,
                max_tokens=PHASE_MAX_TOKENS["ortak_sentez"],
            )
            parsed = parse_structured(response.text, CouncilSynthesis).model_dump()
            meta = {"model": response.model, "usage": response.usage, "connection_id": connection_id}
            self._save_turn(session.id, "ortak_sentez", "moderator", connection_id, parsed, meta)
            self._record_stage(session.id, "ortak_sentez", "moderator", "completed", payload=parsed, connection_id=connection_id)
            self._record_event(session.id, "persona_completed", {"phase": "ortak_sentez", "persona_id": "moderator"})
            return parsed, meta
        except (ProviderError, StructuredOutputError, KeyError) as exc:
            self._record_stage(session.id, "ortak_sentez", "moderator", "failed", error=str(exc), connection_id=connection_id)
            self._record_event(session.id, "persona_failed", {"phase": "ortak_sentez", "persona_id": "moderator", "message": str(exc)[:300]})
            raise CouncilRunError("synthesis_failed", str(exc)) from exc

    def _client(self, connection_id: str) -> ProviderClient:
        with SessionLocal() as db:
            connection = db.get(ProviderConnection, connection_id)
            if not connection or not connection.enabled:
                raise ProviderError("connection_not_found", "Sağlayıcı bağlantısı bulunamadı.")
            try:
                profile = self.registry.get(connection.provider_id)
            except KeyError as exc:
                raise ProviderError("provider_not_found", "Sağlayıcı profili bulunamadı.") from exc
            return ProviderClient(
                profile=profile,
                model=connection.model,
                api_key=self.vault.decrypt(connection.encrypted_api_key),
                base_url=connection.base_url,
                extra_config=connection.extra_config,
                timeout=self.settings.provider_timeout_seconds,
            )

    def _load_session(self, session_id: str) -> AnalysisSession:
        with SessionLocal() as db:
            session = db.scalar(
                select(AnalysisSession)
                .where(AnalysisSession.id == session_id)
                .options(
                    selectinload(AnalysisSession.turns),
                    selectinload(AnalysisSession.source_packages).selectinload(
                        AnalysisSourcePackage.items
                    ),
                )
            )
            if not session:
                raise CouncilRunError("session_not_found", "Analiz oturumu bulunamadı.")
            return session

    @staticmethod
    def _load_attachments(session: AnalysisSession) -> list[dict[str, str]]:
        """Görsel kaynakları güvenli boyut sınırıyla provider'a aktarılabilir hâle getirir."""
        latest = session.source_packages[-1] if session.source_packages else None
        if not latest:
            return []
        attachments: list[dict[str, str]] = []
        with SessionLocal() as db:
            for item in latest.items:
                if item.source_type != "visual":
                    continue
                artifact = db.get(Artifact, item.source_ref)
                if not artifact:
                    continue
                path = Path(artifact.storage_path)
                if not path.is_file() or artifact.size_bytes > 12 * 1024 * 1024:
                    continue
                try:
                    encoded = base64.b64encode(path.read_bytes()).decode("ascii")
                except OSError:
                    continue
                attachments.append(
                    {
                        "media_type": artifact.media_type or mimetypes.guess_type(path.name)[0] or "image/jpeg",
                        "data": encoded,
                        "label": artifact.original_name,
                    }
                )
        return attachments

    def _replace_evidence(self, session: AnalysisSession, segments: list[Any]) -> None:
        with SessionLocal.begin() as db:
            db.execute(delete(EvidenceItem).where(EvidenceItem.analysis_session_id == session.id))
            for segment in segments:
                db.add(
                    EvidenceItem(
                        case_id=session.case_id,
                        analysis_session_id=session.id,
                        evidence_key=segment.key,
                        content=segment.content,
                        content_hash=segment.content_hash,
                        kind=session.source_type,
                    )
                )

    def _save_turn(
        self,
        session_id: str,
        phase: str,
        persona_id: str,
        connection_id: str | None,
        payload: dict[str, Any],
        meta: dict[str, Any],
    ) -> None:
        with SessionLocal.begin() as db:
            db.add(
                CouncilTurn(
                    session_id=session_id,
                    phase=phase,
                    persona_id=persona_id,
                    provider_connection_id=connection_id,
                    payload={**payload, "_meta": meta},
                )
            )

    def _set_progress(self, session_id: str, phase: str) -> None:
        with SessionLocal.begin() as db:
            session = db.get(AnalysisSession, session_id)
            if session:
                session.progress_phase = phase
                session.heartbeat_at = datetime.now(UTC)

    def _complete(self, session_id: str, result: dict[str, Any]) -> None:
        with SessionLocal.begin() as db:
            session = db.get(AnalysisSession, session_id)
            if session:
                session.status = JobStatus.completed.value
                session.progress_phase = "tamamlandı"
                session.result = result
                session.heartbeat_at = datetime.now(UTC)
