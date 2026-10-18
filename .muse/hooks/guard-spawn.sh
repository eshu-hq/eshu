#!/usr/bin/env bash
# .muse/hooks/guard-spawn.sh -- Muse Code PreToolUse guard (subagent_spawn, workflow).
#
# Typo/override guard (not the launch mechanism): native subagent_spawn
# already applies each definition's model — proven live, arbiter-eshu ran
# muse-wonder-full-1.0 with no override. This guard only blocks unknown
# *-eshu role names and explicit wrong-tier model/effort overrides.
# Validation lives in goal-role-router.py
# (muse-dispatch mode, next to claude_dispatch) so one router serves both.
# Fail closed, like scripts/guard-codex-spawn.py: a guard that cannot run
# must not silently wave a wrong-tier arbiter through.
set -uo pipefail

# shellcheck source=.muse/hooks/lib/muse-root.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib/muse-root.sh"

root="$(muse_repo_root)" || { echo 'Eshu dispatch guard cannot resolve the repo root' >&2; exit 2; }
[[ -f "${root}/scripts/goal-role-router.py" ]] || { echo 'Eshu dispatch guard is missing (scripts/goal-role-router.py)' >&2; exit 2; }
command -v python3 >/dev/null 2>&1 || { echo 'Eshu dispatch guard needs python3' >&2; exit 2; }

exec python3 "${root}/scripts/goal-role-router.py" muse-dispatch
