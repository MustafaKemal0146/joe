from __future__ import annotations

import asyncio
import logging
import signal
from datetime import UTC, datetime, timedelta

from sqlalchemy import or_, select, update

from .config import get_settings
from .corpus import CorpusError, CorpusService
from .council.engine import CouncilEngine, CouncilRunError
from .db import SessionLocal, init_db
from .importers.instagram import import_instagram_archive
from .models import AnalysisSession, Corpus, ImportBatch, JobStatus, OsintRun
from .osint import OsintService


logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)
logger = logging.getLogger("joe-worker")
# OSINT sorgularındaki kullanıcı adları ve tam adlar URL içinde yer alabilir.
# İstemci kütüphanelerinin istek bazlı INFO kayıtları yerel loga yazılmaz.
logging.getLogger("httpx").setLevel(logging.WARNING)
logging.getLogger("httpcore").setLevel(logging.WARNING)


class Worker:
    def __init__(self) -> None:
        self.settings = get_settings()
        self.stop_event = asyncio.Event()
        self.council = CouncilEngine()
        self.osint = OsintService()
        self.corpus = CorpusService()

    def stop(self) -> None:
        self.stop_event.set()

    async def run(self) -> None:
        init_db()
        self._recover_stale_jobs()
        logger.info("Joe worker hazır")
        while not self.stop_event.is_set():
            analysis_id = self._claim_analysis()
            if analysis_id:
                await self._run_analysis(analysis_id)
                continue
            osint_id = self._claim_osint()
            if osint_id:
                await self._run_osint(osint_id)
                continue
            corpus_id = self._claim_corpus()
            if corpus_id:
                await self._run_corpus(corpus_id)
                continue
            import_id = self._claim_import()
            if import_id:
                await self._run_import(import_id)
                continue
            try:
                await asyncio.wait_for(
                    self.stop_event.wait(), timeout=self.settings.worker_poll_seconds
                )
            except TimeoutError:
                pass

    def _claim_analysis(self) -> str | None:
        with SessionLocal.begin() as db:
            job = db.scalar(
                select(AnalysisSession)
                .where(AnalysisSession.status == JobStatus.queued.value)
                .order_by(AnalysisSession.created_at)
                .with_for_update(skip_locked=True)
                .limit(1)
            )
            if not job:
                return None
            job.status = JobStatus.running.value
            job.progress_phase = "hazırlanıyor"
            job.heartbeat_at = datetime.now(UTC)
            return job.id

    def _claim_osint(self) -> str | None:
        with SessionLocal.begin() as db:
            job = db.scalar(
                select(OsintRun)
                .where(OsintRun.status == JobStatus.queued.value)
                .order_by(OsintRun.created_at)
                .with_for_update(skip_locked=True)
                .limit(1)
            )
            if not job:
                return None
            job.status = JobStatus.running.value
            job.heartbeat_at = datetime.now(UTC)
            return job.id

    def _claim_corpus(self) -> str | None:
        with SessionLocal.begin() as db:
            job = db.scalar(
                select(Corpus)
                .where(Corpus.status == JobStatus.queued.value)
                .order_by(Corpus.created_at)
                .with_for_update(skip_locked=True)
                .limit(1)
            )
            if not job:
                return None
            job.status = JobStatus.running.value
            job.heartbeat_at = datetime.now(UTC)
            return job.id

    async def _run_analysis(self, analysis_id: str) -> None:
        logger.info("Analiz başladı: %s", analysis_id)
        try:
            await self.council.run(analysis_id)
        except CouncilRunError as exc:
            self._fail_analysis(analysis_id, exc.code, str(exc))
        except Exception as exc:  # defensive boundary; details stay in local logs
            logger.exception("Analiz beklenmeyen hatayla durdu: %s", analysis_id)
            self._fail_analysis(analysis_id, "internal_error", str(exc)[:1000])

    async def _run_osint(self, run_id: str) -> None:
        logger.info("OSINT çalışması başladı: %s", run_id)
        try:
            await self.osint.run(run_id)
        except Exception as exc:
            logger.exception("OSINT çalışması beklenmeyen hatayla durdu: %s", run_id)
            with SessionLocal.begin() as db:
                run = db.get(OsintRun, run_id)
                if run:
                    run.status = JobStatus.failed.value
                    run.error_code = "internal_error"
                    run.error_message = str(exc)[:1000]

    async def _run_corpus(self, corpus_id: str) -> None:
        logger.info("Dizin indeksi başladı: %s", corpus_id)
        try:
            await asyncio.to_thread(self.corpus.scan, corpus_id)
        except CorpusError:
            logger.warning("Dizin indeksi tamamlanamadı: %s", corpus_id)
        except Exception:
            logger.exception("Dizin indeksi beklenmeyen hatayla durdu: %s", corpus_id)

    def _claim_import(self) -> str | None:
        with SessionLocal.begin() as db:
            job = db.scalar(
                select(ImportBatch)
                .where(ImportBatch.status == JobStatus.queued.value)
                .order_by(ImportBatch.created_at)
                .with_for_update(skip_locked=True)
                .limit(1)
            )
            if not job:
                return None
            job.status = JobStatus.running.value
            job.heartbeat_at = datetime.now(UTC)
            return job.id

    async def _run_import(self, batch_id: str) -> None:
        logger.info("İçe aktarma başladı: %s", batch_id)
        try:
            await asyncio.to_thread(self._import_sync, batch_id)
        except Exception as exc:
            logger.exception("İçe aktarma beklenmeyen hatayla durdu: %s", batch_id)
            with SessionLocal.begin() as db:
                batch = db.get(ImportBatch, batch_id)
                if batch and batch.status == JobStatus.running.value:
                    batch.status = JobStatus.failed.value
                    batch.error_code = "import_error"
                    batch.error_message = str(exc)[:1000]

    @staticmethod
    def _import_sync(batch_id: str) -> None:
        # SQLAlchemy Session thread-safe değildir; Session'ı to_thread dışında
        # oluşturmak yerine import thread'inin içinde açıyoruz.
        with SessionLocal() as db:
            import_instagram_archive(db, batch_id)

    def _fail_analysis(self, analysis_id: str, code: str, message: str) -> None:
        with SessionLocal.begin() as db:
            session = db.get(AnalysisSession, analysis_id)
            if session:
                session.status = JobStatus.failed.value
                session.error_code = code
                session.error_message = message
                session.progress_phase = "başarısız"
                session.heartbeat_at = datetime.now(UTC)

    def _recover_stale_jobs(self) -> None:
        cutoff = datetime.now(UTC) - timedelta(minutes=30)
        with SessionLocal.begin() as db:
            db.execute(
                update(AnalysisSession)
                .where(
                    AnalysisSession.status == JobStatus.running.value,
                    or_(
                        AnalysisSession.heartbeat_at.is_(None),
                        AnalysisSession.heartbeat_at < cutoff,
                    ),
                )
                .values(status=JobStatus.queued.value, progress_phase="yeniden sıraya alındı")
            )
            db.execute(
                update(OsintRun)
                .where(
                    OsintRun.status == JobStatus.running.value,
                    or_(OsintRun.heartbeat_at.is_(None), OsintRun.heartbeat_at < cutoff),
                )
                .values(status=JobStatus.queued.value)
            )
            db.execute(
                update(Corpus)
                .where(
                    Corpus.status == JobStatus.running.value,
                    or_(Corpus.heartbeat_at.is_(None), Corpus.heartbeat_at < cutoff),
                )
                .values(status=JobStatus.queued.value)
            )


async def main() -> None:
    worker = Worker()
    loop = asyncio.get_running_loop()
    for signal_name in (signal.SIGINT, signal.SIGTERM):
        try:
            loop.add_signal_handler(signal_name, worker.stop)
        except NotImplementedError:
            pass
    await worker.run()


if __name__ == "__main__":
    asyncio.run(main())
