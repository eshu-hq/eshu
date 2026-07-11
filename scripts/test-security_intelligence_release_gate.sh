#!/usr/bin/env bash
# Test harness for scripts/security_intelligence_release_gate.sh.
#
# Exercises the offline phases (state, focused, fixtures, provider) against a
# synthesized repo so the harness itself is verified without standing up the
# remote Compose stack or a Kubernetes cluster.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="${repo_root}/scripts/security_intelligence_release_gate.sh"

if [ ! -x "${gate}" ]; then
    printf 'gate script missing or not executable: %s\n' "${gate}" >&2
    exit 1
fi

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

# init_fake_repo $name [--scanner-worker] [--with-private]
init_fake_repo() {
    local name="$1"
    shift
    local with_scanner=0
    local with_private=0
    while [ $# -gt 0 ]; do
        case "$1" in
            --scanner-worker) with_scanner=1 ;;
            --with-private) with_private=1 ;;
        esac
        shift
    done

    local dir="${tmp_root}/${name}"
    mkdir -p "${dir}/deploy/helm/eshu" "${dir}/schema/data-plane/postgres" "${dir}/docs"
    git -C "${dir}" init -q
    git -C "${dir}" config user.email "test@example.invalid"
    git -C "${dir}" config user.name "Eshu Release Gate Test"

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
    image: ${NORNICDB_IMAGE:-timothyswt/nornicdb-cpu-bge:v9.9.9@sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789}
YAML

    if [ "${with_scanner}" -eq 1 ]; then
        # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1
        # writes the entire heredoc body to a pipe before forking the reader,
        # and macOS's 512-byte pipe buffer deadlocks on any body over that
        # size (#5074).
        cat "${repo_root}/scripts/lib/test-security_intelligence_release_gate-docker-compose-remote-e2e.yaml" \
            >"${dir}/docker-compose.remote-e2e.yaml"
    fi

    for i in 001 002 003 004; do
        printf 'CREATE TABLE t_%s (id int);\n' "${i}" >"${dir}/schema/data-plane/postgres/${i}_table.sql"
    done

    # Stub the parity verifier so the fixtures phase has something to call. The
    # real verifier needs the fixtures tree; for the synthesized repo we just
    # need an executable that exits 0.
    mkdir -p "${dir}/scripts"
    cat >"${dir}/scripts/verify_vulnerability_parity_fixtures.sh" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
    chmod +x "${dir}/scripts/verify_vulnerability_parity_fixtures.sh"

    if [ "${with_private}" -eq 1 ]; then
        printf '%s\n' '{"aggregate_class":"matched","count":12,"package_name":"private-pkg-name","alert_url":"https://github.com/myorg/myrepo/security/dependabot/1"}' >"${dir}/private-compare.json"
    fi

    git -C "${dir}" add -A
    git -C "${dir}" commit -q -m initial

    printf '%s\n' "${dir}"
}

run_gate() {
    local dir="$1"
    shift
    local out_dir="${dir}/_evidence"
    ESHU_RELEASE_GATE_REPO_ROOT="${dir}" \
        ESHU_RELEASE_GATE_SKIP_GO_TESTS=1 \
        "${gate}" --out-dir "${out_dir}" "$@" >"${dir}/_gate.out" 2>"${dir}/_gate.err"
}

expect_pass() {
    local dir="$1"
    shift
    if ! run_gate "${dir}" "$@"; then
        printf 'expected gate to pass in %s\n' "${dir}" >&2
        printf '%s\n' '--- stderr ---' >&2
        sed -n '1,200p' "${dir}/_gate.err" >&2
        printf '%s\n' '--- stdout ---' >&2
        sed -n '1,200p' "${dir}/_gate.out" >&2
        exit 1
    fi
}

expect_fail() {
    local dir="$1"
    shift
    if run_gate "${dir}" "$@"; then
        printf 'expected gate to fail in %s\n' "${dir}" >&2
        printf '%s\n' '--- stderr ---' >&2
        sed -n '1,200p' "${dir}/_gate.err" >&2
        printf '%s\n' '--- stdout ---' >&2
        sed -n '1,200p' "${dir}/_gate.out" >&2
        exit 1
    fi
}

