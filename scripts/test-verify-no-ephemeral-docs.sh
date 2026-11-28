#!/usr/bin/env bash
# Regression and seeded-violation tests for scripts/verify-no-ephemeral-docs.sh.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-no-ephemeral-docs.sh"

tmp_root="$(mktemp -d)"
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

init_repo() {
  local name="$1"
  local dir="${tmp_root}/${name}"
  mkdir -p "${dir}/docs/public/decisions" "${dir}/go/internal/demo" "${dir}/docs/internal/evidence"
  git -C "${dir}" init -q
  git -C "${dir}" config user.email "test@example.invalid"
  git -C "${dir}" config user.name "Eshu Test"
  printf '# Demo package\n\nOwns demo logic.\n' >"${dir}/go/internal/demo/README.md"
  printf '# Legacy note\n' >"${dir}/docs/internal/evidence/legacy.md"
  git -C "${dir}" add .
  git -C "${dir}" commit -q -m "initial"
  printf '%s\n' "${dir}"
}

run_case() {
  local label="$1" expected="$2" dir="$3"
  shift 3
  local rc=0
  ESHU_NO_EPHEMERAL_DOCS_REPO_ROOT="${dir}" \
    ESHU_NO_EPHEMERAL_DOCS_BASE=HEAD~1 \
    bash "${verifier}" "$@" >"${tmp_root}/out" 2>"${tmp_root}/err" || rc=$?
  if [ "$rc" -ne "$expected" ]; then
    printf 'FAIL [%s]: expected exit %s, got %s\n' "$label" "$expected" "$rc" >&2
    cat "${tmp_root}/out" "${tmp_root}/err" >&2
    exit 1
  fi
  printf 'PASS [%s]\n' "$label"
}

# 1. Clean architecture README edit and durable ADR pass (GREEN).
clean_dir="$(init_repo clean)"
printf '\n## Invariants\n\nAll writes are idempotent.\n' >>"${clean_dir}/go/internal/demo/README.md"
printf '# ADR 001\n\nDurable decision.\n' >"${clean_dir}/docs/public/decisions/001-demo.md"
git -C "${clean_dir}" add . && git -C "${clean_dir}" commit -q -m "clean docs"
run_case "clean README and ADR pass" 0 "${clean_dir}"
run_case "clean README and ADR pass in --files mode" 0 "${clean_dir}" --files go/internal/demo/README.md docs/public/decisions/001-demo.md

# 2. New file under docs/internal/evidence/ fails (RED).
ev_dir="$(init_repo new-evidence)"
printf '# PR shim\n\n## Summary\n\nMeasured locally.\n' >"${ev_dir}/docs/internal/evidence/1234-shim.md"
git -C "${ev_dir}" add . && git -C "${ev_dir}" commit -q -m "add evidence"
run_case "new docs/internal/evidence file rejected" 1 "${ev_dir}"
run_case "new docs/internal/evidence file rejected in --files mode" 1 "${ev_dir}" --files docs/internal/evidence/1234-shim.md

# 3. New file under docs/internal/arbiter/ fails (RED).
arb_dir="$(init_repo new-arbiter)"
mkdir -p "${arb_dir}/docs/internal/arbiter"
printf '# Ruling\n' >"${arb_dir}/docs/internal/arbiter/1234-ruling.md"
git -C "${arb_dir}" add . && git -C "${arb_dir}" commit -q -m "add arbiter file"
run_case "new docs/internal/arbiter file rejected" 1 "${arb_dir}"
run_case "new docs/internal/arbiter file rejected in --files mode" 1 "${arb_dir}" --files docs/internal/arbiter/1234-ruling.md

# 4. New file under docs/superpowers/ and docs/plans/ fails (RED).
plan_dir="$(init_repo new-plan)"
mkdir -p "${plan_dir}/docs/superpowers" "${plan_dir}/docs/plans"
printf '# Scratch\n' >"${plan_dir}/docs/superpowers/scratch.md"
printf '# Plan\n' >"${plan_dir}/docs/plans/scratch.md"
git -C "${plan_dir}" add . && git -C "${plan_dir}" commit -q -m "add plans"
run_case "new docs/superpowers and docs/plans rejected" 1 "${plan_dir}"

