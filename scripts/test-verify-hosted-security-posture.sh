#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-hosted-security-posture.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

write_values() {
	local path="$1"
	local body="$2"
	printf '%s\n' "${body}" >"${path}"
}

expect_pass() {
	if ! "${verifier}" "$@" >"${tmp_dir}/pass.out" 2>"${tmp_dir}/pass.err"; then
		printf 'expected hosted security posture verifier to pass\n' >&2
		sed -n '1,160p' "${tmp_dir}/pass.err" >&2
		exit 1
	fi
}

expect_fail() {
	local label="$1"
	local expected="$2"
	shift 2
	if "${verifier}" "$@" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to fail\n' "${label}" >&2
		exit 1
	fi
	rg --fixed-strings --quiet -- "${expected}" "${tmp_dir}/${label}.err" \
		|| { printf 'expected %s failure to include %s\n' "${label}" "${expected}" >&2; sed -n '1,160p' "${tmp_dir}/${label}.err" >&2; exit 1; }
}

expect_pass

postgres_secret="${tmp_dir}/postgres-secret.yaml"
write_values "${postgres_secret}" 'contentStore:
  secretName: "eshu-postgres"
  dsnKey: "dsn"'
expect_pass --values "${postgres_secret}"

missing_auth="${tmp_dir}/missing-api-auth.yaml"
write_values "${missing_auth}" 'apiAuth:
  secretName: ""'
expect_fail missing_auth "missing API auth secret" --values "${missing_auth}"

public_pprof="${tmp_dir}/public-pprof.yaml"
write_values "${public_pprof}" 'api:
  env:
    ESHU_PPROF_ADDR: "0.0.0.0:6060"'
expect_fail public_pprof "pprof must not bind publicly" --values "${public_pprof}"

public_docs="${tmp_dir}/public-docs.yaml"
write_values "${public_docs}" 'env:
  ESHU_ENABLE_PUBLIC_DOCS: "true"'
expect_fail public_docs "public API docs require explicit verifier opt-in" --values "${public_docs}"
expect_pass --allow-public-docs --values "${public_docs}"

inline_secret="${tmp_dir}/inline-secret.yaml"
write_values "${inline_secret}" 'contentStore:
  dsn: "postgresql://eshu:literal-secret@postgres.example.invalid:5432/eshu"'
expect_fail inline_secret "credential env vars must use secretKeyRef" --values "${inline_secret}"

conflicting_postgres="${tmp_dir}/conflicting-postgres.yaml"
write_values "${conflicting_postgres}" 'contentStore:
  dsn: "postgresql://eshu:literal-secret@postgres.example.invalid:5432/eshu"
  secretName: "eshu-postgres"
  dsnKey: "dsn"'
expect_fail conflicting_postgres "contentStore.dsn and contentStore.secretName cannot both be set" --values "${conflicting_postgres}"

collector_secret="${tmp_dir}/collector-secret.yaml"
write_values "${collector_secret}" 'confluenceCollector:
  enabled: true
  baseUrl: "https://example.invalid"
  spaceId: "docs"
  credentials:
    secretName: ""'
expect_fail collector_secret "confluenceCollector.credentials.secretName is required" --values "${collector_secret}"

printf 'hosted security posture verifier tests passed\n'