evidence_json() {
    local dir="$1"
    printf '%s/_evidence/evidence.json' "${dir}"
}

# --- Test 1: offline default phases produce a complete state envelope.
repo1="$(init_fake_repo case1 --scanner-worker)"
expect_pass "${repo1}" --image-tag-candidate "v0.0.3-pre-release-9"
state_file="$(evidence_json "${repo1}")"
[ -s "${state_file}" ] || { printf 'evidence.json was empty in case1\n' >&2; exit 1; }
jq -e '.phases | index("state")' "${state_file}" >/dev/null \
    || { printf 'state phase missing in case1 evidence\n' >&2; exit 1; }
jq -e '.state.git_commit and (.state.git_commit|length>=40)' "${state_file}" >/dev/null \
    || { printf 'state.git_commit missing or short\n' >&2; exit 1; }
jq -e '.state.helm_chart_version == "0.0.3-pre-release-9"' "${state_file}" >/dev/null \
    || { printf 'state.helm_chart_version mismatch\n' >&2; exit 1; }
jq -e '.state.helm_app_version == "v0.0.3-pre-release-9"' "${state_file}" >/dev/null \
    || { printf 'state.helm_app_version mismatch\n' >&2; exit 1; }
jq -e '.state.image_tag_candidate == "v0.0.3-pre-release-9"' "${state_file}" >/dev/null \
    || { printf 'state.image_tag_candidate missing\n' >&2; exit 1; }
jq -e '.state.nornicdb_image | test("timothyswt/nornicdb-cpu-bge:v9\\.9\\.9@sha256:[0-9a-f]{64}$")' "${state_file}" >/dev/null \
    || { printf 'state.nornicdb_image not captured\n' >&2; exit 1; }
jq -e '.state.nornicdb_digest | test("^sha256:[0-9a-f]{64}$")' "${state_file}" >/dev/null \
    || { printf 'state.nornicdb_digest missing\n' >&2; exit 1; }
jq -e '.state.schema_migration_count == 4' "${state_file}" >/dev/null \
    || { printf 'state.schema_migration_count wrong\n' >&2; exit 1; }
jq -e '.state.schema_latest_migration == "004_table.sql"' "${state_file}" >/dev/null \
    || { printf 'state.schema_latest_migration wrong\n' >&2; exit 1; }
jq -e '.state.remote_e2e_services | index("scanner-worker")' "${state_file}" >/dev/null \
    || { printf 'state.remote_e2e_services missing scanner-worker\n' >&2; exit 1; }
jq -e '.state.remote_e2e_services | index("collector-vulnerability-intelligence")' "${state_file}" >/dev/null \
    || { printf 'state.remote_e2e_services missing vulnerability-intelligence\n' >&2; exit 1; }
jq -e '.state.scanner_worker_limits.ESHU_SCANNER_WORKER_MAX_FACTS == "50000"' "${state_file}" >/dev/null \
    || { printf 'state.scanner_worker_limits not captured\n' >&2; exit 1; }
jq -e '.pass == true' "${state_file}" >/dev/null \
    || { printf 'evidence.pass was not true on offline run\n' >&2; exit 1; }
[ -s "${repo1}/_evidence/evidence.md" ] || { printf 'evidence.md missing in case1\n' >&2; exit 1; }
rg --fixed-strings --quiet -- "Security Intelligence Release Gate" "${repo1}/_evidence/evidence.md" \
    || { printf 'evidence.md missing title in case1\n' >&2; exit 1; }
rg --fixed-strings --quiet -- "v0.0.3-pre-release-9" "${repo1}/_evidence/evidence.md" \
    || { printf 'evidence.md missing image tag candidate in case1\n' >&2; exit 1; }

# --- Test 2: unknown phase fails fast with a clear message.
repo2="$(init_fake_repo case2 --scanner-worker)"
expect_fail "${repo2}" --phases bogus
rg --fixed-strings --quiet -- 'unknown phase: bogus' "${repo2}/_gate.err" \
    || { printf 'expected unknown-phase error in case2\n' >&2; exit 1; }

