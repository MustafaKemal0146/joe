from app.security.vault import SecretVault


def test_local_vault_encrypts_without_returning_plaintext(tmp_path) -> None:
    vault = SecretVault(tmp_path / "master.key")
    encrypted = vault.encrypt("çok-gizli-anahtar")

    assert encrypted
    assert "çok-gizli-anahtar" not in encrypted
    assert vault.decrypt(encrypted) == "çok-gizli-anahtar"
