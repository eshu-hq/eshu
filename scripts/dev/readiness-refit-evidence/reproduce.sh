#!/usr/bin/env bash
# Re-derive the pooled readiness refit evidence (#9519) and compare it with the
# recorded outputs in testdata/readiness-refit-evidence/expected/. Exit 0 only
# if every output matches byte for byte and the refit reproduces the committed
# weights files.
#
# Usage: scripts/dev/readiness-refit-evidence/reproduce.sh [RUNS_DIR]
#   RUNS_DIR  directory holding one subdirectory per run ID (run.json,
#             jobs.json, shardN.log). Default: the archive
#             testdata/readiness-refit-evidence/inputs.tar.gz, unpacked into a
#             temporary directory.
# Environment:
#   EVAL_REF  commit to evaluate: its scripts/lib (parser, refresher, planner)
#             and its committed weights. Default 3c92eed021, the #9564 refit.
#
# It runs in a temporary detached worktree of EVAL_REF, because the refresher
# reads each source run's test inventory from git at that run's head SHA.
# It writes nothing in this checkout and removes the worktree on exit.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(git -C "$here" rev-parse --show-toplevel)"
data="$repo/testdata/readiness-refit-evidence"
eval_ref="${EVAL_REF:-3c92eed02156540d27574670db460b5a4180527d}"
work="$(mktemp -d "${TMPDIR:-/tmp}/refit-evidence.XXXXXX")"
tree="$work/eval"
out="$work/out"
# shellcheck disable=SC2329  # invoked through the EXIT trap
cleanup() {
  git -C "$repo" worktree remove --force "$tree" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

if [ $# -ge 1 ]; then
  runs="$(cd "$1" && pwd)"
else
  mkdir -p "$work/inputs"
  tar -xzf "$data/inputs.tar.gz" -C "$work/inputs"
  runs="$work/inputs/runs"
fi

pool=(40867705415 40878710102 40879134083 40885865002 40887740898 40888833062 40894770170 40899888098 40900152112)
older=(40672571941 40687668044 40753558584 40777558802 40780015081 40784467495 40784513987 40798562144
  40800579562 40812860062 40816391526 40818333400 40833905620 40847362740 40854348722 40859217483)
six_shard=(40913641758 40927154134 40916025838)
# Cancelled shard-1 jobs at the 30-minute cap, scored as lower bounds on the
# old six-shard shard-1 slice (run 40913641758): wall seconds from the jobs API
# for 40915510776 attempts 1 and 2, 40916025838 attempt 1, 40933609276 attempt 1.
censored_walls=(1822 1821 1819 1819)

dirs() { local id; for id in "$@"; do printf '%s\n' "$runs/$id"; done; }
mapfile -t pool_dirs < <(dirs "${pool[@]}")
mapfile -t older_dirs < <(dirs "${older[@]}")
mapfile -t six_dirs < <(dirs "${six_shard[@]}")
fit_dirs=("${pool_dirs[@]:0:7}")
holdout_dirs=("${pool_dirs[@]:7:2}")
samples() { local d; for d in "$@"; do printf -- '--sample\n%s=%s\n' "$(basename "$d")" "$d"; done; }
mapfile -t pool_samples < <(samples "${pool_dirs[@]}")
mapfile -t fit_samples < <(samples "${fit_dirs[@]}")
censored=()
for wall in "${censored_walls[@]}"; do censored+=("$runs/40913641758:1:$wall"); done
csv() { local IFS=,; printf '%s' "$*"; }

if ! git -C "$repo" worktree add --detach "$tree" "$eval_ref" >/dev/null 2>"$work/worktree.err"; then
  echo "reproduce.sh: cannot make a worktree of $eval_ref:" >&2
  cat "$work/worktree.err" >&2
  echo "It needs a checkout with full history: $eval_ref and each run's head SHA." >&2
  exit 1
fi
cd "$tree"
mkdir -p "$out"
evidence=(python3 -I -B "$here/refit_evidence.py")
refresh=(python3 -I scripts/lib/live_postgres_readiness_weights_refresh.py)
v2=specs/live-postgres-readiness-weights.v2.json
v1=specs/live-postgres-readiness-weights.v1.json
echo "evaluating $(git rev-parse --short HEAD) on runs in $runs"

"${evidence[@]}" poolcheck "${pool_dirs[@]}" > "$out/poolcheck.txt"
"${refresh[@]}" --schema-version 2 --out "$out/v2.json" "${pool_samples[@]}" > "$out/refit-v2.stdout"
fixed="$("${evidence[@]}" fixed "${pool_dirs[@]}")"
"${refresh[@]}" --out "$out/v1.json" --fixed-job-seconds "$fixed" "${pool_samples[@]}" > "$out/refit-v1.stdout"
"${refresh[@]}" --schema-version 2 --out "$out/holdout.v2.json" "${fit_samples[@]}" > "$out/holdout.stdout"
"${evidence[@]}" score "$(csv "${pool[@]:0:7}")" --weights "$out/holdout.v2.json" \
  --runs "${holdout_dirs[@]}" > "$out/holdout.txt"
"${evidence[@]}" score "$(csv "${pool[@]}")" --weights "$v2" --runs "${older_dirs[@]}" > "$out/oos.txt"
"${evidence[@]}" shards "$v2" "${pool_dirs[@]}" > "$out/pool-shards.txt"
"${evidence[@]}" tail "$v2" "${pool_dirs[@]}" > "$out/tail-pool.txt"
"${evidence[@]}" tail "$v2" "${older_dirs[@]}" > "$out/tail-oos.txt"
"${evidence[@]}" plan "$v2" > "$out/plan.txt"
"${evidence[@]}" loo "$out/loo" --pool "${pool_dirs[@]}" --extra "${older_dirs[@]}" "${six_dirs[@]}" \
  --censored "${censored[@]}" > "$out/loo-p75.txt"
"${evidence[@]}" loo "$out/loo-mean" --mean --pool "${pool_dirs[@]}" --extra "${older_dirs[@]}" "${six_dirs[@]}" \
  --censored "${censored[@]}" > "$out/loo-mean.txt"

status=0
check() { # label, produced, expected
  if cmp -s "$2" "$3"; then
    echo "MATCH $1"
  else
    echo "DIFFER $1"
    diff "$3" "$2" | head -20 || true
    status=1
  fi
}
check "v1 fixed-job-seconds 83.5" <(printf '%s\n' "$fixed") <(printf '83.5\n')
check "v2 weights = committed $v2" "$out/v2.json" "$v2"
check "v1 weights = committed $v1" "$out/v1.json" "$v1"
check "loo full-pool fit = committed $v2" "$out/loo/full.json" "$v2"
for name in poolcheck.txt refit-v2.stdout holdout.v2.json holdout.txt oos.txt pool-shards.txt \
  tail-pool.txt tail-oos.txt plan.txt loo-p75.txt loo-mean.txt; do
  check "$name" "$out/$name" "$data/expected/$name"
done
echo "holdout all-shard max: $(awk -F' [|] ' 'NR==3 {print $11}' "$out/holdout.txt") (bar 1.15)"
echo "out-of-sample all-shard max, shards over 1.15: $(awk -F' [|] ' 'NR==3 {print $11 ", " $12 " of " $8}' "$out/oos.txt")"
if [ "$status" -eq 0 ]; then echo "ALL MATCH"; else echo "MISMATCH"; fi
exit "$status"
