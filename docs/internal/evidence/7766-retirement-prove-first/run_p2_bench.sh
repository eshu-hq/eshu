#!/bin/bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
# #7766 P2 wrapper, as run on 2026-10-08. It is a record of how the numbers in
# out/p2_bench.txt were produced, not a harness you can re-run as is: it builds
# the scratch benchmark BenchmarkCommitScopeGenerationLive, and that Go code
# (a benchmark, an 87-line gate shim, and a 32-line patch into the commit path)
# is described in the evidence note and not committed. P2' re-measures on the
# real code in PR 3.
#
# Required environment:
#   PROOF_PG_DSN  DSN of the disposable proof database (no credential is stored here)
#   PROOF_REPO    path to a checkout carrying the scratch benchmark
set -euo pipefail
: "${PROOF_PG_DSN:?set PROOF_PG_DSN to the disposable proof database}"
: "${PROOF_REPO:?set PROOF_REPO to a checkout carrying the scratch benchmark}"
here="$(cd "$(dirname "$0")" && pwd)"
export PROOF_PG_DSN
cd "$PROOF_REPO/go"
go test -c -o "$here/bin_p2bench" ./internal/storage/postgres/
cd "$PROOF_REPO/go/internal/storage/postgres"
for n in 0 10000; do
  "$here/psql.sh" -v n=$n < "$here/sql/p2_setup.sql" > /dev/null
  "$here/psql.sh" -c "VACUUM (ANALYZE) repository_retirements" > /dev/null
  echo "### repository_retirements rows=$n"
  "$here/bin_p2bench" -test.run '^$' -test.bench 'BenchmarkCommitScopeGenerationLive/facts=1$' -test.benchtime=1200x -test.count=6 2>&1 | rg --line-buffered 'Benchmark|FAIL|panic'
  "$here/bin_p2bench" -test.run '^$' -test.bench 'BenchmarkCommitScopeGenerationLive/facts=400$' -test.benchtime=240x -test.count=4 2>&1 | rg --line-buffered 'Benchmark|FAIL|panic'
done
echo ALLDONE
