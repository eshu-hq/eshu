#!/usr/bin/env bash
# Workflow-artifact evidence pointers for verify-performance-evidence.sh.
#
# Sourced, never executed. A hot-path PR can satisfy the performance gate
# either with benchmark markers in a committed evidence file (the verifier's
# own block) or by pointing at the workflow run that recorded the evidence
# instead of committing the record. This file owns the second path while
# evidence leaves docs/internal/evidence/.
#
# Sourced after changed_files, _perf_diff_cache, and TMPDIR exist. Results
# come back in globals, because the no-artifact case is a quiet pass-through
# that a command substitution would complicate under `set -e`.
#
# A pointer counts only in the PR's own ADDED lines (same attribution rule
# as committed markers: an untouched pointer from an earlier PR must not
# satisfy this one) and only outside testdata/vendor/generated trees (a
# fixture carrying a marker must never satisfy the gate, eshu-hq/eshu#5542
# follow-up). The ref must be namespaced -- run-id/artifact-name or a full
# Actions run URL, both of which contain a `/` a bare word never does. The
# gate checks the pointer is present and shaped, not that the artifact
# resolves; the reviewer verifies that.

# Collects the added diff lines of EVERY listed file in one awk pass over
# the already-fetched _perf_diff_cache, instead of re-scanning the whole
# cache per file (a 6.5MB cache times 840 evidence files). Callers pass a
# set-or-nothing question ("did any listed file contribute a matching added
# line"), so scanning the union with the unchanged marker patterns decides
# exactly what a per-file loop would decide. Uses exact path membership
# (associative array loaded from the list file), never regex, so paths
# containing regex metacharacters (e.g. a literal `.` in a filename) still
# compare exactly.
evidence_added_union() {
  local list_path="$1"
  # shellcheck disable=SC2154  # _perf_diff_cache is set by the sourcing verifier.
  [ -n "${_perf_diff_cache}" ] || return 0
  printf '%s\n' "${_perf_diff_cache}" | awk -v list="${list_path}" '
    BEGIN {
      while ((getline line < list) > 0) evidence[line] = 1
    }
    substr($0, 1, 6) == "+++ b/" {
      # No /dev/null guard needed here: "+++ b/" is exactly six characters, so
      # substr($0, 7) is always the path *after* b/ -- for "+++ b/dev/null" it
      # is "dev/null", never "/dev/null" or "b/dev/null". A real deleted-file
      # header is "+++ /dev/null", which does not match this rule at all and is
      # handled by the next one. The shell loop in _perf_code_change_map keeps
      # an equivalent check for its own reasons; do not copy it back here.
      cur = substr($0, 7)
      next
    }
    $0 == "+++ /dev/null" { cur = ""; next }
    substr($0, 1, 6) == "--- a/" { next }
    $0 == "--- /dev/null" { next }
    substr($0, 1, 1) == "+" {
      if (cur in evidence) print substr($0, 2)
      next
    }
  '
}

# perf_artifact_has_performance / perf_artifact_has_observability report the
# artifact path: 0 when an added line outside fixture trees carries a shaped
# workflow-artifact marker for that half of the gate, 1 otherwise. The
# verifier ORs these into its own flags, so the committed-file path stays
# accepted unchanged.
# shellcheck disable=SC2034  # consumed by the sourcing verifier.
perf_artifact_has_performance=1
# shellcheck disable=SC2034  # consumed by the sourcing verifier.
perf_artifact_has_observability=1

perf_apply_workflow_artifact_evidence() {
  # shellcheck disable=SC2034  # consumed by the sourcing verifier.
  perf_artifact_has_performance=1
  # shellcheck disable=SC2034  # consumed by the sourcing verifier.
  perf_artifact_has_observability=1
  # shellcheck disable=SC2154  # changed_files is set by the sourcing verifier.
  [ "${#changed_files[@]}" -gt 0 ] || return 0
  # shellcheck disable=SC2154  # _perf_diff_cache is set by the sourcing verifier.
  [ -n "${_perf_diff_cache}" ] || return 0
  _perf_artifact_added_path="$(mktemp "${TMPDIR:-/tmp}/eshu-performance-artifact-added.XXXXXX")"
  _perf_artifact_list="$(mktemp "${TMPDIR:-/tmp}/eshu-performance-artifact-list.XXXXXX")"
  for _perf_changed_file in "${changed_files[@]}"; do
    case "${_perf_changed_file}" in
      testdata/*|*/testdata/*|vendor/*|*/vendor/*|generated/*|*/generated/*) continue ;;
    esac
    printf '%s\n' "${_perf_changed_file}" >>"$_perf_artifact_list"
  done
  evidence_added_union "$_perf_artifact_list" >"$_perf_artifact_added_path"
  rm -f "$_perf_artifact_list"
  if [ -s "$_perf_artifact_added_path" ]; then
    # Match against a file, never a pipe: `printf ... | rg -q` races under
    # `set -o pipefail` (see the committed-marker block in the verifier).
    if rg -q -e '(^|[[:space:]])(Performance Evidence|Benchmark Evidence|No-Regression Evidence)[[:space:]]*\(workflow-artifact:[^()]*\/[^()]*\)[[:space:]]*:' "$_perf_artifact_added_path"; then
      # shellcheck disable=SC2034  # consumed by the sourcing verifier.
      perf_artifact_has_performance=0
    fi
    if rg -q -e '(^|[[:space:]])(Observability Evidence|No-Observability-Change)[[:space:]]*\(workflow-artifact:[^()]*\/[^()]*\)[[:space:]]*:' "$_perf_artifact_added_path"; then
      # shellcheck disable=SC2034  # consumed by the sourcing verifier.
      perf_artifact_has_observability=0
    fi
  fi
  rm -f "$_perf_artifact_added_path"
}

# perf_print_artifact_hint prints the failure-block lines that teach the
# artifact alternative. Called only on the failing path.
perf_print_artifact_hint() {
  printf '\nOr point at the workflow run that recorded the evidence instead of\n'
  printf 'committing the record, with a namespaced artifact ref in any added line:\n'
  printf '  - Performance Evidence (workflow-artifact: <run-id>/<artifact-name>):\n'
  printf '  - No-Regression Evidence (workflow-artifact: https://github.com/<org>/<repo>/actions/runs/<id>):\n'
  printf 'and the matching Observability Evidence (workflow-artifact: <ref>): form.\n'
  printf 'The reviewer verifies the artifact resolves; the gate checks the pointer.\n'
}
