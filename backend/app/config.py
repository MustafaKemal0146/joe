from __future__ import annotations

from functools import lru_cache
from pathlib import Path
from typing import Annotated

from pydantic import field_validator
from pydantic_settings import BaseSettings, NoDecode, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="JOE_",
        env_file=(".env", "../.env"),
        env_file_encoding="utf-8",
        extra="ignore",
    )

    app_name: str = "Joe"
    environment: str = "development"
    api_prefix: str = "/api/v1"
    database_url: str = "sqlite:///./joe.db"
    redis_url: str = "redis://localhost:6379/0"
    data_dir: Path = Path("./data")
    import_root: Path = Path("./imports")
    cors_origins: Annotated[list[str], NoDecode] = [
        "http://localhost:4177",
        "http://127.0.0.1:4177",
        "http://localhost:3000",
        "http://127.0.0.1:3000",
        "http://localhost:5173",
        "http://127.0.0.1:5173",
    ]
    worker_poll_seconds: float = 1.5
    provider_timeout_seconds: float = 90.0
    osint_timeout_seconds: int = 600
    max_upload_bytes: int = 75 * 1024 * 1024
    # Bu sınır yükleme/ZIP sınırından ayrıdır: metin tabanlı analiz kaynakları
    # için açık bir bellek ve sağlayıcı bağlamı korumasıdır. İçerik hiçbir zaman
    # sessizce kesilmez; toplam bu sınırı aşarsa API anlaşılır biçimde reddeder.
    max_evidence_chars: int = 1_000_000
    max_index_file_bytes: int = 128 * 1024 * 1024
    max_index_document_chars: int = 500_000

    @field_validator("cors_origins", mode="before")
    @classmethod
    def parse_origins(cls, value: object) -> object:
        if isinstance(value, str):
            return [item.strip() for item in value.split(",") if item.strip()]
        return value

    def ensure_directories(self) -> None:
        self.data_dir.mkdir(parents=True, exist_ok=True)
        (self.data_dir / "artifacts").mkdir(parents=True, exist_ok=True)
        (self.data_dir / "secrets").mkdir(parents=True, exist_ok=True)
        # import_root bilinçli olarak oluşturulmaz. Docker'da salt-okunur bağın
        # gerçekten mevcut olup olmadığı API üzerinden açıkça raporlanır.


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    settings = Settings()
    settings.ensure_directories()
    return settings
