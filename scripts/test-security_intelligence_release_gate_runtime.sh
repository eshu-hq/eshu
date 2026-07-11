#!/usr/bin/env bash
# Focused runtime proof tests for scripts/security_intelligence_release_gate.sh.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="${repo_root}/scripts/security_intelligence_release_gate.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

init_fake_repo() {
    local name="$1"
    local dir="${tmp_root}/${name}"
    mkdir -p "${dir}/deploy/helm/eshu" "${dir}/schema/data-plane/postgres" "${dir}/scripts" "${dir}/_bin"
    git -C "${dir}" init -q
    git -C "${dir}" config user.email "test@example.invalid"
    git -C "${dir}" config user.name "Eshu Runtime Gate Test"

    cat >"${dir}/deploy/helm/eshu/Chart.yaml" <<'YAML'
apiVersion: v2
name: eshu
description: test fixture
type: application
version: 0.0.3-pre-release-9
appVersion: "v0.0.3-pre-release-9"
YAML

    cat >"${dir}/docker-compose.yaml" <<'YAML'
services:
  nornicdb:
    image: timothyswt/nornicdb-cpu-bge:v9.9.9@sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789
YAML

    printf 'CREATE TABLE t (id int);\n' >"${dir}/schema/data-plane/postgres/001_table.sql"
    cat >"${dir}/scripts/verify_remote_e2e_runtime_state.sh" <<'SH'
#!/usr/bin/env bash
exit 0
SH
    chmod +x "${dir}/scripts/verify_remote_e2e_runtime_state.sh"

    # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
    # the entire heredoc body to a pipe before forking the reader, and
    # macOS's 512-byte pipe buffer deadlocks on any body over that size
    # (#5074).
    cat "${repo_root}/scripts/lib/test-security_intelligence_release_gate_runtime-fake-curl.sh" >"${dir}/_bin/curl"
    chmod +x "${dir}/_bin/curl"

    cat >"${dir}/_bin/docker" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  "stats --no-stream --format "*)
    if [ "${ESHU_FAKE_DOCKER_STATS_EMPTY:-0}" = "1" ]; then
      exit 0
    fi
    if [ "${ESHU_FAKE_DOCKER_STATS_NO_MEM:-0}" = "1" ]; then
      printf '{"name":"eshu","cpu":"12.5%%"}\n'
      exit 0
    fi
    printf '{"name":"eshu","cpu":"12.5%%","mem":"256MiB / 1GiB","net":"0B / 0B","block":"0B / 0B"}\n'
    ;;
  *)
    printf 'unexpected docker args: %s\n' "$*" >&2
    exit 1
    ;;
esac
SH
    chmod +x "${dir}/_bin/docker"

    git -C "${dir}" add -A
    git -C "${dir}" commit -q -m initial
    printf '%s\n' "${dir}"
}

run_gate() {
    local dir="$1"
    local out_dir="$2"
    shift 2
    PATH="${dir}/_bin:${PATH}" \
        ESHU_RELEASE_GATE_REPO_ROOT="${dir}" \
        ESHU_RELEASE_GATE_SKIP_GO_TESTS=1 \
        "${gate}" --out-dir "${out_dir}" "$@" >"${dir}/_gate.out" 2>"${dir}/_gate.err"
}

write_volume_proof() {
    local path="$1"
    local kind="$2"
    if [ "${kind}" = "clean" ]; then
        cat >"${path}" <<'JSON'
{
  "schema_version": 1,
  "proof_id": "clean-volume-proof-v1",
  "run_kind": "clean",
  "clean_volume_state": "reset_before_run",
  "backing_stores": {
    "nornicdb_data": {"status": "pass", "before": "absent", "after": "present"},
    "postgres_data": {"status": "pass", "before": "absent", "after": "present"},
    "eshu_data": {"status": "pass", "before": "absent", "after": "present"}
  }
}
JSON
    else
        cat >"${path}" <<'JSON'
{
  "schema_version": 1,
  "proof_id": "preserved-volume-proof-v1",
  "run_kind": "preserved",
  "previous_run_kind": "clean",
  "restart_without_prune": true,
  "backing_stores": {
    "nornicdb_data": {"status": "pass", "same_as_clean": true},
    "postgres_data": {"status": "pass", "same_as_clean": true},
    "eshu_data": {"status": "pass", "same_as_clean": true}
  }
}
JSON
    fi
}

