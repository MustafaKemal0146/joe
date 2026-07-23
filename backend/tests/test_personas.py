from app.personas import get_persona, list_personas


def test_expected_collective_is_complete() -> None:
    personas = list_personas()
    ids = {item.id for item in personas}

    assert len(personas) == 13
    assert {"freud", "jung", "klein", "reich", "fromm", "kristeva", "zizek"} <= ids
    assert all(item.analysis_questions for item in personas)
    assert all("KANIT SÖZLEŞMESİ" in item.system_prompt for item in personas)


def test_jung_and_klein_use_the_requested_theoretical_axes() -> None:
    assert "Kolektif bilinçdışı" in get_persona("jung").concepts
    assert "Paranoid-şizoid konum" in get_persona("klein").concepts

