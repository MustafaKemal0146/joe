from __future__ import annotations

import json
from typing import Any

from ..personas.catalog import PersonaDefinition


ANALYSIS_SCHEMA = {
    "thesis": "ihtiyatlı tez",
    "observations": [
        {"claim": "gözlem", "evidence_ids": ["K1"], "interpretation": "kuramsal yorum", "alternatives": ["karşı açıklama"], "confidence": "düşük|orta|yüksek"}
    ],
    "tensions": ["metin içi çatışma"],
    "unknowns": ["eksik bağlam"],
    "abstentions": ["yapılmayan çıkarım"],
}

CHALLENGE_SCHEMA = {
    "challenges": [
        {"target_persona_id": "hedef", "target_claim": "itiraz edilen iddia", "issue": "sorun", "evidence_ids": ["K2"], "requested_revision": "nasıl sınırlandırılmalı"}
    ],
    "agreements": ["kanıtla desteklenen uzlaşma"],
    "blind_spots": ["gözden kaçan bağlam"],
}

REVISION_SCHEMA = {
    "revised_thesis": "güncel tez",
    "observations": ANALYSIS_SCHEMA["observations"],
    "changed_positions": ["değişen görüş ve nedeni"],
    "retained_positions": ["korunan görüş ve kanıtı"],
    "unresolved_disagreements": ["çözülemeyen ayrım"],
    "unknowns": ["eksik bağlam"],
}

SYNTHESIS_SCHEMA = {
    "executive_summary": "dengeli ve ihtiyatlı sonuç",
    "claims": [
        {"statement": "sonuç cümlesi", "kind": "gözlem|kuramsal yorum|karşı hipotez|belirsizlik", "evidence_ids": ["K1"], "supporting_personas": ["freud"], "dissenting_personas": ["foucault"], "confidence": "düşük|orta|yüksek"}
    ],
    "convergences": ["uzlaşı"],
    "disagreements": ["giderilmemiş ayrılık"],
    "missing_context": ["eksik veri"],
    "scope_note": "Bu çıktı klinik tanı değildir; sunulan içerik üzerinde kuramsal yorumdur.",
}


def _compact_analysis(analysis: dict[str, Any], max_observations: int = 4) -> dict[str, Any]:
    """Fazlar arası gönderim için persona çıktısını kısalt.

    Kanıt kimlikleri ve iddialar korunur; ayrıntılı yorumlar azaltılır.
    """
    observations = analysis.get("observations", []) or []
    compact: list[dict[str, Any]] = []
    for obs in observations[:max_observations]:
        compact.append({
            "claim": obs.get("claim", ""),
            "evidence_ids": obs.get("evidence_ids", []),
            "confidence": obs.get("confidence", "orta"),
        })
    return {
        "thesis": analysis.get("thesis", ""),
        "observations": compact,
        "tensions": (analysis.get("tensions", []) or [])[:4],
        "unknowns": (analysis.get("unknowns", []) or [])[:4],
        "abstentions": (analysis.get("abstentions", []) or [])[:4],
    }


def analysis_prompt(persona: PersonaDefinition, evidence: str) -> str:
    questions = "\n".join(f"- {item}" for item in persona.analysis_questions)
    return f"""Aşağıdaki kanıt paketini {persona.name} kuramsal merceğiyle bağımsız olarak incele.

YÖNLENDİRİCİ SORULAR
{questions}

KANIT PAKETİ
{evidence}

Bu merceğin kendine özgü kavramlarını kullan; genel-geçer yorum yazma. Kanıt yeterliyse
üç ila altı somut gözlem üret. Her gözlemde K* kanıtını, kuramsal bağını ve alternatif
açıklamayı ayrı ver. Veri kısaysa çekimser kal.

Yalnızca geçerli JSON döndür. Şema:
{json.dumps(ANALYSIS_SCHEMA, ensure_ascii=False, indent=2)}
"""


def challenge_prompt(
    persona: PersonaDefinition,
    peer_analyses: dict[str, dict[str, Any]],
) -> str:
    compact_peers = {
        key: _compact_analysis(value)
        for key, value in peer_analyses.items()
    }
    return f"""Kendi kuramsal sınırlarını koruyarak diğer konsey üyelerinin iddialarını çapraz sorgula.
En fazla dört somut itiraz üret. İtiraz kanıt yetersizliği, alternatif açıklama, bağlam atlama
veya aşırı kesinlik göstermelidir.

SANA ÖZEL ELEŞTİRİ GÖREVİ
{persona.challenge_focus}

DİĞER BAĞIMSIZ GÖRÜŞLER (özet)
{json.dumps(compact_peers, ensure_ascii=False, indent=2)}

Yalnızca geçerli JSON döndür. Şema:
{json.dumps(CHALLENGE_SCHEMA, ensure_ascii=False, indent=2)}
"""


def revision_prompt(
    persona: PersonaDefinition,
    own_analysis: dict[str, Any],
    incoming_challenges: list[dict[str, Any]],
) -> str:
    return f"""İlk görüşünü sana yöneltilen eleştiriler ışığında yeniden değerlendir.
Eleştiriyi otomatik kabul etme; kanıt güçlü ise değiştir, değilse koru. Yeni kanıt kimliği
üretme ve ilk analizde bulunmayan kesinlik ekleme.

İLK GÖRÜŞÜN (özet)
{json.dumps(_compact_analysis(own_analysis), ensure_ascii=False, indent=2)}

SANA YÖNELTİLEN ELEŞTİRİLER
{json.dumps(incoming_challenges[:6], ensure_ascii=False, indent=2)}

Yalnızca geçerli JSON döndür. Şema:
{json.dumps(REVISION_SCHEMA, ensure_ascii=False, indent=2)}
"""


MODERATOR_SYSTEM = """Sen Joe Kuramsal Konseyinin nötr sentez moderatörüsün.
Tarihsel kişiyi taklit etmezsin. Klinik tanı, tedavi önerisi, tehlikelilik, suçluluk veya
değişmez kişilik etiketi üretmezsin. Çoğunluk görüşünü otomatik doğru saymazsın.
Doğrudan gözlem, kuramsal yorum, karşı hipotez ve belirsizliği ayrı tutarsın.
Her iddiayı var olan kanıt kimliklerine bağlar; uydurma kanıt üretmezsin.
Türkçe, nötr ve açık yazarsın."""


def synthesis_prompt(
    evidence: str,
    persona_outputs: dict[str, dict[str, Any]],
) -> str:
    compact_outputs = {
        key: _compact_analysis(value)
        for key, value in persona_outputs.items()
    }
    return f"""Aşağıdaki gözden geçirilmiş konsey görüşlerinden kanıta dayalı ortak sonuç üret.
Farklılıkları silme; önemli muhalefeti görünür tut. Kuramsal yorumları olgu gibi yazma.
`executive_summary` en fazla üç anlaşılır paragraftan oluşsun: temalar, merceklerin
birleştiği/ayrıldığı noktalar, verinin sınırları.

KANIT PAKETİ
{evidence}

GÖZDEN GEÇİRİLMİŞ GÖRÜŞLER (özet)
{json.dumps(compact_outputs, ensure_ascii=False, indent=2)}

Yalnızca geçerli JSON döndür. Şema:
{json.dumps(SYNTHESIS_SCHEMA, ensure_ascii=False, indent=2)}
"""