expect_pass() {
    local dir="$1"
    local out_dir="$2"
    shift 2
    if ! run_gate "${dir}" "${out_dir}" "$@"; then
        printf 'expected runtime gate to pass in %s\n' "${dir}" >&2
        sed -n '1,200p' "${dir}/_gate.err" >&2
        exit 1
    fi
}

expect_fail() {
    local dir="$1"
    local out_dir="$2"
    shift 2
    if run_gate "${dir}" "${out_dir}" "$@"; then
        printf 'expected runtime gate to fail in %s\n' "${dir}" >&2
        exit 1
    fi
}

repo1="$(init_fake_repo case1)"
clean_volume="${repo1}/clean-volume-proof.json"
preserved_volume="${repo1}/preserved-volume-proof.json"
write_volume_proof "${clean_volume}" clean
write_volume_proof "${preserved_volume}" preserved
clean_out="${repo1}/_evidence_clean"
expect_pass "${repo1}" "${clean_out}" \
    --phases state,runtime \
    --runtime-run-kind clean \
    --runtime-volume-proof "${clean_volume}" \
    --image-tag-candidate v0.0.3-test \
    --api-base-url "http://127.0.0.1:18080" \
    --pprof-base-url "http://127.0.0.1:16060"
clean_ev="${clean_out}/evidence.json"
jq -e '.runtime.status == "pass" and .runtime.run_kind == "clean"' "${clean_ev}" >/dev/null \
    || { printf 'clean runtime evidence missing run_kind/status\n' >&2; exit 1; }
jq -e '.runtime.index_status.queue.dead_letter == 0 and .runtime.queue_terminal_ok == true' "${clean_ev}" >/dev/null \
    || { printf 'runtime queue fields were not normalized\n' >&2; exit 1; }
jq -e '.runtime.docker_stats_status == "captured" and .runtime.pprof_status == "reachable"' "${clean_ev}" >/dev/null \
    || { printf 'runtime CPU/memory or pprof status not captured\n' >&2; exit 1; }

preserved_out="${repo1}/_evidence_preserved"
expect_pass "${repo1}" "${preserved_out}" \
    --phases state,runtime \
    --runtime-run-kind preserved \
    --previous-runtime-evidence "${clean_ev}" \
    --runtime-volume-proof "${preserved_volume}" \
    --image-tag-candidate v0.0.3-test \
    --api-base-url "http://127.0.0.1:18080" \
    --pprof-base-url "http://127.0.0.1:16060"
preserved_ev="${preserved_out}/evidence.json"
jq -e '.runtime.run_kind == "preserved" and .runtime.previous_runtime.run_kind == "clean"' "${preserved_ev}" >/dev/null \
    || { printf 'preserved runtime did not attach prior clean evidence\n' >&2; exit 1; }
jq -e '.runtime.volume_proof.run_kind == "preserved" and .runtime.volume_proof.restart_without_prune == true' "${preserved_ev}" >/dev/null \
    || { printf 'preserved runtime did not attach same-volume proof\n' >&2; exit 1; }

repo2="$(init_fake_repo case2)"
expect_fail "${repo2}" "${repo2}/_evidence_missing_kind" \
    --phases state,runtime \
    --image-tag-candidate v0.0.3-test \
    --api-base-url "http://127.0.0.1:18080"
rg --fixed-strings --quiet -- "runtime phase requires --runtime-run-kind" "${repo2}/_gate.err" \
    || { printf 'missing runtime-run-kind did not fail closed\n' >&2; exit 1; }

repo3="$(init_fake_repo case3)"
bad_prev="${repo3}/bad-previous.json"
cat >"${bad_prev}" <<'JSON'
{"runtime":{"status":"pass","run_kind":"preserved"}}
JSON
expect_fail "${repo3}" "${repo3}/_evidence_bad_previous" \
    --phases state,runtime \
    --runtime-run-kind preserved \
    --previous-runtime-evidence "${bad_prev}" \
    --image-tag-candidate v0.0.3-test \
    --api-base-url "http://127.0.0.1:18080"
rg --fixed-strings --quiet -- "previous runtime evidence" "${repo3}/_gate.err" \
    || { printf 'bad previous runtime evidence did not fail closed\n' >&2; exit 1; }

