#!/usr/bin/env bash
# .muse/hooks/on-compact.sh -- Muse Code SessionStart + PreCompact hook.
#
# The Claude sibling is registered with the `compact|resume` matcher so it
# only runs when skills were actually discarded. Muse has no documented
# matcher values for that, but its payload carries the equivalent signal:
# `SessionStart` arrives with `source` (`startup` observed live on a fresh
# session), and compaction has its own `PreCompact` event. So this wrapper
# gates before delegating to .claude/hooks/on-compact.sh:
#
#   PreCompact                    -> delegate (compaction discards skills)
#   SessionStart, source=startup  -> silent exit 0 (fresh session: emitting
#                                    "CONTEXT WAS COMPACTED" would be a lie,
#                                    and clearing markers is pointless)
#   SessionStart, any other or    -> delegate (resume or an unknown future
#   missing source                  source: stale markers are the worse risk)
#   anything else / unreadable    -> silent exit 0
#
# The resume-source value is inferred, not yet observed live; the suite pins
# the fresh-startup silence, which is the case that must never be wrong.
set -uo pipefail

# shellcheck source=.muse/hooks/lib/muse-root.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib/muse-root.sh"

payload="$(cat)"
command -v python3 >/dev/null 2>&1 || exit 0
[ -n "${payload}" ] || exit 0

route="$(printf '%s' "${payload}" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    print("silent")
    raise SystemExit(0)
event = d.get("hook_event_name") or ""
if event == "PreCompact":
    print("delegate")
elif event == "SessionStart":
    print("silent" if (d.get("source") or "") == "startup" else "delegate")
else:
    print("silent")
' 2>/dev/null)" || route="silent"

[ "${route}" = "delegate" ] || exit 0
printf '%s' "${payload}" | exec "$(muse_repo_root)/.claude/hooks/on-compact.sh"
