from __future__ import annotations

import re
from typing import Any


DIAGNOSTIC_PATTERNS = [
    re.compile(pattern, re.IGNORECASE)
    for pattern in [
        r"\bkesinlikle\s+(narsis|psikopat|sosyopat)",
        r"\bteşhis(?:i|lidir|tir)?\b",
        r"\btanı(?:sı|dır| koy)",
        r"\bsuç işlemeye yatkın",
        r"\btehlikeli kişilik",
    ]
]


def collect_evidence_ids(payload: Any) -> list[str]:
    found: list[str] = []
    if isinstance(payload, dict):
        for key, value in payload.items():
            if key == "evidence_ids" and isinstance(value, list):
                found.extend(str(item) for item in value)
            else:
                found.extend(collect_evidence_ids(value))
    elif isinstance(payload, list):
        for item in payload:
            found.extend(collect_evidence_ids(item))
    return found


def audit_payload(payload: dict[str, Any], valid_evidence_ids: set[str]) -> dict[str, Any]:
    citations = collect_evidence_ids(payload)
    invalid = sorted({citation for citation in citations if citation not in valid_evidence_ids})
    serialized = str(payload)
    diagnostic_flags = [pattern.pattern for pattern in DIAGNOSTIC_PATTERNS if pattern.search(serialized)]

    claims = payload.get("claims", []) if isinstance(payload, dict) else []
    unsupported_claims = []
    for index, claim in enumerate(claims):
        if not isinstance(claim, dict):
            continue
        if claim.get("kind") in {"gözlem", "kuramsal yorum"} and not claim.get("evidence_ids"):
            unsupported_claims.append(index)

    return {
        "valid": not invalid and not diagnostic_flags and not unsupported_claims,
        "invalid_evidence_ids": invalid,
        "unsupported_claim_indexes": unsupported_claims,
        "diagnostic_language_flags": diagnostic_flags,
        "citation_count": len(citations),
        "unique_citation_count": len(set(citations)),
    }

