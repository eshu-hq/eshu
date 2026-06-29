#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify_remote_e2e_degradation_report.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

run_report() {
	local input_file="$1"
	local output_json="${tmp_root}/report.json"
	local output_markdown="${tmp_root}/report.md"
	"${verifier}" \
		--input "${input_file}" \
		--output-json "${output_json}" \
		--output-markdown "${output_markdown}" \
		>/tmp/eshu-degradation-report.out 2>/tmp/eshu-degradation-report.err
}

expect_pass() {
	local input_file="$1"
	if ! run_report "${input_file}"; then
		printf 'expected degradation report verifier to pass\n' >&2
		sed -n '1,160p' /tmp/eshu-degradation-report.err >&2
		exit 1
	fi
}

expect_fail_with() {
	local input_file="$1"
	local pattern="$2"
	if run_report "${input_file}"; then
		printf 'expected degradation report verifier to fail with %s\n' "${pattern}" >&2
		sed -n '1,160p' /tmp/eshu-degradation-report.out >&2
		exit 1
	fi
	if ! rg -q "${pattern}" /tmp/eshu-degradation-report.err; then
		printf 'expected failure output to contain %s\n' "${pattern}" >&2
		sed -n '1,160p' /tmp/eshu-degradation-report.err >&2
		exit 1
	fi
}

assert_json_equals() {
	local filter="$1"
	local expected="$2"
	local actual
	actual="$(jq -r "${filter}" "${tmp_root}/report.json")"
	if [[ "${actual}" != "${expected}" ]]; then
		printf 'expected %s to equal %s, got %s\n' "${filter}" "${expected}" "${actual}" >&2
		jq . "${tmp_root}/report.json" >&2
		exit 1
	fi
}

assert_markdown_contains() {
	local pattern="$1"
	if ! rg -q "${pattern}" "${tmp_root}/report.md"; then
		printf 'expected markdown report to contain %s\n' "${pattern}" >&2
		sed -n '1,200p' "${tmp_root}/report.md" >&2
		exit 1
	fi
}

degraded_input="${tmp_root}/degraded.json"
cat >"${degraded_input}" <<'JSON'
{
  "run": {
    "id": "remote-full-corpus-2026-06-29",
    "commit": "b514d2f395f9c2edc25e010c32229e2f6f0005de",
    "nornicdb_image": "timothyswt/nornicdb-cpu-bge:v1.1.9",
    "nornicdb_digest": "sha256:9a5126d306a48c01869809da47a869a4521b9328a7ab1c855327f5fd7541e4cd"
  },
  "startup": {
    "schema_bootstrap": "passed",
    "bootstrap_index": "passed",
    "hosted_profile_startup": "passed"
  },
  "services": [
    {"name": "eshu", "state": "running", "health": "healthy"},
    {"name": "projector", "state": "running", "health": "healthy"},
    {"name": "workflow-coordinator", "state": "running", "health": "healthy"}
  ],
  "index_status": {
    "status": "progressing",
    "queue": {
      "outstanding": 2405,
      "in_flight": 6,
      "pending": 2380,
      "retrying": 19,
      "failed": 0,
      "dead_letter": 0
    },
    "coordinator": {
      "run_status_counts": [{"name": "running", "count": 3}],
      "completeness_counts": [{"name": "incomplete", "count": 3}]
    }
  },
  "work": {
    "retrying_by_failure_class": [
      {"failure_class": "graph_canonical_retract_timeout", "count": 19}
    ],
    "pending_domains": [
      {"domain": "search_document_readiness", "pending": 2187, "oldest_age_seconds": 3540}
    ]
  },
  "postgres": {
    "active_queries": [
      {"age_seconds": 3540, "query_shape": "WITH active_docs AS MATERIALIZED SELECT document_key FROM eshu_search_documents"},
      {"age_seconds": 936, "query_shape": "canonical source-local retract transaction waiting on graph write"}
    ],
    "ungranted_locks": 0,
    "relation_sizes": [
      {"name": "fact_records", "bytes": 7516192768},
      {"name": "eshu_search_index_terms", "bytes": 3328599654}
    ]
  },
  "logs": [
    {"service": "projector", "message": "canonical source-local retract timed out after retry budget"}
  ]
}
JSON

