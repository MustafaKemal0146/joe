import pytest
from pydantic import ValidationError

from app.schemas import AnalysisCreate, AnalysisSourceInput, MAX_ANALYSIS_SOURCE_CHARS


def test_analysis_mode_is_not_a_user_selectable_field() -> None:
    with pytest.raises(ValidationError):
        AnalysisCreate.model_validate(
            {
                "title": "Sözleşme denetimi",
                "mode": "quick",
                "source_text": "Yapısal sözleşmeyi sınamak için yeterli uzunlukta metin.",
                "selected_personas": ["freud", "jung"],
                "default_provider_connection_id": "bağlantı",
            }
        )


def test_analysis_source_accepts_long_conversation_without_silent_truncation() -> None:
    content = "x" * MAX_ANALYSIS_SOURCE_CHARS

    source = AnalysisSourceInput(
        source_type="whatsapp",
        source_ref="inline",
        source_label="Uzun konuşma",
        content=content,
    )

    assert len(source.content) == MAX_ANALYSIS_SOURCE_CHARS
