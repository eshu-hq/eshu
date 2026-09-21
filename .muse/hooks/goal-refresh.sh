#!/usr/bin/env bash
# .muse/hooks/goal-refresh.sh -- Muse Code UserPromptSubmit hook (no matcher).
#
# Envelope contract, recorded live from `muse exec` (see
# docs/internal/agent-hooks-muse.md): the Muse payload carries `session_id`,
# `cwd`, and `prompt` under the same names as Claude's, so no translation is
# needed. This wrapper exists to resolve the sibling by wrapper location
# (invocation cwd may be any subdirectory) and as the seam if the Muse
# envelope ever diverges. Delegates to .claude/hooks/goal-refresh.sh.
set -uo pipefail

# shellcheck source=.muse/hooks/lib/muse-root.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib/muse-root.sh"

exec "$(muse_repo_root)/.claude/hooks/goal-refresh.sh"
