#!/usr/bin/env bash
# Raw Go file-and-line citation support for verify-doc-citations.sh (#6383).
# This file is sourced after repo_root, baseline_path, tmp_dir, log, and the
# shared diff-base resolver exist.

# SVG and XML are intentionally absent: they are text and remain eligible.
citation_binary_suffix_ere='7z|aac|apng|avi|avif|bin|bmp|bz2|class|db|db3|dll|dmg|doc|docx|dylib|eot|exe|flac|gif|gz|heic|heif|ico|iso|jar|jpeg|jpg|jxl|m4a|m4v|mkv|mov|mp3|mp4|mpeg|mpg|o|obj|ogg|opus|otf|pdf|png|ppt|pptx|psd|rar|so|sqlite|sqlite3|tar|tgz|tif|tiff|ttf|txz|war|wasm|wav|webm|webp|woff|woff2|xls|xlsx|xz|zip|zst'
citation_raw_line_ere='go/internal/[[:alnum:]_./-]+[.]go:[0-9]+'
citation_permalink_ere='https?://[^[:space:]]+/blob/[^[:space:]]+/go/internal/[[:alnum:]_./-]+[.]go([?][^[:space:]#]*)?#L[0-9]+'
doc_citation_files_state='unprepared'
doc_citation_files_path="${tmp_dir}/line-citation-files.nul"
doc_citation_scan_state='unscanned'
doc_citation_scan_path="${tmp_dir}/line-citation-matches.txt"

is_known_binary_citation_path() {
  local path="$1" restore_nocase=0 result
  if ! shopt -q nocasematch; then
    shopt -s nocasematch
    restore_nocase=1
  fi
  if [[ "${path}" =~ \.(${citation_binary_suffix_ere})$ ]]; then
    result=0
  else
    result=1
  fi
  [[ "${restore_nocase}" -eq 1 ]] && shopt -u nocasematch
  return "${result}"
}

enumerate_doc_citation_files() {
  local output="$1" scanner="${ESHU_DOC_CITATIONS_RG:-rg}" status
  set --
  [[ -d "${repo_root}/go" ]] && set -- "$@" go
  [[ -d "${repo_root}/docs" ]] && set -- "$@" docs
  : >"${output}"
  [[ "$#" -gt 0 ]] || return 0

  if git -C "${repo_root}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    if ! git -C "${repo_root}" ls-files -z --cached --others \
      --exclude-standard -- "$@" >"${output}"; then
      log "line citation file enumeration failed: git ls-files exited nonzero"
      return 1
    fi
    return 0
  fi

  set +e
  (cd "${repo_root}" && "${scanner}" --hidden --no-ignore \
    --glob '!.git/**' --glob '!.git' --files -0 -- "$@") >"${output}" 2>/dev/null
  status=$?
  set -e
  case "${status}" in
    0 | 1) return 0 ;;
    *)
      log "line citation file enumeration failed: ${scanner} exited ${status}"
      return 1
      ;;
  esac
}

scan_doc_citation_batch() {
  local output="$1" scanner="${ESHU_DOC_CITATIONS_RG:-rg}" status
  shift
  set +e
  (cd "${repo_root}" && "${scanner}" --no-ignore --with-filename \
    --no-heading --line-number -e "${citation_raw_line_ere}" \
    -e "${citation_permalink_ere}" \
    -- "$@") >>"${output}" 2>/dev/null
  status=$?
  set -e
  case "${status}" in
    0 | 1) return 0 ;;
    *)
      log "line citation/permalink scan failed: ${scanner} exited ${status}"
      return 1
      ;;
  esac
}

