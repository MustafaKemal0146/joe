from __future__ import annotations

from urllib.parse import quote_plus


def _search_url(engine: str, query: str) -> str:
    encoded = quote_plus(query)
    if engine == "Google":
        return f"https://www.google.com/search?q={encoded}"
    if engine == "Bing":
        return f"https://www.bing.com/search?q={encoded}"
    return f"https://duckduckgo.com/?q={encoded}"


def build_manual_plan(query: str, query_type: str) -> dict[str, object]:
    clean = " ".join(query.strip().split())
    if query_type == "username":
        variants = [
            clean,
            f'"{clean}"',
            f'"{clean}" profile',
            f'"{clean}" site:github.com OR site:gitlab.com',
            f'"{clean}" site:reddit.com OR site:medium.com',
        ]
        direct = [
            {"label": "GitHub profili", "url": f"https://github.com/{quote_plus(clean)}"},
            {"label": "Reddit profili", "url": f"https://www.reddit.com/user/{quote_plus(clean)}"},
            {"label": "Keybase profili", "url": f"https://keybase.io/{quote_plus(clean)}"},
        ]
    else:
        variants = [
            f'"{clean}"',
            f'"{clean}" site:linkedin.com/in',
            f'"{clean}" site:github.com',
            f'"{clean}" filetype:pdf',
            f'"{clean}" haber OR röportaj OR özgeçmiş',
        ]
        direct = []

    searches = []
    for variant in variants:
        for engine in ("Google", "Bing", "DuckDuckGo"):
            searches.append(
                {
                    "engine": engine,
                    "query": variant,
                    "url": _search_url(engine, variant),
                    "classification": "araştırma_adayı",
                }
            )
    return {
        "query": clean,
        "query_type": query_type,
        "searches": searches,
        "direct_profile_leads": direct,
        "notice": "Bu bağlantılar kanıt değil, analistin doğrulaması gereken araştırma adaylarıdır.",
    }

