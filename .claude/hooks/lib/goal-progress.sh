#!/usr/bin/env bash
# Sourced by lib/goal-issues.sh, and through it by goal-continue.sh -- not run on
# its own.
#
# The one transcript progress signal: tool calls appended to a transcript since a
# byte offset. goal-continue.sh's no-progress budget and the issue-ledger DONE
# refusal's escalation both ask "did the agent do any work since I last stopped
# it?", and they must ask it the same way, so the scan lives here once.
#
# Prints two lines: the tool_use count in the bytes past OFFSET, then the
# transcript size to use as the next offset. A transcript that is missing or
# unreadable reports 0 progress and leaves the offset alone: a progress signal
# that failed OPEN would remove the bound it feeds. Muse exec mode has no
# transcript, so that is also its answer.
#
# Read from a stored offset rather than re-reading the file; transcripts run to
# 8MB.
gp_tool_use_since() { # transcript offset
	local transcript="${1:-}" offset="${2:-0}"
	case "${offset}" in '' | *[!0-9]*) offset=0 ;; esac
	if [ -z "${transcript}" ] || [ "${transcript}" = "-" ] || [ ! -f "${transcript}" ]; then
		printf '0\n%s\n' "${offset}"
		return 0
	fi
	TX="${transcript}" OFF="${offset}" python3 -c '
import os, re, sys
path = os.environ["TX"]
try:
    off = int(os.environ.get("OFF") or 0)
    size = os.path.getsize(path)
    if off > size:  # transcript replaced or truncated: rescan from the start
        off = 0
    with open(path, "rb") as fh:
        fh.seek(off)
        tail = fh.read()
    # Tolerate whitespace from the transcript writer. Pinning the exact
    # compact spelling meant one space after a colon silently restored the
    # pre-change behaviour: real work invisible, the agent released by
    # exhaustion, the mirror still green.
    print(len(re.findall(rb"\"type\"\s*:\s*\"tool_use\"", tail)))
    print(size)
except Exception:
    print(0); print(os.environ.get("OFF") or 0)
' 2>/dev/null
}
