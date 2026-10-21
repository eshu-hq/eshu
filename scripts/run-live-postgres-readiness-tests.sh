#!/usr/bin/env bash
# Run the readiness, dead-code incoming, status route-selection,
# quiet-generation, activation obligation, #7584 targeted-maintenance,
# reindex watermark plan/correctness, container image identity epoch-gate,
# #7209 projector zombie-heal, and #7471 marked-write-age signal proofs
# enrolled by markers in the test files (rendered to generated registration
# by scripts/generate-live-postgres-readiness.py) on disposable PostgreSQL 18
# (plus a disposable Neo4j for the zombie-heal graph proof when its env is
# configured; without one the graph proofs are excused and named in the
# summary), one go test per package.
#
# With --shard N/4 (one CI matrix leg) the runner executes only that pinned
# quarter of the enrolled proofs through the shards planner and leaves a
# fail-closed per-shard JSON report instead of the suite summary; the
# aggregator job verifies the four reports cover the full plan.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
results="${repo_root}/scripts/lib/live_postgres_readiness_results.py"
shards="${repo_root}/scripts/lib/live_postgres_readiness_shards.py"
ledger="${repo_root}/specs/live-tests.v1.yaml"

die() { printf 'live-postgres-readiness: %s\n' "$*" >&2; exit 1; }

shard="all"
report_path=""
opt_head=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --shard) shard="${2:-}"; shift 2 ;;
    --report) report_path="${2:-}"; shift 2 ;;
    --head) opt_head="${2:-}"; shift 2 ;;
    *) die "unknown argument: $1 (usage: [--shard N/4 --report PATH] [--head SHA])" ;;
  esac
done
if [[ "${shard}" == "all" ]]; then
  [[ -z "${report_path}" && -z "${opt_head}" ]] ||
    die "--report/--head require --shard"
else
  [[ -n "${report_path}" ]] || die "--shard requires --report PATH"
fi
command -v go >/dev/null 2>&1 || die "go is required"
command -v python3 >/dev/null 2>&1 || die "python3 is required"

# The required proof env lists below are generated from the enrollment
# markers in the test files (scripts/lib/live_postgres_readiness_env.sh);
# do not hand-edit that file. ESHU_POSTGRES_DSN and
# ESHU_WEBHOOK_CLAIM_LEASE_PROOF_DSN carry no _DISPOSABLE twin: their tests
# create and drop isolated schemas instead of gating on a
# disposable-database flag.
# shellcheck source=lib/live_postgres_readiness_env.sh
source "${repo_root}/scripts/lib/live_postgres_readiness_env.sh"
for name in "${READINESS_DSN_VARS[@]}"; do
  [[ -n "${!name:-}" ]] || die "${name} must name the administrative postgres database"
  [[ "${!name}" == */postgres\?* || "${!name}" == */postgres ]] ||
    die "${name} must target the administrative postgres database"
done
for name in "${READINESS_DISPOSABLE_VARS[@]}"; do
  [[ "${!name:-}" == "1" ]] || die "${name} must be 1"
done
# Only the Neo4j graph proofs need a backend; everything else runs on plain
# Postgres. All three Neo4j variables set selects strict mode (the CI path):
# the backend must be neo4j and every enrolled proof must pass. None set
# selects degrade mode: the graph proofs are excused from verification and
# named in the summary, so a developer without a container still runs every
# other proof. A partial set dies loud instead of silently degrading.
neo4j_missing=()
for name in \
  ESHU_NEO4J_URI \
  ESHU_NEO4J_USERNAME \
  ESHU_NEO4J_PASSWORD; do
  [[ -n "${!name:-}" ]] || neo4j_missing+=("${name}")
done
skip_args=()
if [[ "${#neo4j_missing[@]}" -eq 0 ]]; then
  [[ "${ESHU_GRAPH_BACKEND:-}" == "neo4j" ]] || die "ESHU_GRAPH_BACKEND must be neo4j"
elif [[ "${#neo4j_missing[@]}" -eq 3 ]]; then
  graph_tests="$(python3 "${results}" list-neo4j-tests)" ||
    die "neo4j test list from the results verifier is invalid"
  for name in ${graph_tests}; do skip_args+=(--skip "${name}"); done
  if [[ "${#skip_args[@]}" -gt 0 ]]; then
    printf 'live-postgres-readiness: no Neo4j proof backend configured; excusing %s\n' "${graph_tests}"
  fi
else
  die "${neo4j_missing[*]} must name the Neo4j proof backend (set all three or none)"
fi

python3 "${results}" verify-ledger "${ledger}" "${repo_root}" ||
  die "postgres_ci ledger selection is invalid"

# The package list and each package's anchored -run pattern come from PACKAGES
# in the results verifier, the single source of truth, so this runner cannot
# drift from the ledger-checked expectations. Records are tab separated:
# package path (relative to go/), -run pattern, expected test names.
scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT
python3 "${results}" list-packages >"${scratch}/full.tsv" ||
  die "package list from the results verifier is invalid"
[[ -s "${scratch}/full.tsv" ]] || die "results verifier listed no packages"
if [[ "${shard}" == "all" ]]; then
  cp "${scratch}/full.tsv" "${scratch}/packages.tsv"
else
  python3 "${shards}" plan --shard "${shard}" <"${scratch}/full.tsv" >"${scratch}/packages.tsv" ||
    die "shard selection is invalid"
  [[ -s "${scratch}/packages.tsv" ]] || die "shard ${shard} selected no packages"
fi

