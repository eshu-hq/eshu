#!/usr/bin/env bash
#
# dockerhub-login-ledger.sh - the CI image ledger model of
# verify-dockerhub-login.sh (#7886). Sourced, never run, after
# dockerhub-login-images.sh and dockerhub-login-dockerfile.sh.
#
# specs/ci-images.v1.json is the only approved-image list. This lib reads it, and
# the required-image set of scripts/lib/ci_image_compose.py, from the tree under
# test at run time; no digest or image name is copied into the gate. A step is in
# CI image mode when ESHU_CI_IMAGES=1 and the env pin of every required ledger
# image equals the ledger destination@digest, which is exactly when
# scripts/lib/ci-image-compose.sh renders an overlay instead of failing. CI image
# mode settles one thing: a call of the CI compose wrapper (ci_wrapper_active).
# It does not settle a direct compose call, whatever it appends or prepares: that
# call is judged by the images its files name.
#
# The caller defines: root, proc_env (LOOK).
#
# shellcheck disable=SC2034,SC2154  # globals are shared with the sourcing verifier

LEDGER_LOADED=0
LEDGER_REQUIRED=""   # id<TAB>env<TAB>pin per required image

# ledger_unreadable <reason>: says on stderr that CI image mode cannot be active.
# The table stays empty, so a wrapper call is judged by the images its files name
# (the gate fails closed).
ledger_unreadable() {
  printf 'verify-dockerhub-login: the CI image ledger is unreadable (%s); CI image mode cannot be active, so a call of the CI compose wrapper is judged by the images its files name\n' "$1" >&2
}

# ledger_load: reads the ledger and the helper's required ids once. Leaves the
# tables empty (CI image mode can never be active) when either file is missing or
# unreadable.
ledger_load() {
  local ledger="${root}/specs/ci-images.v1.json" helper="${root}/scripts/lib/ci_image_compose.py" ids id rows
  [[ "${LEDGER_LOADED}" -eq 0 ]] || return 0
  LEDGER_LOADED=1
  if [[ ! -f "${ledger}" || ! -f "${helper}" ]]; then ledger_unreadable "missing ${ledger##*/} or ${helper##*/}"; return 0; fi
  ids="$(rg -o -N '_REQUIRED = frozenset\(\{[^}]*\}\)' "${helper}" | rg -o '"[a-z0-9-]+"' | tr -d '"' || true)"
  [[ -n "${ids}" ]] || { ledger_unreadable "no required image ids in ${helper##*/}"; return 0; }
  rows="$(jq -r '.images[] | [.id, (.env // ""), (.destination + "@" + .digest)] | @tsv' "${ledger}" 2>/dev/null)" || { ledger_unreadable "${ledger##*/} is not valid ledger JSON"; return 0; }
  while IFS= read -r id; do
    [[ -n "${id}" ]] || continue
    LEDGER_REQUIRED="${LEDGER_REQUIRED}$(printf '%s\n' "${rows}" | awk -F'\t' -v id="${id}" '$1 == id { print }')"$'\n'
  done <<<"${ids}"
}

# ci_mode_active: 0 when the step's process env (step over job over workflow)
# puts the stack in CI image mode, 1 otherwise.
ci_mode_active() {
  local id env pin
  ledger_load
  [[ -n "${LEDGER_REQUIRED//[$'\n']/}" ]] || return 1
  proc_env ESHU_CI_IMAGES
  [[ "${LOOK}" == 1 ]] || return 1
  while IFS=$'\t' read -r id env pin; do
    [[ -n "${id}" ]] || continue
    [[ -n "${env}" ]] || return 1
    proc_env "${env}"
    [[ "${LOOK}" == "${pin}" ]] || return 1
  done <<<"${LEDGER_REQUIRED}"
  return 0
}

# ci_wrapper_active: 0 when the step is in CI image mode and the CI compose
# wrapper is in the tree under test. Then a call of scripts/ci/compose.sh is
# settled: the wrapper renders an overlay that covers every image or exits 1, and
# in CI it refuses to run without CI image mode. Otherwise a wrapper call is an
# ordinary call, judged by the images its files name.
ci_wrapper_active() {
  ci_mode_active || return 1
  [[ -f "${root}/scripts/ci/compose.sh" ]]
}
