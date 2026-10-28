#!/usr/bin/env bash
# Workflow-artifact citation support for verify-doc-citations.sh (#9315).
# This file is sourced after repo_root, docs_root, tmp_dir, and log exist.
#
# Docs may cite the CI run that recorded evidence instead of a committed
# file: `artifact:<run-id>/<artifact-name>` or a full Actions run URL. Runs
# live outside the repo, so a well-formed pointer always resolves: it never
# enters the baseline. A malformed pointer (a run id with no artifact name,
# or a name with no run id) fails like a subtest-form citation -- never
# baseline-able, with a message naming the expected shape.
#
# Only pointer-shaped tokens are citations. `artifact:<GoIdentifier>` prose
# (e.g. artifact:EvidenceArtifact) and a bare `artifact:` with no ref are
# not citation attempts and are ignored.

# scan_artifact_citations emits one "<doc-relpath> <citation>" line per
# pointer-shaped `artifact:` token across the scanned doc set, sorted for
# determinism. Same exclusion rules as the test/fixture scans
# (placeholder/glob shorthand, parent-escape, absolute path, ellipsis).
scan_artifact_citations() {
  (cd "${docs_root}" && rg --hidden --no-ignore --no-heading --no-line-number -o \
    -g '*.md' \
    'artifact:([0-9]+(/[A-Za-z0-9_./#:-]*)?|https?://[^[:space:]<>*]+|/[A-Za-z0-9_./#:-]+)' . 2>/dev/null || true) \
    | LC_ALL=C awk '{
        idx = index($0, ":"); if (idx == 0) next
        rel = substr($0, 1, idx - 1); m = substr($0, idx + 1)
        sub(/^\.\//, "", rel)
        if (m == "" || rel == "") next
        if (m ~ /\.\./) next
        if (index(m, "<") || index(m, ">") || index(m, "*") || index(m, "\xe2\x80\xa6")) next
        print rel " " m
      }' \
    | LC_ALL=C sort -u
}

# artifact_ref_wellformed reports whether the ref after `artifact:` names a
# resolvable run output: a numeric run id plus an artifact name, or a full
# run URL. Anything else matched the pointer scan but is not a pointer.
artifact_ref_wellformed() {
  local ref="${1#artifact:}"
  case "${ref}" in
    https://*|http://*) return 0 ;;
    [0-9]*/*)
      [ -n "${ref#*/}" ] && [[ "${ref%%/*}" != *[^0-9]* ]] && return 0
      return 1
      ;;
    *) return 1 ;;
  esac
}

# classify_artifact_citations splits scan_artifact_citations' output into
# malformed rows (written to $2, never baseline-able). Well-formed rows need
# no record: runs resolve outside the repo.
classify_artifact_citations() {
  local citations_file="$1" malformed_out="$2"
  local rel citation
  : >"${malformed_out}"
  while IFS=' ' read -r rel citation; do
    [ -z "${rel}" ] && continue
    if ! artifact_ref_wellformed "${citation}"; then
      printf '%s %s\n' "${rel}" "${citation}" >>"${malformed_out}"
    fi
  done <"${citations_file}"
}

# check_artifact_citations scans, classifies, and fails on malformed
# pointers. Returns 0 when every artifact citation is well-formed.
check_artifact_citations() {
  local workdir="$1"
  local malformed_count
  scan_artifact_citations >"${workdir}/artifact-citations.txt"
  classify_artifact_citations "${workdir}/artifact-citations.txt" \
    "${workdir}/artifact-malformed.txt"
  malformed_count="$(awk 'NF' "${workdir}/artifact-malformed.txt" | wc -l | tr -d ' ')"
  if [[ "${malformed_count}" -gt 0 ]]; then
    while IFS=' ' read -r rel citation; do
      [ -z "${rel}" ] && continue
      log "${rel} cites malformed workflow artifact ${citation} -- use artifact:<run-id>/<artifact-name> or a full run URL"
    done <"${workdir}/artifact-malformed.txt"
    log "${malformed_count} malformed artifact citation(s) (never baseline-able)"
    return 1
  fi
  return 0
}

# warn_artifact_citations is the -update path: malformed pointers are
# reported but never written to the baseline, mirroring subtest handling.
warn_artifact_citations() {
  local workdir="$1"
  local malformed_count
  scan_artifact_citations >"${workdir}/artifact-citations.txt"
  classify_artifact_citations "${workdir}/artifact-citations.txt" \
    "${workdir}/artifact-malformed.txt"
  malformed_count="$(awk 'NF' "${workdir}/artifact-malformed.txt" | wc -l | tr -d ' ')"
  if [[ "${malformed_count}" -gt 0 ]]; then
    log "WARNING: ${malformed_count} malformed artifact citation(s) present; these are never baselined and will still fail -- fix them to artifact:<run-id>/<artifact-name> or a full run URL:"
    while IFS=' ' read -r rel citation; do
      [ -z "${rel}" ] && continue
      log "  ${rel} cites ${citation}"
    done <"${workdir}/artifact-malformed.txt"
  fi
}
