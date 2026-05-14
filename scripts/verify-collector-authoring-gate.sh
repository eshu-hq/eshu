#!/usr/bin/env bash
set -euo pipefail

repo_root="${ESHU_COLLECTOR_AUTHORING_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi

base="${ESHU_COLLECTOR_AUTHORING_BASE:-}"
if [ -z "$base" ] && [ -n "${GITHUB_BASE_REF:-}" ]; then
  git -C "$repo_root" fetch --no-tags --depth=1 origin "$GITHUB_BASE_REF" >/dev/null 2>&1 || true
  if git -C "$repo_root" rev-parse --verify "origin/$GITHUB_BASE_REF" >/dev/null 2>&1; then
    base="origin/$GITHUB_BASE_REF"
  fi
fi
if [ -z "$base" ]; then
  if git -C "$repo_root" rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    base="HEAD~1"
  else
    printf 'verify-collector-authoring-gate: no base commit available, skipping\n'
    exit 0
  fi
fi

changed_files=()
tmp_file="$(mktemp)"
trap 'rm -f "$tmp_file"' EXIT
if git -C "$repo_root" diff --name-only "$base"...HEAD >"$tmp_file" 2>/dev/null; then
  :
else
  git -C "$repo_root" diff --name-only "$base" HEAD >"$tmp_file"
fi
while IFS= read -r file; do
  [ -n "$file" ] && changed_files+=("$file")
done <"$tmp_file"

is_collector_source() {
  local path="$1"
  case "$path" in
    *.go) ;;
    *) return 1 ;;
  esac
  case "$path" in
    *_test.go|*_bench_test.go|*/testdata/*|*/vendor/*|*/doc.go) return 1 ;;
  esac
  case "$path" in
    go/internal/collector/*|go/cmd/collector-*/*.go) return 0 ;;
    *) return 1 ;;
  esac
}

is_evidence_file() {
  local path="$1"
  case "$path" in
    docs/docs/*.md|docs/docs/*/*.md|docs/docs/*/*/*.md) return 0 ;;
    go/*/README.md|go/*/*/README.md|go/*/*/*/README.md|go/*/*/*/*/README.md|go/*/*/*/*/*/README.md) return 0 ;;
    go/*/AGENTS.md|go/*/*/AGENTS.md|go/*/*/*/AGENTS.md|go/*/*/*/*/AGENTS.md|go/*/*/*/*/*/AGENTS.md) return 0 ;;
    go/*/doc.go|go/*/*/doc.go|go/*/*/*/doc.go|go/*/*/*/*/doc.go|go/*/*/*/*/*/doc.go) return 0 ;;
    *) return 1 ;;
  esac
}

is_telemetry_contract_file() {
  local path="$1"
  case "$path" in
    go/internal/telemetry/contract.go|go/internal/telemetry/instruments.go) return 0 ;;
    *) return 1 ;;
  esac
}

declare -A collector_package_dirs=()
evidence_files=()
telemetry_contract_changed=1

for file in "${changed_files[@]}"; do
  if is_collector_source "$file"; then
    collector_package_dirs["${file%/*}"]=1
  fi
  if is_evidence_file "$file" && [ -f "$repo_root/$file" ]; then
    evidence_files+=("$repo_root/$file")
  fi
  if is_telemetry_contract_file "$file"; then
    telemetry_contract_changed=0
  fi
done

if [ "${#collector_package_dirs[@]}" -eq 0 ]; then
  printf 'verify-collector-authoring-gate: no collector source packages changed\n'
  exit 0
fi

missing=0
for dir in "${!collector_package_dirs[@]}"; do
  for required in doc.go README.md AGENTS.md; do
    if [ ! -f "$repo_root/$dir/$required" ]; then
      printf 'verify-collector-authoring-gate: %s is missing %s\n' "$dir" "$required" >&2
      missing=1
    fi
  done
  tests=("$repo_root/$dir"/*_test.go)
  if [ ! -e "${tests[0]}" ]; then
    printf 'verify-collector-authoring-gate: %s has no *_test.go coverage\n' "$dir" >&2
    missing=1
  fi
done

has_performance=1
has_observability=1
has_deployment=1
has_no_observability_change=1
if [ "${#evidence_files[@]}" -gt 0 ]; then
  rg -q '(^|[[:space:]])Collector Performance Evidence:' "${evidence_files[@]}" && has_performance=0
  rg -q '(^|[[:space:]])Collector Observability Evidence:' "${evidence_files[@]}" && has_observability=0
  rg -q '(^|[[:space:]])Collector Deployment Evidence:' "${evidence_files[@]}" && has_deployment=0
  rg -q '(^|[[:space:]])No-Observability-Change:' "${evidence_files[@]}" && has_no_observability_change=0
fi

if [ "$has_performance" -ne 0 ]; then
  printf 'verify-collector-authoring-gate: missing Collector Performance Evidence marker\n' >&2
  missing=1
fi
if [ "$has_observability" -ne 0 ]; then
  printf 'verify-collector-authoring-gate: missing Collector Observability Evidence marker\n' >&2
  missing=1
fi
if [ "$has_deployment" -ne 0 ]; then
  printf 'verify-collector-authoring-gate: missing Collector Deployment Evidence marker\n' >&2
  missing=1
fi
if [ "$telemetry_contract_changed" -ne 0 ] && [ "$has_no_observability_change" -ne 0 ]; then
  printf 'verify-collector-authoring-gate: collector source changed without telemetry contract changes or No-Observability-Change marker\n' >&2
  missing=1
fi

if [ "$missing" -ne 0 ]; then
  {
    printf '\nChanged collector packages must include package docs, tests, tracked\n'
    printf 'collector performance evidence, collector observability evidence, and\n'
    printf 'a deployment/ServiceMonitor decision note. If the existing telemetry\n'
    printf 'already covers the changed collector path, add No-Observability-Change:\n'
    printf 'and name the exact existing signals.\n'
    printf '\nChanged collector package dirs:\n'
    for dir in "${!collector_package_dirs[@]}"; do
      printf '  - %s\n' "$dir"
    done
  } >&2
  exit 1
fi

printf 'verify-collector-authoring-gate: collector package docs, tests, evidence, and telemetry gate passed\n'
