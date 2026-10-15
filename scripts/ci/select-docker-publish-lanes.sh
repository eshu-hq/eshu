#!/usr/bin/env bash
set -euo pipefail

if [[ "${GITHUB_REF_TYPE}" == tag || "${GITHUB_EVENT_NAME}" == workflow_dispatch ]]; then
  {
    echo 'image=true'
    echo 'chart=true'
    echo 'apkfloors=true'
  } >> "${GITHUB_OUTPUT}"
  exit 0
fi

merge_group_lane=false
case "${GITHUB_EVENT_NAME}" in
  pull_request)
    base_sha="${PR_BASE_SHA}"
    head_sha="${PR_HEAD_SHA}"
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
chart=false
apkfloors=false
while IFS= read -r path; do
  case "${path}" in
    Dockerfile)
      image=true
      apkfloors=true
      ;;
    .dockerignore)
      image=true
      ;;
    scripts/verify-apk-floors.sh|scripts/test-verify-apk-floors.sh|scripts/fixtures/apk-floors/*)
      apkfloors=true
      ;;
    scripts/verify-runtime-image-tools.sh|scripts/test-verify-runtime-image-tools.sh)
      image=true
      ;;
    go/*)
      if [[ "${merge_group_lane}" == false ]]; then
        image=true
      fi
      ;;
    deploy/helm/*)
      chart=true
      ;;
    .github/workflows/docker-publish.yml|scripts/ci/select-docker-publish-lanes.sh)
      image=true
      chart=true
      apkfloors=true
      ;;
    scripts/ci/push-helm-chart.sh)
      chart=true
      ;;
  esac
done < <(git diff --name-only "${base_sha}" "${head_sha}")
{
  echo "image=${image}"
  echo "chart=${chart}"
  echo "apkfloors=${apkfloors}"
} >> "${GITHUB_OUTPUT}"
