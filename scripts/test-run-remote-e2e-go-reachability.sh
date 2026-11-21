#!/usr/bin/env bash
set -euo pipefail

# Self-test for the go-reachability remote e2e bring-up + capture driver
# (#10031). It proves the driver is well-formed, documents its contract in
# --help, and enforces the digest gate hermetically: a missing seed image,
# a tag-pinned ref, a truncated digest, and a non-sha256 algorithm are all
# refused before any container starts, with no docker, no network, and no
# live backend. A well-formed placeholder digest passes the gate and fails
# later on the missing checkout/fragment, which proves the gate order. The
# live bring-up itself is the operator/CI gate, not this script.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
driver="${repo_root}/scripts/run-remote-e2e-go-reachability.sh"

die() {
	printf 'test-run-remote-e2e-go-reachability: %s\n' "$*" >&2
	exit 1
}

[[ -f "${driver}" ]] || die "missing driver: ${driver}"
bash -n "${driver}" || die "driver failed bash syntax check"

# --help documents the contract without touching anything.
help_log="$(bash "${driver}" --help)"
for needle in "ESHU_DEPLOY_CHECKOUT" "ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE" "ESHU_GITHUB_APP_ID" "ESHU_API_KEY" "evidence mode" "--keep-up"; do
	rg --fixed-strings --quiet -- "${needle}" < <(printf '%s\n' "${help_log}") \
		|| die "--help output missing ${needle}"
done

# A missing seed image is refused before anything starts.
if ESHU_DEPLOY_CHECKOUT=/nonexistent-checkout ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE= \
	bash "${driver}" --artifacts "$(mktemp -d)" >/dev/null 2>&1; then
	die "driver accepted a missing seed image"
fi

# A stub checkout with the base file but no scanner fragment isolates the
# App gate: nothing after it (docker, compose, fragment check) may run, and
# the refusal must name the App variable, not the fragment. The component
# fails derived claims terminally without a host-minted token, so a live
# bring-up without these can never scan.
stub_checkout="$(mktemp -d)"
mkdir -p "${stub_checkout}/compose"
touch "${stub_checkout}/compose/docker-compose.remote-e2e.yaml"
placeholder_digest='ghcr.io/eshu-hq/scanner-go-reachability@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'

# A missing GitHub App ID is refused before any container starts.
app_log="$(ESHU_DEPLOY_CHECKOUT="${stub_checkout}" \
	ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE="${placeholder_digest}" \
	ESHU_GITHUB_APP_ID= \
	bash "${driver}" --artifacts "$(mktemp -d)" 2>&1 || true)"
rg --quiet 'ESHU_GITHUB_APP_ID' < <(printf '%s\n' "${app_log}") \
	|| die "driver did not refuse a missing github app id: ${app_log}"

# A missing App private key is refused the same way.
app_log="$(ESHU_DEPLOY_CHECKOUT="${stub_checkout}" \
	ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE="${placeholder_digest}" \
	ESHU_GITHUB_APP_ID=12345 ESHU_GITHUB_APP_PRIVATE_KEY= \
	bash "${driver}" --artifacts "$(mktemp -d)" 2>&1 || true)"
rg --quiet 'ESHU_GITHUB_APP_PRIVATE_KEY' < <(printf '%s\n' "${app_log}") \
	|| die "driver did not refuse a missing github app private key: ${app_log}"

# A missing installations mapping is refused the same way.
app_log="$(ESHU_DEPLOY_CHECKOUT="${stub_checkout}" \
	ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE="${placeholder_digest}" \
	ESHU_GITHUB_APP_ID=12345 ESHU_GITHUB_APP_PRIVATE_KEY=dummy ESHU_GITHUB_APP_INSTALLATIONS= \
	bash "${driver}" --artifacts "$(mktemp -d)" 2>&1 || true)"
rg --quiet 'ESHU_GITHUB_APP_INSTALLATIONS' < <(printf '%s\n' "${app_log}") \
	|| die "driver did not refuse missing github app installations: ${app_log}"

# With all three App variables the run passes the App gate and fails later
# on the missing fragment — proving the gate runs before the file checks.
app_log="$(ESHU_DEPLOY_CHECKOUT="${stub_checkout}" \
	ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE="${placeholder_digest}" \
	ESHU_GITHUB_APP_ID=12345 ESHU_GITHUB_APP_PRIVATE_KEY=dummy ESHU_GITHUB_APP_INSTALLATIONS='{}' \
	bash "${driver}" --artifacts "$(mktemp -d)" 2>&1 || true)"
rg --quiet 'scanner fragment not found' < <(printf '%s\n' "${app_log}") \
	|| die "full app env did not reach the fragment check: ${app_log}"
rm -rf "${stub_checkout}"

# A tag-pinned ref (no digest) is refused.
if ESHU_DEPLOY_CHECKOUT=/nonexistent-checkout \
	ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE=ghcr.io/eshu-hq/scanner-go-reachability:latest \
	bash "${driver}" --artifacts "$(mktemp -d)" >/dev/null 2>&1; then
	die "driver accepted a tag-pinned seed image"
fi

# A truncated digest is refused.
if ESHU_DEPLOY_CHECKOUT=/nonexistent-checkout \
	ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE='ghcr.io/eshu-hq/scanner-go-reachability@sha256:bbbb' \
	bash "${driver}" --artifacts "$(mktemp -d)" >/dev/null 2>&1; then
	die "driver accepted a truncated digest"
fi

# A non-sha256 algorithm is refused.
if ESHU_DEPLOY_CHECKOUT=/nonexistent-checkout \
	ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE='ghcr.io/eshu-hq/scanner-go-reachability@sha512:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' \
	bash "${driver}" --artifacts "$(mktemp -d)" >/dev/null 2>&1; then
	die "driver accepted a non-sha256 digest"
fi

# A well-formed placeholder digest passes the gate and fails later on the
# missing checkout — proving the gate runs first and stays hermetic. The
# App variables ride along so the later App gate does not mask the order.
gate_log="$(ESHU_DEPLOY_CHECKOUT=/nonexistent-checkout \
	ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE='ghcr.io/eshu-hq/scanner-go-reachability@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' \
	ESHU_GITHUB_APP_ID=12345 ESHU_GITHUB_APP_PRIVATE_KEY=dummy ESHU_GITHUB_APP_INSTALLATIONS='{}' \
	bash "${driver}" --artifacts "$(mktemp -d)" 2>&1 || true)"
rg --quiet 'ESHU_DEPLOY_CHECKOUT|base compose file not found' < <(printf '%s\n' "${gate_log}") \
	|| die "well-formed digest did not reach the checkout check: ${gate_log}"

# The proof overlay must carry the live-chain wiring the driver depends
# on: explicit fixture ingest on the one-shot bootstrap (no discovery loop
# runs anywhere else), the host-mint App slots plus file-proxy/vulnDB slots
# and the shared scan-inputs mount on the worker, and the minted-key
# writeback path on core-api. All hermetic file reads.
overlay="${repo_root}/scripts/docker-compose.go-reachability-proof-deps.yaml"
for needle in "ESHU_REPO_SOURCE_MODE: explicit" "eshu-go-reachability-fixture" \
	"ESHU_GITHUB_APP_ID" "ESHU_GITHUB_APP_INSTALLATIONS" \
	"GOPROXY:" "GOVULNDB:" "target: /scan-inputs" \
	"ESHU_AUTO_GENERATE_API_KEY" "ESHU_API_KEY:"; do
	rg --fixed-strings --quiet -- "${needle}" "${overlay}" \
		|| die "proof overlay misses ${needle}"
done

printf 'go-reachability driver self-test passed\n'
