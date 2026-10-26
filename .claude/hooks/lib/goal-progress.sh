#!/usr/bin/env bash
# Sourced by lib/goal-issues.sh, and through it by goal-continue.sh -- not run on
# its own.
#
# The one transcript progress signal counts tool calls appended to a transcript
# since a byte offset. goal-continue.sh's no-progress budget and the issue-ledger DONE
# refusal's escalation both ask "did the agent do any work since I last stopped
# it?", and they must ask it the same way, so the scan lives here once.
#
# Prints two lines: the recognized tool-call count in the bytes past OFFSET,
# then the transcript size to use as the next offset. A transcript that is
# missing or unreadable reports 0 progress and leaves the offset alone: a signal
# that failed OPEN would remove the bound it feeds. Muse exec mode has no
# transcript, so that is also its answer.
#
# Read from a stored offset rather than re-reading the file; transcripts run to
# 8MB.
gp_tool_calls_since() { # transcript offset
	local transcript="${1:-}" offset="${2:-0}"
	case "${offset}" in '' | *[!0-9]*) offset=0 ;; esac
	if [ -z "${transcript}" ] || [ "${transcript}" = "-" ] || [ ! -f "${transcript}" ]; then
		printf '0\n%s\n' "${offset}"
		return 0
	fi
	TX="${transcript}" OFF="${offset}" python3 -c '
"""Count tool calls in appended Claude and Codex JSONL records."""
import json
import os
import sys
from typing import Any

path = os.environ["TX"]
try:
    off = int(os.environ.get("OFF") or 0)
    size = os.path.getsize(path)
    if off > size:  # transcript replaced or truncated: rescan from the start
        off = 0
    with open(path, "rb") as fh:
        fh.seek(off)
        tail = fh.read()
    complete_tail = tail
    next_offset = size
    if tail and not tail.endswith(b"\n"):
        last_newline = tail.rfind(b"\n")
        final_record = tail[last_newline + 1:]
        try:
            json.loads(final_record)
        except (ValueError, UnicodeDecodeError):
            complete_tail = tail[:last_newline + 1] if last_newline >= 0 else b""
            next_offset = off + max(last_newline + 1, 0)

    def count_tool_calls(record: Any) -> int:
        """Count harness tool events without matching nested tool-like data."""
        if not isinstance(record, dict):
            return 0
        if record.get("type") == "assistant":
            message = record.get("message")
            content = message.get("content") if isinstance(message, dict) else None
            if isinstance(content, list):
                return sum(
                    isinstance(item, dict) and item.get("type") == "tool_use"
                    for item in content
                )
        if record.get("type") == "response_item":
            payload = record.get("payload")
            if isinstance(payload, dict) and payload.get("type") in {
                "function_call", "custom_tool_call"
            }:
                return 1
        return 0

    count = 0
    for line in complete_tail.splitlines():
        try:
            count += count_tool_calls(json.loads(line))
        except (ValueError, UnicodeDecodeError):
            continue
    print(count)
    print(next_offset)
except Exception:
    print(0)
    print(os.environ.get("OFF") or 0)
' 2>/dev/null
}