# --- Test 3: provider phase refuses to record private data.
repo3="$(init_fake_repo case3 --scanner-worker --with-private)"
expect_fail "${repo3}" \
    --phases state,provider \
    --image-tag-candidate v0.0.3-test \
    --provider-compare "${repo3}/private-compare.json"
rg --fixed-strings --quiet -- "private data" "${repo3}/_gate.err" \
    || { printf 'expected private-data rejection in case3\n' >&2; exit 1; }

# --- Test 4: provider phase accepts a bounded aggregate-only payload.
repo4="$(init_fake_repo case4 --scanner-worker)"
cat >"${repo4}/aggregate-compare.json" <<'JSON'
{"comparison_id":"synthetic-aggregate","provider":"generic","totals":{"matched":12,"provider_only":3,"eshu_only":1,"fixed_dismissed_mismatch":0,"missing_dependency_evidence":0,"missing_advisory_evidence":0,"missing_sbom_image_evidence":0,"unsupported_ecosystem":0}}
JSON
expect_pass "${repo4}" \
    --phases state,provider \
    --image-tag-candidate v0.0.3-test \
    --provider-compare "${repo4}/aggregate-compare.json"
prov_file="$(evidence_json "${repo4}")"
jq -e '.provider.totals.matched == 12' "${prov_file}" >/dev/null \
    || { printf 'provider.totals.matched not captured\n' >&2; exit 1; }
jq -e '.provider.totals.provider_only == 3' "${prov_file}" >/dev/null \
    || { printf 'provider.totals.provider_only not captured\n' >&2; exit 1; }
jq -e '.provider.comparison_id == "synthetic-aggregate"' "${prov_file}" >/dev/null \
    || { printf 'provider.comparison_id not captured\n' >&2; exit 1; }
jq -e '.provider | has("package_name") | not' "${prov_file}" >/dev/null \
    || { printf 'provider envelope leaked package_name\n' >&2; exit 1; }

# --- Test 5: missing Chart.yaml fails the state phase.
repo5="$(init_fake_repo case5 --scanner-worker)"
rm "${repo5}/deploy/helm/eshu/Chart.yaml"
git -C "${repo5}" add -A
git -C "${repo5}" commit -q -m "drop chart"
expect_fail "${repo5}" --phases state
rg --fixed-strings --quiet -- "Chart.yaml" "${repo5}/_gate.err" \
    || { printf 'expected Chart.yaml error in case5\n' >&2; exit 1; }

# --- Test 6: runtime/k8s phases noop without endpoints/namespace but state
# still passes when explicitly requested as additional phases.
repo6="$(init_fake_repo case6 --scanner-worker)"
expect_fail "${repo6}" --phases state,runtime
rg --fixed-strings --quiet -- "runtime phase requires" "${repo6}/_gate.err" \
    || { printf 'expected runtime requirement message in case6\n' >&2; exit 1; }

# --- Test 7: runtime phase fails closed when endpoints error and writes
# sanitized relative readback references under runtime-readback/.
repo7="$(init_fake_repo case7 --scanner-worker)"
mkdir -p "${repo7}/scripts"
cat >"${repo7}/scripts/verify_remote_e2e_runtime_state.sh" <<'SH'
#!/usr/bin/env bash
exit 0
SH
chmod +x "${repo7}/scripts/verify_remote_e2e_runtime_state.sh"
expect_fail "${repo7}" \
    --phases state,runtime \
    --runtime-run-kind clean \
    --image-tag-candidate v0.0.3-test \
    --api-base-url "http://127.0.0.1:1"
ev7="$(evidence_json "${repo7}")"
jq -e '.pass == false' "${ev7}" >/dev/null \
    || { printf 'runtime phase did not fail closed on unreachable endpoints in case7\n' >&2; exit 1; }
jq -e '.runtime.runtime_state_ok == true' "${ev7}" >/dev/null \
    || { printf 'runtime_state_ok wrong in case7 (fake verifier returned 0)\n' >&2; exit 1; }
