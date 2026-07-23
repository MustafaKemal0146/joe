"""SSE olay akışı — canlı OSINT ve Analiz event'leri."""

from __future__ import annotations

import asyncio
import json
from datetime import UTC, datetime

from sqlalchemy import select

from ..db import SessionLocal
from ..models import OsintRunEvent


class EventStream:
    def __init__(self) -> None:
        self._subscribers: dict[str, list[asyncio.Queue]] = {}

    async def subscribe(self, run_id: str) -> asyncio.Queue:
        queue: asyncio.Queue = asyncio.Queue()
        self._subscribers.setdefault(run_id, []).append(queue)
        return queue

    def unsubscribe(self, run_id: str, queue: asyncio.Queue) -> None:
        queues = self._subscribers.get(run_id)
        if queues:
            try:
                queues.remove(queue)
            except ValueError:
                pass

    async def publish(self, run_id: str, event_type: str, payload: dict) -> None:
        data = json.dumps({
            "event_type": event_type,
            "payload": payload,
            "timestamp": datetime.now(UTC).isoformat(),
        })
        for q in self._subscribers.get(run_id, []):
            await q.put(data)

    async def replay_since(self, run_id: str, since: int) -> list[dict]:
        with SessionLocal() as db:
            events = db.scalars(
                select(OsintRunEvent)
                .where(
                    OsintRunEvent.osint_run_id == run_id,
                    OsintRunEvent.sequence > since,
                )
                .order_by(OsintRunEvent.sequence)
            ).all()
            return [
                {
                    "event_type": evt.event_type,
                    "payload": evt.payload,
                    "sequence": evt.sequence,
                }
                for evt in events
            ]


event_stream = EventStream()
