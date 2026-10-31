#!/usr/bin/env bash
# verify-scanner-digest-pins.sh - fail when a published scanner image digest
# drifted from its artifactDigests pin in
# go/internal/collector/scannerworker/component/manifest.go (#9491).
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
#   2. `gh attestation verify` re-verifies the pinned digest with the
#      in-workflow token.
#   3. Staleness: no scanner Go source newer than the anchor on the main
#      line. The range is anchor..merge-base(HEAD, origin/main), so a PR's
#      own input changes never flag it; only inputs already on main do.
#      Scanner inputs are *.go under go/internal/collector/scannerworker and
#      the go/cmd/scanner-* trees, excluding the pin files themselves
#      (manifest.go carries the pins and the anchor; manifest_test.go repeats
#      the pins, so a follow-up pin PR touching only those stays green).
#      The Dockerfile, the publish workflow, and the base-image pins are NOT
#      file-granularity staleness inputs: they changed after the anchor
#      without moving the anchored tags, and file-granularity tracking
#      cannot tell a comment from a stage change. Section 4 below covers
#      them with a normalized content hash instead, so a republish driven
#      only by those files now reddens this gate; the anchored digest and
#      attestation checks still pin the activation contract.
#   4. Non-Go staleness (#9503): the normalized content hash of the
#      scanner-relevant non-Go inputs is identical at the anchor and at the
#      merge-base. The hashed content is the Dockerfile base-pin ARGs, the
#      builder toolchain lines feeding the scanner binaries, and the
#      scanner-* stages (comments and blank lines stripped, so a
#      comment-only edit stays green); the workflow env mirrors plus the
#      scanner publish job; the xx/golang/alpine digests from
#      specs/ci-images.v1.json; and the raw go/go.mod + go/go.sum bytes (a
#      module bump fails here and is recorded as staleness). Both hashes are
#      recomputed from history with `git show`: no new pin file, no stored
#      hash to drift. A mismatch names the differing input class and points
#      at the follow-up pin PR flow.
#
# Usage:
#   scripts/verify-scanner-digest-pins.sh [--manifest PATH]
#
# Environment:
#   SCANNER_PINS_REGISTRY  Registry holding the scanner images
#                          (default ghcr.io/eshu-hq).
#   SCANNER_PINS_REPO      Repository owning the build attestations
#                          (default eshu-hq/eshu).
#   CRANE_BIN              crane binary (default crane).
#   GH_BIN                 gh binary (default gh).
#
# Exit codes:
#   0 - every pin matches its anchored tag, every attestation verifies, and
#       no scanner input is newer than the anchor.
#   1 - drift: a digest mismatch, a missing tag, a failed attestation, or a
#       stale anchor. The failure names the component and the expected
#       digest; a stale anchor also names the changed inputs and points at
#       the follow-up pin PR flow.
#   2 - fail closed: bad usage, unreadable manifest, a missing or malformed
#       anchor line, an unparseable pin, an anchor outside history, a missing
#       origin/main ref, or a transport failure. A network error never
#       passes and never reports drift.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="${repo_root}/go/internal/collector/scannerworker/component/manifest.go"
registry="${SCANNER_PINS_REGISTRY:-ghcr.io/eshu-hq}"
attest_repo="${SCANNER_PINS_REPO:-eshu-hq/eshu}"
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
for tool in "${crane_bin}" "${gh_bin}" git python3 sha256sum; do
	command -v "${tool}" >/dev/null 2>&1 || die "required tool not found: ${tool}"
done

anchor_count="$(grep -c '^// Scanner-Publish-SHA: ' "${manifest}" || true)"
[[ "${anchor_count}" == 1 ]] || die "want exactly one '// Scanner-Publish-SHA: <40-hex>' line in ${manifest}, found ${anchor_count}"
anchor="$(sed -n 's|^// Scanner-Publish-SHA: \([0-9a-f]*\)$|\1|p' "${manifest}")"
[[ "${anchor}" =~ ^[0-9a-f]{40}$ ]] || die "malformed anchor line in ${manifest}; want '// Scanner-Publish-SHA: <40-hex>'"

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
	if ! "${gh_bin}" attestation verify "${oci}" -R "${attest_repo}" >"${work}/gh-${comp}.out" 2>"${work}/gh-${comp}.err"; then
		printf 'DRIFT scanner-%s: attestation verification failed for %s; expected digest: %s\n' \
			"${comp}" "${oci}" "${pin}" >&2
		drift=1
		continue
	fi
	printf 'ok - scanner-%s pinned %s matches sha-%s with a verified attestation\n' "${comp}" "${pin}" "${anchor:0:12}"
done

# Staleness last, so a state that is both drifted and stale reports the
# concrete digest mismatch above AND the anchor verdict below.
manifest_repo="$(git -C "$(dirname "${manifest}")" rev-parse --show-toplevel 2>/dev/null)" ||
	die "cannot find the git repo holding ${manifest}"
git -C "${manifest_repo}" rev-parse --verify --quiet origin/main >/dev/null ||
	die "need the origin/main ref (checkout with fetch-depth 0); cannot prove freshness without it"
end="$(git -C "${manifest_repo}" merge-base HEAD origin/main 2>/dev/null)" ||
	die "cannot find merge-base(HEAD, origin/main)"
git -C "${manifest_repo}" merge-base --is-ancestor "${anchor}" "${end}" ||
	die "anchor ${anchor} is not history of ${end}; re-anchor to the merged publish that produced the pins"
