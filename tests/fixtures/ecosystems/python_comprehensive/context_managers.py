"""Context manager patterns for parser testing."""

from contextlib import contextmanager, asynccontextmanager
from typing import Generator, AsyncGenerator


class FileHandler:
    """Context manager via __enter__/__exit__."""

    def __init__(self, filename: str, mode: str = "r"):
        self.filename = filename
        self.mode = mode
        self.file = None

    def __enter__(self):
        self.file = open(self.filename, self.mode)
        return self.file

    def __exit__(self, exc_type, exc_val, exc_tb):
        if self.file:
            self.file.close()
        return False


@contextmanager
def temp_directory(prefix: str = "tmp") -> Generator[str, None, None]:
    """Context manager via decorator."""
    import tempfile
    import shutil

    path = tempfile.mkdtemp(prefix=prefix)
    try:
        yield path
    finally:
        shutil.rmtree(path, ignore_errors=True)


@asynccontextmanager
async def async_connection(host: str) -> AsyncGenerator[dict, None]:
    """Async context manager."""
    conn = {"host": host, "connected": True}
    try:
        yield conn
    finally:
        conn["connected"] = False


def use_context_managers():
    """Function using with statements."""
    with FileHandler("test.txt", "w") as f:
        f.write("test")

    with temp_directory("myapp") as d:
        print(f"Using {d}")
