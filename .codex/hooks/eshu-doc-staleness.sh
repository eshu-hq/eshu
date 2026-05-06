#!/usr/bin/env bash
# .codex/hooks/eshu-doc-staleness.sh
#
# Codex PostToolUse hook (matcher: ^apply_patch$).
# Reads Codex's tool payload from stdin. Codex's apply_patch shape varies by
# version, and the patch body lists multiple files at once. Rather than parse
# the patch, we run scripts/check-docs-stale.sh --all to produce a fresh
# snapshot of drift across go/. The script is fast (stat-based) and the
# --all mode truncates state so duplicate appends don't accumulate.

set -u
set -o pipefail

# Drain stdin so Codex doesn't block on a closed pipe; we don't need the body.
cat >/dev/null

repo_root="${CODEX_PROJECT_DIR:-}"
[ -z "$repo_root" ] && repo_root="$(git rev-parse --show-toplevel 2>/dev/null \
  || (cd "$(dirname "$0")/../.." && pwd))"

ESHU_DOC_KEEPER_TOOL=codex \
  "$repo_root/scripts/check-docs-stale.sh" --all

exit 0