jq -e '.failures | map(select(.phase == "runtime")) | length > 0' "${ev7}" >/dev/null \
    || { printf 'runtime failure not recorded in case7 despite endpoint errors\n' >&2; exit 1; }
jq -e '.runtime.endpoints_failed > 0' "${ev7}" >/dev/null \
    || { printf 'runtime.endpoints_failed not surfaced in case7\n' >&2; exit 1; }
[ -d "${repo7}/_evidence/runtime-readback" ] \
    || { printf 'runtime-readback/ directory missing in case7\n' >&2; exit 1; }
rg --files "${repo7}/_evidence/runtime-readback" | rg --fixed-strings --quiet -- '_api_v0_index-status.json' \
    || { printf 'expected index-status readback file under runtime-readback/ in case7\n' >&2; exit 1; }
# No body files should leak outside runtime-readback/ (path-separator bug).
shopt -s nullglob
leaked_files=("${repo7}/_evidence"/runtime-readback_*)
shopt -u nullglob
leaked="${#leaked_files[@]}"
if [ "${leaked}" != "0" ]; then
    printf 'readback files leaked outside runtime-readback/ (count=%s) in case7\n' "${leaked}" >&2
    exit 1
fi
jq -e '.runtime.pprof_status == "missing"' "${ev7}" >/dev/null \
    || { printf 'pprof missing status not recorded when --pprof-base-url is absent in case7\n' >&2; exit 1; }
jq -e '.runtime.readback["/api/v0/index-status"].body == "runtime-readback/_api_v0_index-status.json"' "${ev7}" >/dev/null \
    || { printf 'runtime readback did not store relative evidence path in case7\n' >&2; exit 1; }

# --- Test 8: missing verify_remote_e2e_runtime_state.sh is recorded as a runtime failure.
repo8="$(init_fake_repo case8 --scanner-worker)"
# Intentionally do not create scripts/verify_remote_e2e_runtime_state.sh
expect_fail "${repo8}" \
    --phases state,runtime \
    --runtime-run-kind clean \
    --image-tag-candidate v0.0.3-test \
    --api-base-url "http://127.0.0.1:1"
ev8="$(evidence_json "${repo8}")"
jq -e '.runtime.runtime_state_ok == false' "${ev8}" >/dev/null \
    || { printf 'missing verifier did not flip runtime_state_ok in case8\n' >&2; exit 1; }
jq -e '.failures | map(select(.phase == "runtime" and (.message | test("verify_remote_e2e_runtime_state.sh")))) | length > 0' "${ev8}" >/dev/null \
    || { printf 'missing verifier did not record a runtime failure in case8\n' >&2; exit 1; }

# --- Test 9: k8s phase records a failure when kubectl exits non-zero.
repo9="$(init_fake_repo case9 --scanner-worker)"
mkdir -p "${repo9}/_bin"
cat >"${repo9}/_bin/kubectl" <<'SH'
#!/usr/bin/env bash
echo "kubectl-test: simulated failure" >&2
exit 1
SH
chmod +x "${repo9}/_bin/kubectl"
saved_path="${PATH}"
PATH="${repo9}/_bin:${PATH}" \
    expect_fail "${repo9}" \
        --phases state,k8s \
        --image-tag-candidate v0.0.3-test \
        --k8s-namespace eshu
PATH="${saved_path}"
ev9="$(evidence_json "${repo9}")"
jq -e '.failures | map(select(.phase == "k8s")) | length > 0' "${ev9}" >/dev/null \
    || { printf 'k8s failure not recorded in case9 when kubectl fails\n' >&2; exit 1; }
jq -e '.pass == false' "${ev9}" >/dev/null \
    || { printf 'gate did not fail closed on kubectl failure in case9\n' >&2; exit 1; }