inputs=(
	go/internal/collector/scannerworker
	go/cmd/scanner-component-image
	go/cmd/scanner-component-sbom
	go/cmd/scanner-component-osruntime
	go/cmd/scanner-worker
)
changed="$(git -C "${manifest_repo}" diff --name-only "${anchor}" "${end}" -- "${inputs[@]}")" ||
	die "cannot diff ${anchor}..${end} over the scanner inputs"
stale="$(printf '%s\n' "${changed}" | grep '\.go$' |
	grep -v -x -e 'go/internal/collector/scannerworker/component/manifest.go' \
		-e 'go/internal/collector/scannerworker/component/manifest_test.go' || true)"
if [[ -n "${stale}" ]]; then
	printf 'STALE scanner inputs advanced past anchor %s:\n' "${anchor}" >&2
	printf '%s\n' "${stale}" | sed 's/^/  /' >&2
	printf 'File a follow-up pin PR bumping Scanner-Publish-SHA and the pins together (the pattern #9494 uses).\n' >&2
	for comp in "${components[@]}"; do
		printf 'stale pin scanner-%s; expected digest: %s\n' "${comp}" "${pin_of[${comp}]}" >&2
	done
	drift=1
fi

# Content-hash staleness for scanner-relevant non-Go inputs (#9503). Each
# *_at helper prints the normalized class content at a git ref (empty when
# the file is absent there, so a file missing at both ends hashes the same).
# Comments and blank lines are stripped where file-granularity tracking would
# false-positive, and unrelated stages/jobs are excluded entirely.
nongo_dockerfile_at() {
	git -C "${manifest_repo}" show "$1:Dockerfile" 2>/dev/null | awk '
		/^ARG ESHU_/ { line = $0; sub(/[[:space:]][[:space:]]*#.*$/, "", line); print line; next }
		/^FROM[[:space:]]/ { stage = $NF; if (stage == "builder" || stage ~ /^scanner-/) { line = $0; sub(/[[:space:]][[:space:]]*#.*$/, "", line); print line }; next }
		stage == "builder" { emit = 1 }
		stage ~ /^scanner-/ { emit = 1 }
		emit { line = $0; sub(/^[[:space:]]*#.*$/, "", line); sub(/[[:space:]][[:space:]]*#.*$/, "", line); if (line !~ /^[[:space:]]*$/) print line; emit = 0 }
	' || true
}
nongo_workflow_at() {
	{
		git -C "${manifest_repo}" show "$1:.github/workflows/docker-publish.yml" 2>/dev/null |
			grep -E 'ESHU_(XX|GO|ALPINE)_IMAGE:|IMAGE_PLATFORMS:' |
			sed -e 's/[[:space:]][[:space:]]*#.*$//' -e '/^[[:space:]]*#/d' -e '/^[[:space:]]*$/d' || true
		git -C "${manifest_repo}" show "$1:.github/workflows/docker-publish.yml" 2>/dev/null | awk '
			/^  build-and-push-scanner-images:/ { in_job = 1; next }
			/^  [A-Za-z]/ { in_job = 0 }
			in_job { line = $0; sub(/^[[:space:]]*#.*$/, "", line); sub(/[[:space:]][[:space:]]*#.*$/, "", line); if (line !~ /^[[:space:]]*$/) print line }
		' || true
	} || true
}
nongo_ciimages_at() {
	git -C "${manifest_repo}" show "$1:specs/ci-images.v1.json" 2>/dev/null | python3 -c '
import json, sys
try:
    doc = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for img in doc.get("images", []):
    if img.get("id") in ("xx", "golang", "alpine") and img.get("digest"):
        print(img["digest"])
' 2>/dev/null || true
}
nongo_gomod_at() {
	{
		git -C "${manifest_repo}" show "$1:go/go.mod" 2>/dev/null || true
		printf '%s\n' '--- go.sum ---'
		git -C "${manifest_repo}" show "$1:go/go.sum" 2>/dev/null || true
	} || true
}

nongo_classes=(dockerfile workflow ciimages gomod)
nongo_label_dockerfile="Dockerfile (base pins, builder toolchain, scanner stages)"
nongo_label_workflow=".github/workflows/docker-publish.yml (env mirrors, scanner job)"
nongo_label_ciimages="specs/ci-images.v1.json (xx/golang/alpine digests)"
nongo_label_gomod="go/go.mod + go/go.sum (module files)"
nongo_changed=()
for class in "${nongo_classes[@]}"; do
	fn="nongo_${class}_at"
	old_hash="$("${fn}" "${anchor}" | sha256sum | cut -d' ' -f1)"
	new_hash="$("${fn}" "${end}" | sha256sum | cut -d' ' -f1)"
	[[ "${old_hash}" == "${new_hash}" ]] || nongo_changed+=("${class}")
done
if ((${#nongo_changed[@]} > 0)); then
	printf 'STALE scanner non-Go inputs advanced past anchor %s (content-hash):\n' "${anchor}" >&2
	for class in "${nongo_changed[@]}"; do
		label_var="nongo_label_${class}"
		printf '  %s\n' "${!label_var}" >&2
	done
	printf 'File a follow-up pin PR bumping Scanner-Publish-SHA and the pins together (the pattern #9494 uses).\n' >&2
	for comp in "${components[@]}"; do
		printf 'stale pin scanner-%s; expected digest: %s\n' "${comp}" "${pin_of[${comp}]}" >&2
	done
	drift=1
fi

if ((drift > 0)); then
	printf 'verify-scanner-digest-pins: %d failing component(s); see DRIFT/STALE lines above\n' "${drift}" >&2
	exit 1
fi
printf 'verify-scanner-digest-pins: PASS (3 components at sha-%s, inputs fresh)\n' "${anchor:0:12}"
