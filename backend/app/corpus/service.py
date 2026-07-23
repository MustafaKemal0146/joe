from __future__ import annotations

import hashlib
import mimetypes
import os
import re
from datetime import UTC, datetime
from pathlib import Path, PurePosixPath
from typing import Any

from sqlalchemy import delete, or_, select

from ..artifacts import ArtifactError, ArtifactService
from ..config import get_settings
from ..db import SessionLocal
from ..models import Corpus, CorpusDocument, JobStatus


SUPPORTED_SUFFIXES = {
    ".txt",
    ".md",
    ".csv",
    ".log",
    ".json",
    ".png",
    ".jpg",
    ".jpeg",
    ".webp",
    ".bmp",
    ".tiff",
    ".pdf",
    ".docx",
    ".zip",
}


class CorpusError(RuntimeError):
    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


class CorpusService:
    """Salt-okunur içe aktarma kökünü indeksler; ham dosyaları kopyalamaz."""

    def __init__(self) -> None:
        self.settings = get_settings()
        self.artifacts = ArtifactService()

    @property
    def root(self) -> Path:
        return self.settings.import_root.resolve()

    def status(self) -> dict[str, object]:
        available = self.settings.import_root.exists() and self.settings.import_root.is_dir()
        return {
            "configured": True,
            "available": available,
            "root_label": "Yerel içe aktarma alanı" if available else "Bağlı dizin bulunamadı",
        }

    def resolve_directory(self, relative_path: str) -> Path:
        raw = (relative_path or ".").strip().replace("\\", "/")
        if "\x00" in raw or re.match(r"^[A-Za-z]:", raw):
            raise CorpusError("invalid_path", "Yalnız bağlı alanın içindeki göreli dizinler kullanılabilir.")
        relative = PurePosixPath(raw)
        if relative.is_absolute() or ".." in relative.parts:
            raise CorpusError("path_outside_root", "Dizin, izin verilen içe aktarma alanının dışında.")
        root = self.root
        if not root.exists() or not root.is_dir():
            raise CorpusError(
                "import_root_unavailable",
                "Salt-okunur içe aktarma dizini Docker'a bağlanmamış.",
            )
        candidate = (root / Path(*relative.parts)).resolve()
        if candidate != root and root not in candidate.parents:
            raise CorpusError("path_outside_root", "Dizin, izin verilen içe aktarma alanının dışında.")
        if not candidate.exists() or not candidate.is_dir():
            raise CorpusError("directory_not_found", "Seçilen dizin bulunamadı.")
        return candidate

    def browse(self, relative_path: str = ".") -> dict[str, object]:
        directory = self.resolve_directory(relative_path)
        root = self.root
        current = directory.relative_to(root).as_posix() or "."
        parent = None
        if directory != root:
            parent_value = directory.parent.relative_to(root).as_posix()
            parent = parent_value or "."
        entries: list[dict[str, object]] = []
        try:
            children = sorted(
                directory.iterdir(),
                key=lambda item: (not item.is_dir(), item.name.casefold()),
            )
        except OSError as exc:
            raise CorpusError("directory_unreadable", "Dizin içeriği okunamadı.") from exc
        for child in children[:1000]:
            if child.is_symlink():
                continue
            try:
                relative = child.relative_to(root).as_posix()
                if child.is_dir():
                    entries.append({"name": child.name, "relative_path": relative, "kind": "dizin"})
                elif child.is_file():
                    entries.append(
                        {
                            "name": child.name,
                            "relative_path": relative,
                            "kind": "dosya",
                            "size_bytes": child.stat().st_size,
                        }
                    )
            except OSError:
                continue
        return {"path": current, "parent_path": parent, "entries": entries}

    def scan(self, corpus_id: str) -> None:
        with SessionLocal() as db:
            corpus = db.get(Corpus, corpus_id)
            if not corpus:
                raise CorpusError("corpus_not_found", "Dizin indeksi bulunamadı.")
            relative_path = corpus.relative_path

        try:
            source = self.resolve_directory(relative_path)
            self._scan_directory(corpus_id, source)
        except CorpusError as exc:
            self._fail(corpus_id, exc.code, str(exc))
            raise
        except Exception as exc:
            self._fail(corpus_id, "index_failed", str(exc)[:1000])
            raise

    def _scan_directory(self, corpus_id: str, source: Path) -> None:
        with SessionLocal.begin() as db:
            db.execute(delete(CorpusDocument).where(CorpusDocument.corpus_id == corpus_id))

        file_count = 0
        indexed_count = 0
        total_bytes = 0
        errors: list[dict[str, str]] = []
        extractor_counts: dict[str, int] = {}

        for directory, dirnames, filenames in os.walk(source, followlinks=False):
            base = Path(directory)
            dirnames[:] = [name for name in dirnames if not (base / name).is_symlink()]
            for filename in filenames:
                path = base / filename
                file_count += 1
                try:
                    if path.is_symlink() or not path.is_file():
                        continue
                    stat = path.stat()
                    total_bytes += stat.st_size
                    relative = path.relative_to(source).as_posix()
                    suffix = path.suffix.lower()
                    if suffix not in SUPPORTED_SUFFIXES:
                        continue
                    if stat.st_size > self.settings.max_index_file_bytes:
                        errors.append({"path": relative, "code": "file_too_large"})
                        continue
                    data = path.read_bytes()
                    media_type = mimetypes.guess_type(path.name)[0] or "application/octet-stream"
                    text, extractor, metadata = self.artifacts.extract_bytes(
                        path.name, media_type, data
                    )
                    if not text or not text.strip():
                        continue
                    clean_text = text.strip()[: self.settings.max_index_document_chars]
                    digest = hashlib.sha256(data).hexdigest()
                    with SessionLocal.begin() as db:
                        db.add(
                            CorpusDocument(
                                corpus_id=corpus_id,
                                relative_path=relative,
                                media_type=media_type,
                                size_bytes=stat.st_size,
                                sha256=digest,
                                extractor=extractor,
                                extracted_text=clean_text,
                                document_metadata={
                                    **metadata,
                                    "text_truncated": len(text.strip()) > len(clean_text),
                                },
                            )
                        )
                    indexed_count += 1
                    extractor_counts[extractor] = extractor_counts.get(extractor, 0) + 1
                except (ArtifactError, OSError, ValueError) as exc:
                    errors.append(
                        {
                            "path": self._safe_relative(path, source),
                            "code": getattr(exc, "code", "read_failed"),
                            "message": str(exc)[:300],
                        }
                    )
                if file_count % 25 == 0:
                    self._progress(corpus_id, file_count, indexed_count, total_bytes, len(errors))

        with SessionLocal.begin() as db:
            corpus = db.get(Corpus, corpus_id)
            if corpus:
                corpus.status = JobStatus.completed.value
                corpus.file_count = file_count
                corpus.indexed_file_count = indexed_count
                corpus.total_bytes = total_bytes
                corpus.error_count = len(errors)
                corpus.scan_summary = {
                    "extractors": extractor_counts,
                    "errors": errors[:200],
                    "raw_files_copied": False,
                    "source_access": "salt-okunur",
                }
                corpus.heartbeat_at = datetime.now(UTC)

    def search(self, corpus_id: str, query: str, max_results: int = 20) -> dict[str, Any]:
        with SessionLocal() as db:
            corpus = db.get(Corpus, corpus_id)
            if not corpus:
                raise CorpusError("corpus_not_found", "Dizin indeksi bulunamadı.")
            if corpus.status != JobStatus.completed.value:
                raise CorpusError("corpus_not_ready", "Dizin indeksi henüz hazır değil.")

            terms = self._terms(query)
            if not terms:
                raise CorpusError("empty_search", "Arama için en az bir anlamlı sözcük gerekli.")
            conditions = [CorpusDocument.extracted_text.ilike(f"%{self._escape_like(term)}%", escape="\\") for term in terms]
            documents = list(
                db.scalars(
                    select(CorpusDocument)
                    .where(CorpusDocument.corpus_id == corpus_id, or_(*conditions))
                    .limit(500)
                ).all()
            )

        ranked: list[tuple[float, CorpusDocument]] = []
        for document in documents:
            lowered = document.extracted_text.casefold()
            path_lowered = document.relative_path.casefold()
            matches = [lowered.count(term) for term in terms]
            coverage = sum(1 for count in matches if count)
            path_bonus = sum(2 for term in terms if term in path_lowered)
            score = coverage * 10 + min(sum(matches), 100) + path_bonus
            ranked.append((float(score), document))
        ranked.sort(key=lambda item: (-item[0], item[1].relative_path.casefold()))

        hits: list[dict[str, Any]] = []
        analysis_parts: list[str] = []
        analysis_length = 0
        truncated = False
        for score, document in ranked[:max_results]:
            excerpt = self._excerpt(document.extracted_text, terms)
            hit = {
                "document_id": document.id,
                "relative_path": document.relative_path,
                "extractor": document.extractor,
                "score": score,
                "excerpt": excerpt,
            }
            hits.append(hit)
            block = f"[DOSYA: {document.relative_path}]\n{excerpt}"
            if analysis_length + len(block) + 2 <= self.settings.max_evidence_chars:
                analysis_parts.append(block)
                analysis_length += len(block) + 2
            else:
                truncated = True
                break

        return {
            "corpus_id": corpus_id,
            "query": query,
            "hits": hits,
            "analysis_text": "\n\n".join(analysis_parts),
            "truncated": truncated or len(ranked) > max_results,
        }

    @staticmethod
    def _terms(query: str) -> list[str]:
        return list(dict.fromkeys(re.findall(r"[^\W_]{2,}", query.casefold(), flags=re.UNICODE)))[:12]

    @staticmethod
    def _escape_like(value: str) -> str:
        return value.replace("\\", "\\\\").replace("%", "\\%").replace("_", "\\_")

    @staticmethod
    def _excerpt(text: str, terms: list[str], radius: int = 900) -> str:
        lowered = text.casefold()
        positions = [lowered.find(term) for term in terms]
        positions = [position for position in positions if position >= 0]
        center = min(positions) if positions else 0
        start = max(0, center - radius // 3)
        end = min(len(text), start + radius)
        prefix = "…" if start else ""
        suffix = "…" if end < len(text) else ""
        return prefix + text[start:end].strip() + suffix

    def _progress(
        self, corpus_id: str, file_count: int, indexed_count: int, total_bytes: int, error_count: int
    ) -> None:
        with SessionLocal.begin() as db:
            corpus = db.get(Corpus, corpus_id)
            if corpus:
                corpus.file_count = file_count
                corpus.indexed_file_count = indexed_count
                corpus.total_bytes = total_bytes
                corpus.error_count = error_count
                corpus.heartbeat_at = datetime.now(UTC)

    def _fail(self, corpus_id: str, code: str, message: str) -> None:
        with SessionLocal.begin() as db:
            corpus = db.get(Corpus, corpus_id)
            if corpus:
                corpus.status = JobStatus.failed.value
                corpus.error_code = code
                corpus.error_message = message
                corpus.heartbeat_at = datetime.now(UTC)

    @staticmethod
    def _safe_relative(path: Path, root: Path) -> str:
        try:
            return path.relative_to(root).as_posix()
        except ValueError:
            return path.name
