#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

workflow=".github/workflows/docker-publish.yml"

require_workflow_pattern() {
	local pattern="$1"
	local message="$2"

	if ! rg -F -q -- "$pattern" "$workflow"; then
		printf '%s\n' "$message" >&2
		exit 1
	fi
}

require_workflow_pattern \
	"IMAGE_PLATFORMS: \${{ github.event_name == 'pull_request' && 'linux/amd64' || 'linux/amd64,linux/arm64' }}" \
	'docker publish workflow must limit pull_request image builds to linux/amd64'

require_workflow_pattern \
	'platforms: ${{ env.IMAGE_PLATFORMS }}' \
	'docker publish build step must use IMAGE_PLATFORMS'

require_workflow_pattern \
	'uses: sigstore/cosign-installer@v4.1.0' \
	'docker publish workflow must install cosign for image signing and verification'

require_workflow_pattern \
	'cosign sign --yes "${IMAGE_REF}"' \
	'docker publish workflow must keylessly sign pushed image digests'

require_workflow_pattern \
	'cosign verify "${IMAGE_REF}" \' \
	'docker publish workflow must verify pushed image signatures in CI'

require_workflow_pattern \
	'uses: anchore/sbom-action@v0' \
	'docker publish workflow must generate a Syft SBOM'

require_workflow_pattern \
	'image: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}@${{ steps.build.outputs.digest }}' \
	'docker publish workflow must generate the SBOM from the pushed image digest'

require_workflow_pattern \
	'format: spdx-json' \
	'docker publish workflow must produce an SPDX JSON SBOM'

require_workflow_pattern \
	'upload-release-assets: false' \
	'docker publish workflow must keep release asset upload out of the PR-capable build job'

require_workflow_pattern \
	'uses: actions/download-artifact@v4' \
	'docker publish workflow must download the generated SBOM artifact before release upload'

require_workflow_pattern \
	'gh release upload "${GITHUB_REF_NAME}" "dist/eshu-image-sbom.spdx.json" \' \
	'docker publish workflow must attach the generated SBOM to the GitHub Release'

require_workflow_pattern \
	'--clobber' \
	'docker publish workflow must make SBOM release asset upload idempotent'

require_workflow_pattern \
	"if: needs.changes.outputs.image == 'true' && github.ref_type == 'tag'" \
	'docker publish workflow must restrict release SBOM asset publishing to image tag releases'

require_workflow_pattern \
	'uses: actions/attest@v4' \
	'docker publish workflow must publish an SBOM attestation'

require_workflow_pattern \
	'sbom-path: dist/eshu-image.spdx.json' \
	'docker publish workflow must attest the generated SBOM'

require_workflow_pattern \
	'gh attestation verify "${OCI_IMAGE_REF}" \' \
	'docker publish workflow must verify image attestations in CI'

require_workflow_pattern \
	'--predicate-type https://spdx.dev/Document/v2.3' \
	'docker publish workflow must verify SPDX SBOM attestations in CI'

require_workflow_pattern \
	'actions: read' \
	'docker publish workflow must allow SBOM release asset lookup'

require_workflow_pattern \
	'contents: write' \
	'docker publish workflow must allow tag releases to attach SBOM assets'

if ! rg -F -q 'packages: write' "$workflow" ||
	! rg -F -q 'id-token: write' "$workflow" ||
	! rg -F -q 'attestations: write' "$workflow"; then
	printf 'docker publish workflow must grant package, OIDC, and attestation permissions\n' >&2
	exit 1
fi

printf 'docker publish release integrity guards passed\n'
