#!/usr/bin/env bash
# Build temporary flat views for legacy text assertions after Go Load validates
# the ordered registry fragments. Never commit these views as a second registry.
ci_gates_flat_view() {
  local root="$1" output="$2" repo="$3" ref
  if ! rg -q '^gate_fragments:' "${root}"; then
    cp "${root}" "${output}"
    return
  fi
  (cd "${repo}/go" && go run ./cmd/ci-gates layers --registry "${root}" >/dev/null) || return 1
  awk '/^gate_fragments:/{exit}{print}' "${root}" >"${output}"
  printf 'gates:\n' >>"${output}"
  while IFS= read -r ref; do
    [[ -f "${repo}/specs/${ref}" ]] || return 1
    sed -n '3,$p' "${repo}/specs/${ref}" >>"${output}"
  done < <(awk '/^gate_fragments:/{active=1;next} active && /^  - ci-gates.d\//{print $2;next} active && /^[^ #]/{exit}' "${root}")
  awk '/^hygiene_hooks:/{active=1} active{print}' "${root}" >>"${output}"
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
