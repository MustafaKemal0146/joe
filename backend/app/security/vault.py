from __future__ import annotations

import os
from pathlib import Path

from cryptography.fernet import Fernet, InvalidToken

from ..config import get_settings


class SecretVaultError(RuntimeError):
    pass


class SecretVault:
    """Local secret encryption backed by a persistent key file.

    The key never enters the database and the API never returns decrypted values.
    """

    def __init__(self, key_path: Path | None = None) -> None:
        settings = get_settings()
        self.key_path = key_path or settings.data_dir / "secrets" / "master.key"
        self.key_path.parent.mkdir(parents=True, exist_ok=True)
        self._fernet = Fernet(self._load_or_create_key())

    def _load_or_create_key(self) -> bytes:
        env_key = os.getenv("JOE_MASTER_KEY")
        if env_key:
            return env_key.encode("ascii")
        if self.key_path.exists():
            return self.key_path.read_bytes().strip()
        key = Fernet.generate_key()
        try:
            with self.key_path.open("xb") as handle:
                handle.write(key)
        except FileExistsError:
            return self.key_path.read_bytes().strip()
        try:
            os.chmod(self.key_path, 0o600)
        except OSError:
            pass
        return key

    def encrypt(self, value: str | None) -> str | None:
        if not value:
            return None
        return self._fernet.encrypt(value.encode("utf-8")).decode("ascii")

    def decrypt(self, token: str | None) -> str | None:
        if not token:
            return None
        try:
            return self._fernet.decrypt(token.encode("ascii")).decode("utf-8")
        except InvalidToken as exc:
            raise SecretVaultError("Saklanan sağlayıcı anahtarı çözülemedi.") from exc

