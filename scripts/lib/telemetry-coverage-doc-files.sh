#!/usr/bin/env bash
#
# telemetry-coverage-doc-files.sh — resolve the set of Markdown files the X2
# telemetry-coverage gate reads, split out of scripts/verify-telemetry-coverage.sh
# to keep that script under the repo's 500-line file cap.
#
# Sourced by scripts/verify-telemetry-coverage.sh; not meant to run
# standalone. Both functions expect repo_root and doc_path to be set by the
# caller.
#
# #9789 split docs/public/observability/telemetry-coverage.md into an index
# (the original path, so every existing link and agent instruction still
# lands on it) plus sibling part files named telemetry-coverage-<area>.md. A
# part is any telemetry-coverage-*.md beside the index except
# telemetry-coverage-notes.md, which is prose about the gate, not a table.
# Discovering parts by glob, not from a hand-kept list, means a new part is
# read the moment it exists; a list would let a part's rows go unchecked
# until someone remembered to register it.

# collect_telemetry_coverage_doc_files sets the global array DOC_FILES to the
# index followed by every part, as absolute paths in byte order. Global-array
# output rather than a nameref keeps bash 3.2 compatibility, matching
# resolve_row_cell_paths_into in telemetry-coverage-row-check.sh.
collect_telemetry_coverage_doc_files() {
  local index dir part
  index="$repo_root/$doc_path"
  dir="$(dirname "$index")"
  DOC_FILES=("$index")
  DOC_PART_FILES=()
  while IFS= read -r part; do
    [ -n "$part" ] || continue
    case "${part##*/}" in
      telemetry-coverage-notes.md) continue ;;
    esac
    DOC_FILES+=("$part")
    DOC_PART_FILES+=("$part")
  done < <(compgen -G "$dir/telemetry-coverage-*.md" | LC_ALL=C sort)
}

# check_telemetry_coverage_parts_linked fails the gate for a part the index
# does not link. Readers and agents start at the index, so an unlinked part
# is a table nobody is told to read or extend. It mutates the caller's global
# report and drift, like the numbered checks in the verifier.
check_telemetry_coverage_parts_linked() {
  local part name
  for part in ${DOC_PART_FILES[@]+"${DOC_PART_FILES[@]}"}; do
    name="${part##*/}"
    if ! rg -qF "](${name})" "$repo_root/$doc_path"; then
      report="${report}  - part ${name} is not linked from ${doc_path}; add it to the index's part list
"
      drift=1
    fi
  done
}
