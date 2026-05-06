"""Async patterns for parser testing."""

import asyncio
from typing import AsyncIterator


async def fetch_data(url: str) -> dict:
    """Fetch data from a URL asynchronously."""
    await asyncio.sleep(0.1)
    return {"url": url, "status": 200}


async def process_batch(urls: list[str]) -> list[dict]:
    """Process a batch of URLs concurrently."""
    tasks = [fetch_data(url) for url in urls]
    return await asyncio.gather(*tasks)


async def stream_events() -> AsyncIterator[str]:
    """Async generator that yields events."""
    for i in range(10):
        await asyncio.sleep(0.01)
        yield f"event-{i}"


class AsyncWorker:
    """Worker with async methods."""

    def __init__(self, name: str):
        self.name = name
        self._running = False

    async def start(self) -> None:
        self._running = True
        async for event in stream_events():
            await self.handle(event)

    async def handle(self, event: str) -> None:
        result = await fetch_data(event)
        print(f"{self.name} handled: {result}")

    async def shutdown(self) -> None:
        self._running = False