# 5. New go/**/evidence-*.md fails (RED).
go_ev_dir="$(init_repo go-evidence)"
printf '# Local proof\n' >"${go_ev_dir}/go/internal/demo/evidence-1234-proof.md"
git -C "${go_ev_dir}" add . && git -C "${go_ev_dir}" commit -q -m "add go evidence"
run_case "new go/**/evidence-*.md rejected" 1 "${go_ev_dir}"
run_case "new go/**/evidence-*.md rejected in --files mode" 1 "${go_ev_dir}" --files go/internal/demo/evidence-1234-proof.md

# 6. Appending Performance/No-Regression/Observability markers to go/**/README.md fails (RED).
marker_dir="$(init_repo go-markers)"
printf '\nNo-Regression Evidence (#1234): unchanged query shape.\n' >>"${marker_dir}/go/internal/demo/README.md"
git -C "${marker_dir}" add . && git -C "${marker_dir}" commit -q -m "append marker"
run_case "PR marker added to go/**/README.md rejected" 1 "${marker_dir}"
run_case "PR marker added to go/**/README.md rejected in --files mode" 1 "${marker_dir}" --files go/internal/demo/README.md

# 6b. Bolded Markdown PR marker in go/**/README.md also fails (RED).
bold_dir="$(init_repo go-bold-markers)"
printf '\n- **Performance Evidence:** 12ms -> 6ms.\n' >>"${bold_dir}/go/internal/demo/README.md"
git -C "${bold_dir}" add . && git -C "${bold_dir}" commit -q -m "append bold marker"
run_case "bolded PR marker in go/**/README.md rejected" 1 "${bold_dir}"
run_case "bolded PR marker in go/**/README.md rejected in --files mode" 1 "${bold_dir}" --files go/internal/demo/README.md

# 6c. Renaming (git mv) into a banned path or moving a marker file into go/**/README.md fails (RED).
mv_dir="$(init_repo rename-bypass)"
printf '# Scratch note\n' >"${mv_dir}/docs/public/decisions/tmp-note.md"
printf '# Demo\n\nNo-Regression Evidence: old note.\n' >"${mv_dir}/docs/public/decisions/tmp-marker.md"
git -C "${mv_dir}" add . && git -C "${mv_dir}" commit -q -m "seed rename sources"
git -C "${mv_dir}" mv docs/public/decisions/tmp-note.md docs/internal/evidence/9999-moved.md
git -C "${mv_dir}" mv -f docs/public/decisions/tmp-marker.md go/internal/demo/README.md
git -C "${mv_dir}" commit -q -m "rename into banned path and go README"
run_case "git mv into banned path and go README rejected" 1 "${mv_dir}"

# 7. Deleting legacy evidence files passes cleanly (GREEN).
del_dir="$(init_repo delete-legacy)"
git -C "${del_dir}" rm -q docs/internal/evidence/legacy.md
git -C "${del_dir}" commit -q -m "remove legacy evidence"
run_case "deleting legacy evidence file passes" 0 "${del_dir}"

# 8. GitHub pull_request merge ref (2-parent merge commit) catches PR violation (RED).
merge_dir="$(init_repo pr-merge-ref)"
git -C "${merge_dir}" checkout -q -b pr-branch
printf '# PR note\n' >"${merge_dir}/docs/internal/evidence/9999-pr.md"
git -C "${merge_dir}" add . && git -C "${merge_dir}" commit -q -m "pr commit"
git -C "${merge_dir}" checkout -q -
printf 'main moved\n' >"${merge_dir}/main.txt"
git -C "${merge_dir}" add . && git -C "${merge_dir}" commit -q -m "main advances"
git -C "${merge_dir}" merge -q --no-ff pr-branch -m "synthetic merge ref"
rc=0
GITHUB_EVENT_NAME=pull_request \
  ESHU_NO_EPHEMERAL_DOCS_REPO_ROOT="${merge_dir}" \
  bash "${verifier}" >"${tmp_root}/out" 2>"${tmp_root}/err" || rc=$?
if [ "$rc" -ne 1 ]; then
  printf 'FAIL [pull_request merge ref catches new evidence]: expected 1, got %s\n' "$rc" >&2
  cat "${tmp_root}/out" "${tmp_root}/err" >&2
  exit 1
fi
  printf 'PASS [pull_request merge ref catches new evidence]\n'

