#!/usr/bin/env python3
"""Copy an H1 API log while replacing known runtime credentials."""

import os
from pathlib import Path
import sys


SENSITIVE_NAMES = (
    "ESHU_API_KEY", "ESHU_POSTGRES_PASSWORD", "ESHU_POSTGRES_DSN",
    "ESHU_POSTGRES_READ_DSN", "ESHU_CONTENT_STORE_DSN", "NEO4J_PASSWORD",
    "ESHU_NEO4J_PASSWORD", "GATE_API_KEY", "GATE_POSTGRES_PASSWORD",
    "GATE_NEO4J_PASSWORD",
)


def redact(raw: str, environ: dict[str, str]) -> str:
    """Replace nonempty credential values before archival."""
    secrets = {environ.get(name, "") for name in SENSITIVE_NAMES}
    for secret in sorted((value for value in secrets if value), key=len, reverse=True):
        raw = raw.replace(secret, "[REDACTED]")
    return raw


def main() -> None:
    """Write only the sanitized log at the requested artifact path."""
    if len(sys.argv) != 3:
        raise SystemExit("usage: h1-redact-log.py INPUT OUTPUT")
    source, output = Path(sys.argv[1]), Path(sys.argv[2])
    output.write_text(redact(source.read_text(encoding="utf-8", errors="replace"),
                             dict(os.environ)), encoding="utf-8")
    os.chmod(output, 0o600)


if __name__ == "__main__":
    main()
