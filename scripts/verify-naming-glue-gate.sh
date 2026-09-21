#!/usr/bin/env bash
# Glued-compound directory-name gate (naming.md rule 3), model-judged --
# see go/cmd/naming-glue-gate/doc.go for the full contract.
#
# Modes:
#   --staged        pre-commit: compares HEAD against the current staged
#                    index (via `git write-tree`, no commit needed) and runs
#                    -blocking=true -- a finding fails the commit.
#   --range <base>  CI: compares <base> against HEAD and runs
#                    -blocking=false -- findings are reported but this
#                    script always exits 0, since this is the first
#                    non-deterministic gate in the repo and stays advisory
#                    at merge time until its false-positive rate is measured
#                    against real PRs (see go/cmd/naming-glue-gate/AGENTS.md).
#
# DEEPSEEK_API_KEY missing or unreachable fails OPEN in both modes (the
# underlying tool's own behavior, not re-implemented here): an
# infrastructure problem is not a naming violation.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

mode="${1:-}"
case "$mode" in
  --staged)
    base_ref="HEAD"
    head_ref="$(git write-tree)"
    blocking="true"
    ;;
  --range)
    base_ref="${2:?usage: $(basename "$0") --range <base>}"
    head_ref="HEAD"
    blocking="false"
    ;;
  *)
    echo "usage: $(basename "$0") [--staged | --range <base>]" >&2
    exit 2
    ;;
esac

build_dir="$(mktemp -d)"
trap 'rm -rf "$build_dir"' EXIT
(cd go && go build -o "$build_dir/naming-glue-gate" ./cmd/naming-glue-gate)

"$build_dir/naming-glue-gate" \
  -repo-root "$repo_root" \
  -base-ref "$base_ref" \
  -head-ref "$head_ref" \
  -dirs go/internal,go/cmd,go/pkg \
  -blocking="$blocking"
