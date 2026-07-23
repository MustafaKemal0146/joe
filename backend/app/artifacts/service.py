from __future__ import annotations

import hashlib
import io
import json
import re
import zipfile
from pathlib import Path, PurePosixPath
from typing import Any

import fitz
import pytesseract
from charset_normalizer import from_bytes
from docx import Document
from fastapi import UploadFile
from PIL import Image
from sqlalchemy import select

from ..config import get_settings
from ..db import SessionLocal
from ..models import Artifact


class ArtifactError(RuntimeError):
    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


class ArtifactService:
    def __init__(self) -> None:
        self.settings = get_settings()

    async def ingest(self, upload: UploadFile, case_id: str | None = None) -> Artifact:
        filename = Path(upload.filename or "isimsiz-dosya").name
        data = await self._read_limited(upload)
        digest = hashlib.sha256(data).hexdigest()
        suffix = Path(filename).suffix.lower()[:12]
        destination = self.settings.data_dir / "artifacts" / f"{digest}{suffix}"
        if not destination.exists():
            destination.write_bytes(data)

        with SessionLocal() as db:
            existing = db.scalar(select(Artifact).where(Artifact.sha256 == digest))
            if existing:
                return existing

        media_type = upload.content_type or "application/octet-stream"
        extracted_text, extractor, metadata = self.extract_bytes(filename, media_type, data)
        if extracted_text:
            extracted_text = extracted_text[: self.settings.max_evidence_chars]
        with SessionLocal.begin() as db:
            artifact = Artifact(
                case_id=case_id,
                original_name=filename,
                media_type=media_type,
                storage_path=str(destination),
                sha256=digest,
                size_bytes=len(data),
                extractor=extractor,
                extracted_text=extracted_text,
                artifact_metadata=metadata,
            )
            db.add(artifact)
            db.flush()
            db.refresh(artifact)
            return artifact

    def extract_bytes(
        self, filename: str, media_type: str, data: bytes
    ) -> tuple[str | None, str, dict[str, Any]]:
        """Dosya yükleme ve salt-okunur dizin indeksi için ortak çıkarıcı sınırı."""
        return self._extract(filename, media_type, data)

    async def _read_limited(self, upload: UploadFile) -> bytes:
        chunks: list[bytes] = []
        total = 0
        while chunk := await upload.read(1024 * 1024):
            total += len(chunk)
            if total > self.settings.max_upload_bytes:
                raise ArtifactError(
                    "upload_too_large",
                    f"Dosya sınırı {self.settings.max_upload_bytes // (1024 * 1024)} MB.",
                )
            chunks.append(chunk)
        if not chunks:
            raise ArtifactError("empty_upload", "Yüklenen dosya boş.")
        return b"".join(chunks)

    def _extract(
        self, filename: str, media_type: str, data: bytes
    ) -> tuple[str | None, str, dict[str, Any]]:
        suffix = Path(filename).suffix.lower()
        if suffix in {".txt", ".md", ".csv", ".log"} or media_type.startswith("text/"):
            return self._decode_text(data), "metin", {"source_kind": "text"}
        if suffix == ".json" or media_type == "application/json":
            return self._extract_json(data)
        if suffix in {".png", ".jpg", ".jpeg", ".webp", ".bmp", ".tiff"}:
            return self._extract_image(data)
        if suffix == ".pdf" or media_type == "application/pdf":
            return self._extract_pdf(data)
        if suffix == ".docx":
            return self._extract_docx(data)
        if suffix == ".zip" or media_type in {"application/zip", "application/x-zip-compressed"}:
            return self._extract_zip(data)
        return None, "desteklenmiyor", {
            "source_kind": "binary",
            "warning": "Dosya saklandı ancak bu sürümde metin çıkarılamadı.",
        }

    @staticmethod
    def _decode_text(data: bytes) -> str:
        best = from_bytes(data).best()
        if best is None:
            return data.decode("utf-8", errors="replace")
        return str(best)

    def _extract_json(self, data: bytes) -> tuple[str, str, dict[str, Any]]:
        try:
            payload = json.loads(self._decode_text(data))
        except json.JSONDecodeError as exc:
            raise ArtifactError("invalid_json", "JSON dosyası ayrıştırılamadı.") from exc
        strings: list[str] = []
        key_counts: dict[str, int] = {}
        conversation_lines: list[str] = []
        self._walk_conversations(payload, conversation_lines)
        self._walk_json(payload, "$", strings, key_counts)
        top_level = list(payload.keys()) if isinstance(payload, dict) else ["liste"]
        extracted = "\n".join(conversation_lines) if conversation_lines else "\n".join(strings)
        return extracted, "json", {
            "source_kind": "json",
            "top_level_keys": top_level[:100],
            "observed_keys": sorted(key_counts, key=key_counts.get, reverse=True)[:100],
            "string_count": len(strings),
            "conversation_message_count": len(conversation_lines),
            "conversation_structure_detected": bool(conversation_lines),
        }

    def _walk_conversations(
        self, value: Any, lines: list[str], depth: int = 0
    ) -> None:
        """Instagram/WhatsApp benzeri dışa aktarımları şema adına bağımlı olmadan normalleştirir."""
        if depth > 24 or len(lines) >= 100_000:
            return
        if isinstance(value, dict):
            sender = value.get("sender_name") or value.get("sender") or value.get("from")
            content = value.get("content") or value.get("text") or value.get("message")
            if isinstance(sender, str) and isinstance(content, str) and content.strip():
                timestamp = (
                    value.get("timestamp_ms")
                    or value.get("timestamp")
                    or value.get("created_at")
                    or "zaman yok"
                )
                lines.append(f"[{timestamp}] {sender.strip()}: {content.strip()}")
            for item in value.values():
                self._walk_conversations(item, lines, depth + 1)
        elif isinstance(value, list):
            for item in value:
                self._walk_conversations(item, lines, depth + 1)

    def _walk_json(
        self,
        value: Any,
        path: str,
        strings: list[str],
        key_counts: dict[str, int],
        depth: int = 0,
    ) -> None:
        if depth > 20 or len(strings) >= 20_000:
            return
        if isinstance(value, dict):
            for key, item in value.items():
                key_counts[str(key)] = key_counts.get(str(key), 0) + 1
                self._walk_json(item, f"{path}.{key}", strings, key_counts, depth + 1)
        elif isinstance(value, list):
            for index, item in enumerate(value):
                self._walk_json(item, f"{path}[{index}]", strings, key_counts, depth + 1)
        elif isinstance(value, str) and value.strip():
            clean = value.strip()
            if len(clean) > 2 and not self._looks_like_private_technical_value(path, clean):
                strings.append(f"{path}: {clean}")

    @staticmethod
    def _looks_like_private_technical_value(path: str, value: str) -> bool:
        lowered = path.casefold()
        sensitive_keys = ("ip_address", "cookie", "password", "token", "session_id")
        if any(key in lowered for key in sensitive_keys):
            return True
        return bool(re.fullmatch(r"(?:\d{1,3}\.){3}\d{1,3}", value))

    def _extract_image(self, data: bytes) -> tuple[str, str, dict[str, Any]]:
        try:
            with Image.open(io.BytesIO(data)) as image:
                width, height = image.size
                converted = image.convert("RGB")
                try:
                    text = pytesseract.image_to_string(converted, lang="tur+eng")
                    language = "tur+eng"
                except pytesseract.TesseractError:
                    text = pytesseract.image_to_string(converted, lang="eng")
                    language = "eng"
        except pytesseract.TesseractNotFoundError as exc:
            raise ArtifactError(
                "ocr_unavailable",
                "OCR motoru bulunamadı. Docker kurulumu Türkçe OCR paketini içerir.",
            ) from exc
        except OSError as exc:
            raise ArtifactError("invalid_image", "Görüntü dosyası açılamadı.") from exc
        return text.strip(), "ocr", {
            "source_kind": "screenshot",
            "width": width,
            "height": height,
            "ocr_language": language,
        }

    @staticmethod
    def _extract_pdf(data: bytes) -> tuple[str, str, dict[str, Any]]:
        try:
            document = fitz.open(stream=data, filetype="pdf")
            pages = [page.get_text("text") for page in document]
            page_count = document.page_count
            document.close()
        except Exception as exc:
            raise ArtifactError("invalid_pdf", "PDF dosyası ayrıştırılamadı.") from exc
        return "\n\n".join(pages).strip(), "pdf", {
            "source_kind": "document",
            "page_count": page_count,
        }

    @staticmethod
    def _extract_docx(data: bytes) -> tuple[str, str, dict[str, Any]]:
        try:
            document = Document(io.BytesIO(data))
            paragraphs = [paragraph.text for paragraph in document.paragraphs if paragraph.text.strip()]
        except Exception as exc:
            raise ArtifactError("invalid_docx", "Word dosyası ayrıştırılamadı.") from exc
        return "\n\n".join(paragraphs), "docx", {
            "source_kind": "document",
            "paragraph_count": len(paragraphs),
        }

    def _extract_zip(self, data: bytes) -> tuple[str, str, dict[str, Any]]:
        texts: list[str] = []
        parsed_files: list[str] = []
        skipped_files: list[str] = []
        total_uncompressed = 0
        max_uncompressed = 250 * 1024 * 1024
        try:
            archive = zipfile.ZipFile(io.BytesIO(data))
        except zipfile.BadZipFile as exc:
            raise ArtifactError("invalid_zip", "ZIP arşivi açılamadı.") from exc

        members = archive.infolist()
        if len(members) > 5000:
            raise ArtifactError("zip_too_many_files", "Arşiv 5.000'den fazla dosya içeriyor.")
        for member in members:
            if member.is_dir():
                continue
            path = PurePosixPath(member.filename.replace("\\", "/"))
            if path.is_absolute() or ".." in path.parts or member.flag_bits & 0x1:
                skipped_files.append(member.filename)
                continue
            total_uncompressed += member.file_size
            if total_uncompressed > max_uncompressed:
                raise ArtifactError("zip_expansion_limit", "Arşivin açılmış boyutu güvenlik sınırını aşıyor.")
            if member.file_size > 10 * 1024 * 1024:
                skipped_files.append(member.filename)
                continue
            suffix = Path(member.filename).suffix.lower()
            if suffix not in {".txt", ".md", ".json", ".csv"}:
                continue
            raw = archive.read(member)
            if suffix == ".json":
                try:
                    extracted, _, _ = self._extract_json(raw)
                except ArtifactError:
                    skipped_files.append(member.filename)
                    continue
            else:
                extracted = self._decode_text(raw)
            if extracted.strip():
                texts.append(f"### {member.filename}\n{extracted.strip()}")
                parsed_files.append(member.filename)
            if sum(len(item) for item in texts) >= self.settings.max_evidence_chars:
                break
        archive.close()
        return "\n\n".join(texts), "arşiv", {
            "source_kind": "archive",
            "archive_file_count": len(members),
            "parsed_file_count": len(parsed_files),
            "parsed_files": parsed_files[:200],
            "skipped_files": skipped_files[:100],
            "truncated": sum(len(item) for item in texts) >= self.settings.max_evidence_chars,
        }
