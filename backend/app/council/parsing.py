from __future__ import annotations

import json
from typing import TypeVar

from pydantic import BaseModel, ValidationError


T = TypeVar("T", bound=BaseModel)


class StructuredOutputError(RuntimeError):
    pass


def _extract_json_object(text: str) -> str:
    candidate = text.strip()
    if candidate.startswith("```"):
        lines = candidate.splitlines()
        if lines and lines[0].startswith("```"):
            lines = lines[1:]
        if lines and lines[-1].strip() == "```":
            lines = lines[:-1]
        candidate = "\n".join(lines).strip()
    try:
        json.loads(candidate)
        return candidate
    except json.JSONDecodeError:
        pass

    start = candidate.find("{")
    if start < 0:
        raise StructuredOutputError("Model yanıtında JSON nesnesi bulunamadı.")
    depth = 0
    in_string = False
    escaped = False
    for index in range(start, len(candidate)):
        char = candidate[index]
        if in_string:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == '"':
                in_string = False
            continue
        if char == '"':
            in_string = True
        elif char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return candidate[start : index + 1]
    raise StructuredOutputError("Model yanıtındaki JSON nesnesi tamamlanmamış.")


def parse_structured(text: str, schema: type[T]) -> T:
    try:
        payload = json.loads(_extract_json_object(text))
        return schema.model_validate(payload)
    except (json.JSONDecodeError, ValidationError, StructuredOutputError) as exc:
        raise StructuredOutputError(f"Yapılandırılmış çıktı doğrulanamadı: {exc}") from exc

