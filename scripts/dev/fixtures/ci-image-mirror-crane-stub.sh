#!/usr/bin/env bash
set -euo pipefail

command_name="$1"
shift
printf '%s %s\n' "${command_name}" "$*" >> "${CRANE_CALLS}"
case "${command_name}" in
  digest)
    ref="$1"
    case "${ref}" in
      *ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873)
        [[ -f "${CRANE_STATE}/alpine" ]] || exit 1
        digest='sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873'
        ;;
      *ci-postgres-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c)
        [[ -f "${CRANE_STATE}/bookworm" ]] || exit 1
        digest='sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c'
        ;;
      *ci-neo4j-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f)
        [[ -f "${CRANE_STATE}/neo4j" ]] || exit 1
        digest='sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f'
        ;;
      *postgres:18-alpine@*)
        digest='sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873'
        ;;
      *postgres:18.6-bookworm@*)
        digest='sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c'
        ;;
      *neo4j:2026-community@*)
        digest='sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f'
        ;;
      *) exit 2 ;;
    esac
    if [[ "${CRANE_BAD_SOURCE:-}" == true && "${ref}" == mirror.gcr.io/* ]]; then
      digest='sha256:0000000000000000000000000000000000000000000000000000000000000000'
    fi
    if [[ "${CRANE_BAD_DEST:-}" == true && "${ref}" == ghcr.io/* ]]; then
      digest='sha256:1111111111111111111111111111111111111111111111111111111111111111'
    fi
    printf '%s\n' "${digest}"
    ;;
  cp)
    [[ "$1" == '--no-clobber' ]] || exit 3
    case "$3" in
      *ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873) name=alpine ;;
      *ci-postgres-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c) name=bookworm ;;
      *ci-neo4j-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f) name=neo4j ;;
      *) exit 4 ;;
    esac
    [[ ! -f "${CRANE_STATE}/${name}" ]] || exit 1
    if [[ "${CRANE_COPY_FAIL_ON:-}" == "${name}" ]]; then
      exit 1
    fi
    touch "${CRANE_STATE}/${name}"
    if [[ "${CRANE_RACE_TARGET:-}" == "${name}" ]]; then
      exit 1
    fi
    ;;
  *) exit 5 ;;
esac
