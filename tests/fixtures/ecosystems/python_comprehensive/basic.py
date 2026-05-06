"""Basic Python constructs for parser testing."""

import os
import sys
from pathlib import Path
from typing import Optional, List

# Module-level variables
MAX_RETRIES = 3
DEFAULT_TIMEOUT = 30.0


def greet(name: str) -> str:
    """Greet a person by name."""
    return f"Hello, {name}!"


def calculate_sum(numbers: List[int]) -> int:
    """Calculate the sum of a list of numbers."""
    total = 0
    for n in numbers:
        total += n
    return total


class Config:
    """Application configuration container."""

    def __init__(self, env: str = "development"):
        self.env = env
        self.debug = env != "production"
        self.base_path = Path("/app")

    def get_setting(self, key: str, default: Optional[str] = None) -> Optional[str]:
        """Retrieve a configuration setting."""
        return os.environ.get(key, default)

    def is_production(self) -> bool:
        return self.env == "production"


class Application:
    """Main application class."""

    def __init__(self, config: Config):
        self.config = config
        self.running = False

    def start(self) -> None:
        """Start the application."""
        self.running = True
        greeting = greet("World")
        print(greeting)

    def stop(self) -> None:
        self.running = False
