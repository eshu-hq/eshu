#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-codeql-setup.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT
verifier_out="${tmp_root}/verify-codeql-setup.out"
verifier_err="${tmp_root}/verify-codeql-setup.err"

init_repo() {
	local name="$1"
	local dir="${tmp_root}/${name}"
	mkdir -p "${dir}/.github/workflows"
	printf '%s\n' "${dir}"
}

run_verifier() {
	local dir="$1"
	"${verifier}" "${dir}" >"${verifier_out}" 2>"${verifier_err}"
}

expect_pass() {
	local dir="$1"
	if ! run_verifier "${dir}"; then
		printf 'expected CodeQL setup verifier to pass in %s\n' "${dir}" >&2
		sed -n '1,120p' "${verifier_err}" >&2
		exit 1
	fi
}

expect_fail() {
	local dir="$1"
	if run_verifier "${dir}"; then
		printf 'expected CodeQL setup verifier to fail in %s\n' "${dir}" >&2
		sed -n '1,120p' "${verifier_out}" >&2
		exit 1
	fi
	if ! rg -q 'CodeQL advanced setup or CodeQL result upload is not allowed' "${verifier_err}"; then
		printf 'expected failure to explain the CodeQL setup conflict\n' >&2
		sed -n '1,120p' "${verifier_err}" >&2
		exit 1
	fi
}

empty_repo="$(init_repo empty)"
expect_pass "${empty_repo}"

ordinary_repo="$(init_repo ordinary)"
cat >"${ordinary_repo}/.github/workflows/test.yml" <<'YAML'
name: Test
on: [pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
YAML
expect_pass "${ordinary_repo}"

advanced_repo="$(init_repo advanced)"
cat >"${advanced_repo}/.github/workflows/codeql.yml" <<'YAML'
name: CodeQL
on: [pull_request]
jobs:
  analyze:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: github/codeql-action/init@v4
        with:
          languages: go
      - uses: github/codeql-action/analyze@v4
YAML
expect_fail "${advanced_repo}"

third_party_sarif_repo="$(init_repo third-party-sarif)"
cat >"${third_party_sarif_repo}/.github/workflows/sarif-upload.yaml" <<'YAML'
name: Eshu SARIF upload
on: [pull_request]
jobs:
  upload:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: github/codeql-action/upload-sarif@v4
        with:
          sarif_file: eshu-vulnerability-scan.sarif
YAML
expect_pass "${third_party_sarif_repo}"

codeql_cli_repo="$(init_repo codeql-cli)"
cat >"${codeql_cli_repo}/.github/workflows/codeql-cli.yml" <<'YAML'
name: CodeQL CLI upload
on: [pull_request]
jobs:
  analyze:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - run: codeql github upload-results --sarif=go-codeql.sarif --repository=eshu-hq/eshu
YAML
expect_fail "${codeql_cli_repo}"

printf 'verify-codeql-setup tests passed\n'