# --- Test 10: pprof-base-url is probed when provided.
repo10="$(init_fake_repo case10 --scanner-worker)"
mkdir -p "${repo10}/scripts"
cat >"${repo10}/scripts/verify_remote_e2e_runtime_state.sh" <<'SH'
#!/usr/bin/env bash
exit 0
SH
chmod +x "${repo10}/scripts/verify_remote_e2e_runtime_state.sh"
expect_fail "${repo10}" \
    --phases state,runtime \
    --runtime-run-kind clean \
    --image-tag-candidate v0.0.3-test \
    --api-base-url "http://127.0.0.1:1" \
    --pprof-base-url "http://127.0.0.1:1"
ev10="$(evidence_json "${repo10}")"
jq -e '.runtime.pprof_status == "not_reachable"' "${ev10}" >/dev/null \
    || { printf 'pprof probe did not run against --pprof-base-url in case10\n' >&2; exit 1; }

# --- Test 11: api_base_url is normalized so a trailing /api/v0 does not
# double-prefix the documented supply-chain endpoints.
repo11="$(init_fake_repo case11 --scanner-worker)"
mkdir -p "${repo11}/scripts"
cat >"${repo11}/scripts/verify_remote_e2e_runtime_state.sh" <<'SH'
#!/usr/bin/env bash
exit 0
SH
chmod +x "${repo11}/scripts/verify_remote_e2e_runtime_state.sh"
ESHU_RELEASE_GATE_SKIP_GO_TESTS=1 \
    "${gate}" \
    --out-dir "${repo11}/_evidence" \
    --phases state,runtime \
    --runtime-run-kind clean \
    --image-tag-candidate v0.0.3-test \
    --api-base-url "http://127.0.0.1:1/api/v0" \
    >"${repo11}/_gate.out" 2>"${repo11}/_gate.err" \
    && { printf 'expected gate to fail in case11 (endpoints unreachable)\n' >&2; exit 1; } \
    || true
ev11="$(evidence_json "${repo11}")"
jq -e '.runtime.api_base_url == "http://127.0.0.1:1"' "${ev11}" >/dev/null \
    || { printf 'api_base_url not normalized in case11\n' >&2; exit 1; }
jq -e '.runtime.readback | keys | all(. | test("^/api/v0/[^/]+(/[^/]+)*(\\?.+)?$"))' "${ev11}" >/dev/null \
    || { printf 'endpoint keys not anchored at /api/v0/ in case11\n' >&2; exit 1; }
# The endpoints list itself must not double-prefix /api/v0/api/v0/.
jq -e '.runtime.readback | keys | all(. | test("^/api/v0/api/v0") | not)' "${ev11}" >/dev/null \
    || { printf 'endpoint key double-prefixed /api/v0/api/v0 in case11\n' >&2; exit 1; }

# --- Test 12: fixtures phase fails closed when the shell verifier is missing.
repo12="$(init_fake_repo case12 --scanner-worker)"
# init_fake_repo seeds a no-op parity verifier; remove it so we exercise the
# missing-shell-verifier branch.
rm "${repo12}/scripts/verify_vulnerability_parity_fixtures.sh"
ESHU_RELEASE_GATE_REPO_ROOT="${repo12}" \
    ESHU_RELEASE_GATE_SKIP_GO_TESTS=1 \
    ESHU_RELEASE_GATE_FORCE_FIXTURES_GO_OK=1 \
    "${gate}" \
    --out-dir "${repo12}/_evidence" \
    --phases state,fixtures \
    --image-tag-candidate v0.0.3-test \
    >"${repo12}/_gate.out" 2>"${repo12}/_gate.err" \
    && { printf 'expected gate to fail when verify_vulnerability_parity_fixtures.sh is missing in case12\n' >&2; exit 1; } \
    || true
ev12="$(evidence_json "${repo12}")"
jq -e '.fixtures.status == "fail"' "${ev12}" >/dev/null \
    || { printf 'fixtures phase did not fail when verifier missing in case12\n' >&2; exit 1; }
jq -e '.failures | map(select(.phase == "fixtures" and (.message | test("verify_vulnerability_parity_fixtures.sh")))) | length > 0' "${ev12}" >/dev/null \
    || { printf 'missing fixtures verifier did not record a failure in case12\n' >&2; exit 1; }

