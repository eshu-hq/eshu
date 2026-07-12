#!/usr/bin/env bash
# Focused Kubernetes evidence tests for the security intelligence release gate.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="${repo_root}/scripts/security_intelligence_release_gate.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

init_fake_repo() {
    local dir="${tmp_root}/repo"
    mkdir -p "${dir}/deploy/helm/eshu" "${dir}/schema/data-plane/postgres"
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
    image: timothyswt/nornicdb-cpu-bge:v9.9.9@sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789
YAML

    printf 'CREATE TABLE t (id int);\n' >"${dir}/schema/data-plane/postgres/001_table.sql"
    git -C "${dir}" add -A
    git -C "${dir}" commit -q -m initial
    printf '%s\n' "${dir}"
}

install_fake_cluster_tools() {
    local dir="$1"
    mkdir -p "${dir}/_bin"

    # Each fake tool implementation lives in a sibling data file, not a
    # heredoc: Homebrew bash >= 5.1 writes an entire heredoc body to a pipe
    # before forking the reader, and macOS's 512-byte pipe buffer deadlocks
    # on these outer bodies (kubectl 4334B, helm 1173B, curl 1399B) (#5074).
    # None of the three has expansion (all were quoted <<'SH'), so copying
    # each file is behavior-identical. The kubectl fixture's own inner pods
    # (1046B) and servicemonitors (557B) heredocs, and the helm fixture's
    # own inner manifest heredoc (516B), are #5074 NESTED-heredoc cases --
    # they re-trip the deadlock when the fixture itself runs as a
    # subprocess -- so those three are converted to printf inside their
    # lib files too. curl's inner heredocs all stay under 512 bytes and are
    # left as heredocs.
    cat "${repo_root}/scripts/lib/test-security_intelligence_release_gate_k8s-fake-kubectl.sh" >"${dir}/_bin/kubectl"
    chmod +x "${dir}/_bin/kubectl"

    cat "${repo_root}/scripts/lib/test-security_intelligence_release_gate_k8s-fake-helm.sh" >"${dir}/_bin/helm"
    chmod +x "${dir}/_bin/helm"

    cat "${repo_root}/scripts/lib/test-security_intelligence_release_gate_k8s-fake-curl.sh" >"${dir}/_bin/curl"
    chmod +x "${dir}/_bin/curl"
}

repo="$(init_fake_repo)"
install_fake_cluster_tools "${repo}"
out_dir="${repo}/_evidence"

PATH="${repo}/_bin:${PATH}" \
    ESHU_RELEASE_GATE_REPO_ROOT="${repo}" \
    ESHU_RELEASE_GATE_SKIP_GO_TESTS=1 \
    "${gate}" \
        --out-dir "${out_dir}" \
        --phases state,k8s \
        --image-tag-candidate v0.0.3-test \
        --api-base-url "http://127.0.0.1:8080" \
        --pprof-base-url "http://127.0.0.1:6060" \
        --k8s-namespace eshu \
        --helm-release eshu \
        >"${repo}/_gate.out" 2>"${repo}/_gate.err"

ev="${out_dir}/evidence.json"
jq -e '.pass == true' "${ev}" >/dev/null \
    || { printf 'expected k8s gate to pass with complete fake evidence\n' >&2; exit 1; }
jq -e '.k8s.status == "pass"' "${ev}" >/dev/null \
    || { printf 'k8s status was not pass\n' >&2; exit 1; }
jq -e '.k8s.pprof_status == "reachable"' "${ev}" >/dev/null \
    || { printf 'k8s pprof reachability was not recorded\n' >&2; exit 1; }
jq -e '.k8s.logs_ok == true and .k8s.logs_captured == 2' "${ev}" >/dev/null \
    || { printf 'sanitized pod log capture was not recorded\n' >&2; exit 1; }
jq -e '.k8s.queue_readback_ok == true and .k8s.queue_retrying == 0 and .k8s.queue_dead_letter == 0' "${ev}" >/dev/null \
    || { printf 'queue/admin readback summary was not recorded\n' >&2; exit 1; }
jq -e '.k8s.resource_snapshot_ok == true and .k8s.resource_snapshot_file == "k8s/resource-snapshot.txt"' "${ev}" >/dev/null \
    || { printf 'resource snapshot summary was not recorded\n' >&2; exit 1; }
jq -e '
    .k8s.service_monitor_ok == true and
    .k8s.network_policy_ok == true and
    .k8s.pdb_ok == true and
    .k8s.schema_bootstrap_job_ok == true and
    .k8s.helm_manifest_ok == true and
    .k8s.service_monitor_count >= 1 and
    .k8s.network_policy_count >= 1 and
    .k8s.pdb_count >= 1 and
    .k8s.schema_bootstrap_job_count >= 1
' "${ev}" >/dev/null \
    || { printf 'k8s rollout shape evidence was not recorded\n' >&2; exit 1; }
jq -e '.k8s.service_monitor_file == "k8s/servicemonitors-summary.json" and .k8s.helm_manifest_file == "k8s/helm-manifest.sanitized.yaml"' "${ev}" >/dev/null \
    || { printf 'k8s rollout evidence file refs were not recorded\n' >&2; exit 1; }

rg --fixed-strings --quiet -- '[redacted-url]' "${out_dir}/k8s/helm-values.sanitized.yaml" \
    || { printf 'expected helm values to be sanitized\n' >&2; exit 1; }
