#!/usr/bin/env bash
# verify-scanner-digest-pins.sh - fail when a published scanner image digest
# drifted from its artifactDigests pin in
# go/internal/collector/scannercomponent/manifest.go (#9491, relocated by
# #8878).
#
# Why: the scanner ComponentPackage manifests address their analyzer image by
# immutable sha256 digest, but nothing failed when a republish moved the
# published digest past the pin, so activation kept running the old analyzer
# binary. This script is the drift detector; the pin bump itself stays a
# reviewed follow-up PR (the pattern #9494 uses), never a publish-time
# auto-update.
#
# What it checks, for the image, sbom, and osruntime components:
#   1. `crane digest <registry>/scanner-<name>:sha-<publish-sha>` equals the
#      pinned digest, where <publish-sha> comes from the
#      `// Scanner-Publish-SHA: <40-hex>` anchor line above artifactDigests.
#      Anonymous registry reads: no credentials needed.
#   2. `gh attestation verify` re-verifies the pinned digest against the
#      publish repository named by the `// Scanner-Publish-Repo: <owner/name>`
#      anchor line, with the in-workflow token.
#   3. Anchor validity: the anchor names a commit in the publish repository
#      (`gh api repos/<repo>/commits/<sha>`). An unknown anchor fails closed.
#
# Publish-repo semantics (#8878): execution moved to the
# eshu-hq/eshu-scanner-worker satellite, so the anchor names the satellite
# repo's publish commit going forward. The anchor carried by the removal
# itself still names the last eshu-side publish (eshu-hq/eshu); the first
# satellite re-pin flips both anchor lines together with the pins. In-tree
# scanner sources are gone, so this gate no longer diffs scanner inputs for
# post-anchor staleness: satellite-source staleness is the satellite repo's
# own gate responsibility, and the anchor advances only through reviewed
# re-pin PRs.
#
# Usage:
#   scripts/verify-scanner-digest-pins.sh [--manifest PATH]
#
# Environment:
#   SCANNER_PINS_REGISTRY  Registry holding the scanner images
#                          (default ghcr.io/eshu-hq).
#   CRANE_BIN              crane binary (default crane).
#   GH_BIN                 gh binary (default gh).
#
# Exit codes:
#   0 - every pin matches its anchored tag and every attestation verifies.
#   1 - drift: a digest mismatch, a missing tag, or a failed attestation.
#       The failure names the component and the expected digest.
#   2 - fail closed: bad usage, unreadable manifest, a missing or malformed
#       anchor line, an unparseable pin, an unknown anchor commit, or a
#       transport failure. A network error never passes and never reports
#       drift.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="${repo_root}/go/internal/collector/scannercomponent/manifest.go"
registry="${SCANNER_PINS_REGISTRY:-ghcr.io/eshu-hq}"
crane_bin="${CRANE_BIN:-crane}"
gh_bin="${GH_BIN:-gh}"

die() {
	printf 'verify-scanner-digest-pins: FAIL-CLOSED: %s\n' "$*" >&2
	exit 2
}

