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

    cat >"${dir}/_bin/kubectl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
args="$*"
case "${args}" in
  "-n eshu get pods -o json")
    cat <<'JSON'
{"items":[
  {"metadata":{"name":"eshu-api-6bd9f-private","labels":{"app.kubernetes.io/name":"eshu","app.kubernetes.io/instance":"private-release","app.kubernetes.io/component":"api"}},"spec":{"nodeName":"node.private.internal","containers":[{"name":"api","image":"registry.private.example.com/eshu/api:dev","resources":{"requests":{"cpu":"250m","memory":"256Mi"},"limits":{"cpu":"1","memory":"1Gi"}}}]},"status":{"phase":"Running","podIP":"10.42.0.15","hostIP":"172.20.1.11","containerStatuses":[{"name":"api","ready":true,"restartCount":1}]}},
  {"metadata":{"name":"eshu-reducer-77dd-private","labels":{"app.kubernetes.io/name":"eshu","app.kubernetes.io/instance":"private-release","app.kubernetes.io/component":"reducer"}},"spec":{"nodeName":"node.private.internal","containers":[{"name":"reducer","resources":{"requests":{"cpu":"500m","memory":"512Mi"},"limits":{"cpu":"2","memory":"2Gi"}}}]},"status":{"phase":"Running","podIP":"10.42.0.16","hostIP":"172.20.1.11","containerStatuses":[{"name":"reducer","ready":true,"restartCount":0}]}}
]}
JSON
    ;;
  "-n eshu top pods --no-headers")
    printf 'eshu-api-6bd9f-private 12m 190Mi\n'
    printf 'eshu-reducer-77dd-private 80m 512Mi\n'
    ;;
  "-n eshu logs eshu-api-6bd9f-private --all-containers --tail=200")
    printf 'level=info repository=private/repo package_name=private-package provider_url=https://provider.private.example.com/path token=ghp_abcdef path=/Users/alice/private/repo host=api.private.example.com ip=10.42.0.15 PASSWORD: hunter2 AWS_SECRET_ACCESS_KEY: aws-secret apiKey: api-key-secret client_secret: client-secret Authorization: Bearer bearer-secret role_arn=arn:aws:iam::123456789012:role/private-role\n'
    ;;
  "-n eshu logs eshu-reducer-77dd-private --all-containers --tail=200")
    printf '{"level":"warn","repo":"private/repo","package":"private-package","url":"https://provider.private.example.com/path","message":"retrying queue item","host":"reducer.private.example.com","path":"/home/alice/private/repo","PASSWORD":"hunter2","AWS_SECRET_ACCESS_KEY":"aws-secret","apiKey":"api-key-secret","client_secret":"client-secret","authorization":"Bearer bearer-secret","role_arn":"arn:aws:iam::123456789012:role/private-role"}\n'
    ;;
  *)
    printf 'unexpected kubectl args: %s\n' "${args}" >&2
    exit 1
    ;;
esac
SH
    chmod +x "${dir}/_bin/kubectl"

    cat >"${dir}/_bin/helm" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
if [ "$*" != "get values eshu -n eshu" ]; then
    printf 'unexpected helm args: %s\n' "$*" >&2
    exit 1
fi
cat <<'YAML'
api:
  env:
    PROVIDER_URL: https://provider.private.example.com/path
    TOKEN: github_pat_secret
    WORKSPACE: /Users/alice/private/repo
    PASSWORD: hunter2
    AWS_SECRET_ACCESS_KEY: aws-secret
    apiKey: api-key-secret
    client_secret: client-secret
    Authorization: Bearer bearer-secret
    ROLE_ARN: arn:aws:iam::123456789012:role/private-role
    ACCOUNT_ID: "123456789012"
YAML
SH
    chmod +x "${dir}/_bin/helm"

    cat >"${dir}/_bin/curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
url="${@: -1}"
case "${url}" in
  */debug/pprof/)
    printf 'pprof index\n'
    ;;
  */admin/status?format=json)
    if [ "${ESHU_FAKE_QUEUE_STATE:-terminal}" = "nonterminal" ]; then
        cat <<'JSON'
{"queue":{"outstanding":3,"pending":2,"in_flight":1,"retrying":1,"failed":0,"dead_letter":0,"overdue_claims":0},"retry_policies":[{"stage":"reducer","retry_delay":"1m"}],"vulnerability_sources":[{"source":"osv","terminal_status":"running","result_count":4,"warning_count":0}]}
JSON
        exit 0
    fi
    cat <<'JSON'
{"queue":{"outstanding":0,"pending":0,"in_flight":0,"retrying":0,"failed":0,"dead_letter":0,"overdue_claims":0},"retry_policies":[{"stage":"reducer","retry_delay":"1m"}],"vulnerability_sources":[{"source":"osv","terminal_status":"succeeded","result_count":4,"warning_count":0}]}
JSON
    ;;
  */api/v0/index-status)
    if [ "${ESHU_FAKE_QUEUE_STATE:-terminal}" = "nonterminal" ]; then
        cat <<'JSON'
{"status":"progressing","queue":{"outstanding":3,"pending":2,"in_flight":1,"retrying":1,"failed":0,"dead_letter":0},"health":{"state":"progressing"}}
JSON
        exit 0
    fi
    cat <<'JSON'
{"status":"healthy","queue":{"outstanding":0,"pending":0,"in_flight":0,"retrying":0,"failed":0,"dead_letter":0},"health":{"state":"healthy"}}
JSON
    ;;
  *)
    printf 'unexpected curl url: %s\n' "${url}" >&2
    exit 1
    ;;
esac
SH
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

printf 'security-intelligence release gate k8s tests passed\n'