prepare_doc_citation_files() {
  case "${doc_citation_files_state}" in
    prepared) return 0 ;;
    failed) return 1 ;;
  esac

  local enumerated="${tmp_dir}/line-citation-files-enumerated.nul" rel
  local -a batch=()
  doc_citation_files_state='failed'
  enumerate_doc_citation_files "${enumerated}" || return 1
  : >"${doc_citation_files_path}"
  while IFS= read -r -d '' rel; do
    if [[ "${rel}" == *$'\n'* ]]; then
      log "line citation file path contains a newline; refusing an ambiguous scan"
      return 1
    fi
    if is_known_binary_citation_path "${rel}"; then
      continue
    fi
    if [[ "${rel}" =~ :[0-9]+: ]]; then
      log "line citation file path contains an ambiguous :number: segment; refusing a source-identity collision"
      return 1
    fi
    printf '%s\0' "${rel}" >>"${doc_citation_files_path}"
    batch[${#batch[@]}]="${rel}"
    if [[ "${#batch[@]}" -eq 128 ]]; then
      reject_nul_citation_files "${batch[@]}" || return 1
      batch=()
    fi
  done <"${enumerated}"
  if [[ "${#batch[@]}" -gt 0 ]]; then
    reject_nul_citation_files "${batch[@]}" || return 1
  fi
  doc_citation_files_state='prepared'
}

reject_nul_citation_files() {
  local trusted_rg matches="${tmp_dir}/line-nul-files.nul" status rel failed=0
  trusted_rg="$(command -v rg)"
  if [[ -z "${trusted_rg}" ]]; then
    log "NUL-byte inspection failed: trusted rg is unavailable"
    return 1
  fi
  set +e
  (cd "${repo_root}" && "${trusted_rg}" --text --files-with-matches --null \
    '\x00' -- "$@") >"${matches}" 2>/dev/null
  status=$?
  set -e
  case "${status}" in
    0)
      while IFS= read -r -d '' rel; do
        log "NUL byte in eligible citation file: ${rel}; refusing a binary-suppressed scan"
        failed=1
      done <"${matches}"
      [[ "${failed}" -eq 0 ]] && log "NUL-byte inspection returned matches without file names"
      return 1
      ;;
    1) return 0 ;;
    *)
      log "NUL-byte inspection failed: ${trusted_rg} exited ${status}"
      return 1
      ;;
  esac
}

prepare_doc_citation_scan() {
  case "${doc_citation_scan_state}" in
    scanned) return 0 ;;
    failed) return 1 ;;
  esac

  local rel
  local -a batch=()
  doc_citation_scan_state='failed'
  prepare_doc_citation_files || return 1
  : >"${doc_citation_scan_path}"
  while IFS= read -r -d '' rel; do
    batch[${#batch[@]}]="${rel}"
    if [[ "${#batch[@]}" -eq 128 ]]; then
      scan_doc_citation_batch "${doc_citation_scan_path}" "${batch[@]}" || return 1
      batch=()
    fi
  done <"${doc_citation_files_path}"
  if [[ "${#batch[@]}" -gt 0 ]]; then
    scan_doc_citation_batch "${doc_citation_scan_path}" "${batch[@]}" || return 1
  fi
  doc_citation_scan_state='scanned'
}

scan_line_citations() {
  local pairs="${tmp_dir}/line-citation-pairs-raw.txt"
  local contexts="${tmp_dir}/line-current-authority-raw.txt"
  local authority="${tmp_dir}/line-current-authority.txt"

  prepare_doc_citation_scan || return 1

  : >"${pairs}"
  : >"${contexts}"
  # Keep the burn-down ledger keyed only by source and target. Separately bind
  # immutable branch authority to the exact containing-line bytes. Physical
  # line numbers are intentionally absent, so byte-identical lines may move.
  if ! awk -v pairs="${pairs}" -v contexts="${contexts}" \
    -v raw_pattern="${citation_raw_line_ere}" '
    {
      if (!match($0, /:[0-9]+:/)) next
      source = substr($0, 1, RSTART - 1)
      context = substr($0, RSTART + RLENGTH)
      rest = context
      while (match(rest, raw_pattern)) {
        target = substr(rest, RSTART, RLENGTH)
        print source " " target >> pairs
        print source "\t" target "\t" context >> contexts
        rest = substr(rest, RSTART + RLENGTH)
      }
    }
  ' "${doc_citation_scan_path}"; then
    log "line citation match parsing failed"
    return 1
  fi
  if ! LC_ALL=C sort "${contexts}" >"${authority}"; then
    log "line citation authority sorting failed"
    return 1
  fi
  LC_ALL=C sort "${pairs}"
}

scan_base_line_citations() {
  local base="$1" raw="${tmp_dir}/line-citations-base-raw.txt" status
  set +e
  git -C "${repo_root}" grep -n -E \
    "${citation_raw_line_ere}" "${base}" -- go docs \
    >"${raw}" 2>/dev/null
  status=$?
  set -e
  case "${status}" in
    0 | 1) ;;
    *)
      log "immutable base line citation scan failed at ${base}: git grep exited ${status}"
      return 1
      ;;
  esac

  # Bind authority to source, cited target, and the exact containing-line
  # bytes. Do not retain the mutable physical source line number.
  awk -v base="${base}" -v binary_suffixes="${citation_binary_suffix_ere}" '
    {
      row = $0
      sub("^" base ":", "", row)
      if (!match(row, /:[0-9]+:/)) next
      source = substr(row, 1, RSTART - 1)
      if (tolower(source) ~ "\\.(" binary_suffixes ")$") next
      context = substr(row, RSTART + RLENGTH)
      rest = context
      while (match(rest, /go\/internal\/[[:alnum:]_.\/-]+\.go:[0-9]+/)) {
        target = substr(rest, RSTART, RLENGTH)
        print source "\t" target "\t" context
        rest = substr(rest, RSTART + RLENGTH)
      }
    }
  ' "${raw}" | LC_ALL=C sort
}

