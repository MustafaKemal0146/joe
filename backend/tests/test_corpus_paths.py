from pathlib import Path

import pytest

from app.config import Settings
from app.corpus import CorpusError, CorpusService


def corpus_service_for(root: Path, data_dir: Path) -> CorpusService:
    service = CorpusService()
    service.settings = Settings(
        _env_file=None,
        import_root=root,
        data_dir=data_dir,
    )
    service.artifacts.settings = service.settings
    return service


def test_browse_is_confined_to_configured_root(tmp_path: Path) -> None:
    allowed = tmp_path / "allowed"
    allowed.mkdir()
    (allowed / "alt").mkdir()
    (allowed / "not.txt").write_text("Yapısal dizin denetimi.", encoding="utf-8")
    service = corpus_service_for(allowed, tmp_path / "data")

    result = service.browse(".")

    assert {entry["name"] for entry in result["entries"]} == {"alt", "not.txt"}
    with pytest.raises(CorpusError, match="dışında"):
        service.browse("../")
    with pytest.raises(CorpusError, match="göreli"):
        service.browse("C:/Windows")
