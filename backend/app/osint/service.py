from __future__ import annotations

import asyncio
import hashlib
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlsplit, urlunsplit

from sqlalchemy import select

from ..db import SessionLocal
from ..models import (
    JobStatus,
    OsintFinding,
    OsintFindingSource,
    OsintRun,
    OsintRunEvent,
)
from .base import OsintConnectorError
from .maigret import MaigretConnector
from .planner import build_manual_plan
from .public_profiles import PublicProfilesConnector
from .sherlock import SherlockConnector
from .whatsmyname import WhatsMyNameConnector


AUTO_CONNECTORS: dict[str, list[str]] = {
    "username": ["sherlock", "maigret", "whatsmyname", "public_profiles"],
    "full_name": ["public_profiles"],
}


def automatic_connector_ids(query_type: str) -> list[str]:
    return list(AUTO_CONNECTORS.get(query_type, []))


class OsintService:
    def __init__(self) -> None:
        items = [
            SherlockConnector(),
            MaigretConnector(),
            WhatsMyNameConnector(),
            PublicProfilesConnector(),
        ]
        self.connectors = {connector.id: connector for connector in items}
        self._event_seq: dict[str, int] = {}

    async def run(self, run_id: str) -> dict[str, Any]:
        with SessionLocal() as db:
            run = db.get(OsintRun, run_id)
            if not run:
                raise RuntimeError("OSINT çalışması bulunamadı.")
            query = run.query
            query_type = run.query_type
            connector_ids = automatic_connector_ids(query_type)

        result: dict[str, Any] = {
            "manual_plan": build_manual_plan(query, query_type),
            "connector_results": {},
            "errors": [],
            "findings": [],
            "coverage": {
                "requested_connectors": connector_ids,
                "completed_connectors": [],
            },
        }

        async def execute(
            connector_id: str,
        ) -> tuple[str, dict[str, Any] | None, dict[str, str] | None]:
            connector = self.connectors.get(connector_id)
            if not connector:
                return connector_id, None, {
                    "connector": connector_id,
                    "code": "unknown_connector",
                    "message": "Bilinmeyen bağlayıcı.",
                }
            try:
                payload = await connector.run(query, query_type)
                return connector_id, payload, None
            except OsintConnectorError as exc:
                return connector_id, None, {
                    "connector": connector_id,
                    "code": exc.code,
                    "message": str(exc),
                }
            except Exception as exc:
                return connector_id, None, {
                    "connector": connector_id,
                    "code": "connector_failed",
                    "message": str(exc)[:500],
                }

        raw_findings: list[dict[str, Any]] = []
        tasks = [asyncio.create_task(execute(item)) for item in connector_ids]
        for completed_task in asyncio.as_completed(tasks):
            connector_id, payload, error = await completed_task
            if error:
                result["errors"].append(error)
                self._record_event(run_id, "connector_failed", {
                    "connector": connector_id,
                    "code": error["code"],
                })
            elif payload is not None:
                result["connector_results"][connector_id] = payload
                result["coverage"]["completed_connectors"].append(connector_id)
                new_findings = payload.get("findings", [])
                raw_findings.extend(new_findings)
                self._commit_findings(run_id, connector_id, new_findings)
                self._record_event(run_id, "connector_completed", {
                    "connector": connector_id,
                    "finding_count": len(new_findings),
                })
                for source_error in payload.get("source_errors", []):
                    result["errors"].append(
                        {
                            "connector": f"{connector_id}/{source_error.get('source', 'kaynak')}",
                            "code": "source_failed",
                            "message": source_error.get("message", "Kaynak sorgusu tamamlanamadı."),
                        }
                    )
                    self._record_event(run_id, "source_rate_limited", {
                        "connector": connector_id,
                        "source": source_error.get("source"),
                    })
            result["findings"] = self._deduplicate(raw_findings)
            result["coverage"].update(
                {
                    "raw_finding_count": len(raw_findings),
                    "unique_finding_count": len(result["findings"]),
                    "partial": len(result["coverage"]["completed_connectors"]) < len(connector_ids),
                }
            )
            self._persist_progress(run_id, result)

        result["findings"] = self._deduplicate(raw_findings)
        result["coverage"].update(
            {
                "raw_finding_count": len(raw_findings),
                "unique_finding_count": len(result["findings"]),
                "partial": bool(result["errors"]),
            }
        )

        completed = bool(result["coverage"]["completed_connectors"])
        final_status = JobStatus.completed.value if completed else JobStatus.failed.value
        error_code = None if completed else "all_connectors_failed"
        error_message = (
            None if completed else "Uygun OSINT bağlayıcılarının hiçbiri tamamlanamadı."
        )

        with SessionLocal.begin() as db:
            run = db.get(OsintRun, run_id)
            if run:
                run.status = final_status
                run.result = result
                run.error_code = error_code
                run.error_message = error_message
                run.heartbeat_at = datetime.now(UTC)
        return result

    def _next_seq(self, run_id: str) -> int:
        seq = self._event_seq.get(run_id, 0) + 1
        self._event_seq[run_id] = seq
        return seq

    def _record_event(self, run_id: str, event_type: str, payload: dict[str, Any]) -> None:
        with SessionLocal.begin() as db:
            db.add(OsintRunEvent(
                osint_run_id=run_id,
                sequence=self._next_seq(run_id),
                event_type=event_type,
                payload=payload,
            ))

    @staticmethod
    def _commit_findings(run_id: str, connector_id: str, findings: list[dict[str, Any]]) -> int:
        count = 0
        with SessionLocal.begin() as db:
            for f in findings:
                profile_url = str(f.get("profile_url", "")).strip()
                if not profile_url:
                    continue
                dedup_hash = hashlib.sha256(profile_url.encode()).hexdigest()
                existing = db.scalar(
                    select(OsintFinding).where(
                        OsintFinding.osint_run_id == run_id,
                        OsintFinding.dedup_hash == dedup_hash,
                    )
                )
                if existing:
                    source_exists = db.scalar(
                        select(OsintFindingSource).where(
                            OsintFindingSource.finding_id == existing.id,
                            OsintFindingSource.connector == connector_id,
                        )
                    )
                    if not source_exists:
                        db.add(OsintFindingSource(
                            finding_id=existing.id,
                            connector=connector_id,
                            site=f.get("site", ""),
                        ))
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
        return count

    @staticmethod
    def _persist_progress(run_id: str, result: dict[str, Any]) -> None:
        with SessionLocal.begin() as db:
            run = db.get(OsintRun, run_id)
            if run and run.status == JobStatus.running.value:
                run.result = result
                run.heartbeat_at = datetime.now(UTC)

    @staticmethod
    def _deduplicate(findings: list[dict[str, Any]]) -> list[dict[str, Any]]:
        merged: dict[str, dict[str, Any]] = {}
        for finding in findings:
            url = str(finding.get("profile_url") or "").strip()
            if not url:
                continue
            parsed = urlsplit(url)
            key = urlunsplit(
                (
                    parsed.scheme.casefold(),
                    parsed.netloc.casefold(),
                    parsed.path.rstrip("/"),
                    parsed.query,
                    "",
                )
            )
            existing = merged.get(key)
            if not existing:
                sources = finding.get("sources") or [finding.get("connector")]
                merged[key] = {
                    **finding,
                    "sources": list(dict.fromkeys(item for item in sources if item)),
                }
                continue
            sources = [
                *(existing.get("sources") or []),
                *(finding.get("sources") or [finding.get("connector")]),
            ]
            existing["sources"] = list(dict.fromkeys(item for item in sources if item))
        return sorted(
            merged.values(),
            key=lambda item: (
                str(item.get("site", "")).casefold(),
                str(item.get("profile_url", "")),
            ),
        )
