from __future__ import annotations

import hashlib
import re
import unicodedata
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class EvidenceSegment:
    key: str
    content: str
    content_hash: str
    ordinal: int


def normalize_text(text: str) -> str:
    normalized = unicodedata.normalize("NFC", text.replace("\r\n", "\n").replace("\r", "\n"))
    normalized = "\n".join(line.rstrip() for line in normalized.splitlines())
    return re.sub(r"\n{4,}", "\n\n\n", normalized).strip()


def _split_long_block(block: str, max_chars: int) -> list[str]:
    if len(block) <= max_chars:
        return [block]
    sentences = re.split(r"(?<=[.!?…])\s+", block)
    chunks: list[str] = []
    current = ""
    for sentence in sentences:
        if len(sentence) > max_chars:
            if current:
                chunks.append(current)
                current = ""
            chunks.extend(
                sentence[index : index + max_chars]
                for index in range(0, len(sentence), max_chars)
            )
            continue
        candidate = f"{current} {sentence}".strip()
        if len(candidate) > max_chars and current:
            chunks.append(current)
            current = sentence
        else:
            current = candidate
    if current:
        chunks.append(current)
    return chunks


def build_evidence_pack(text: str, max_segment_chars: int = 900) -> list[EvidenceSegment]:
    normalized = normalize_text(text)
    blocks = [block.strip() for block in re.split(r"\n\s*\n", normalized) if block.strip()]
    if len(blocks) == 1 and "\n" in blocks[0]:
        blocks = [line.strip() for line in blocks[0].splitlines() if line.strip()]
    expanded: list[str] = []
    for block in blocks:
        expanded.extend(_split_long_block(block, max_segment_chars))
    return [
        EvidenceSegment(
            key=f"K{index}",
            content=content,
            content_hash=hashlib.sha256(content.encode("utf-8")).hexdigest(),
            ordinal=index,
        )
        for index, content in enumerate(expanded, start=1)
    ]


def render_evidence_pack(segments: list[EvidenceSegment]) -> str:
    return "\n\n".join(f"[{segment.key}] {segment.content}" for segment in segments)

