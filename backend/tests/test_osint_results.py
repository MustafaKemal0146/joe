from app.osint.service import OsintService, automatic_connector_ids


def test_username_uses_every_registered_compatible_connector() -> None:
    assert automatic_connector_ids("username") == [
        "sherlock",
        "maigret",
        "whatsmyname",
        "public_profiles",
    ]
    assert automatic_connector_ids("full_name") == ["public_profiles"]


def test_duplicate_profile_urls_keep_all_provenance() -> None:
    findings = [
        {
            "connector": "sherlock",
            "sources": ["sherlock"],
            "site": "GitHub",
            "profile_url": "https://github.com/sherlock-project/",
        },
        {
            "connector": "maigret",
            "sources": ["maigret"],
            "site": "GitHub",
            "profile_url": "https://github.com/sherlock-project",
        },
    ]

    result = OsintService._deduplicate(findings)

    assert len(result) == 1
    assert result[0]["sources"] == ["sherlock", "maigret"]
