#!/usr/bin/env bash
# Test harness for the proof-matrix phase in security_intelligence_release_gate.sh.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="${repo_root}/scripts/security_intelligence_release_gate.sh"

if [ ! -x "${gate}" ]; then
    printf 'gate script missing or not executable: %s\n' "${gate}" >&2
    exit 1
fi

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

init_fake_repo() {
    local name="$1"
    local dir="${tmp_root}/${name}"
    mkdir -p "${dir}"
    git -C "${dir}" init -q
    git -C "${dir}" config user.email "test@example.invalid"
    git -C "${dir}" config user.name "Eshu Proof Matrix Test"
    touch "${dir}/README.md"
    git -C "${dir}" add README.md
    git -C "${dir}" commit -q -m initial
    printf '%s\n' "${dir}"
}

write_valid_matrix() {
    local path="$1"
    # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
    # the entire heredoc body to a pipe before forking the reader, and
    # macOS's 512-byte pipe buffer deadlocks on any body over that size
    # (#5074).
    cat "${repo_root}/scripts/lib/test-security_intelligence_release_gate_proof_matrix-valid-matrix.json" >"${path}"
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
        printf 'expected proof-matrix gate to pass in %s\n' "${dir}" >&2
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
        printf 'expected proof-matrix gate to fail in %s\n' "${dir}" >&2
        printf '%s\n' '--- stdout ---' >&2
        sed -n '1,200p' "${dir}/_gate.out" >&2
        exit 1
    fi
}

evidence_json() {
    local dir="$1"
    printf '%s/_evidence/evidence.json' "${dir}"
}

# --- Test 1: valid proof matrix records only public-safe aggregate evidence.
repo1="$(init_fake_repo case1)"
matrix1="${repo1}/proof-matrix.json"
write_valid_matrix "${matrix1}"
expect_pass "${repo1}" --phases proof-matrix --proof-matrix "${matrix1}"
evidence1="$(evidence_json "${repo1}")"
jq -e '.proof_matrix.status == "pass"' "${evidence1}" >/dev/null \
    || { printf 'proof_matrix.status was not pass\n' >&2; exit 1; }
jq -e '.proof_matrix.repository_count == 24' "${evidence1}" >/dev/null \
    || { printf 'proof_matrix.repository_count mismatch\n' >&2; exit 1; }
jq -e '.proof_matrix.ecosystems.npm.affected_rows == 1' "${evidence1}" >/dev/null \
    || { printf 'proof_matrix ecosystem counts missing\n' >&2; exit 1; }
jq -e '.proof_matrix.mismatch_classes.version_matching == 1' "${evidence1}" >/dev/null \
    || { printf 'proof_matrix mismatch classes missing\n' >&2; exit 1; }
jq -e '.proof_matrix.evidence_families.relationship_evidence.evidence_rows == 2' "${evidence1}" >/dev/null \
    || { printf 'proof_matrix relationship evidence family missing\n' >&2; exit 1; }
jq -e '.proof_matrix.follow_up_issues.total == 2' "${evidence1}" >/dev/null \
    || { printf 'proof_matrix follow-up issue summary missing\n' >&2; exit 1; }
jq -e '.proof_matrix | has("repository") | not' "${evidence1}" >/dev/null \
    || { printf 'proof_matrix leaked repository field\n' >&2; exit 1; }

# --- Test 2: every supported ecosystem must be represented or classified.
repo2="$(init_fake_repo case2)"
matrix2="${repo2}/proof-matrix.json"
write_valid_matrix "${matrix2}"
jq 'del(.ecosystems.nuget)' "${matrix2}" >"${matrix2}.tmp"
mv "${matrix2}.tmp" "${matrix2}"
expect_fail "${repo2}" --phases proof-matrix --proof-matrix "${matrix2}"
rg --fixed-strings --quiet -- "required ecosystem coverage" "${repo2}/_gate.err" \
    || { printf 'expected required ecosystem coverage failure\n' >&2; exit 1; }

# --- Test 2b: durable relationship evidence must be represented or classified.
repo2b="$(init_fake_repo case2b)"
matrix2b="${repo2b}/proof-matrix.json"
write_valid_matrix "${matrix2b}"
jq 'del(.evidence_families.relationship_evidence)' "${matrix2b}" >"${matrix2b}.tmp"
mv "${matrix2b}.tmp" "${matrix2b}"
expect_fail "${repo2b}" --phases proof-matrix --proof-matrix "${matrix2b}"
rg --fixed-strings --quiet -- "required ecosystem coverage" "${repo2b}/_gate.err" \
    || { printf 'expected required relationship evidence failure\n' >&2; exit 1; }

# --- Test 3: private-looking repository/package payloads are rejected.
repo3="$(init_fake_repo case3)"
matrix3="${repo3}/proof-matrix.json"
write_valid_matrix "${matrix3}"
jq '.repository = "private-owner/private-repo" | .package_name = "private-package"' \
    "${matrix3}" >"${matrix3}.tmp"
mv "${matrix3}.tmp" "${matrix3}"
expect_fail "${repo3}" --phases proof-matrix --proof-matrix "${matrix3}"
rg --fixed-strings --quiet -- "private data" "${repo3}/_gate.err" \
    || { printf 'expected private-data failure\n' >&2; exit 1; }

# --- Test 4: nonzero mismatch classes require public follow-up issues.
repo4="$(init_fake_repo case4)"
matrix4="${repo4}/proof-matrix.json"
write_valid_matrix "${matrix4}"
jq 'del(.follow_up_issues)' "${matrix4}" >"${matrix4}.tmp"
mv "${matrix4}.tmp" "${matrix4}"
expect_fail "${repo4}" --phases proof-matrix --proof-matrix "${matrix4}"
rg --fixed-strings --quiet -- "follow-up issue" "${repo4}/_gate.err" \
    || { printf 'expected follow-up issue failure\n' >&2; exit 1; }

# --- Test 5: release proof matrix must include captured CPU/memory, pprof, and logs.
repo5="$(init_fake_repo case5)"
matrix5="${repo5}/proof-matrix.json"
write_valid_matrix "${matrix5}"
jq '.readback.cpu_memory_snapshot = "not_captured" | .readback.pprof_status = "unchecked" | .readback.logs_status = "not_captured"' \
    "${matrix5}" >"${matrix5}.tmp"
mv "${matrix5}.tmp" "${matrix5}"
expect_fail "${repo5}" --phases proof-matrix --proof-matrix "${matrix5}"
rg --fixed-strings --quiet -- "queue-zero readback" "${repo5}/_gate.err" \
    || { printf 'expected readback evidence failure\n' >&2; exit 1; }

printf 'security-intelligence proof-matrix gate tests passed\n'