rg --fixed-strings --quiet -- '[redacted-token]' "${out_dir}/k8s/helm-values.sanitized.yaml" \
    || { printf 'expected helm token to be sanitized\n' >&2; exit 1; }
if rg --fixed-strings --quiet -- "${repo}" "${ev}" "${out_dir}/evidence.md"; then
    printf 'public evidence leaked raw repo root path: %s\n' "${repo}" >&2
    exit 1
fi

for forbidden in \
    'private/repo' \
    'private-package' \
    'https://provider.private.example.com' \
    'ghp_abcdef' \
    'github_pat_secret' \
    'hunter2' \
    'aws-secret' \
    'api-key-secret' \
    'client-secret' \
    'bearer-secret' \
    'arn:aws:iam::123456789012:role/private-role' \
    '123456789012' \
    '10.42.0.15' \
    '172.20.1.11' \
    'private.example.com' \
    '/Users/alice/private/repo' \
    '/home/alice/private/repo'
do
    if rg --fixed-strings --quiet -- "${forbidden}" "${out_dir}/k8s" "${ev}"; then
        printf 'public k8s evidence leaked forbidden value: %s\n' "${forbidden}" >&2
        exit 1
    fi
done

nonterminal_out_dir="${repo}/_evidence_nonterminal"
if PATH="${repo}/_bin:${PATH}" \
    ESHU_RELEASE_GATE_REPO_ROOT="${repo}" \
    ESHU_RELEASE_GATE_SKIP_GO_TESTS=1 \
    ESHU_FAKE_QUEUE_STATE=nonterminal \
    "${gate}" \
        --out-dir "${nonterminal_out_dir}" \
        --phases state,k8s \
        --image-tag-candidate v0.0.3-test \
        --api-base-url "http://127.0.0.1:8080" \
        --pprof-base-url "http://127.0.0.1:6060" \
        --k8s-namespace eshu \
        --helm-release eshu \
        >"${repo}/_gate_nonterminal.out" 2>"${repo}/_gate_nonterminal.err"
then
    printf 'expected k8s gate to fail with non-terminal queue readback\n' >&2
    exit 1
fi
nonterminal_ev="${nonterminal_out_dir}/evidence.json"
jq -e '.pass == false and .k8s.status == "fail"' "${nonterminal_ev}" >/dev/null \
    || { printf 'non-terminal queue did not fail the k8s phase\n' >&2; exit 1; }
jq -e '.k8s.queue_terminal_ok == false and .k8s.queue_pending == 2 and .k8s.queue_in_flight == 1 and .k8s.queue_retrying == 1' "${nonterminal_ev}" >/dev/null \
    || { printf 'non-terminal queue counts were not recorded\n' >&2; exit 1; }

missing_monitor_out_dir="${repo}/_evidence_missing_monitor"
if PATH="${repo}/_bin:${PATH}" \
    ESHU_RELEASE_GATE_REPO_ROOT="${repo}" \
    ESHU_RELEASE_GATE_SKIP_GO_TESTS=1 \
    ESHU_FAKE_K8S_NO_SERVICE_MONITOR=1 \
    "${gate}" \
        --out-dir "${missing_monitor_out_dir}" \
        --phases state,k8s \
        --image-tag-candidate v0.0.3-test \
        --api-base-url "http://127.0.0.1:8080" \
        --pprof-base-url "http://127.0.0.1:6060" \
        --k8s-namespace eshu \
        --helm-release eshu \
        >"${repo}/_gate_missing_monitor.out" 2>"${repo}/_gate_missing_monitor.err"
then
    printf 'expected k8s gate to fail when ServiceMonitor evidence is missing\n' >&2
    exit 1
fi
missing_monitor_ev="${missing_monitor_out_dir}/evidence.json"
jq -e '.pass == false and .k8s.service_monitor_ok == false and .k8s.service_monitor_count == 0' "${missing_monitor_ev}" >/dev/null \
    || { printf 'missing ServiceMonitor did not fail the k8s phase\n' >&2; exit 1; }

bad_bootstrap_out_dir="${repo}/_evidence_bad_bootstrap"
if PATH="${repo}/_bin:${PATH}" \
    ESHU_RELEASE_GATE_REPO_ROOT="${repo}" \
    ESHU_RELEASE_GATE_SKIP_GO_TESTS=1 \
    ESHU_FAKE_K8S_BAD_BOOTSTRAP=1 \
    "${gate}" \
        --out-dir "${bad_bootstrap_out_dir}" \
        --phases state,k8s \
        --image-tag-candidate v0.0.3-test \
        --api-base-url "http://127.0.0.1:8080" \
        --pprof-base-url "http://127.0.0.1:6060" \
        --k8s-namespace eshu \
        --helm-release eshu \
        >"${repo}/_gate_bad_bootstrap.out" 2>"${repo}/_gate_bad_bootstrap.err"
then
    printf 'expected k8s gate to fail when schema bootstrap job is degraded\n' >&2
    exit 1
fi
bad_bootstrap_ev="${bad_bootstrap_out_dir}/evidence.json"
jq -e '.pass == false and .k8s.schema_bootstrap_job_ok == false and .k8s.schema_bootstrap_failed == 1' "${bad_bootstrap_ev}" >/dev/null \
    || { printf 'degraded schema bootstrap job did not fail the k8s phase\n' >&2; exit 1; }

printf 'security-intelligence release gate k8s tests passed\n'
