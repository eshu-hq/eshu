#!/usr/bin/env bash
# Security Intelligence Release Gate harness. Aggregates the proofs required
# before cutting the next prerelease image with vulnerability /
# security-intelligence work. The harness never cuts or pushes an image and
# never persists private provider payloads. Runbook + phase contract:
# docs/public/reference/security-intelligence-release-gate.md.

set -euo pipefail

phases_default="state,focused,fixtures"
phases_arg=""
out_dir=""
image_tag_candidate="${ESHU_RELEASE_GATE_IMAGE_TAG_CANDIDATE:-}"
provider_compare="${ESHU_RELEASE_GATE_PROVIDER_COMPARE:-}"
proof_matrix="${ESHU_RELEASE_GATE_PROOF_MATRIX:-}"
readback_proof="${ESHU_RELEASE_GATE_READBACK_PROOF:-}"
api_base_url="${ESHU_RELEASE_GATE_API_BASE_URL:-}"
api_key="${ESHU_RELEASE_GATE_API_KEY:-}"
k8s_namespace="${ESHU_RELEASE_GATE_K8S_NAMESPACE:-}"
helm_release="${ESHU_RELEASE_GATE_HELM_RELEASE:-eshu}"
pprof_base_url="${ESHU_RELEASE_GATE_PPROF_BASE_URL:-}"
runtime_run_kind="${ESHU_RELEASE_GATE_RUNTIME_RUN_KIND:-}"
previous_runtime_evidence="${ESHU_RELEASE_GATE_PREVIOUS_RUNTIME_EVIDENCE:-}"
runtime_volume_proof="${ESHU_RELEASE_GATE_RUNTIME_VOLUME_PROOF:-}"

repo_root="${ESHU_RELEASE_GATE_REPO_ROOT:-}"
if [ -z "${repo_root}" ]; then
    if repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null)"; then :
    else
        repo_root="$(cd "$(dirname "$0")/.." && pwd)"
    fi
fi

usage() {
    # printf (a builtin, no pipe) instead of a heredoc: this body is over 512
    # bytes and would deadlock under Homebrew bash >= 5.1's pipe-buffer
    # heredoc write (#5074).
    printf '%s\n' \
        "Usage: $(basename "$0") [options]" \
        "" \
        "Phases (default: ${phases_default}; \"all\" enables every phase):" \
        "  state, focused, fixtures, proof-matrix, runtime, readback-proof, k8s, provider" \
        "" \
        "Options:" \
        "  --phases <list>               Comma-separated phases." \
        '  --out-dir <path>              Evidence dir (default ${TMPDIR:-/tmp}/eshu-security-intel-release-gate/<ts>).' \
        "  --image-tag-candidate <tag>   Image tag this gate is judging. Recorded in evidence." \
        "  --provider-compare <file>     Aggregate-only provider parity JSON (provider phase)." \
        "  --proof-matrix <file>         Aggregate-only representative corpus proof matrix JSON." \
        "  --readback-proof <file>       Aggregate-only API/MCP/CLI readback proof JSON." \
        "  --api-base-url <url>          Base URL for runtime and k8s phase readback." \
        "  --api-key <token>             Bearer token for runtime/k8s API readback." \
        "  --pprof-base-url <url>        Base URL for the runtime/k8s pprof probe." \
        "                                Pprof is exposed via a separate listener and is" \
        "                                required for runtime/k8s release proof." \
        "  --runtime-run-kind <kind>     Runtime proof kind: clean or preserved." \
        "  --previous-runtime-evidence <file>" \
        "                                Prior clean evidence.json for preserved runtime proof." \
        "  --runtime-volume-proof <file> Aggregate-only clean/preserved Compose volume proof JSON." \
        "  --k8s-namespace <name>        Namespace for k8s phase snapshots." \
        "  --helm-release <name>         Helm release name (default: ${helm_release})." \
        "  -h, --help                    Show this help and exit." \
        "" \
        "Override repo root with ESHU_RELEASE_GATE_REPO_ROOT. Set" \
        "ESHU_RELEASE_GATE_SKIP_GO_TESTS=1 to skip Go test invocations."
}

die() {
    printf 'security-intelligence-release-gate: %s\n' "$*" >&2
    exit 1
}

