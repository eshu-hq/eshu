"""Decorator patterns for parser testing."""

import functools
import time
from typing import Callable, Any


def timer(func: Callable) -> Callable:
    """Measure function execution time."""
    @functools.wraps(func)
    def wrapper(*args: Any, **kwargs: Any) -> Any:
        start = time.monotonic()
        result = func(*args, **kwargs)
        elapsed = time.monotonic() - start
        print(f"{func.__name__} took {elapsed:.3f}s")
        return result
    return wrapper


def retry(max_attempts: int = 3):
    """Retry decorator with configurable attempts."""
    def decorator(func: Callable) -> Callable:
        @functools.wraps(func)
        def wrapper(*args: Any, **kwargs: Any) -> Any:
            for attempt in range(max_attempts):
                try:
                    return func(*args, **kwargs)
                except Exception:
                    if attempt == max_attempts - 1:
                        raise
        return wrapper
    return decorator


class Service:
    """Service class with decorated methods."""

    @staticmethod
    def create_id() -> str:
        return "svc-001"

    @classmethod
    def from_config(cls, config: dict) -> "Service":
        return cls()

    @property
    def name(self) -> str:
        return "default-service"

    @timer
    def process(self, data: list) -> list:
        return sorted(data)

    @retry(max_attempts=5)
    @timer
    def fetch_remote(self, url: str) -> str:
        """Stacked decorators example."""
        return f"data from {url}"
