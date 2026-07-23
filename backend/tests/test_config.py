from app.config import Settings


def test_cors_origins_accept_compose_comma_list(monkeypatch):
    monkeypatch.setenv(
        "JOE_CORS_ORIGINS",
        "http://localhost:4177,http://127.0.0.1:4177",
    )

    settings = Settings(_env_file=None)

    assert settings.cors_origins == [
        "http://localhost:4177",
        "http://127.0.0.1:4177",
    ]