while [ $# -gt 0 ]; do
    case "$1" in
        --phases) phases_arg="$2"; shift 2 ;;
        --out-dir) out_dir="$2"; shift 2 ;;
        --image-tag-candidate) image_tag_candidate="$2"; shift 2 ;;
        --provider-compare) provider_compare="$2"; shift 2 ;;
        --proof-matrix) proof_matrix="$2"; shift 2 ;;
        --readback-proof) readback_proof="$2"; shift 2 ;;
        --api-base-url) api_base_url="$2"; shift 2 ;;
        --api-key) api_key="$2"; shift 2 ;;
        --pprof-base-url) pprof_base_url="$2"; shift 2 ;;
        --runtime-run-kind) runtime_run_kind="$2"; shift 2 ;;
        --previous-runtime-evidence) previous_runtime_evidence="$2"; shift 2 ;;
        --runtime-volume-proof) runtime_volume_proof="$2"; shift 2 ;;
        --k8s-namespace) k8s_namespace="$2"; shift 2 ;;
        --helm-release) helm_release="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) die "unknown option: $1" ;;
    esac
done

phases_arg="${phases_arg:-${ESHU_RELEASE_GATE_PHASES:-${phases_default}}}"
if [ "${phases_arg}" = "all" ]; then
    phases_arg="state,focused,fixtures,proof-matrix,runtime,readback-proof,k8s,provider"
fi

IFS=',' read -r -a phases <<<"${phases_arg}"
for p in "${phases[@]}"; do
    case "${p}" in
        state|focused|fixtures|proof-matrix|runtime|readback-proof|k8s|provider) ;;
        '' ) ;;
        *) die "unknown phase: ${p}" ;;
    esac
done

command -v jq >/dev/null 2>&1 || die "jq is required"

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
if [ -z "${out_dir}" ]; then
    out_dir="${TMPDIR:-/tmp}/eshu-security-intel-release-gate/${timestamp}"
fi
mkdir -p "${out_dir}"
out_json="${out_dir}/evidence.json"
out_md="${out_dir}/evidence.md"

# evidence is built incrementally as JSON; start with shell-safe scaffolding.
phases_json="$(printf '%s\n' "${phases[@]}" | jq -R . | jq -sc 'map(select(length>0))')"
jq -n --arg ts "${timestamp}" --argjson phases "${phases_json}" \
    '{schema_version: 1, generated_at: $ts, repo_root: "[redacted-path]", phases: $phases, pass: true, failures: []}' \
    >"${out_json}.tmp"

# Extract the first sha256 digest token from stdin in a portable way.
extract_digest() {
    awk 'match($0, /sha256:[0-9a-f]{64}/) { print substr($0, RSTART, RLENGTH); exit }'
}

