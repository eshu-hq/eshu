#!/usr/bin/env bash
# CI selector and workflow parity cases for #6023. Sourced by the registry test.
# shellcheck disable=SC2154 # registry, workflow, and repo paths are owned by the sourcing test.

check_docs_cli_env_refs_trigger_parity() {
  local gate workflow_filter input selection
  gate="$(sed -n '/^  - id: docs-cli-env-refs$/,/^  - id:/p' "${registry}")"
  [[ -n "${gate}" ]] || fail "missing docs-cli-env-refs registry gate"
  printf '%s\n' "${gate}" |
    rg --multiline --quiet 'requirements:\n[[:space:]]+- go' ||
    fail "docs-cli-env-refs registry gate omits its Go requirement"
  workflow_filter="$(sed -n '/^[[:space:]]*docsclienvrefs:/,/^[[:space:]]*[a-z][a-z0-9]*:/p' "${static_contract_workflow}")"
  [[ -n "${workflow_filter}" ]] || fail "missing docsclienvrefs workflow filter"

  for input in 'docs/public/**' 'go/cmd/eshu/**' 'go/internal/envregistry/**' 'go/cmd/docs-cli-env-refs/**' 'scripts/verify-docs-cli-env-refs.sh' 'scripts/test-verify-docs-cli-env-refs.sh' 'scripts/docs-cli-env-refs-baseline.txt' 'specs/ci-gates.v1.yaml'; do
    require_path_line "${gate}" "${input}" "docs-cli-env-refs registry triggers omit ${input}"
    printf '%s\n' "${workflow_filter}" |
      rg --fixed-strings --line-regexp --quiet "              - '${input}'" ||
      fail "docsclienvrefs workflow filter omits ${input}"
  done

  require "docs-cli-env-refs workflow matrix entry" 'append_gate "${{ steps.filter.outputs.docsclienvrefs }}" "docsclienvrefs" "Verify docs CLI/env refs gate" "bash scripts/test-verify-docs-cli-env-refs.sh" "bash scripts/verify-docs-cli-env-refs.sh"' "${static_contract_workflow}"

  for input in 'docs/public/reference/cli-reference.md' 'go/cmd/eshu/docs.go' 'go/internal/envregistry/entries.go' 'scripts/test-verify-docs-cli-env-refs.sh'; do
    selection="$(printf '%s\n' "${input}" | (cd "${repo_root}/go" && go run ./cmd/ci-gates select --registry "${registry}" --tier pre-pr --paths-from - --explain))"
    printf '%s\n' "${selection}" |
      rg --quiet '^SELECTED[[:space:]]+docs-cli-env-refs[[:space:]]' ||
      fail "${input} does not select docs-cli-env-refs"
  done

  for input in 'go/cmd/docs-cli-env-refs/main.go' 'go/internal/capabilitycatalog/data/surface-inventory.generated.json'; do
    selection="$(printf '%s\n' "${input}" | (cd "${repo_root}/go" && go run ./cmd/ci-gates select --registry "${registry}" --tier pre-pr --paths-from - --explain))"
    printf '%s\n' "${selection}" |
      rg --quiet '^SELECTED[[:space:]]+capability-surface-inventory[[:space:]]' ||
      fail "${input} does not select capability-surface-inventory"
  done
}
