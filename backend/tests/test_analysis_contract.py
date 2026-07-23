import pytest
from pydantic import ValidationError

from app.schemas import AnalysisCreate


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
