#!/usr/bin/env bash
# RED/GREEN suite for the collector template CI caller (#10986):
# templates/collector/.github/workflows/ci.yml must stay a thin caller of the
# pinned eshu-hq/.github reusable (never inline lane bodies, never mutable
# action tags), with the stable jobs changes/lanes/ci-complete and an
# always-on ci-complete aggregator. Seeded-violation cases prove the checks
# reject a stale inline caller and a bare-tag pin.
#
# Run: bash scripts/ci/test-template-collector-caller.sh (no network, no Go build).
# CI wiring: the templatecollectorcaller cell in
# .github/workflows/static-contract-gates.yml runs this file as both its test
# mirror and its gate (#10999); the registry row template-collector-caller
# and the dorny filter key of the same name keep it selected on template or
# script edits.
set -euo pipefail
LC_ALL=C
root="$(cd "$(dirname "$0")/../.." && pwd)"
target="${1:-${root}/templates/collector/.github/workflows/ci.yml}"
fail=0

check() { # $1=name $2...=command; records PASS/FAIL, never aborts the suite
	local name="$1"; shift
	if "$@" >/dev/null 2>&1; then printf 'PASS %s\n' "${name}"; else printf 'FAIL %s\n' "${name}"; fail=1; fi
}

inline_lane_bodies() { # $1=file; print every run: step body outside changes/ci-complete
	local file="$1"
	python3 - "${file}" <<'EOF'
import sys, yaml
doc = yaml.safe_load(open(sys.argv[1])) or {}
jobs = doc.get('jobs') or {}
for name, job in jobs.items():
    if name in ('changes', 'ci-complete'):
        continue
    steps = job.get('steps') if isinstance(job, dict) else None
    for step in steps or []:
        if isinstance(step, dict) and 'run' in step and step['run'] is not None:
            print(step['run'])
EOF
}

is_thin_caller() { # $1=file; true when the reusable pin is present and no inline lane bodies exist
	local file="$1"
	rg -q 'uses:\s*eshu-hq/\.github/\.github/workflows/go-collector-ci\.yml@[0-9a-f]{40}\s+#\s*v[0-9]+' "${file}" || return 1
	# An inline lane body is a run step invoking a lane tool. Bodies are
	# read parsed (block scalars included), not grepped: a `run: |` header
	# line alone never names the tool. Input names (golangci-lint-version:)
	# live under `with:`, never under a printed body, so only run steps count.
	! inline_lane_bodies "${file}" |
		rg -v '^\s*#' |
		rg -q 'go\s+(build|test|vet|mod)\b|golangci-lint\s+run|shellcheck|docker\s+build|govulncheck|gosec\s|actionlint( |$)' || return 1
	return 0
}

has_stable_jobs() { # $1=file
	local file="$1"
	rg -q '^\s{2}changes:' "${file}" || return 1
	rg -q '^\s{2}lanes:' "${file}" || return 1
	rg -q '^\s{2}ci-complete:' "${file}" || return 1
	rg -q 'if:\s*always\(\)' "${file}" || return 1
	return 0
}

all_uses_pinned() { # $1=file; every uses: carries a 40-hex SHA, never a mutable tag
	local file="$1"
	# Reusable-call jobs name `uses:` without a list dash; both spellings count.
	! rg -N '^\s*(-\s*)?uses:' "${file}" | rg -v '@[0-9a-f]{40}([[:space:]]|#|$)' | rg -q . || return 1
	return 0
}

yaml_parses() { # $1=file
	python3 - "$1" <<'EOF'
import sys, yaml
yaml.safe_load(open(sys.argv[1]))
EOF
}

readme_names_reusable() { # $1=template dir; the porter-facing README must point at the reusable lane source
	rg -q 'go-collector-ci\.yml' "$1/README.md" || return 1
	return 0
}

check "thin-caller ${target}" is_thin_caller "${target}"
check "stable-jobs ${target}" has_stable_jobs "${target}"
check "all-uses-pinned ${target}" all_uses_pinned "${target}"
check "yaml-parses ${target}" yaml_parses "${target}"
check "readme-names-reusable ${root}/templates/collector" readme_names_reusable "${root}/templates/collector"

# Seeded violations: each must be REJECTED (the checker returns nonzero).
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
cat >"${work}/stale.yml" <<'EOF'
name: collector-ci
on: [push, pull_request]
jobs:
  conformance:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: go build ./...
      - run: go test ./... -count=1
EOF
cat >"${work}/baretag.yml" <<'EOF'
jobs:
  lanes:
    uses: eshu-hq/.github/.github/workflows/go-collector-ci.yml@v2
    with: {}
  ci-complete:
    if: always()
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
EOF
cat >"${work}/blockscalar.yml" <<'EOF'
jobs:
  lanes:
    uses: eshu-hq/.github/.github/workflows/go-collector-ci.yml@4d01ab90baf5dd77c21cc1ee6bdf701332613449 # v2
    with: {}
  smuggled:
    runs-on: ubuntu-latest
    steps:
      - name: quiet build
        run: |
          go test ./... -count=1
  ci-complete:
    if: always()
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
EOF
cat >"${work}/dashless-tag.yml" <<'EOF'
jobs:
  lanes:
    uses: eshu-hq/.github/.github/workflows/go-collector-ci.yml@4d01ab90baf5dd77c21cc1ee6bdf701332613449 # v2
    with: {}
  evil:
    uses: eshu-hq/.github/.github/workflows/other.yml@v2
  ci-complete:
    if: always()
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
EOF
if is_thin_caller "${work}/stale.yml"; then printf 'FAIL negative-stale-rejected\n'; fail=1; else printf 'PASS negative-stale-rejected\n'; fi
if is_thin_caller "${work}/baretag.yml"; then printf 'FAIL negative-baretag-rejected\n'; fail=1; else printf 'PASS negative-baretag-rejected\n'; fi
if is_thin_caller "${work}/blockscalar.yml"; then printf 'FAIL negative-blockscalar-rejected\n'; fail=1; else printf 'PASS negative-blockscalar-rejected\n'; fi
if all_uses_pinned "${work}/stale.yml"; then printf 'FAIL negative-unpinned-rejected\n'; fail=1; else printf 'PASS negative-unpinned-rejected\n'; fi
if all_uses_pinned "${work}/dashless-tag.yml"; then printf 'FAIL negative-dashless-tag-rejected\n'; fail=1; else printf 'PASS negative-dashless-tag-rejected\n'; fi
if all_uses_pinned "${work}/blockscalar.yml"; then printf 'PASS positive-dashless-pin-accepted\n'; else printf 'FAIL positive-dashless-pin-accepted\n'; fail=1; fi

exit "${fail}"
