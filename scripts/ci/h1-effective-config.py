#!/usr/bin/env python3
"""Write a secret-free snapshot of the running H1 API process and defaults."""

import json
import os
from pathlib import Path
import re
import sys


# These literal defaults are from envregistry/entries.go at source_commit.
CODE_DEFAULTS = {
    "ESHU_STATUS_SUMMARY_READ_ENABLED": "false",
    "ESHU_STATUS_SUMMARY_STALE_AFTER": "33s",
    "ESHU_POSTGRES_MAX_OPEN_CONNS": "30",
    "ESHU_POSTGRES_MAX_IDLE_CONNS": "10",
    "ESHU_POSTGRES_CONN_MAX_LIFETIME": "30m",
    "ESHU_POSTGRES_CONN_MAX_IDLE_TIME": "10m",
    "ESHU_POSTGRES_PING_TIMEOUT": "10s",
    "ESHU_GRAPH_BACKEND": "nornicdb",
}
DERIVED_UNSAMPLED = (
    "ESHU_POSTGRES_READ_MAX_OPEN_CONNS",
    "ESHU_POSTGRES_READ_MAX_IDLE_CONNS",
    "GOMAXPROCS",
    "GOGC",
)


def safe_value(key: str, value: str) -> str:
    """Keep only a documented nonsecret setting shape in the artifact."""
    if key.endswith("ENABLED"):
        safe = value.lower() in {"true", "false", "1", "0"}
    elif key == "ESHU_GRAPH_BACKEND":
        safe = value in {"neo4j", "nornicdb"}
    elif key in {"GOMAXPROCS", "GOGC"} or "CONNS" in key:
        safe = bool(re.fullmatch(r"[0-9]+|off", value))
    else:
        safe = bool(re.fullmatch(r"[0-9]+(?:ns|us|ms|s|m|h)", value))
    return value if safe else "invalid-or-redacted"


def main() -> None:
    """Label process values, source defaults, and unresolved runtime values."""
    if len(sys.argv) != 4:
        raise SystemExit("usage: h1-effective-config.py PID OUTPUT SOURCE_COMMIT")
    raw = Path(f"/proc/{int(sys.argv[1])}/environ").read_bytes()
    values = dict(entry.decode("utf-8", "replace").split("=", 1)
                  for entry in raw.split(b"\0") if b"=" in entry)
    settings = {}
    for key in (*CODE_DEFAULTS, *DERIVED_UNSAMPLED):
        if key in values:
            value = values[key]
            settings[key] = {
                "value": safe_value(key, value) if value else "",
                "origin": "process_env" if value else "process_env_empty_unresolved",
            }
        elif key in CODE_DEFAULTS:
            settings[key] = {"value": CODE_DEFAULTS[key], "origin": "code_default"}
        else:
            settings[key] = {"value": "unknown", "origin": "runtime_derived_unsampled"}
    writer = values.get("ESHU_POSTGRES_DSN")
    reader = values.get("ESHU_POSTGRES_READ_DSN")
    settings["ESHU_POSTGRES_READ_DSN_PRESENT"] = {
        "value": "ESHU_POSTGRES_READ_DSN" in values,
        "origin": "process_env_presence",
    }
    settings["ESHU_POSTGRES_READ_DSN_EQUALS_WRITER"] = {
        "value": reader == writer if reader else None,
        "origin": "process_env_comparison" if reader else "not_applicable",
    }
    settings["ESHU_POSTGRES_READ_MEMBERS_PRESENT"] = {
        "value": bool(values.get("ESHU_POSTGRES_READ_MEMBERS")),
        "origin": "process_env_presence",
    }
    settings["OTLP_EXPORTER_LOCAL"] = {
        "value": values.get("OTEL_EXPORTER_OTLP_ENDPOINT") == "http://127.0.0.1:14317",
        "origin": "process_env_comparison",
    }
    output = Path(sys.argv[2])
    output.write_text(json.dumps({
        "source": "running_api_proc_environ",
        "defaults_source": {"path": "go/internal/envregistry/entries.go",
                            "source_commit": sys.argv[3]},
        "settings": settings,
    }, sort_keys=True, indent=2) + "\n", encoding="utf-8")
    os.chmod(output, 0o600)


if __name__ == "__main__":
    main()
