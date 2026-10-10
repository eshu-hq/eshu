#!/usr/bin/env bash
set -euo pipefail

# A stub registry for the publisher safety test. CRANE_ROWS is a tab-separated
# file the test writes from its own approved table: name, source reference,
# destination reference, index digest. References match whole, so a publisher
# that spells a source or destination differently from the table is rejected
# here instead of being accepted by pattern.
command_name="$1"
shift
printf '%s %s\n' "${command_name}" "$*" >> "${CRANE_CALLS}"

# Sets row_name and row_digest when "$2" is the source or the destination of a
# row; sets row_side to source or destination.
lookup_row() {
  local ref="$1" name source target digest
  row_name='' row_digest='' row_side=''
  while IFS=$'\t' read -r name source target digest; do
    if [[ "${ref}" == "${target}" ]]; then
      row_name="${name}" row_digest="${digest}" row_side=destination
      return 0
    fi
    if [[ "${ref}" == "${source}" ]]; then
      row_name="${name}" row_digest="${digest}" row_side=source
      return 0
    fi
  done < "${CRANE_ROWS}"
  return 1
}

case "${command_name}" in
  digest)
    ref="$1"
    lookup_row "${ref}" || exit 2
    if [[ "${row_side}" == destination ]]; then
      [[ -f "${CRANE_STATE}/${row_name}" ]] || exit 1
    fi
    digest="${row_digest}"
    if [[ "${row_side}" == source ]] &&
        [[ "${CRANE_BAD_SOURCE:-}" == true || "${CRANE_BAD_SOURCE_ROW:-}" == "${row_name}" ]]; then
      digest='sha256:0000000000000000000000000000000000000000000000000000000000000000'
    fi
    if [[ "${row_side}" == destination ]] &&
        [[ "${CRANE_BAD_DEST:-}" == true || "${CRANE_BAD_DEST_ROW:-}" == "${row_name}" ]]; then
      digest='sha256:1111111111111111111111111111111111111111111111111111111111111111'
    fi
    printf '%s\n' "${digest}"
    ;;
  cp)
    [[ "$1" == '--no-clobber' ]] || exit 3
    lookup_row "$3" && [[ "${row_side}" == destination ]] || exit 4
    name="${row_name}"
    # The copy must read the source this row approved, not another row's.
    lookup_row "$2" && [[ "${row_side}" == source && "${row_name}" == "${name}" ]] || exit 4
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
