#!/usr/bin/env bash
# Deterministically warm the shared `go test -race` GOCACHE on main pushes.
#
# Building from a single dedicated warmer avoids the 12-shard save race in
# test.yml where whichever fast shard finished first locked in an immutable
# partial cache missing heavy third-party trees (aws-sdk-go-v2, k8s.io/client-go).
#
# Phase 1 compiles every unique non-internal direct import (Imports, TestImports,
# XTestImports) and its transitive dependencies with `-race`.
# Phase 2 compiles `./...` with `-race` so unchanged internal packages are also
# warm for PRs.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
go_dir="${GO_RACE_CACHE_WARM_GO_DIR:-${repo_root}/go}"
module_prefix="${GO_RACE_CACHE_WARM_MODULE_PREFIX:-github.com/eshu-hq/eshu/go}"

if [[ ! -d "${go_dir}" ]]; then
	printf 'warm-go-race-cache: go directory not found: %s\n' "${go_dir}" >&2
	exit 1
fi

cd "${go_dir}"

tmp_list="$(mktemp)"
trap 'rm -f "${tmp_list}"' EXIT

go list -json ./... | python3 -c '
import json
import sys

prefix = sys.argv[1].rstrip("/")
raw = sys.stdin.read()
if not raw.strip():
    sys.stderr.write("warm-go-race-cache: go list -json ./... produced empty output\n")
    sys.exit(1)

dec = json.JSONDecoder()
idx = 0
pkg_count = 0
ext_imports = set()

while idx < len(raw):
    while idx < len(raw) and raw[idx].isspace():
        idx += 1
    if idx >= len(raw):
        break
    try:
        obj, idx = dec.raw_decode(raw, idx)
    except json.JSONDecodeError as exc:
        sys.stderr.write(f"warm-go-race-cache: invalid JSON from go list: {exc}\n")
        sys.exit(1)
    if not isinstance(obj, dict):
        sys.stderr.write("warm-go-race-cache: expected JSON object from go list\n")
        sys.exit(1)
    pkg_count += 1
    for field in ("Imports", "TestImports", "XTestImports"):
        for imp in obj.get(field) or []:
            if not isinstance(imp, str) or not imp or imp == "C":
                continue
            if imp == prefix or imp.startswith(prefix + "/"):
                continue
            ext_imports.add(imp)

if pkg_count == 0:
    sys.stderr.write("warm-go-race-cache: parsed zero packages from go list\n")
    sys.exit(1)
if not ext_imports:
    sys.stderr.write("warm-go-race-cache: found zero external/stdlib imports to warm\n")
    sys.exit(1)

for imp in sorted(ext_imports):
    print(imp)
' "${module_prefix}" >"${tmp_list}"

ext_count="$(wc -l <"${tmp_list}" | tr -d ' ')"
printf 'warm-go-race-cache: phase 1 — building %s external/stdlib direct imports with -race\n' "${ext_count}"
t0="$(date +%s)"
xargs -s 131072 go build -race <"${tmp_list}"
t1="$(date +%s)"
printf 'warm-go-race-cache: phase 1 finished in %ds\n' "$((t1 - t0))"

printf 'warm-go-race-cache: phase 2 — building ./... with -race\n'
go build -race ./...
t2="$(date +%s)"
printf 'warm-go-race-cache: phase 2 finished in %ds (total %ds)\n' "$((t2 - t1))" "$((t2 - t0))"
