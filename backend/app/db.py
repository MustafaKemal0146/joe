from __future__ import annotations

from collections.abc import Generator
from pathlib import Path

from alembic import command
from alembic.config import Config
from sqlalchemy import create_engine, inspect
from sqlalchemy.orm import DeclarativeBase, Session, sessionmaker

from .config import get_settings


class Base(DeclarativeBase):
    pass


def _engine_options(database_url: str) -> dict[str, object]:
    if database_url.startswith("sqlite"):
        return {"connect_args": {"check_same_thread": False}}
    return {"pool_pre_ping": True, "pool_size": 10, "max_overflow": 20}


settings = get_settings()
engine = create_engine(settings.database_url, **_engine_options(settings.database_url))
SessionLocal = sessionmaker(bind=engine, expire_on_commit=False, autoflush=False)


def init_db() -> None:
    from . import models  # noqa: F401

    # İlk yerel sürümlerde şema create_all ile oluşturulmuş olabiliyor. Bu
    # durumda veriyi silmeden mevcut şemayı Alembic head'e damgalarız; sonraki
    # açılışlarda gerçek migration zinciri çalışır. Temiz veritabanında ise
    # modeller başlangıç şemasını kurar ve aynı head damgalanır.
    inspector = inspect(engine)
    project_root = Path(__file__).resolve().parents[1]
    alembic_ini = project_root / "alembic.ini"
    if not inspector.has_table("alembic_version"):
        Base.metadata.create_all(bind=engine)
        config = Config(str(alembic_ini))
        command.stamp(config, "head")
        return

    config = Config(str(alembic_ini))
    command.upgrade(config, "head")


def get_db() -> Generator[Session, None, None]:
    db = SessionLocal()
    try:
        yield db
    finally:
        db.close()