# --- Test 13: every enabled phase emits a status field so the runbook claim
# is mechanically verifiable.
repo13="$(init_fake_repo case13 --scanner-worker)"
expect_pass "${repo13}" --image-tag-candidate v0.0.3-test
ev13="$(evidence_json "${repo13}")"
for ph in state focused fixtures; do
    jq -e --arg ph "${ph}" '.[$ph].status | IN("pass","fail","skipped")' "${ev13}" >/dev/null \
        || { printf 'phase %s missing status field in case13\n' "${ph}" >&2; exit 1; }
done

# --- Test 14: readback-proof phase accepts public-safe API/MCP/CLI aggregate evidence.
repo14="$(init_fake_repo case14 --scanner-worker)"
cat >"${repo14}/readback-proof.json" <<'JSON'
{
  "schema_version": 1,
  "proof_id": "security-readback-proof-v1",
  "surfaces": {
    "api": {"status": "pass", "checked": 6, "failed": 0},
    "mcp": {"status": "pass", "checked": 4, "failed": 0},
    "cli": {"status": "pass", "checked": 3, "failed": 0}
  },
  "queue": {"retrying": 0, "failed": 0, "dead_letters": 0},
  "transcript_status": "captured"
}
JSON
expect_pass "${repo14}" \
    --phases state,readback-proof \
    --image-tag-candidate v0.0.3-test \
    --readback-proof "${repo14}/readback-proof.json"
ev14="$(evidence_json "${repo14}")"
jq -e '.readback_proof.status == "pass" and .readback_proof.surfaces.mcp.checked == 4' "${ev14}" >/dev/null \
    || { printf 'readback-proof summary did not capture MCP aggregate evidence in case14\n' >&2; exit 1; }
jq -e '.readback_proof.queue.dead_letters == 0 and .readback_proof.transcript_status == "captured"' "${ev14}" >/dev/null \
    || { printf 'readback-proof queue/transcript summary wrong in case14\n' >&2; exit 1; }

# --- Test 15: readback-proof phase rejects private transcript content.
repo15="$(init_fake_repo case15 --scanner-worker)"
cat >"${repo15}/private-readback-proof.json" <<'JSON'
{
  "schema_version": 1,
  "proof_id": "security-readback-proof-v1",
  "surfaces": {
    "api": {"status": "pass", "checked": 6, "failed": 0},
    "mcp": {"status": "pass", "checked": 4, "failed": 0},
    "cli": {"status": "pass", "checked": 3, "failed": 0}
  },
  "queue": {"retrying": 0, "failed": 0, "dead_letters": 0},
  "transcript_status": "captured",
  "repository": "private-org/private-repo"
}
JSON
expect_fail "${repo15}" \
    --phases state,readback-proof \
    --image-tag-candidate v0.0.3-test \
    --readback-proof "${repo15}/private-readback-proof.json"
rg --fixed-strings --quiet -- "readback-proof looks like private data" "${repo15}/_gate.err" \
    || { printf 'private readback proof was not rejected in case15\n' >&2; exit 1; }

# --- Test 16: readback-proof phase fails closed when MCP/CLI evidence is missing.
repo16="$(init_fake_repo case16 --scanner-worker)"
cat >"${repo16}/incomplete-readback-proof.json" <<'JSON'
{
  "schema_version": 1,
  "proof_id": "security-readback-proof-v1",
  "surfaces": {
    "api": {"status": "pass", "checked": 6, "failed": 0},
    "cli": {"status": "pass", "checked": 3, "failed": 0}
  },
  "queue": {"retrying": 0, "failed": 0, "dead_letters": 0},
  "transcript_status": "captured"
}
JSON
expect_fail "${repo16}" \
    --phases state,readback-proof \
    --image-tag-candidate v0.0.3-test \
    --readback-proof "${repo16}/incomplete-readback-proof.json"
rg --fixed-strings --quiet -- "readback-proof does not satisfy API/MCP/CLI" "${repo16}/_gate.err" \
    || { printf 'incomplete readback proof did not fail closed in case16\n' >&2; exit 1; }

printf 'security-intelligence release gate tests passed\n'
