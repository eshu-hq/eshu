#!/usr/bin/env bash
#
# refresh-eol-snapshot.sh — #9163: re-fetch the vendored endoflife.date
# snapshot for the eight mapped runtimes and stamp snapshot_date.
#
# The query read path (go/internal/eol) reads only
# these vendored files, so air-gapped deployments behave identically modulo
# staleness. Run this, review the diff, and open the update as a normal PR.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
snapshot_dir="${repo_root}/go/internal/eol/snapshots"
mkdir -p "${snapshot_dir}"

command -v curl >/dev/null 2>&1 || { printf 'refresh-eol-snapshot: missing curl\n' >&2; exit 1; }

for slug in nodejs go python ruby php rust terraform dotnet; do
	curl -sS --max-time 60 "https://endoflife.date/api/${slug}.json" -o "${snapshot_dir}/${slug}.json"
done
date -u +%F > "${snapshot_dir}/snapshot_date"

printf 'refresh-eol-snapshot: wrote %s (%s, %s total)\n' \
	"${snapshot_dir}" "$(cat "${snapshot_dir}/snapshot_date")" \
	"$(du -sh "${snapshot_dir}" | cut -f1)" >&2
