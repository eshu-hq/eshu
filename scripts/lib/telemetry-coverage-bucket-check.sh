#!/usr/bin/env bash
#
# telemetry-coverage-bucket-check.sh — X2 histogram-bucket-boundary
# bidirectional check, split out of scripts/verify-telemetry-coverage.sh to
# keep that script under the repo's 500-line file cap (#5855).
#
# Sourced by scripts/verify-telemetry-coverage.sh; not meant to run
# standalone. check_histogram_bucket_agreement expects repo_root, doc_path,
# and instruments_path to already be set by the caller, and mutates the
# caller's global `report` and `drift` variables — the same pattern the
# other numbered checks in verify-telemetry-coverage.sh use throughout.
#
# Registered as its own trigger of the telemetry-coverage gate in BOTH
# specs/ci-gates.v1.yaml (the registry) and the `telemetry:` dorny
# path-filter block in .github/workflows/static-contract-gates.yml (the
# actual CI enforcement), so a change to this file re-runs the gate. Both
# registrations are required: dorny/paths-filter is gitignore-style, where a
# single `*` does not cross `/`, so a registry-only trigger does not make CI
# select this gate for a PR confined to this file — the exact gap #5855's
# third review round found (the registry had the trigger; the workflow's
# filter still only matched direct children of scripts/, e.g.
# scripts/*verify-telemetry-coverage*.sh, which this scripts/lib/ file is
# not). The convention mirrors other scripts/lib/*.sh chunks (e.g.
# parser_relationship_language_ledger.sh). The point of dual registration is
# to NOT recreate the self-trigger gap tracked in #5873 for this new file —
# that gap is about the gate's own change-detection completeness in
# general, not something this comment claims to fix for
# verify-telemetry-coverage.sh as a whole.

# canonicalize_buckets <comma-separated-numbers> — normalize a bucket
# boundary list to a stable, whitespace-free, sorted-numeric form so the
# doc and code representations compare equal regardless of spacing or
# declaration order.
canonicalize_buckets() {
  printf '%s' "$1" | tr -d '[:space:]' | tr ',' '\n' | sort -n | paste -sd ',' -
}

check_histogram_bucket_agreement() {
  doc_buckets_tmp="$(mktemp)"
  code_buckets_tmp="$(mktemp)"

  # 4a: Parse documented bucket sets from the histogram-buckets section.
  section_line=$(rg -n '<!-- eshu:metric:section=histogram-buckets -->' "$repo_root/$doc_path" | head -1 | cut -d: -f1 || true)
  if [ -n "$section_line" ]; then
    next_section_line=$(rg -n '<!-- eshu:metric:section=' "$repo_root/$doc_path" | \
      awk -F: -v s="$section_line" '$1 > s {print $1; exit}' || true)
    [ -z "$next_section_line" ] && next_section_line=$(( $(wc -l < "$repo_root/$doc_path") + 1 ))
    sed -n "$((section_line + 1)),$((next_section_line - 1))p" "$repo_root/$doc_path" | \
      while IFS= read -r line; do
        printf '%s' "$line" | rg -q '^\|[-:|[:space:]]+\|' && continue
        if printf '%s' "$line" | rg -q '^\|.*\|.*[0-9].*\|'; then
          boundaries=$(printf '%s' "$line" | sed -n 's/^|[^|]*|\([^|]*\)|$/\1/p')
          if [ -n "$boundaries" ]; then
            boundaries=$(printf '%s' "$boundaries" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
            canonicalize_buckets "$boundaries"
          fi
        fi
      done | sort -u >"$doc_buckets_tmp" 2>/dev/null || true
  fi

  # 4b: Parse bucket boundary definitions from instruments.go.
  # rg --replace is line-oriented even with -U (multiline), so a bucket list
  # split across lines produces one output line per number rather than one
  # combined capture. Collapse the file to a single line first (remove
  # newlines inside []float64{...} and WithExplicitBucketBoundaries(...)
  # blocks), then extract the sets with single-line regexps.
  : >"$code_buckets_tmp"
  # python3 one-liner: join lines inside matching bracket pairs so each
  # bucket definition becomes a single line, then extract.
  python3 -c "
import re, sys
with open(sys.argv[1]) as f:
    text = f.read()

# Collapse newlines inside []float64{...} and WithExplicitBucketBoundaries(...)
# blocks so each becomes a single line.
text = re.sub(r'\[\]float64\{[^}]+\}', lambda m: m.group(0).replace('\n', ' '), text)
text = re.sub(r'WithExplicitBucketBoundaries\([^)]+\)', lambda m: m.group(0).replace('\n', ' '), text)

# Extract named variables: = []float64{...}
for m in re.finditer(r'=\s*\[\]float64\{([^}]+)\}', text):
    raw = m.group(1).strip()
    sys.stdout.write(raw + '\n')

# Extract inline literals: WithExplicitBucketBoundaries(N, N, ...)
for m in re.finditer(r'WithExplicitBucketBoundaries\(([^)]+)\)', text):
    raw = m.group(1).strip()
    if not re.search(r'[a-zA-Z_]', raw):  # skip variable references
        sys.stdout.write(raw + '\n')
" "$repo_root/$instruments_path" 2>/dev/null | \
    while IFS= read -r raw_set; do
      [ -n "$raw_set" ] && canonicalize_buckets "$raw_set" >>"$code_buckets_tmp"
    done || true
  sort -u -o "$code_buckets_tmp" "$code_buckets_tmp"

  # 4c: Bidirectional assertion.
  # Every code bucket set must have a matching documented set.
  while IFS= read -r code_set; do
    [ -n "$code_set" ] || continue
    if ! rg -qx "$code_set" "$doc_buckets_tmp"; then
      report="${report}  - bucket set [${code_set}] is in ${instruments_path} but not documented in ${doc_path}\n"
      drift=1
    fi
  done <"$code_buckets_tmp"

  # Every documented bucket set must have a matching code definition.
  while IFS= read -r doc_set; do
    [ -n "$doc_set" ] || continue
    if ! rg -qx "$doc_set" "$code_buckets_tmp"; then
      report="${report}  - bucket set [${doc_set}] is documented in ${doc_path} but not defined in ${instruments_path}\n"
      drift=1
    fi
  done <"$doc_buckets_tmp"
}
