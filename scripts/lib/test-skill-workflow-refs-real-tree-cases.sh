#!/usr/bin/env bash
# test-skill-workflow-refs-real-tree-cases.sh — the real-tree case for
# the skill-workflow-refs test mirror. Sourced, not run on its own.
#
# The shared skills live in the eshu-agent-kit, and the workflow citations
# are in the kit's skill sources, not in the thin alias wrappers the
# installer materializes. So the census is the kit's skills/eshu tree,
# resolved from the installed manifest's symlink target the same way the
# verifier resolves it.
#
# A trigger path for the skill-workflow-refs gate, so editing it alone
# still selects the gate that runs it.

# Case 7: the real, committed skill tree against the real, committed
# .github/workflows tree passes today. This is the regression proof: it
# fails on the pre-fix tree (three telemetry-coverage-discipline
# references plus two generator-script-discipline references to workflow
# files consolidated away by #4218) and passes once #5855 corrects them.
#
# The shared skills live in the eshu-agent-kit, and the workflow citations
# are in the kit's skill sources, so the census is the kit's skills/eshu
# tree. It is resolved from the installed manifest's symlink target, the
# same way the verifier resolves it.
test_real_tree_passes() {
  local out="${tmp_root}/case7.out"
  local kit_root=""
  if [ -f "${repo_root}/.agents/roles.json" ]; then
    kit_root="$(cd "${repo_root}" && python3 -c '
import pathlib, sys
try:
    target = pathlib.Path(".agents/roles.json").resolve()
except OSError:
    sys.exit(1)
root = target.parent.parent
if (root / "skills" / "eshu").is_dir():
    print(root)
else:
    sys.exit(1)
' 2>/dev/null || true)"
  fi
  if [ -z "${kit_root}" ]; then
    record_fail "case7: the kit skill tree is not installed, so the real-tree proof cannot run"
    return
  fi
  # The kit's skills live in skills/eshu and skills/core; the core ones are
  # not aliased, so they are the skills the harness tree carries under their
  # own names. The verifier takes one census directory, so the case runs
  # against each layer in turn and both must pass.
  local layer
  for layer in eshu core; do
    if [ ! -d "${kit_root}/skills/${layer}" ]; then
      record_fail "case7: the kit has no skills/${layer} tree, so the real-tree proof cannot run"
      return
    fi
    if ESHU_SKILL_WORKFLOW_REFS_SKILLS_DIR="${kit_root}/skills/${layer}" \
       ESHU_SKILL_WORKFLOW_REFS_WORKFLOWS_DIR="${repo_root}/.github/workflows" \
       "${BASH:-bash}" "${verifier}" >"${out}" 2>&1; then
      assert_contains "OK:" "${out}" "case7: real committed ${layer} skill docs pass against real committed workflows"
    else
      record_fail "case7: real committed ${layer} skill docs pass against real committed workflows"
      cat "${out}" >&2
    fi
  done
}
