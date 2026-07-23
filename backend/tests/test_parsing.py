import pytest

from app.council.contracts import PersonaAnalysis
from app.council.parsing import StructuredOutputError, parse_structured


def test_parses_json_inside_fence_without_regex_fallback_values() -> None:
    parsed = parse_structured(
        """```json
        {"thesis":"İhtiyatlı tez", "observations":[], "tensions":[], "unknowns":[], "abstentions":[]}
        ```""",
        PersonaAnalysis,
    )
    assert parsed.thesis == "İhtiyatlı tez"


def test_invalid_model_output_fails_instead_of_fabricating_analysis() -> None:
    with pytest.raises(StructuredOutputError):
        parse_structured("Bence kişi kesinlikle şöyledir.", PersonaAnalysis)

