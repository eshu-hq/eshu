#!/usr/bin/env bash
#
# dockerhub-login-ledger.sh - the CI image ledger model of
# verify-dockerhub-login.sh (#7886). Sourced, never run, after
# dockerhub-login-images.sh and dockerhub-login-dockerfile.sh.
#
# specs/ci-images.v1.json is the only approved-image list. This lib reads it, and
# the required-image set of scripts/lib/ci_image_compose.py, from the tree under
# test at run time; no digest or image name is copied into the gate. A stack is in
# CI image mode when ESHU_CI_IMAGES=1 and the env pin of every required ledger
# image equals the ledger destination@digest, which is exactly when
# scripts/lib/ci-image-compose.sh renders an overlay instead of failing. In that
# mode the overlay rewrites each Compose service image that is a ledger runtime
# source to its ledger pin, fails an image that is not a ledger runtime pin or an
# allowlisted runtime image, and gives a canonical root build the ledger build
# args. Outside it the stack keeps the images its files name and the host rules
# of image_is_hub apply.
#
# The caller defines: root, proc_env (LOOK), DF_ARGS.
#
# shellcheck disable=SC2034,SC2154  # globals are shared with the sourcing verifier

LEDGER_LOADED=0
LEDGER_REQUIRED=""   # id<TAB>env<TAB>pin per required image
LEDGER_RUNTIME=""    # runtime source, one per line
LEDGER_PINS=""       # pins of images that have runtime sources, one per line
LEDGER_ALLOW=""      # runtime_allowlist pins, one per line
LEDGER_BUILDARGS=""  # NAME=pin per ledger build_arg

# ledger_unreadable <reason>: says on stderr that CI image mode cannot be active.
# The tables stay empty, so every overlay-settled job is judged by the images its
# files name (the gate fails closed).
ledger_unreadable() {
  printf 'verify-dockerhub-login: the CI image ledger is unreadable (%s); CI image mode cannot be active, so a job that relies on the overlay is judged by the images its files name\n' "$1" >&2
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
  LEDGER_RUNTIME="$(jq -r '.images[].runtime_sources[]?' "${ledger}" 2>/dev/null || true)"
  LEDGER_PINS="$(jq -r '.images[] | select((.runtime_sources // []) | length > 0) | .destination + "@" + .digest' "${ledger}" 2>/dev/null || true)"
  LEDGER_ALLOW="$(jq -r '.runtime_allowlist[]?' "${ledger}" 2>/dev/null || true)"
  LEDGER_BUILDARGS="$(jq -r '.images[] | select(.build_arg != null) | .build_arg + "=" + .destination + "@" + .digest' "${ledger}" 2>/dev/null || true)"
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

# ledger_has <table> <value>: 0 when the newline-separated table holds the value.
ledger_has() {
  local line
  while IFS= read -r line; do [[ "${line}" != "$2" ]] || return 0; done <<<"$1"
  return 1
}

# ci_service_image_ok <expanded image>: in CI image mode, 0 when the overlay
# accepts a Compose service image: a ledger runtime source (rewritten to its
# pin), a ledger runtime pin, or an allowlisted runtime image. Any other image,
# Docker Hub or not, fails the helper at run time, so it is a reach here.
ci_service_image_ok() {
  [[ -n "$1" && "$1" != *'$'* && "$1" != *@UNRESOLVED@* ]] || return 1
  ledger_has "${LEDGER_RUNTIME}" "$1" && return 0
  ledger_has "${LEDGER_PINS}" "$1" && return 0
  ledger_has "${LEDGER_ALLOW}" "$1" && return 0
  return 1
}

# ci_overlay_build_args: sets DF_ARGS to the overlay's build args (the ledger
# build_arg pins) for a canonical root build in CI image mode.
ci_overlay_build_args() {
  DF_ARGS="${LEDGER_BUILDARGS}"$'\n'
}
