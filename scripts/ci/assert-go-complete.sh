#!/usr/bin/env bash
# Fail closed when a PR's trusted execution plan transfers a Go verdict.
set -euo pipefail

fail() {
  printf '::error::%s\n' "$*" >&2
  exit 1
}

[[ "${LANE:-}" == go-core || "${LANE:-}" == go-race ]] || fail 'invalid Go lane'
[[ "${PLAN_RESULT:-}" == success ]] || fail 'trusted PR plan failed'
case "${MODE:-}" in
  full|reuse|smoke) ;;
  *) fail 'trusted PR plan has an invalid mode' ;;
esac

if [[ "${GITHUB_EVENT_NAME:-}" == pull_request ]]; then
  [[ "${HEAD_SHA:-}" == "${PR_HEAD_SHA:-}" && "${HEAD_SHA:-}" =~ ^[0-9a-f]{40}$ ]] || fail 'PR plan does not bind this head'
  if [[ "${MODE}" == reuse || "${MODE}" == smoke ]]; then
    [[ "${SOURCE_SHA:-}" =~ ^[0-9a-f]{40}$ && "${TREE_SHA:-}" =~ ^[0-9a-f]{40}$ ]] || fail 'transferred verdict has no valid source/tree provenance'
    if [[ "${MODE}" == reuse || "${SELECTED:-}" != true ]]; then
      printf 'transferred %s verdict from %s\n' "${LANE}" "${SOURCE_SHA}"
      exit 0
    fi
    [[ "${LANE_RESULT:-}" == success ]] || fail "selected smoke ${LANE} result=${LANE_RESULT:-missing}"
    exit 0
  fi
fi

[[ "${CHANGES_RESULT:-}" == success ]] || fail "changes gate did not succeed (result=${CHANGES_RESULT:-missing})"
[[ "${LANE_RESULT:-}" == success || "${LANE_RESULT:-}" == skipped ]] || fail "${LANE} did not succeed (result=${LANE_RESULT:-missing})"