expect_pass "${degraded_input}"
assert_json_equals '.schema_version' '1'
assert_json_equals '.summary.status' 'degraded'
assert_json_equals '.classification.startup.status' 'passed'
assert_json_equals '.classification.graph_write_timeout.status' 'blocked'
assert_json_equals '.classification.search_index_tail.status' 'blocked'
assert_json_equals '.classification.schema_lock_wait.status' 'not_observed'
assert_json_equals '.classification.finite_completion.status' 'degraded'
assert_json_equals '.classification.hosted_collectors.status' 'passed'
assert_json_equals '.summary.service_health[0].name' 'eshu'
assert_json_equals '.summary.oldest_active_queries[0].query_shape' 'WITH active_docs AS MATERIALIZED SELECT document_key FROM eshu_search_documents'
assert_json_equals '.summary.relation_sizes[0].name' 'fact_records'
assert_markdown_contains '# Full-Corpus Degradation Report'
assert_markdown_contains 'graph_write_timeout'
assert_markdown_contains 'search_index_tail'
assert_markdown_contains 'schema_lock_wait'
assert_markdown_contains 'WITH active_docs AS MATERIALIZED'
assert_markdown_contains 'service=eshu state=running health=healthy'
assert_markdown_contains 'retrying_failure_class=graph_canonical_retract_timeout count=19'
assert_markdown_contains 'top_pending_domain=search_document_readiness pending=2187'
assert_markdown_contains 'relation_size=fact_records bytes=7516192768'

private_input="${tmp_root}/private.json"
cat >"${private_input}" <<'JSON'
{
  "run": {"id": "private", "commit": "b514d2f395f9c2edc25e010c32229e2f6f0005de"},
  "postgres": {
    "active_queries": [
      {"age_seconds": 1, "query_shape": "COPY /example/private/repo/redacted.txt account 123456789012"}
    ]
  }
}
JSON

expect_fail_with "${private_input}" 'public-safe'

private_numeric_input="${tmp_root}/private-numeric.json"
cat >"${private_numeric_input}" <<'JSON'
{
  "run": {"id": "private-numeric", "commit": "b514d2f395f9c2edc25e010c32229e2f6f0005de"},
  "startup": {"schema_bootstrap": "passed"},
  "services": [{"name": "eshu", "state": "running", "health": "healthy"}],
  "index_status": {"status": "healthy", "queue": {"outstanding": 0}},
  "work": {
    "pending_domains": [
      {"domain": "search_document_readiness", "pending": 0, "oldest_age_seconds": 0, "account_id": 123456789012}
    ]
  },
  "postgres": {"active_queries": [], "ungranted_locks": 0, "relation_sizes": []}
}
JSON

expect_fail_with "${private_numeric_input}" 'public-safe'

healthy_input="${tmp_root}/healthy.json"
cat >"${healthy_input}" <<'JSON'
{
  "run": {
    "id": "remote-smoke",
    "commit": "b514d2f395f9c2edc25e010c32229e2f6f0005de",
    "nornicdb_image": "timothyswt/nornicdb-cpu-bge:v1.1.9"
  },
  "startup": {
    "schema_bootstrap": "passed",
    "bootstrap_index": "passed"
  },
  "services": [
    {"name": "eshu", "state": "running", "health": "healthy"}
  ],
  "index_status": {
    "status": "healthy",
    "queue": {
      "outstanding": 0,
      "in_flight": 0,
      "pending": 0,
      "retrying": 0,
      "failed": 0,
      "dead_letter": 0
    }
  },
  "postgres": {
    "active_queries": [],
    "ungranted_locks": 0,
    "relation_sizes": [
      {"name": "eshu_search_index_terms", "bytes": 3328599654}
    ]
  }
}
JSON

expect_pass "${healthy_input}"
assert_json_equals '.summary.status' 'passed'
assert_json_equals '.classification.graph_write_timeout.status' 'not_observed'
assert_json_equals '.classification.search_index_tail.status' 'not_observed'
assert_json_equals '.classification.schema_lock_wait.status' 'not_observed'

printf 'remote E2E degradation report tests passed\n'
