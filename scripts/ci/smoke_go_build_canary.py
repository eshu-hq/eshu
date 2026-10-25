#!/usr/bin/env python3
"""Fail one explicitly targeted smoke Go build after the real build succeeds."""

from __future__ import annotations

import json
import os
import pathlib
import re
import sys
from datetime import datetime, timezone


CONFIG_FIELDS = {
    "repository", "pr", "head_sha", "attempt", "mode", "gate", "expires_at"
}
HEAD_SHA = re.compile(r"[0-9a-f]{40}\Z")
UTC_EXPIRY = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z\Z")


def main() -> int:
    """Return a fixed failure only for the matching selected smoke build."""
    if os.environ.get("GITHUB_EVENT_NAME") != "pull_request":
        return 0
    if os.environ.get("CI_PLAN_MODE") != "smoke":
        return 0

    try:
        gates = json.loads(os.environ.get("CI_PLAN_GATES", ""))
    except json.JSONDecodeError:
        return 0
    if not isinstance(gates, list) or "go-build" not in gates:
        return 0

    raw_config = os.environ.get("CI_SMOKE_GO_BUILD_FAIL_CANARY", "")
    if not raw_config:
        return 0
    try:
        config = json.loads(raw_config)
    except json.JSONDecodeError:
        return 0
    if not isinstance(config, dict) or set(config) != CONFIG_FIELDS:
        return 0
    if config["mode"] != "smoke" or config["gate"] != "go-build":
        return 0
    if not isinstance(config["repository"], str) or not config["repository"]:
        return 0
    if type(config["pr"]) is not int or config["pr"] < 1:
        return 0
    if type(config["attempt"]) is not int or config["attempt"] < 1:
        return 0
    if not isinstance(config["head_sha"], str) or not HEAD_SHA.fullmatch(config["head_sha"]):
        return 0
    expiry = config["expires_at"]
    if not isinstance(expiry, str) or not UTC_EXPIRY.fullmatch(expiry):
        return 0
    try:
        deadline = datetime.strptime(expiry, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=timezone.utc)
    except ValueError:
        return 0
    if datetime.now(timezone.utc) >= deadline:
        return 0

    attempt = os.environ.get("GITHUB_RUN_ATTEMPT", "")
    if not attempt.isdecimal() or int(attempt) != config["attempt"]:
        return 0
    event_path = os.environ.get("GITHUB_EVENT_PATH", "")
    if not event_path:
        return 0
    try:
        event = json.loads(pathlib.Path(event_path).read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return 0
    if not isinstance(event, dict):
        return 0
    repository = event.get("repository")
    pull_request = event.get("pull_request")
    if not isinstance(repository, dict) or not isinstance(pull_request, dict):
        return 0
    head = pull_request.get("head")
    if not isinstance(head, dict):
        return 0
    if (
        repository.get("full_name") != config["repository"]
        or event.get("number") != config["pr"]
        or pull_request.get("number") != config["pr"]
        or head.get("sha") != config["head_sha"]
    ):
        return 0

    print("deliberate smoke go-build failure: exact canary target matched", flush=True)
    return 1


if __name__ == "__main__":
    sys.exit(main())
