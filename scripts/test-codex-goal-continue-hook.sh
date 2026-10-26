#!/usr/bin/env bash
# Codex Stop hook contract: active goals continue across ordinary and error stops.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
hook="${repo_root}/.codex/hooks/goal-continue.sh"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
mkdir -p "${work}/home/.claude" "${work}/tmp" "${work}/project/.claude"
export HOME="${work}/home" TMPDIR="${work}/tmp"
unset CLAUDE_GOAL_FILE CLAUDE_GOAL_OFF CLAUDE_GOAL_CONSENT CLAUDE_GOAL_MAX_NUDGES

goal="${work}/project/.claude/active-goal.codex-test"
sid="codex-test"
passed=0
failed=0

ok() { printf 'ok - %s\n' "$1"; passed=$((passed + 1)); }
no() { printf 'not ok - %s\n' "$1" >&2; failed=$((failed + 1)); }
payload() {
  python3 - "$1" "$2" "$3" <<'PY'
import json
import sys

print(json.dumps({
    "cwd": sys.argv[1],
    "hook_event_name": "Stop",
    "session_id": sys.argv[2],
    "turn_id": "turn-1",
    "stop_hook_active": len(sys.argv) > 4 and sys.argv[4] == "true",
    "last_assistant_message": sys.argv[3],
}))
PY
}
run() { printf '%s' "$1" | bash "${hook}" 2>/dev/null; }
is_blocked() { printf '%s' "$1" | rg '"decision"[[:space:]]*:[[:space:]]*"block"' >/dev/null; }

if [[ ! -x "${hook}" ]]; then no 'Codex goal Stop hook is executable'; fi

rm -f "${goal}"
out="$(run "$(payload "${work}/project" "${sid}" '')")"
if [[ -z "${out}" ]]; then ok 'no active goal allows the stop'; else no 'no active goal allows the stop'; fi

printf 'Finish the requested Eshu goal.\n' >"${goal}"
out="$(run "$(payload "${work}/project" "${sid}" 'Selected model is at capacity. Please try a different model.')")"
if is_blocked "${out}" && printf '%s' "${out}" | rg -q 'Finish the requested Eshu goal'; then
  ok 'capacity error with an active goal requests continuation'
else
  no 'capacity error with an active goal requests continuation'
fi
if printf '%s' "${out}" | rg -qi 'at capacity'; then
  ok 'continuation includes the model capacity error'
else
  no 'continuation includes the model capacity error'
fi

out="$(run "$(payload "${work}/project" "${sid}" 'Security policy denied the attempted operation.')")"
if is_blocked "${out}"; then ok 'security error with an active goal requests continuation'; else no 'security error with an active goal requests continuation'; fi
if printf '%s' "${out}" | rg -qi 'security policy denied'; then
  ok 'continuation includes the security error'
else
  no 'continuation includes the security error'
fi

out="$(run "$(payload "${work}/project" "${sid}" 'A tool security error occurred.' true)")"
if is_blocked "${out}"; then
  ok 'a Stop already continued once still enforces the active goal'
else
  no 'a Stop already continued once still enforces the active goal'
fi

printf 'DONE\n' >"${goal}"
out="$(run "$(payload "${work}/project" "${sid}" '')")"
if ! is_blocked "${out}"; then ok 'DONE allows the stop'; else no 'DONE allows the stop'; fi

# Stop hook registration is part of the behavior: an unwired hook is inert.
if python3 - "${repo_root}/.codex/hooks.json" <<'PY'
import json
import sys

hooks = json.load(open(sys.argv[1], encoding="utf-8")).get("hooks", {})
registered = any(
    group.get("matcher") is None
    and any(".codex/hooks/goal-continue.sh" in hook.get("command", "")
            for hook in group.get("hooks", []))
    for group in hooks.get("Stop", [])
)
if not registered:
    raise SystemExit(1)
PY
then
  ok 'Codex Stop event is wired to the continuation hook'
else
  no 'Codex Stop event is wired to the continuation hook'
fi

printf '\nCodex goal continuation hook: %s passed, %s failed\n' "${passed}" "${failed}"
[[ "${failed}" -eq 0 ]]
