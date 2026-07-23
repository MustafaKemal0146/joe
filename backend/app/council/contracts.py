from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, Field


Confidence = Literal["düşük", "orta", "yüksek"]


class Observation(BaseModel):
    claim: str = Field(min_length=5, max_length=1400)
    evidence_ids: list[str] = Field(default_factory=list, max_length=12)
    interpretation: str = Field(min_length=5, max_length=1800)
    alternatives: list[str] = Field(default_factory=list, max_length=6)
    confidence: Confidence = "düşük"


class PersonaAnalysis(BaseModel):
    thesis: str = Field(min_length=5, max_length=2400)
    observations: list[Observation] = Field(default_factory=list, max_length=12)
    tensions: list[str] = Field(default_factory=list, max_length=10)
    unknowns: list[str] = Field(default_factory=list, max_length=10)
    abstentions: list[str] = Field(default_factory=list, max_length=10)


class ChallengeItem(BaseModel):
    target_persona_id: str
    target_claim: str = Field(max_length=1400)
    issue: str = Field(max_length=1600)
    evidence_ids: list[str] = Field(default_factory=list, max_length=12)
    requested_revision: str = Field(max_length=1200)


class PersonaChallenge(BaseModel):
    challenges: list[ChallengeItem] = Field(default_factory=list, max_length=8)
    agreements: list[str] = Field(default_factory=list, max_length=8)
    blind_spots: list[str] = Field(default_factory=list, max_length=8)


class PersonaRevision(BaseModel):
    revised_thesis: str = Field(min_length=5, max_length=2600)
    observations: list[Observation] = Field(default_factory=list, max_length=12)
    changed_positions: list[str] = Field(default_factory=list, max_length=10)
    retained_positions: list[str] = Field(default_factory=list, max_length=10)
    unresolved_disagreements: list[str] = Field(default_factory=list, max_length=10)
    unknowns: list[str] = Field(default_factory=list, max_length=10)


class SynthesisClaim(BaseModel):
    statement: str = Field(min_length=5, max_length=1800)
    kind: Literal["gözlem", "kuramsal yorum", "karşı hipotez", "belirsizlik"]
    evidence_ids: list[str] = Field(default_factory=list, max_length=20)
    supporting_personas: list[str] = Field(default_factory=list, max_length=13)
    dissenting_personas: list[str] = Field(default_factory=list, max_length=13)
    confidence: Confidence = "düşük"


class CouncilSynthesis(BaseModel):
    executive_summary: str = Field(min_length=10, max_length=5000)
    claims: list[SynthesisClaim] = Field(default_factory=list, max_length=20)
    convergences: list[str] = Field(default_factory=list, max_length=12)
    disagreements: list[str] = Field(default_factory=list, max_length=12)
    missing_context: list[str] = Field(default_factory=list, max_length=12)
    scope_note: str = Field(min_length=5, max_length=1200)