repo4="$(init_fake_repo case4)"
ESHU_FAKE_DOCKER_STATS_EMPTY=1 \
    expect_fail "${repo4}" "${repo4}/_evidence_empty_stats" \
        --phases state,runtime \
        --runtime-run-kind clean \
        --image-tag-candidate v0.0.3-test \
        --api-base-url "http://127.0.0.1:18080"
empty_stats_ev="${repo4}/_evidence_empty_stats/evidence.json"
jq -e '.runtime.docker_stats_status == "missing" and .pass == false' "${empty_stats_ev}" >/dev/null \
    || { printf 'empty docker stats did not fail and record missing status\n' >&2; exit 1; }

repo5="$(init_fake_repo case5)"
ESHU_FAKE_DOCKER_STATS_NO_MEM=1 \
    expect_fail "${repo5}" "${repo5}/_evidence_invalid_stats" \
        --phases state,runtime \
        --runtime-run-kind clean \
        --image-tag-candidate v0.0.3-test \
        --api-base-url "http://127.0.0.1:18080"
invalid_stats_ev="${repo5}/_evidence_invalid_stats/evidence.json"
jq -e '.runtime.docker_stats_status == "invalid" and .pass == false' "${invalid_stats_ev}" >/dev/null \
    || { printf 'invalid docker stats did not fail and record invalid status\n' >&2; exit 1; }

repo6="$(init_fake_repo case6)"
ESHU_FAKE_INDEX_STATUS_NON_TERMINAL=1 \
    expect_fail "${repo6}" "${repo6}/_evidence_nonterminal_queue" \
        --phases state,runtime \
        --runtime-run-kind clean \
        --image-tag-candidate v0.0.3-test \
        --api-base-url "http://127.0.0.1:18080"
nonterminal_ev="${repo6}/_evidence_nonterminal_queue/evidence.json"
jq -e '.runtime.queue_terminal_ok == false and .pass == false' "${nonterminal_ev}" >/dev/null \
    || { printf 'non-terminal runtime queue did not fail closed\n' >&2; exit 1; }

repo7="$(init_fake_repo case7)"
private_volume="${repo7}/private-volume-proof.json"
cat >"${private_volume}" <<'JSON'
{
  "schema_version": 1,
  "proof_id": "private-volume-proof-v1",
  "run_kind": "clean",
  "clean_volume_state": "reset_before_run",
  "host_path": "opaque-volume-host-path",
  "backing_stores": {
    "nornicdb_data": {"status": "pass", "before": "absent", "after": "present"},
    "postgres_data": {"status": "pass", "before": "absent", "after": "present"},
    "eshu_data": {"status": "pass", "before": "absent", "after": "present"}
  }
}
JSON
expect_fail "${repo7}" "${repo7}/_evidence_private_volume" \
    --phases state,runtime \
    --runtime-run-kind clean \
    --runtime-volume-proof "${private_volume}" \
    --image-tag-candidate v0.0.3-test \
    --api-base-url "http://127.0.0.1:18080" \
    --pprof-base-url "http://127.0.0.1:16060"
rg --fixed-strings --quiet -- "runtime-volume-proof looks like private data" "${repo7}/_gate.err" \
    || { printf 'private runtime volume proof did not fail closed\n' >&2; exit 1; }

repo8="$(init_fake_repo case8)"
sanitized_volume="${repo8}/clean-volume-proof.json"
write_volume_proof "${sanitized_volume}" clean
sanitized_out="${repo8}/_evidence_sanitized"
ESHU_FAKE_RUNTIME_PRIVATE_READBACK=1 \
    expect_pass "${repo8}" "${sanitized_out}" \
        --phases state,runtime \
        --runtime-run-kind clean \
        --runtime-volume-proof "${sanitized_volume}" \
        --image-tag-candidate v0.0.3-test \
        --api-base-url "http://127.0.0.1:18080" \
        --pprof-base-url "http://127.0.0.1:16060"
if rg --quiet -- 'example/private-service|private-package|https://example.invalid|ghp_exampletoken|/Users/example' \
    "${sanitized_out}/runtime-readback" "${sanitized_out}/evidence.json"; then
    printf 'runtime readback leaked private API response data\n' >&2
    exit 1
fi

printf 'security-intelligence release gate runtime tests passed\n'
