from __future__ import annotations


class OsintConnectorError(RuntimeError):
    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


def validate_username(username: str) -> str:
    clean = username.strip().lstrip("@")
    if not clean or len(clean) > 100 or any(char.isspace() for char in clean):
        raise OsintConnectorError(
            "invalid_username",
            "Kullanıcı adı boşluk içermemeli ve 100 karakteri geçmemeli.",
        )
    return clean
