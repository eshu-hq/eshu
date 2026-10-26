#!/usr/bin/env bash
# Adapt Codex Stop events to the shared active-goal continuation policy.
# Only fixed diagnostic categories are passed as recovery context.
set -uo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || exit 0
core_hook="${repo_root}/.claude/hooks/goal-continue.sh"
[ -x "${core_hook}" ] || exit 0
command -v python3 >/dev/null 2>&1 || exit 0

payload="$(cat)"
[ -n "${payload}" ] || exit 0

# Codex provides turn_id and stop_hook_active, not prompt_id. Keep one stable
# shared-hook budget key across Stop continuations; a new owner turn resets it.
# Never copy assistant text into the continuation prompt: it can quote hostile
# tool or repository content. Pass only a fixed diagnostic category.
normalized_payload=""
stop_category=""
{
	IFS= read -r normalized_payload
	IFS= read -r stop_category
} < <(printf '%s' "${payload}" | python3 -c '
"""Normalize a Codex Stop payload for the shared active-goal hook."""
import json
import os
import re
import tempfile
import sys

try:
    payload = json.load(sys.stdin)
except Exception:
    raise SystemExit(0)

session_id = payload.get("session_id")
turn_id = payload.get("turn_id")
if not isinstance(session_id, str):
    session_id = ""
if not isinstance(turn_id, str):
    turn_id = ""
session_id = session_id.replace("\r", " ").replace("\n", " ")
turn_id = turn_id.replace("\r", " ").replace("\n", " ")
sid_safe = re.sub(r"[^A-Za-z0-9._-]", "-", session_id)
state_dir = os.environ.get("TMPDIR") or "/tmp"
state_path = os.path.join(state_dir, "codex-goal-prompt-" + (sid_safe or "unknown"))
continued = payload.get("stop_hook_active") is True
prompt_id = ""
if continued:
    try:
        with open(state_path, encoding="utf-8") as state_file:
            prompt_id = state_file.read().strip()
    except OSError:
        pass
if not prompt_id:
    prompt_id = turn_id or session_id or "-"
    if session_id:
        temporary = ""
        try:
            fd, temporary = tempfile.mkstemp(prefix=".codex-goal-prompt-", dir=state_dir)
            with os.fdopen(fd, "w", encoding="utf-8") as state_file:
                state_file.write(prompt_id)
            os.replace(temporary, state_path)
        except OSError:
            if temporary:
                try:
                    os.unlink(temporary)
                except OSError:
                    pass
            prompt_id = session_id
payload["prompt_id"] = prompt_id

message = payload.get("last_assistant_message")
category = ""
if isinstance(message, str):
    if re.search(r"capacity", message, re.IGNORECASE):
        category = "model-capacity"
    elif re.search(r"security|blocked|denied", message, re.IGNORECASE):
        category = "security-policy"
    elif re.search(r"error|failed", message, re.IGNORECASE):
        category = "reported-error"
print(json.dumps(payload, separators=(",", ":")))
print(category)
' 2>/dev/null)
[ -n "${normalized_payload}" ] || exit 0

ESHU_GOAL_ISSUE_FAIL_CLOSED_UNCHECKED=1 \
CLAUDE_GOAL_ISSUE_CALL_TIMEOUT=0.75 \
CLAUDE_GOAL_ISSUE_TOTAL_TIMEOUT=1.0 \
GOAL_CONTINUE_CODEX_EXHAUSTION_SYSTEM_MESSAGE=1 \
GOAL_CONTINUE_STOP_CATEGORY="${stop_category}" \
python3 -c '
"""Bound the shared hook to Codex Stop runtime and fail closed on timeout."""
import json
import os
import signal
import subprocess
import sys

process = subprocess.Popen(
    ["bash", sys.argv[1]],
    stdin=subprocess.PIPE,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    text=True,
    start_new_session=True,
)
try:
    stdout, stderr = process.communicate(sys.stdin.read(), timeout=2.0)
except subprocess.TimeoutExpired:
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    process.communicate()
    print(json.dumps({
        "decision": "block",
        "reason": "The active-goal Stop check exceeded its time budget; keep the goal active and retry.",
    }))
    raise SystemExit(0)
sys.stdout.write(stdout)
sys.stderr.write(stderr)
raise SystemExit(process.returncode)
' "${core_hook}" <<<"${normalized_payload}"
