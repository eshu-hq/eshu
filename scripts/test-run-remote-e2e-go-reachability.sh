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
for needle in "ESHU_DEPLOY_CHECKOUT" "ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE" "evidence mode" "--keep-up"; do
	rg --fixed-strings --quiet -- "${needle}" < <(printf '%s\n' "${help_log}") \
		|| die "--help output missing ${needle}"
done

# A missing seed image is refused before anything starts.
if ESHU_DEPLOY_CHECKOUT=/nonexistent-checkout ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE= \
	bash "${driver}" --artifacts "$(mktemp -d)" >/dev/null 2>&1; then
	die "driver accepted a missing seed image"
fi

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
# missing checkout — proving the gate runs first and stays hermetic.
gate_log="$(ESHU_DEPLOY_CHECKOUT=/nonexistent-checkout \
	ESHU_SCANNER_GOREACHABILITY_SEED_IMAGE='ghcr.io/eshu-hq/scanner-go-reachability@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' \
	bash "${driver}" --artifacts "$(mktemp -d)" 2>&1 || true)"
rg --quiet 'ESHU_DEPLOY_CHECKOUT|base compose file not found' < <(printf '%s\n' "${gate_log}") \
	|| die "well-formed digest did not reach the checkout check: ${gate_log}"

printf 'go-reachability driver self-test passed\n'
