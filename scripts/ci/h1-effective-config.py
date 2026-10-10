#!/usr/bin/env python3
"""Write an allowlisted, secret-free snapshot of the running H1 API process."""

import json
import os
from pathlib import Path
import re
import sys


DEFAULTS = {
    "ESHU_STATUS_SUMMARY_READ_ENABLED": "false",
    "ESHU_STATUS_SUMMARY_STALE_AFTER": "33s",
    "ESHU_POSTGRES_MAX_OPEN_CONNS": "30",
    "ESHU_POSTGRES_MAX_IDLE_CONNS": "10",
    "ESHU_POSTGRES_READ_MAX_OPEN_CONNS": "derived",
    "ESHU_POSTGRES_READ_MAX_IDLE_CONNS": "derived",
    "ESHU_POSTGRES_CONN_MAX_LIFETIME": "30m",
    "ESHU_POSTGRES_CONN_MAX_IDLE_TIME": "10m",
    "ESHU_POSTGRES_PING_TIMEOUT": "10s",
    "ESHU_GRAPH_BACKEND": "nornicdb",
    "GOMAXPROCS": "runtime-derived",
    "GOGC": "runtime-default",
}


if len(sys.argv) != 3:
    raise SystemExit("usage: h1-effective-config.py PID OUTPUT")
raw = Path(f"/proc/{int(sys.argv[1])}/environ").read_bytes()
values = dict(entry.decode("utf-8", "replace").split("=", 1)
              for entry in raw.split(b"\0") if b"=" in entry)
settings = {}
for key, fallback in DEFAULTS.items():
    value = values.get(key, fallback)
    if key.endswith("ENABLED"):
        safe = value.lower() in {"true", "false", "1", "0"}
    elif key == "ESHU_GRAPH_BACKEND":
        safe = value in {"neo4j", "nornicdb"}
    elif key in {"GOMAXPROCS", "GOGC"} or "CONNS" in key:
        safe = value == fallback or bool(re.fullmatch(r"[0-9]+|off", value))
    else:
        safe = bool(re.fullmatch(r"[0-9]+(?:ns|us|ms|s|m|h)", value))
    settings[key] = value if safe else "invalid-or-redacted"
writer = values.get("ESHU_POSTGRES_DSN")
reader = values.get("ESHU_POSTGRES_READ_DSN")
settings["ESHU_POSTGRES_READ_DSN_PRESENT"] = reader is not None
settings["ESHU_POSTGRES_READ_DSN_EQUALS_WRITER"] = reader == writer if reader else None
settings["ESHU_POSTGRES_READ_MEMBERS_PRESENT"] = bool(values.get("ESHU_POSTGRES_READ_MEMBERS"))
settings["OTLP_EXPORTER_LOCAL"] = values.get("OTEL_EXPORTER_OTLP_ENDPOINT") == "http://127.0.0.1:14317"
output = Path(sys.argv[2])
output.write_text(json.dumps({"source": "running_api_proc_environ", "settings": settings},
                             sort_keys=True, indent=2) + "\n", encoding="utf-8")
os.chmod(output, 0o600)
