#!/usr/bin/env bash
set -euo pipefail

# Selects the eshu-side docker-publish lanes (image, apkfloors, scannerpins)
# from the changed paths. The Helm chart lane moved to eshu-hq/eshu-deploy
# under #8871: eshu-deploy's chart-release.yml owns chart publication, so
# the chart lane and its triggers (deploy/helm/*, push-helm-chart.sh) no
# longer live here. Only image, apkfloors, and scannerpins remain.

if [[ "${GITHUB_REF_TYPE}" == tag || "${GITHUB_EVENT_NAME}" == workflow_dispatch ]]; then
  {
    echo 'image=true'
    echo 'apkfloors=true'
    echo 'scannerpins=true'
  } >> "${GITHUB_OUTPUT}"
  exit 0
fi

merge_group_lane=false
case "${GITHUB_EVENT_NAME}" in
  pull_request)
    head_sha="${PR_HEAD_SHA}"
    # Diff the PR head against its branch point, not the payload base.sha: that
    # is the base branch tip when the event fired, so a PR branched before later
    # main commits picks up the reverse of those commits and selects lanes it
    # never touched (#10248). The merge ref's first parent has the same flaw
    # here (it is a main tip too); only the merge base is the PR's own range.
    # Fall back to the payload value when no merge base resolves.
    base_sha="$(git merge-base "${PR_BASE_SHA}" "${head_sha}" 2>/dev/null || true)"
    base_sha="${base_sha:-${PR_BASE_SHA}}"
    ;;
  merge_group)
    merge_group_lane=true
    head_sha="${GITHUB_SHA}"
    base_sha="$(git merge-base origin/main "${head_sha}")"
    ;;
  *)
    base_sha="${BEFORE_SHA}"
    head_sha="${GITHUB_SHA}"
    if [[ "${base_sha}" =~ ^0+$ ]]; then
      base_sha="$(git rev-parse "${head_sha}^")"
    fi
    ;;
esac

image=false
apkfloors=false
scannerpins=false
while IFS= read -r path; do
  case "${path}" in
    Dockerfile)
      image=true
      apkfloors=true
      scannerpins=true
      ;;
    .dockerignore)
      image=true
      ;;
    scripts/verify-apk-floors.sh|scripts/test-verify-apk-floors.sh|scripts/fixtures/apk-floors/*|scripts/verify-doc-import-pins.sh|scripts/test-verify-doc-import-pins.sh)
      apkfloors=true
      ;;
    # Scanner digest pins (#9491): the drift-gate verifier with its
    # self-test, and the relocated scannercomponent leaf carrying the pins
    # (#8878), must run the scannerpins lane (self-test plus the live
    # anchored-tag and attestation checks). This arm stays above go/*,
    # which matches the same paths first and sets only the image lane.
    scripts/verify-scanner-digest-pins.sh|scripts/test-verify-scanner-digest-pins.sh|scripts/check-scanner-freshness.sh|scripts/test-check-scanner-freshness.sh|go/internal/collector/scannercomponent/*)
      scannerpins=true
      ;;
    # Shared installer used by the scannerpins live job (pinned crane
    # install): a lone touch must still select this gate's lane, or the
    # registry selects the gate while every publish job skips and the
    # aggregator fails closed.
    scripts/ci/go-install-retry.sh)
      scannerpins=true
      ;;
    scripts/verify-runtime-image-tools.sh|scripts/test-verify-runtime-image-tools.sh)
      image=true
      ;;
    # The shared rootfs-resolve library the runtime image verifier sources
    # must run the image lane (build + reproducibility + verify). The
    # scanner destination image builds were removed from eshu with #9741 (port
    # into eshu-scanner-worker in progress, #8878 box 10), so no scanner verifier selects a lane here anymore.
    scripts/lib/rootfs-resolve.sh)
      image=true
      ;;
    # The reproducibility build asserts the canonical mirror digests from
    # specs/ci-images.v1.json and feeds them to the runtime image build, so a
    # change to either file must run the image lane (build + reproducibility).
    # Without this the registry selects docker-image-reproducibility while
    # every publish job skips, and the aggregator fails closed.
    # The base-image spec change is worth a pins re-verification, so it
    # also runs the scannerpins lane; the reproducibility builder itself
    # stays image-only.
    specs/ci-images.v1.json)
      image=true
      scannerpins=true
      ;;
    scripts/ci/build-reproducibility-image.sh)
      image=true
      ;;
    # Document-import pins (#10164): the per-arch tesseract checksums feed
    # the job-document-import target build in build-and-push-image (image
    # lane) and the --stage audit in verify-apk-floors (apkfloors lane), so
    # a pin change must run both. Otherwise the registry selects those gates
    # while every publish job skips, and the aggregator fails closed.
    specs/doc-import-pins/**)
      image=true
      apkfloors=true
      ;;
    # Scanner module files: a go.mod/go.sum bump is worth a pins
    # re-verification, so it runs the scannerpins lane as well as the image
    # lane (which go/* below would otherwise select alone).
    # Stays above go/*, which matches the same paths first.
    go/go.mod|go/go.sum)
      if [[ "${merge_group_lane}" == false ]]; then
        image=true
      fi
      scannerpins=true
      ;;
    go/*)
      if [[ "${merge_group_lane}" == false ]]; then
        image=true
      fi
      ;;
    .github/workflows/docker-publish.yml|scripts/ci/select-docker-publish-lanes.sh)
      image=true
      apkfloors=true
      scannerpins=true
      ;;
  esac
done < <(git diff --name-only "${base_sha}" "${head_sha}")
{
  echo "image=${image}"
  echo "apkfloors=${apkfloors}"
  echo "scannerpins=${scannerpins}"
} >> "${GITHUB_OUTPUT}"