load_immutable_line_authority() {
  local output="$1"
  line_authority_active=0
  line_authority_base=""
  : >"${output}"

  # Hermetic fixtures without commit history exercise the local burn-down
  # contract. A real checkout with HEAD must resolve a branch base; silently
  # weakening to the branch-edited ledger would self-authorize new debt.
  if ! git -C "${repo_root}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    return 0
  fi
  if ! git -C "${repo_root}" rev-parse --verify HEAD >/dev/null 2>&1; then
    return 0
  fi

  eshu_gate_resolve_diff_base "verify-doc-citations" "${repo_root}" \
    "${ESHU_DOC_CITATIONS_BASE:-}"
  line_authority_base="${eshu_gate_diff_base}"
  if [[ -z "${line_authority_base}" ]]; then
    log "cannot resolve immutable diff base for LINE debt authority"
    return 1
  fi
  if ! git -C "${repo_root}" cat-file -e "${line_authority_base}^{commit}" 2>/dev/null; then
    log "immutable diff base is not a commit: ${line_authority_base}"
    return 1
  fi
  scan_base_line_citations "${line_authority_base}" >"${output}" || return 1
  line_authority_active=1
}

reject_branch_line_debt() {
  local current="$1" authority="$2"
  local growth="${tmp_dir}/line-branch-growth.txt"
  local unique="${tmp_dir}/line-branch-growth-unique.txt"
  local source citation context current_count base_count
  LC_ALL=C comm -23 "${current}" "${authority}" >"${growth}" || true
  [[ -s "${growth}" ]] || return 0

  LC_ALL=C sort -u "${growth}" >"${unique}"
  while IFS=$'\t' read -r source citation context; do
    [[ -z "${source}" ]] && continue
    current_count="$(line_authority_pair_count "${source}" "${citation}" "${current}")"
    base_count="$(line_authority_pair_count "${source}" "${citation}" "${authority}")"
    if [[ "${base_count}" -gt 0 && "${current_count}" -eq "${base_count}" ]]; then
      log "branch-replaced LINE context: ${source} ${citation} has containing-line bytes absent from ${line_authority_base}; editing the ledger cannot authorize it"
    else
      log "branch-added LINE debt: ${source} ${citation} increased from ${base_count} to ${current_count} occurrence(s) relative to ${line_authority_base}; editing the ledger cannot authorize it"
    fi
  done <"${unique}"
  return 1
}

line_authority_pair_count() {
  local source="$1" citation="$2" path="$3"
  awk -F '\t' -v source="${source}" -v citation="${citation}" \
    '$1 == source && $2 == citation { count++ } END { print count + 0 }' "${path}"
}

enforce_immutable_line_authority() {
  local current="$1" mode="$2" authority="${tmp_dir}/line-authority.txt"
  load_immutable_line_authority "${authority}" || return 1
  if [[ "${line_authority_active}" -eq 1 ]] &&
    ! reject_branch_line_debt "${current}" "${authority}"; then
    if [[ "${mode}" == "update" ]]; then
      log "refusing -update: branch-added LINE debt cannot be authorized; baseline left unchanged"
    fi
    return 1
  fi
  return 0
}