# Bash 3.2 compatible recursive evidence file listing, relative to out_dir.
list_evidence_files() {
    local dir="$1"
    local entry rel
    shopt -s nullglob
    for entry in "${dir}"/*; do
        if [ -d "${entry}" ]; then
            list_evidence_files "${entry}"
        elif [ -f "${entry}" ]; then
            rel="${entry#"${out_dir}"/}"
            [ "${rel}" = "evidence.md" ] && continue
            printf '%s\n' "${rel}"
        fi
    done
    shopt -u nullglob
}

run_phase_state() {
    local chart="${repo_root}/deploy/helm/eshu/Chart.yaml"
    local compose_root="${repo_root}/docker-compose.yaml"
    local compose_remote="${repo_root}/docker-compose.remote-e2e.yaml"
    local schema_dir="${repo_root}/schema/data-plane/postgres"
    local state_status="pass"

    [ -f "${chart}" ] || die "missing Chart.yaml at ${chart}"
    [ -f "${compose_root}" ] || die "missing docker-compose.yaml at ${compose_root}"
    if [ -z "${image_tag_candidate}" ]; then
        state_status="fail"
        record_failure state "state phase requires --image-tag-candidate"
    fi

    local helm_chart_version helm_app_version git_commit git_branch nornicdb_image nornicdb_digest
    helm_chart_version="$(awk '/^version:/ {print $2; exit}' "${chart}")"
    helm_app_version="$(awk '/^appVersion:/ {gsub(/"/,"",$2); print $2; exit}' "${chart}")"
    [ -n "${helm_chart_version}" ] || die "Chart.yaml has no version line"
    [ -n "${helm_app_version}" ] || die "Chart.yaml has no appVersion line"

    git_commit="$(git -C "${repo_root}" rev-parse HEAD 2>/dev/null || echo "unknown")"
    git_branch="$(git -C "${repo_root}" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")"

    nornicdb_image="$(awk '
        /^  nornicdb:/ { in_block = 1; next }
        in_block && /^  [a-zA-Z]/ && !/^  nornicdb:/ { in_block = 0 }
        in_block && /^[ \t]+image:/ {
            sub(/^[ \t]+image:[ \t]*/, "")
            gsub(/[$"{}]/, "")
            sub(/.*:-/, "")
            print
            exit
        }
    ' "${compose_root}")"
    nornicdb_digest="$(printf '%s' "${nornicdb_image}" | extract_digest)"

    local schema_migration_count schema_latest_migration
    schema_migration_count=0
    schema_latest_migration=""
    if [ -d "${schema_dir}" ]; then
        local sql_files
        shopt -s nullglob
        sql_files=( "${schema_dir}"/*.sql )
        shopt -u nullglob
        schema_migration_count="${#sql_files[@]}"
        if [ "${schema_migration_count}" -gt 0 ]; then
            local sorted_sql
            sorted_sql="$(printf '%s\n' "${sql_files[@]}" | LC_ALL=C sort | tail -n 1)"
            schema_latest_migration="$(basename "${sorted_sql}")"
        fi
    fi

    local remote_services_json="[]"
    local scanner_limits_json="{}"
    if [ -f "${compose_remote}" ]; then
        remote_services_json="$(awk '
            /^services:/ { in_services = 1; next }
            in_services && /^[a-zA-Z]/ && !/^services:/ { in_services = 0 }
            in_services && /^  [a-zA-Z0-9_-]+:[ \t]*$/ {
                gsub(/[: ]/, "", $1); print $1
            }
        ' "${compose_remote}" | jq -R . | jq -sc .)"
        [ -z "${remote_services_json}" ] && remote_services_json="[]"

        scanner_limits_json="$(awk '
            /^  scanner-worker:/ { in_scanner = 1; next }
            in_scanner && /^  [a-zA-Z]/ && !/^  scanner-worker:/ { in_scanner = 0 }
            in_scanner && /ESHU_SCANNER_WORKER_/ {
                gsub(/^[ \t]+/, "")
                idx = index($0, ":")
                if (idx > 0) {
                    key = substr($0, 1, idx - 1)
                    value = substr($0, idx + 1)
                    gsub(/^[ \t]+|[ \t]+$/, "", value)
                    gsub(/^"|"$/, "", value)
                    printf "%s\t%s\n", key, value
                }
            }
        ' "${compose_remote}" | jq -Rc 'split("\t") | select(length == 2) | {(.[0]): .[1]}' | jq -sc 'add // {}')"
        [ -z "${scanner_limits_json}" ] && scanner_limits_json="{}"
    fi

    jq --arg helm_chart_version "${helm_chart_version}" \
       --arg helm_app_version "${helm_app_version}" \
       --arg image_tag_candidate "${image_tag_candidate}" \
       --arg state_status "${state_status}" \
       --arg git_commit "${git_commit}" \
       --arg git_branch "${git_branch}" \
       --arg nornicdb_image "${nornicdb_image}" \
       --arg nornicdb_digest "${nornicdb_digest}" \
       --argjson schema_migration_count "${schema_migration_count}" \
       --arg schema_latest_migration "${schema_latest_migration}" \
       --argjson remote_e2e_services "${remote_services_json}" \
       --argjson scanner_worker_limits "${scanner_limits_json}" \
       '.state = {
            status: $state_status,
            git_commit: $git_commit,
            git_branch: $git_branch,
            helm_chart_version: $helm_chart_version,
            helm_app_version: $helm_app_version,
            image_tag_candidate: $image_tag_candidate,
            nornicdb_image: $nornicdb_image,
            nornicdb_digest: $nornicdb_digest,
            schema_migration_count: $schema_migration_count,
            schema_latest_migration: $schema_latest_migration,
            remote_e2e_services: $remote_e2e_services,
            scanner_worker_limits: $scanner_worker_limits
       }' "${out_json}.tmp" >"${out_json}.tmp.new"
    mv "${out_json}.tmp.new" "${out_json}.tmp"
}

record_failure() {
    local phase="$1"
    local message="$2"
    jq --arg phase "${phase}" --arg message "${message}" \
        '.pass = false | .failures += [{phase: $phase, message: $message}]' \
        "${out_json}.tmp" >"${out_json}.tmp.new"
    mv "${out_json}.tmp.new" "${out_json}.tmp"
}

run_phase_focused() {
    if [ "${ESHU_RELEASE_GATE_SKIP_GO_TESTS:-0}" = "1" ]; then
        jq '.focused = {status: "skipped", skipped: true, reason: "ESHU_RELEASE_GATE_SKIP_GO_TESTS=1"}' \
            "${out_json}.tmp" >"${out_json}.tmp.new"
        mv "${out_json}.tmp.new" "${out_json}.tmp"
        return 0
    fi
    local pkgs=(
        ./internal/vulnerabilityparity
        ./internal/reducer
        ./internal/query
        ./internal/mcp
        ./internal/collector/vulnerabilityintelligence
        ./internal/collector/scannerworker
        ./cmd/scanner-worker
    )
    local log="${out_dir}/focused.log"
    if (cd "${repo_root}/go" && go test "${pkgs[@]}" -count=1) >"${log}" 2>&1; then
        jq --arg log "${log}" '.focused = {status: "pass", log: $log}' \
            "${out_json}.tmp" >"${out_json}.tmp.new"
        mv "${out_json}.tmp.new" "${out_json}.tmp"
    else
        record_failure focused "go test failed; see ${log}"
        jq --arg log "${log}" '.focused = {status: "fail", log: $log}' \
            "${out_json}.tmp" >"${out_json}.tmp.new"
        mv "${out_json}.tmp.new" "${out_json}.tmp"
    fi
}

run_phase_fixtures() {
    local log="${out_dir}/fixtures.log"
    local fixture_script="${repo_root}/scripts/verify_vulnerability_parity_fixtures.sh"
    local go_ok=0 shell_ok=0

    # Fail closed before running anything when the shell verifier is absent,
    # so we never report the fixture gate as green while the privacy/sanity
    # checks documented in vulnerability-parity-gate.md were skipped.
    if [ ! -x "${fixture_script}" ]; then
        printf 'verify_vulnerability_parity_fixtures.sh missing or not executable at %s\n' "${fixture_script}" \
            >"${log}"
        record_failure fixtures "verify_vulnerability_parity_fixtures.sh missing or not executable at ${fixture_script}"
        jq --arg log "${log}" '.fixtures = {status: "fail", log: $log}' \
            "${out_json}.tmp" >"${out_json}.tmp.new"
        mv "${out_json}.tmp.new" "${out_json}.tmp"
        return 0
    fi

    if [ "${ESHU_RELEASE_GATE_SKIP_GO_TESTS:-0}" = "1" ]; then
        # The test harness sets this to keep the Go gate inert while still
        # exercising the shell-verifier branch.
        if [ "${ESHU_RELEASE_GATE_FORCE_FIXTURES_GO_OK:-0}" = "1" ]; then
            go_ok=1
        else
            jq '.fixtures = {status: "skipped", skipped: true, reason: "ESHU_RELEASE_GATE_SKIP_GO_TESTS=1"}' \
                "${out_json}.tmp" >"${out_json}.tmp.new"
            mv "${out_json}.tmp.new" "${out_json}.tmp"
            return 0
        fi
    elif (cd "${repo_root}/go" && go test ./internal/vulnerabilityparity -count=1) >"${log}" 2>&1; then
        go_ok=1
    fi

    if "${fixture_script}" >>"${log}" 2>&1; then
        shell_ok=1
    fi

    if [ "${go_ok}" -eq 1 ] && [ "${shell_ok}" -eq 1 ]; then
        jq --arg log "${log}" '.fixtures = {status: "pass", log: $log}' \
            "${out_json}.tmp" >"${out_json}.tmp.new"
    else
        record_failure fixtures "parity fixture gate failed; see ${log}"
        jq --arg log "${log}" '.fixtures = {status: "fail", log: $log}' \
            "${out_json}.tmp" >"${out_json}.tmp.new"
    fi
    mv "${out_json}.tmp.new" "${out_json}.tmp"
}

# Runtime + k8s phases live in the shared lib so this main script stays under
# the repo file-size rule. They depend on globals declared above plus die() and
# record_failure().
# shellcheck source=scripts/lib/security_intelligence_release_gate_public.sh
source "$(dirname "$0")/lib/security_intelligence_release_gate_public.sh"

# shellcheck source=scripts/lib/security_intelligence_release_gate_runtime.sh
source "$(dirname "$0")/lib/security_intelligence_release_gate_runtime.sh"

# shellcheck source=scripts/lib/security_intelligence_release_gate_phases.sh
source "$(dirname "$0")/lib/security_intelligence_release_gate_phases.sh"

# shellcheck source=scripts/lib/security_intelligence_release_gate_k8s.sh
source "$(dirname "$0")/lib/security_intelligence_release_gate_k8s.sh"

# shellcheck source=scripts/lib/security_intelligence_release_gate_proof_matrix.sh
source "$(dirname "$0")/lib/security_intelligence_release_gate_proof_matrix.sh"

# shellcheck source=scripts/lib/security_intelligence_release_gate_readback.sh
source "$(dirname "$0")/lib/security_intelligence_release_gate_readback.sh"

run_phase_provider() {
    [ -n "${provider_compare}" ] || die "provider phase requires --provider-compare"
    [ -f "${provider_compare}" ] || die "provider-compare file not found: ${provider_compare}"
    local raw
    raw="$(cat "${provider_compare}")"
    if ! printf '%s' "${raw}" | jq -e . >/dev/null 2>&1; then
        die "provider-compare must be valid JSON"
    fi
    # Refuse anything that looks like private data. Only aggregate totals + a
    # synthetic comparison id are accepted into evidence.
    if printf '%s' "${raw}" | rg --quiet \
        'ghp_|github_pat_|glpat-|https?://[^"]*github\.com|https?://[^"]*gitlab\.com|/security/dependabot|"package_name"|"alert_url"|"repository"|"installation"'; then
        die "provider-compare looks like private data; only aggregate totals are accepted"
    fi
    local cmp_id totals
    cmp_id="$(jq -r '.comparison_id // ""' <<<"${raw}")"
    totals="$(jq -c '.totals // {}' <<<"${raw}")"
    [ "${totals}" = "{}" ] && die "provider-compare must include a non-empty .totals object"
    jq --arg id "${cmp_id}" --argjson totals "${totals}" \
        '.provider = {status: "pass", comparison_id: $id, totals: $totals}' \
        "${out_json}.tmp" >"${out_json}.tmp.new"
    mv "${out_json}.tmp.new" "${out_json}.tmp"
}

# Dispatch phases in declared order.
for p in "${phases[@]}"; do
    case "${p}" in
        state) run_phase_state ;;
        focused) run_phase_focused ;;
        fixtures) run_phase_fixtures ;;
        proof-matrix) run_phase_proof_matrix ;;
        runtime) run_phase_runtime ;;
        readback-proof) run_phase_readback_proof ;;
        k8s) run_phase_k8s ;;
        provider) run_phase_provider ;;
        '' ) ;;
    esac
done

mv "${out_json}.tmp" "${out_json}"

# Markdown summary.
{
    echo "# Security Intelligence Release Gate"
    echo
    echo "Generated: ${timestamp}"
    echo
    jq -r '
        "Repo commit: \(.state.git_commit // "unknown")",
        "Branch: \(.state.git_branch // "unknown")",
        "Helm chart version: \(.state.helm_chart_version // "unknown")",
        "Helm appVersion: \(.state.helm_app_version // "unknown")",
        "Image tag candidate: \(.state.image_tag_candidate // "(not provided)")",
        "NornicDB image: \(.state.nornicdb_image // "unknown")",
        "NornicDB digest: \(.state.nornicdb_digest // "unknown")",
        "Schema migrations: \(.state.schema_migration_count // 0) (latest: \(.state.schema_latest_migration // "n/a"))",
        "Phases run: \(.phases | join(", "))",
        "Pass: \(.pass)",
        "Failures: \(.failures | length)"
    ' "${out_json}"
    if jq -e '.failures | length > 0' "${out_json}" >/dev/null; then
        echo
        echo "## Failures"
        jq -r '.failures[] | "- **\(.phase)**: \(.message)"' "${out_json}"
    fi
    echo
    echo "## Evidence files"
    list_evidence_files "${out_dir}" | LC_ALL=C sort | sed 's|^|- |'
} >"${out_md}"

if jq -e '.pass == false' "${out_json}" >/dev/null; then
    printf 'security-intelligence-release-gate: FAIL (see %s)\n' "${out_md}" >&2
    exit 1
fi

printf 'security-intelligence-release-gate: pass (evidence in %s)\n' "${out_dir}"
