#!/usr/bin/env bash
# .muse/hooks/eshu-doc-staleness.sh -- Muse Code PostToolUse hook
# (write_file, edit_file).
#
# The Claude sibling drains stdin and delegates to
# scripts/check-docs-stale.sh, which is harness-neutral already, so no
# translation is needed. Delegates to .claude/hooks/eshu-doc-staleness.sh.
set -uo pipefail

# shellcheck source=.muse/hooks/lib/muse-root.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib/muse-root.sh"

exec "$(muse_repo_root)/.claude/hooks/eshu-doc-staleness.sh"
