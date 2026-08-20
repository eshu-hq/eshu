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

  # The inputs are READ OUT OF the registry gate above rather than restated
  # here. A hand-maintained copy only ever proves the pairs someone remembered
  # to add: `go/internal/cli/**` was added to specs/ci-gates.v1.yaml when the
  # #6059 extractions moved the env-reading CLI code out of go/cmd/eshu, never
  # reached the workflow filter, and this test stayed green because its list
  # did not know about it either. Deriving the list means a trigger added to
  # the spec cannot pass here until the workflow filter carries it too.
  local -a triggers=()
  while IFS= read -r trigger; do
    triggers+=("${trigger}")
  done < <(
    printf '%s\n' "${gate}" |
      sed -n '/^[[:space:]]*triggers:$/,/^[[:space:]]*local:$/p' |
      sed -n 's/^      - "\(.*\)"$/\1/p'
  )
  # Derivation cuts both ways: a trigger commented out or reindented in the
  # spec drops out of this list and takes its workflow assertion with it, so a
  # silent under-parse would prove less while still passing.
  #
  # The count floor alone does not close that. It cannot see a trigger that was
  # ADDED in a style the extraction does not match: an eleventh entry written
  # `- 'x'` or bare `- x` leaves ten parsed entries, so the floor is satisfied
  # and the new trigger is never checked against the workflow filter -- exactly
  # the drift this function exists to catch. Every list item in the registry is
  # six-space double-quoted today and require_path_line assumes that style, but
  # nothing enforces it: the Go loader is YAML and does not care, and no lint
  # rejects the other forms, so the author of a deviating line would get no
  # signal at all.
  #
  # So compare the parsed count against the RAW list-item count in the same
  # block. Any item the extraction dropped -- wrong quoting, wrong indentation,
  # a trailing comment -- makes the two disagree and fails loudly, naming the
  # lines that did not survive.
  local raw_trigger_count parsed_trigger_count
  raw_trigger_count="$(
    printf '%s\n' "${gate}" |
      sed -n '/^[[:space:]]*triggers:$/,/^[[:space:]]*local:$/p' |
      rg --count '^[[:space:]]*- ' || true
  )"
  raw_trigger_count="${raw_trigger_count:-0}"
  parsed_trigger_count="${#triggers[@]}"
  if [[ "${raw_trigger_count}" != "${parsed_trigger_count}" ]]; then
    printf '%s\n' "${gate}" |
      sed -n '/^[[:space:]]*triggers:$/,/^[[:space:]]*local:$/p' |
      rg '^[[:space:]]*- ' |
      rg --invert-match '^      - ".*"$' >&2 || true
    fail "docs-cli-env-refs: ${raw_trigger_count} trigger list items in specs/ci-gates.v1.yaml but only ${parsed_trigger_count} parsed; the lines above are not six-space double-quoted, so they were silently skipped and never checked against the docsclienvrefs workflow filter"
  fi

  # A sanity floor under the derived list, so a block that parsed cleanly but
  # came back near-empty (both counts zero) still fails rather than looping over
  # nothing. Lowering it is fine when a trigger is deliberately removed from the
  # spec -- that is a decision, not a parser bug.
  ((parsed_trigger_count >= 10)) ||
    fail "docs-cli-env-refs registry triggers parsed as ${parsed_trigger_count} entries (expected at least 10); either the trigger-block parser or the registry indentation changed, or a trigger was deliberately removed from the spec -- in which case lower this floor in the same commit"

  for input in "${triggers[@]}"; do
    # Tautological by construction -- every input was just parsed out of
    # ${gate}. Kept as the executable statement of the expected line form, so
    # that if the extraction above is ever rewritten to accept another style,
    # this assertion is the thing that has to be updated deliberately.
    require_path_line "${gate}" "${input}" "docs-cli-env-refs registry triggers omit ${input}"
    printf '%s\n' "${workflow_filter}" |
      rg --fixed-strings --line-regexp --quiet "              - '${input}'" ||
      fail "docsclienvrefs workflow filter omits ${input}, which specs/ci-gates.v1.yaml lists as a docs-cli-env-refs trigger: a PR touching only that path would select the gate locally and skip it in CI"
  done

  require "docs-cli-env-refs workflow matrix entry" 'append_gate "${{ steps.filter.outputs.docsclienvrefs }}" "docsclienvrefs" "Verify docs CLI/env refs gate" "bash scripts/test-verify-docs-cli-env-refs.sh" "bash scripts/verify-docs-cli-env-refs.sh"' "${static_contract_workflow}"

  for input in 'docs/public/reference/cli-reference.md' 'go/cmd/eshu/docs.go' 'go/internal/cli/firstrun/classify.go' 'go/internal/envregistry/entries.go' 'scripts/test-verify-docs-cli-env-refs.sh'; do
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