# 9. Stray proof/evidence/prep/numbered-issue Markdown files in go/ fail (RED), while testdata/ passes (GREEN).
go_stray_dir="$(init_repo go-stray)"
printf '# Cost\n' >"${go_stray_dir}/go/internal/demo/COST_EVIDENCE.md"
git -C "${go_stray_dir}" add . && git -C "${go_stray_dir}" commit -q -m "add uppercase evidence"
run_case "go COST_EVIDENCE.md rejected" 1 "${go_stray_dir}"
run_case "go COST_EVIDENCE.md rejected in --files mode" 1 "${go_stray_dir}" --files go/internal/demo/COST_EVIDENCE.md

go_proof_dir="$(init_repo go-proof)"
printf '# Proof\n' >"${go_proof_dir}/go/internal/demo/consolidation-proof.md"
printf '# Prep\n' >"${go_proof_dir}/go/internal/demo/EXTRACTION_PREP.md"
printf '# Issue dump\n' >"${go_proof_dir}/go/internal/demo/10658-failure.md"
git -C "${go_proof_dir}" add . && git -C "${go_proof_dir}" commit -q -m "add proof prep issue dump"
run_case "go proof/prep/numbered dump rejected" 1 "${go_proof_dir}"

go_td_dir="$(init_repo go-testdata)"
mkdir -p "${go_td_dir}/go/internal/demo/testdata"
printf '# Fixture\n\n## Evidence\n' >"${go_td_dir}/go/internal/demo/testdata/8874-fixture.md"
git -C "${go_td_dir}" add . && git -C "${go_td_dir}" commit -q -m "add testdata fixture"
run_case "go testdata fixture passes" 0 "${go_td_dir}"
run_case "go testdata fixture passes in --files mode" 0 "${go_td_dir}" --files go/internal/demo/testdata/8874-fixture.md

# 10. Raw .sql packets, fork rulings, and implementation plans in docs/internal/ fail (RED).
int_slop_dir="$(init_repo internal-slop)"
mkdir -p "${int_slop_dir}/docs/internal/design"
printf 'SELECT 1;\n' >"${int_slop_dir}/docs/internal/design/11075-a2-packet.sql"
printf '# Fork rulings\n' >"${int_slop_dir}/docs/internal/design/10882-fork-rulings.md"
printf '# Plan\n' >"${int_slop_dir}/docs/internal/security-implementation-plan.md"
git -C "${int_slop_dir}" add . && git -C "${int_slop_dir}" commit -q -m "add internal slop"
run_case "internal sql/fork-ruling/plan rejected" 1 "${int_slop_dir}"
run_case "internal sql rejected in --files mode" 1 "${int_slop_dir}" --files docs/internal/design/11075-a2-packet.sql

# 11. Appending PR-dump H2 headings to go/**/*.md or docs/public/**/*.md fails (RED); generated docs pass (GREEN).
pub_dump_dir="$(init_repo pub-dump)"
mkdir -p "${pub_dump_dir}/docs/public/reference"
printf '# Cypher Perf\n\n## Evidence Notes\n\nIssue #3567 details.\n' >"${pub_dump_dir}/docs/public/reference/cypher.md"
git -C "${pub_dump_dir}" add . && git -C "${pub_dump_dir}" commit -q -m "add public evidence notes"
run_case "docs/public Evidence Notes H2 rejected" 1 "${pub_dump_dir}"
run_case "docs/public Evidence Notes H2 rejected in --files mode" 1 "${pub_dump_dir}" --files docs/public/reference/cypher.md

go_h2_dir="$(init_repo go-issue-h2)"
printf '\n## Deferred backfill tuning (#5096)\n\nRan locally.\n' >>"${go_h2_dir}/go/internal/demo/README.md"
git -C "${go_h2_dir}" add . && git -C "${go_h2_dir}" commit -q -m "add issue H2"
run_case "go issue-numbered H2 rejected" 1 "${go_h2_dir}"
run_case "go issue-numbered H2 rejected in --files mode" 1 "${go_h2_dir}" --files go/internal/demo/README.md

pub_gen_dir="$(init_repo pub-generated)"
mkdir -p "${pub_gen_dir}/docs/public/reference/generated"
printf '# Replay Coverage\n\n## Case (#1234)\n' >"${pub_gen_dir}/docs/public/reference/generated/replay.md"
git -C "${pub_gen_dir}" add . && git -C "${pub_gen_dir}" commit -q -m "add generated reference"
run_case "docs/public/reference/generated passes" 0 "${pub_gen_dir}"

printf 'test-verify-no-ephemeral-docs: all cases passed\n'
