#!/usr/bin/env bash
# Focused tests for the Compose-to-Helm runtime parity verifier.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
gate="${repo_root}/scripts/verify-compose-helm-runtime-parity.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

install_fake_tools() {
    local dir="$1"
    mkdir -p "${dir}/_bin"

    # Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
    # writes an entire heredoc body to a pipe before forking the reader, and
    # macOS's 512-byte pipe buffer deadlocks on this ~2.5KB body (#5074). The
    # body is fully static (was a quoted <<'SH', no shell expansion), so the
    # copied file is byte-identical to the original heredoc body (its own two
    # over-budget inner heredocs were converted to printf so the copied
    # fixture itself stays under the gate budget).
    cp "${repo_root}/scripts/lib/test-verify-compose-helm-runtime-parity-fake-docker.sh" "${dir}/_bin/docker"
    chmod +x "${dir}/_bin/docker"

    # Delivered from a sibling fixture file, not a heredoc: Homebrew bash >= 5.1
    # writes an entire heredoc body to a pipe before forking the reader, and
    # macOS's 512-byte pipe buffer deadlocks on this ~1.2KB body (#5074). The
    # body is fully static (was a quoted <<'SH', no shell expansion), so the
    # copied file is byte-identical to the original heredoc body (its own
    # over-budget inner heredoc was converted to printf so the copied
    # fixture itself stays under the gate budget).
    cp "${repo_root}/scripts/lib/test-verify-compose-helm-runtime-parity-fake-helm.sh" "${dir}/_bin/helm"
    chmod +x "${dir}/_bin/helm"
}

repo="${tmp_root}/repo"
mkdir -p "${repo}"
cp -R "${repo_root}/deploy" "${repo}/deploy"
cp "${repo_root}/docker-compose.yaml" "${repo}/docker-compose.yaml"
cp "${repo_root}/docker-compose.remote-e2e.yaml" "${repo}/docker-compose.remote-e2e.yaml"
cp "${repo_root}/docker-compose.remote-e2e.observability.yaml" "${repo}/docker-compose.remote-e2e.observability.yaml"
cp "${repo_root}/.env.remote-e2e.example" "${repo}/.env.remote-e2e.example"
install_fake_tools "${repo}"

if ! PATH="${repo}/_bin:${PATH}" "${gate}" --repo-root "${repo}" >"${repo}/pass.out" 2>"${repo}/pass.err"; then
    sed -n '1,160p' "${repo}/pass.err" >&2
    exit 1
fi
rg --quiet 'runtime parity verification passed' "${repo}/pass.out" \
    || { printf 'expected parity verifier to pass complete fake surfaces\n' >&2; exit 1; }
rg --quiet 'core ServiceMonitor coverage: pass' "${repo}/pass.out" \
    || { printf 'expected core ServiceMonitor coverage evidence\n' >&2; exit 1; }

component_service_monitor="${repo}/deploy/helm/eshu/templates/servicemonitor-component-extension-collector.yaml"
component_service_monitor_backup="${repo}/servicemonitor-component-extension-collector.yaml.bak"
mv "${component_service_monitor}" "${component_service_monitor_backup}"
if PATH="${repo}/_bin:${PATH}" "${gate}" --repo-root "${repo}" >"${repo}/missing-sm.out" 2>"${repo}/missing-sm.err"; then
    printf 'expected verifier to fail when component extension ServiceMonitor coverage is missing\n' >&2
    exit 1
fi
rg --quiet 'missing collector ServiceMonitor template coverage: app.kubernetes.io/component: component-extension-collector' "${repo}/missing-sm.err" \
    || { printf 'missing component extension ServiceMonitor coverage was not reported\n' >&2; exit 1; }
mv "${component_service_monitor_backup}" "${component_service_monitor}"

if ESHU_FAKE_PARITY_MISSING_REMOTE=1 PATH="${repo}/_bin:${PATH}" "${gate}" --repo-root "${repo}" >"${repo}/fail.out" 2>"${repo}/fail.err"; then
    printf 'expected verifier to fail when a required remote collector is missing\n' >&2
    exit 1
fi
rg --quiet 'missing profile-expanded remote Compose service: collector-jira' "${repo}/fail.err" \
    || { printf 'missing remote collector failure was not reported\n' >&2; exit 1; }

printf 'Compose-to-Helm runtime parity tests passed\n'
