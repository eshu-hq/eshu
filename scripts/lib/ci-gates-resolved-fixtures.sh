#!/usr/bin/env bash
# Build temporary flat views for legacy text assertions after Go Load validates
# the ordered registry fragments. Never commit these views as a second registry.
ci_gates_fragment_paths() {
  local root="$1" repo="$2"
  (cd "${repo}/go" && go run ./cmd/ci-gates fragments --registry "${root}")
}

ci_gates_flat_view() {
  local root="$1" output="$2" repo="$3" code_repo refs ref spec_dir body flat
  code_repo="${ESHU_CI_GATES_GO_REPO:-${repo}}"
  refs="$(ci_gates_fragment_paths "${root}" "${code_repo}")" || return 1
  if [[ -z "${refs}" ]]; then
    cp "${root}" "${output}"
    return
  fi
  spec_dir="$(dirname "${root}")"
  body="$(mktemp)" || return 1
  flat="$(mktemp)" || { rm -f "${body}"; return 1; }
  while IFS= read -r ref; do
    if [[ ! -f "${spec_dir}/${ref}" ]]; then
      rm -f "${body}" "${flat}"
      return 1
    fi
    sed -n '3,$p' "${spec_dir}/${ref}" >>"${body}"
  done <<<"${refs}"
  if ! awk -v body="${body}" -v sq="'" '
    function fragments_key(line) {
      return line ~ /^gate_fragments:/ || line ~ /^"gate_fragments":/ ||
        line ~ ("^" sq "gate_fragments" sq ":")
    }
    fragments_key($0) {
      found = 1
      skipping = 1
      print "gates:"
      while ((getline line < body) > 0) print line
      close(body)
      next
    }
    skipping && /^[^ #][^:]*:/ { skipping = 0 }
    !skipping { print }
    END { if (!found) exit 1 }
  ' "${root}" >"${flat}"; then
    rm -f "${body}" "${flat}"
    return 1
  fi
  if ! (cd "${code_repo}/go" && go run ./cmd/ci-gates compare --left "${root}" --right "${flat}" >/dev/null); then
    rm -f "${body}" "${flat}"
    return 1
  fi
  cp "${flat}" "${output}"
  local result=$?
  rm -f "${body}" "${flat}"
  return "${result}"
}

ci_gates_static_workflow_view() {
  local source="$1" output="$2" repo="$3" filters
  filters="${repo}/.github/static-contract-filters.yml"
  [[ -f "${filters}" ]] || return 1
  rg -q '^          filters: .github/static-contract-filters.yml$' "${source}" || return 1
  awk -v filters="${filters}" '
    $0 == "          filters: .github/static-contract-filters.yml" {
      print "          filters: |"
      while ((getline line < filters) > 0) print "            " line
      close(filters)
      next
    }
    { print }
  ' "${source}" >"${output}"
}
