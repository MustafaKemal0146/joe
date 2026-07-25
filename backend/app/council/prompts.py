from __future__ import annotations

import json
from typing import Any

from ..personas.catalog import PersonaDefinition


ANALYSIS_SCHEMA = {
    "thesis": "Bu merceğin temel, ihtiyatlı tezi",
    "observations": [
        {
            "claim": "Metinde doğrudan gözlenen örüntü",
            "evidence_ids": ["K1"],
            "interpretation": "Kuramsal yorum",
            "alternatives": ["Daha sıradan bir karşı açıklama"],
            "confidence": "düşük | orta | yüksek",
        }
    ],
    "tensions": ["Metin içi çatışma veya gerilim"],
    "unknowns": ["Eksik bağlam"],
    "abstentions": ["Veri yetmediği için yapılmayan çıkarım"],
}

CHALLENGE_SCHEMA = {
    "challenges": [
        {
            "target_persona_id": "hedef kimliği",
            "target_claim": "itiraz edilen iddia",
            "issue": "kanıt, mantık veya kuram sorunu",
            "evidence_ids": ["K2"],
            "requested_revision": "nasıl sınırlandırılmalı",
        }
    ],
    "agreements": ["kanıtla desteklenen uzlaşma"],
    "blind_spots": ["konseyin gözden kaçırdığı bağlam"],
}

REVISION_SCHEMA = {
    "revised_thesis": "Eleştirilerden sonra güncellenmiş tez",
    "observations": ANALYSIS_SCHEMA["observations"],
    "changed_positions": ["değişen görüş ve nedeni"],
    "retained_positions": ["korunan görüş ve kanıtı"],
    "unresolved_disagreements": ["çözülemeyen ayrım"],
    "unknowns": ["eksik bağlam"],
}

SYNTHESIS_SCHEMA = {
    "executive_summary": "Kurullar arası dengeli ve ihtiyatlı sonuç",
    "claims": [
        {
            "statement": "sonuç cümlesi",
            "kind": "gözlem | kuramsal yorum | karşı hipotez | belirsizlik",
            "evidence_ids": ["K1"],
            "supporting_personas": ["freud"],
            "dissenting_personas": ["foucault"],
            "confidence": "düşük | orta | yüksek",
        }
    ],
    "convergences": ["uzlaşı"],
    "disagreements": ["giderilmemiş ayrılık"],
    "missing_context": ["eksik veri"],
    "scope_note": "Bu çıktı klinik tanı değildir; sunulan içerik üzerinde kuramsal yorumdur.",
}


def analysis_prompt(persona: PersonaDefinition, evidence: str) -> str:
    questions = "\n".join(f"- {item}" for item in persona.analysis_questions)
    return f"""Aşağıdaki kanıt paketini {persona.name} kuramsal merceğiyle bağımsız olarak incele.

YÖNLENDİRİCİ SORULAR
{questions}

KANIT PAKETİ
{evidence}

Bu merceğin kendine özgü kavramlarını açıkça kullan: genel-geçer bir yorum yazma.
Kanıt yeterliyse üç ila altı somut gözlem üret; her gözlemde doğrudan alıntılanan
K* kanıtını, kuramsal bağını ve makul alternatif açıklamayı ayrı ver. Veri kısa
veya bağlamsızsa sayıyı zorlamadan neden çekimser kaldığını belirt.

Yalnızca geçerli JSON döndür. Markdown kullanma. Tam şema:
{json.dumps(ANALYSIS_SCHEMA, ensure_ascii=False, indent=2)}
"""


def challenge_prompt(
    persona: PersonaDefinition,
    evidence: str,
    peer_analyses: dict[str, dict[str, Any]],
) -> str:
    return f"""Kendi kuramsal sınırlarını koruyarak diğer konsey üyelerinin iddialarını çapraz sorgula.
En fazla dört güçlü ve somut itiraz üret. İtiraz sırf kuramsal farklılık değil; kanıt yetersizliği,
alternatif açıklama, bağlam atlama veya aşırı kesinlik göstermelidir.

SANA ÖZEL ELEŞTİRİ GÖREVİ
{persona.challenge_focus}

KANIT PAKETİ
{evidence}

DİĞER BAĞIMSIZ GÖRÜŞLER
{json.dumps(peer_analyses, ensure_ascii=False, indent=2)}

Yalnızca geçerli JSON döndür. Markdown kullanma. Tam şema:
{json.dumps(CHALLENGE_SCHEMA, ensure_ascii=False, indent=2)}
"""


def revision_prompt(
    persona: PersonaDefinition,
    evidence: str,
    own_analysis: dict[str, Any],
    incoming_challenges: list[dict[str, Any]],
) -> str:
    return f"""İlk görüşünü sana yöneltilen eleştiriler ışığında yeniden değerlendir.
Eleştiriyi otomatik kabul etme; kanıt güçlü ise değiştir, değilse kanıtla koru.
Yeni kanıt kimliği üretme ve ilk analizde bulunmayan kesinlik ekleme.

KANIT PAKETİ
{evidence}

İLK GÖRÜŞÜN
{json.dumps(own_analysis, ensure_ascii=False, indent=2)}

SANA YÖNELTİLEN ELEŞTİRİLER
{json.dumps(incoming_challenges, ensure_ascii=False, indent=2)}

Yalnızca geçerli JSON döndür. Markdown kullanma. Tam şema:
{json.dumps(REVISION_SCHEMA, ensure_ascii=False, indent=2)}
"""


MODERATOR_SYSTEM = """Sen Joe Kuramsal Konseyinin nötr sentez moderatörüsün.
Tarihsel bir kişiyi taklit etmezsin. Klinik tanı, tedavi önerisi, tehlikelilik,
suçluluk veya değişmez kişilik etiketi üretmezsin. Çoğunluk görüşünü otomatik
doğru saymazsın. Doğrudan gözlem, kuramsal yorum, karşı hipotez ve belirsizliği
ayrı tutarsın. Her iddiayı var olan kanıt kimliklerine bağlar; uydurma kanıt
üretmezsin. Türkçe, nötr ve açık yazarsın."""


def synthesis_prompt(
    evidence: str,
    persona_outputs: dict[str, dict[str, Any]],
) -> str:
    return f"""Aşağıdaki gözden geçirilmiş konsey görüşlerinden kanıta dayalı ortak sonuç üret.
Farklılıkları silme; önemli muhalefeti görünür tut. Kuramsal yorumları olgu gibi yazma.
`executive_summary` en az üç anlaşılır paragraftan oluşsun: önce kanıtta görülen
temalar, sonra hangi merceklerin nerede birleşip ayrıldığı, son olarak verinin
sınırları ve yapılmayan çıkarımlar. Bu açıklama, bütün persona görüşlerini,
itirazları ve revizyonları okuyarak yazılan üst düzey genel yorumdur.

KANIT PAKETİ
{evidence}

GÖZDEN GEÇİRİLMİŞ GÖRÜŞLER
{json.dumps(persona_outputs, ensure_ascii=False, indent=2)}

Yalnızca geçerli JSON döndür. Markdown kullanma. Tam şema:
{json.dumps(SYNTHESIS_SCHEMA, ensure_ascii=False, indent=2)}
"""