scan_invalid_line_permalinks() {
  local candidates="${tmp_dir}/line-permalink-candidates.txt"
  local source link ref_path ref

  prepare_doc_citation_scan || return 1

  if ! awk -v permalink_pattern="${citation_permalink_ere}" '
    {
      if (!match($0, /:[0-9]+:/)) next
      source = substr($0, 1, RSTART - 1)
      rest = substr($0, RSTART + RLENGTH)
      while (match(rest, permalink_pattern)) {
        print source "\t" substr(rest, RSTART, RLENGTH)
        rest = substr(rest, RSTART + RLENGTH)
      }
    }
  ' "${doc_citation_scan_path}" >"${candidates}"; then
    log "line permalink match parsing failed"
    return 1
  fi
  while IFS=$'\t' read -r source link; do
    [[ -z "${source}" ]] && continue
    ref_path="${link#*/blob/}"
    ref="${ref_path%%/*}"
    if [[ ! "${ref}" =~ ^[[:xdigit:]]{40}$ ]]; then
      printf '%s\t%s\n' "${source}" "${link}"
    fi
  done <"${candidates}"
}

check_line_permalink_contract() {
  local invalid="${tmp_dir}/line-permalinks-invalid.txt" source link failed=0
  scan_invalid_line_permalinks >"${invalid}" || return 1
  while IFS=$'\t' read -r source link; do
    [[ -z "${source}" ]] && continue
    log "${source} has mutable line permalink ${link}; permalink must use a full 40-hex commit SHA"
    failed=1
  done <"${invalid}"
  return "${failed}"
}

baseline_line_pairs() {
  local path="$1"
  [[ -f "${path}" ]] || return 0
  rg '^LINE ' "${path}" 2>/dev/null \
    | awk '{ $1 = ""; sub(/^ /, ""); print }' \
    | LC_ALL=C sort || true
}

line_occurrence_count() {
  local pair="$1" path="$2"
  awk -v pair="${pair}" '$0 == pair { count++ } END { print count + 0 }' "${path}"
}

report_line_debt_growth() {
  local growth="$1" current="$2" baseline="$3"
  local source citation pair current_count baseline_count
  LC_ALL=C sort -u "${growth}" >"${tmp_dir}/line-growth-unique.txt"
  while IFS=' ' read -r source citation; do
    [[ -z "${source}" ]] && continue
    pair="${source} ${citation}"
    current_count="$(line_occurrence_count "${pair}" "${current}")"
    baseline_count="$(line_occurrence_count "${pair}" "${baseline}")"
    if [[ "${baseline_count}" -eq 0 ]]; then
      log "${source} has unstable line citation ${citation}; use a symbol/file anchor, or a full-SHA #L permalink for historical evidence"
    else
      log "line citation multiplicity changed: ${source} ${citation} increased from ${baseline_count} to ${current_count} occurrence(s)"
    fi
  done <"${tmp_dir}/line-growth-unique.txt"
}

reject_line_debt_growth() {
  local current="$1" baseline="$2"
  LC_ALL=C comm -23 "${current}" "${baseline}" >"${tmp_dir}/line-growth.txt" || true
  if [[ ! -s "${tmp_dir}/line-growth.txt" ]]; then
    return 0
  fi
  report_line_debt_growth "${tmp_dir}/line-growth.txt" "${current}" "${baseline}"
  return 1
}

write_line_baseline_records() {
  local pairs="$1"
  while IFS=' ' read -r source citation; do
    [[ -z "${source}" ]] && continue
    printf 'LINE %s %s\n' "${source}" "${citation}"
  done <"${pairs}"
}

check_line_citations() {
  local current="$1" baseline="$2"
  local failed=0 count

  count="$(awk 'NF' "${current}" | wc -l | tr -d ' ')"
  if [[ -f "${repo_root}/specs/ci-gates.v1.yaml" && "${count}" -eq 0 ]]; then
    log "found zero raw Go line citations in a real-tree scan; refusing a silent empty result"
    failed=1
  fi

  if ! reject_line_debt_growth "${current}" "${baseline}"; then
    failed=1
  fi

  LC_ALL=C comm -13 "${current}" "${baseline}" >"${tmp_dir}/line-stale.txt" || true
  LC_ALL=C sort -u "${tmp_dir}/line-stale.txt" >"${tmp_dir}/line-stale-unique.txt"
  while IFS=' ' read -r source citation; do
    [[ -z "${source}" ]] && continue
    local pair current_count baseline_count
    pair="${source} ${citation}"
    current_count="$(line_occurrence_count "${pair}" "${current}")"
    baseline_count="$(line_occurrence_count "${pair}" "${baseline}")"
    log "stale LINE baseline: ${source} ${citation} decreased from ${baseline_count} to ${current_count} occurrence(s); regenerate after removing or replacing the citation"
    failed=1
  done <"${tmp_dir}/line-stale-unique.txt"

  return "${failed}"
}