while (($# > 0)); do
	case "$1" in
		--manifest)
			(($# >= 2)) || die "--manifest needs a path"
			manifest="$2"
			shift 2
			;;
		*)
			printf 'verify-scanner-digest-pins: unknown argument %s\n' "$1" >&2
			exit 2
			;;
	esac
done

[[ -r "${manifest}" ]] || die "cannot read manifest: ${manifest}"
for tool in "${crane_bin}" "${gh_bin}" python3 sha256sum; do
	command -v "${tool}" >/dev/null 2>&1 || die "required tool not found: ${tool}"
done

anchor_count="$(grep -c '^// Scanner-Publish-SHA: ' "${manifest}" || true)"
[[ "${anchor_count}" == 1 ]] || die "want exactly one '// Scanner-Publish-SHA: <40-hex>' line in ${manifest}, found ${anchor_count}"
anchor="$(sed -n 's|^// Scanner-Publish-SHA: \([0-9a-f]*\)$|\1|p' "${manifest}")"
[[ "${anchor}" =~ ^[0-9a-f]{40}$ ]] || die "malformed anchor line in ${manifest}; want '// Scanner-Publish-SHA: <40-hex>'"

repo_count="$(grep -c '^// Scanner-Publish-Repo: ' "${manifest}" || true)"
[[ "${repo_count}" == 1 ]] || die "want exactly one '// Scanner-Publish-Repo: <owner/name>' line in ${manifest}, found ${repo_count}"
publish_repo="$(sed -n 's|^// Scanner-Publish-Repo: \([A-Za-z0-9_.-]*/[A-Za-z0-9_.-]*\)$|\1|p' "${manifest}")"
[[ -n "${publish_repo}" ]] || die "malformed publish-repo line in ${manifest}; want '// Scanner-Publish-Repo: <owner/name>'"

pin_for() {
	sed -n "s|^[[:space:]]*$1:[[:space:]]*\"\([0-9a-f]*\)\".*|\1|p" "${manifest}" | head -n 1
}

components=(image sbom osruntime)
ids=(IDImage IDSBOM IDOSRuntime)
declare -A pin_of=()
for i in "${!components[@]}"; do
	pin="$(pin_for "${ids[$i]}")"
	[[ "${pin}" =~ ^[0-9a-f]{64}$ ]] || die "no 64-hex pin for ${ids[$i]} in ${manifest}"
	pin_of["${components[$i]}"]="${pin}"
done

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

if ! "${gh_bin}" api "repos/${publish_repo}/commits/${anchor}" >/dev/null 2>"${work}/gh-anchor.err"; then
	die "anchor ${anchor} is not a commit in ${publish_repo}; re-anchor to the publish that produced the pins"
fi

drift=0
for comp in "${components[@]}"; do
	pin="${pin_of[${comp}]}"
	ref="${registry}/scanner-${comp}:sha-${anchor}"
	if ! observed="$("${crane_bin}" digest "${ref}" 2>"${work}/crane-${comp}.err")"; then
		if grep -q MANIFEST_UNKNOWN "${work}/crane-${comp}.err"; then
			printf 'DRIFT scanner-%s: %s not found (MANIFEST_UNKNOWN); expected digest: %s\n' \
				"${comp}" "${ref}" "${pin}" >&2
			drift=1
			continue
		fi
		die "crane digest ${ref} failed: $(tr '\n' ' ' <"${work}/crane-${comp}.err")"
	fi
	observed="$(printf '%s' "${observed}" | tr -d '[:space:]')"
	if [[ "${observed}" != "sha256:${pin}" ]]; then
		printf 'DRIFT scanner-%s: published %s != pinned sha256:%s (%s); expected digest: %s\n' \
			"${comp}" "${observed}" "${pin}" "${ref}" "${pin}" >&2
		drift=1
		continue
	fi
	oci="oci://${registry}/scanner-${comp}@sha256:${pin}"
	if ! "${gh_bin}" attestation verify "${oci}" -R "${publish_repo}" >"${work}/gh-${comp}.out" 2>"${work}/gh-${comp}.err"; then
		printf 'DRIFT scanner-%s: attestation verification failed for %s; expected digest: %s\n' \
			"${comp}" "${oci}" "${pin}" >&2
		drift=1
		continue
	fi
	printf 'ok - scanner-%s pinned %s matches sha-%s with a verified attestation\n' "${comp}" "${pin}" "${anchor:0:12}"
done

if ((drift > 0)); then
	printf 'verify-scanner-digest-pins: %d failing component(s); see DRIFT lines above\n' "${drift}" >&2
	exit 1
fi
printf 'verify-scanner-digest-pins: PASS (3 components at sha-%s, anchor in %s)\n' "${anchor:0:12}" "${publish_repo}"
