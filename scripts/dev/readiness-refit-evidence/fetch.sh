#!/usr/bin/env bash
# Fetch one live-postgres-readiness workflow run from GitHub into
# OUT_DIR/RUN_ID: run.json, jobs.json, and one raw log per shard job
# (shardN.log). trim_inputs.py reduces the result to what the evidence reads.
#
# Usage: scripts/dev/readiness-refit-evidence/fetch.sh OUT_DIR RUN_ID...
# Requires an authenticated gh CLI. GitHub keeps workflow run logs for a
# limited time (90 days by default), so the logs of an old run may be gone;
# testdata/readiness-refit-evidence/inputs.tar.gz holds the trimmed copy of
# every run the evidence uses.
set -euo pipefail

if [ $# -lt 2 ]; then
  echo "usage: $0 OUT_DIR RUN_ID..." >&2
  exit 2
fi
out="$1"
shift
for run in "$@"; do
  dir="$out/$run"
  mkdir -p "$dir"
  gh api "repos/eshu-hq/eshu/actions/runs/$run" > "$dir/run.json"
  gh api "repos/eshu-hq/eshu/actions/runs/$run/jobs?per_page=100" > "$dir/jobs.json"
  python3 -I - "$dir/jobs.json" > "$dir/shards.tsv" <<'PY'
import json
import re
import sys

for job in json.load(open(sys.argv[1]))["jobs"]:
    match = re.fullmatch(r"live-postgres-readiness \(postgres18\) shard \((\d+)/\d+\)", job["name"])
    if match:
        print(match.group(1), job["id"], job["conclusion"])
PY
  while read -r number job conclusion; do
    # Raw job logs carry ANSI color codes; gh refuses to print them without
    # this flag. trim_inputs.py keeps only lines the parser matches.
    gh api --allow-escape-sequences "repos/eshu-hq/eshu/actions/jobs/$job/logs" > "$dir/shard$number.log"
    echo "$run shard $number job $job $conclusion: $(wc -l < "$dir/shard$number.log") lines"
  done < "$dir/shards.tsv"
  rm "$dir/shards.tsv"
done