started="${SECONDS}"
failed_packages=()
# A failed package leaves its go test -json event stream and stderr here,
# outside the trap-deleted scratch dir, so a dying binary keeps its death
# point for post-mortem instead of the 20-line stderr cap (#8662). The CI
# shard job uploads this dir on failure; green runs never create it. The dir
# is cleared every run, so its leaf name is pinned: an env typo pointing at
# $HOME, the repo root, or / dies here instead of wiping a meaningful tree.
debug_dir="${LIVE_READINESS_DEBUG_DIR:-${repo_root}/.readiness-debug}"
[[ "${debug_dir}" == */.readiness-debug ]] || die "LIVE_READINESS_DEBUG_DIR must end in /.readiness-debug"
rm -rf -- "${debug_dir}"

# Every package runs even after an earlier one fails, so one CI run reports
# every broken proof. The final status fails closed if any package failed.
i=0
while IFS=$'\t' read -r package pattern tests; do
  i=$((i + 1))
  events="${scratch}/events-${i}.jsonl"
  if (cd "${repo_root}/go" && go test -json -count=1 -timeout=15m \
    "${package}" -run "${pattern}") \
    >"${events}" 2>"${scratch}/stderr-${i}" </dev/null; then
    go_status=0
  else
    go_status=$?
  fi
  # A shard verifies exactly its assigned subset: --expect names the row's
  # tests, and any event outside that subset fails closed. The empty-array
  # expansion keeps `set -u` safe on older bash: with no --skip flags this
  # is exactly the historical call.
  expect_args=()
  [[ "${shard}" == "all" ]] || expect_args=(--expect "${tests// /,}")
  # shellcheck disable=SC2086
  if python3 "${results}" verify-results "${events}" "${package}" ${expect_args[@]+"${expect_args[@]}"} ${skip_args[@]+"${skip_args[@]}"} </dev/null; then
    results_status=0
  else
    results_status=$?
  fi
  if [[ "${go_status}" -ne 0 ]]; then
    # Compiler and runtime diagnostics may be on stderr instead of in the JSON
    # stream. Limit them here so a failed proof cannot flood the CI log.
    sed -n '1,20p' "${scratch}/stderr-${i}" | cut -c 1-300 >&2
    printf 'live-postgres-readiness: %s: go test exited %s\n' "${package}" "${go_status}" >&2
    pkg_failed=1
  elif [[ "${results_status}" -ne 0 ]]; then
    printf 'live-postgres-readiness: %s: expected tests did not all pass\n' "${package}" >&2
    pkg_failed=1
  else
    pkg_failed=0
  fi
  if [[ "${pkg_failed}" -ne 0 ]]; then
    # Preserve the full event stream and stderr outside the trap-deleted
    # scratch dir on EITHER failure branch (a verify failure's events are
    # equally destroyed), and print the stream tail inline: its last lines
    # are the death point that tells a dying binary from a selection
    # fault (#8662). Preservation warns instead of dying: a failed copy
    # (e.g. a full disk, plausible when keeping large failure streams) must
    # not abort the every-package-runs loop above.
    debug_pkgdir="${debug_dir}/$(printf '%s' "${package}" | tr -c '[:alnum:]' '_')"
    if mkdir -p -- "${debug_pkgdir}" &&
      cp -- "${events}" "${debug_pkgdir}/events.jsonl" &&
      cp -- "${scratch}/stderr-${i}" "${debug_pkgdir}/stderr.txt" &&
      printf '%s %s\n' "${go_status}" "${results_status}" >"${debug_pkgdir}/exit"; then
      printf 'live-postgres-readiness: %s: event stream tail:\n' "${package}" >&2
      tail -n 5 -- "${events}" | cut -c 1-300 >&2
      printf 'live-postgres-readiness: %s: debug events preserved in %s\n' "${package}" "${debug_pkgdir}" >&2
    else
      printf 'live-postgres-readiness: %s: WARNING: debug preservation failed, continuing\n' "${package}" >&2
    fi
    failed_packages+=("${package}")
  fi
  if [[ "${shard}" != "all" ]]; then
    if [[ "${go_status}" -ne 0 || "${results_status}" -ne 0 ]]; then
      printf '%s|1|%s\n' "${package}" "${tests// /,}" >>"${scratch}/manifest.tsv"
    else
      printf '%s|0|%s\n' "${package}" "${tests// /,}" >>"${scratch}/manifest.tsv"
    fi
  fi
done <"${scratch}/packages.tsv"
printf 'live-postgres-readiness: suite_elapsed=%ss\n' "$((SECONDS - started))"
if [[ "${#failed_packages[@]}" -ne 0 ]]; then
  die "failed packages: ${failed_packages[*]}"
fi
if [[ "${shard}" == "all" ]]; then
  # shellcheck disable=SC2086
  python3 "${results}" summary ${skip_args[@]+"${skip_args[@]}"}
  exit 0
fi
# A shard attests its assignment with a report instead of the suite summary:
# the summary counts the whole ledger and would claim proofs this leg never ran.
# The report head is the checkout the shard actually ran, matching the
# aggregator's `git rev-parse HEAD` at its own checkout of the same ref; a
# mid-run checkout drift fails the aggregate closed instead of mixing heads.
if [[ -n "${opt_head}" ]]; then
  head_sha="${opt_head}"
else
  head_sha="$(git -C "${repo_root}" rev-parse HEAD 2>/dev/null)" ||
    die "no --head and no git HEAD: cannot attest the shard report"
fi
python3 "${shards}" report --head "${head_sha}" --shard "${shard}" \
  --plan "${scratch}/full.tsv" --manifest "${scratch}/manifest.tsv" \
  --output "${report_path}" || die "shard ${shard} report is invalid"
