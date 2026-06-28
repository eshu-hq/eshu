#!/usr/bin/env bash
# test-verify-docs-build-changed.sh — hermetic self-test for the
# docs-build-changed verifier, using a fake `uv` shim so the suite
# needs no real MkDocs/Material/pymdown dependency.
#
# Run with:
#   bash scripts/test-verify-docs-build-changed.sh
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-docs-build-changed.sh"

if [[ ! -x "${verifier}" ]]; then
  echo "test-docs-build: missing executable verifier at ${verifier}" >&2
  exit 1
fi

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

git_in() {
  local repo_dir="$1"
  shift
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR git -C "${repo_dir}" "$@"
}

git_init() {
  local repo_dir="$1"
  mkdir -p "${repo_dir}"
  git_in "${repo_dir}" init -q -b main
  git_in "${repo_dir}" config user.email "test@example.invalid"
  git_in "${repo_dir}" config user.name "Eshu Test"
  git_in "${repo_dir}" config core.hooksPath /dev/null

  # Seed a minimal repo shape: a docs/ dir with one file, a README, AGENTS.md, CLAUDE.md,
  # and one source file that should NOT trigger the docs gate.
  mkdir -p "${repo_dir}/docs/public"
  printf '# Test Docs\n\nPage content.\n' >"${repo_dir}/docs/public/index.md"
  printf '# Project README\n' >"${repo_dir}/README.md"
  printf '# AGENTS\n' >"${repo_dir}/AGENTS.md"
  printf '# CLAUDE\n' >"${repo_dir}/CLAUDE.md"
  mkdir -p "${repo_dir}/src"
  printf '// package foo\npackage foo\n' >"${repo_dir}/src/main.go"

  git_in "${repo_dir}" add -A
  git_in "${repo_dir}" commit -q -m "initial"
  git_in "${repo_dir}" update-ref refs/remotes/origin/main HEAD
  git_in "${repo_dir}" switch -q -c feature
}

# fake_uv: intercepts `uv run --with mkdocs ...` invocations.
# When ESHU_FAKE_UV_FAIL is set, simulates a build failure.
fake_uv() {
  local bin_dir="$1"
  mkdir -p "${bin_dir}"
  cat >"${bin_dir}/uv" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail
# Capture the full command for assertion
echo "[fake-uv] $*" >&2
# Verify the docs command contains the expected mkdocs build flags
# (the config-file path is absolute from the verifier's repo_root)
case "$*" in
  *"mkdocs build --strict --clean --config-file"*)
    if [[ -n "${ESHU_FAKE_UV_FAIL:-}" ]]; then
      echo "[fake-uv] simulating mkdocs build failure" >&2
      exit 1
    fi
    echo "[fake-uv] mkdocs build SUCCESS" >&2
    exit 0
    ;;
  *)
    echo "[fake-uv] unexpected command: $*" >&2
    exit 1
    ;;
esac
SCRIPT
  chmod +x "${bin_dir}/uv"
}

run_verifier() {
  local repo_dir="$1"
  local output_file="$2"
  shift 2
  local fake_bin="${tmp_root}/fake-bin"
  fake_uv "${fake_bin}"
  (
    cd "${repo_dir}"
    # Clear the real PATH of uv so we only use our fake; keep only basic system bins.
    env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
      PATH="${fake_bin}:${PATH}" \
      ESHU_VERIFY_DOCS_BRANCH="" \
      "${verifier}" "$@"
  ) >"${output_file}" 2>&1
}

run_verifier_fail_uv() {
  local repo_dir="$1"
  local output_file="$2"
  shift 2
  local fake_bin="${tmp_root}/fake-bin-fail"
  fake_uv "${fake_bin}"
  (
    cd "${repo_dir}"
    env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
      PATH="${fake_bin}:${PATH}" \
      ESHU_FAKE_UV_FAIL=1 \
      ESHU_VERIFY_DOCS_BRANCH="" \
      "${verifier}" "$@"
  ) >"${output_file}" 2>&1
}

assert_contains() {
  local needle="$1"
  local file="$2"
  if ! rg -q --fixed-strings "${needle}" "${file}"; then
    echo "test-docs-build: expected ${file} to contain: ${needle}" >&2
    cat "${file}" >&2
    exit 1
  fi
}

assert_not_contains() {
  local needle="$1"
  local file="$2"
  if rg -q --fixed-strings "${needle}" "${file}"; then
    echo "test-docs-build: expected ${file} NOT to contain: ${needle}" >&2
    cat "${file}" >&2
    exit 1
  fi
}

# ─── Tests ──────────────────────────────────────────────────────────

# 1. No docs/navigation/project-guidance changes skips without running MkDocs.
test_no_docs_changes_skips() {
  local repo_dir="${tmp_root}/skip-no-docs"
  local out="${tmp_root}/skip-no-docs.out"
  git_init "${repo_dir}"
  printf '// new feature\npackage foo\nvar X = 1\n' >"${repo_dir}/src/feature.go"
  git_in "${repo_dir}" add src/feature.go
  git_in "${repo_dir}" commit -q -m "feat: add feature"
  run_verifier "${repo_dir}" "${out}"
  assert_contains "no docs/navigation/project-guidance changes" "${out}"
  assert_not_contains "[fake-uv]" "${out}"
}

# 2. A changed file under docs/ triggers the exact docs command.
test_changed_docs_triggers_build() {
  local repo_dir="${tmp_root}/trigger-docs"
  local out="${tmp_root}/trigger-docs.out"
  git_init "${repo_dir}"
  printf '# Updated page\n\nContent.\n' >"${repo_dir}/docs/public/index.md"
  git_in "${repo_dir}" add docs/public/index.md
  git_in "${repo_dir}" commit -q -m "docs: update index"
  run_verifier "${repo_dir}" "${out}"
  assert_contains "[fake-uv] mkdocs build SUCCESS" "${out}"
}

# 3. Root README.md triggers the gate.
test_changed_readme_triggers_build() {
  local repo_dir="${tmp_root}/trigger-readme"
  local out="${tmp_root}/trigger-readme.out"
  git_init "${repo_dir}"
  printf '# Updated README\n' >"${repo_dir}/README.md"
  git_in "${repo_dir}" add README.md
  git_in "${repo_dir}" commit -q -m "docs: update readme"
  run_verifier "${repo_dir}" "${out}"
  assert_contains "[fake-uv] mkdocs build SUCCESS" "${out}"
}

# 4. Root AGENTS.md triggers the gate.
test_changed_agents_triggers_build() {
  local repo_dir="${tmp_root}/trigger-agents"
  local out="${tmp_root}/trigger-agents.out"
  git_init "${repo_dir}"
  printf '# Updated AGENTS\n' >"${repo_dir}/AGENTS.md"
  git_in "${repo_dir}" add AGENTS.md
  git_in "${repo_dir}" commit -q -m "docs: update agents"
  run_verifier "${repo_dir}" "${out}"
  assert_contains "[fake-uv] mkdocs build SUCCESS" "${out}"
}

# 5. Root CLAUDE.md triggers the gate.
test_changed_claude_triggers_build() {
  local repo_dir="${tmp_root}/trigger-claude"
  local out="${tmp_root}/trigger-claude.out"
  git_init "${repo_dir}"
  printf '# Updated CLAUDE\n' >"${repo_dir}/CLAUDE.md"
  git_in "${repo_dir}" add CLAUDE.md
  git_in "${repo_dir}" commit -q -m "docs: update claude"
  run_verifier "${repo_dir}" "${out}"
  assert_contains "[fake-uv] mkdocs build SUCCESS" "${out}"
}

# 6. Failure output includes the exact mkdocs command.
test_failure_contains_exact_command() {
  local repo_dir="${tmp_root}/failure-command"
  local out="${tmp_root}/failure-command.out"
  git_init "${repo_dir}"
  printf '# Updated page\n\nContent.\n' >"${repo_dir}/docs/public/index.md"
  git_in "${repo_dir}" add docs/public/index.md
  git_in "${repo_dir}" commit -q -m "docs: update"
  if run_verifier_fail_uv "${repo_dir}" "${out}"; then
    echo "test-docs-build: expected verifier to fail when uv fails" >&2
    cat "${out}" >&2
    exit 1
  fi
  assert_contains "uv run --with mkdocs --with mkdocs-material --with pymdown-extensions" "${out}"
}

# 7. Branch diff fallback does not leak "no merge base" fatal.
test_no_merge_base_leak() {
  local repo_dir="${tmp_root}/no-merge-base"
  local out="${tmp_root}/no-merge-base.out"
  git_init "${repo_dir}"
  mkdir -p "${repo_dir}/docs/public"
  printf '# New feature doc\n\nContent.\n' >"${repo_dir}/docs/public/new.md"
  git_in "${repo_dir}" add docs/public/new.md
  git_in "${repo_dir}" commit -q -m "docs: new page"

  local feature_head
  feature_head="$(git_in "${repo_dir}" rev-parse HEAD)"
  git_in "${repo_dir}" switch -q --orphan unrelated-main
  git_in "${repo_dir}" rm -qr --ignore-unmatch .
  printf 'unrelated\n' >"${repo_dir}/unrelated.txt"
  git_in "${repo_dir}" add unrelated.txt
  git_in "${repo_dir}" commit -q -m "unrelated main"
  git_in "${repo_dir}" update-ref refs/remotes/origin/main HEAD
  git_in "${repo_dir}" switch -q --detach "${feature_head}"

  run_verifier "${repo_dir}" "${out}"
  assert_contains "[fake-uv]" "${out}"
  assert_not_contains "fatal: origin/main" "${out}"
}

# 8. Staged mode respects staged docs changes and skips unrelated staged files.
test_staged_mode_respects_docs_changes() {
  local repo_dir="${tmp_root}/staged-docs"
  local out="${tmp_root}/staged-docs.out"
  git_init "${repo_dir}"
  # Stage a docs change and an unrelated source change.
  printf '# Staged page\n' >"${repo_dir}/docs/public/staged.md"
  git_in "${repo_dir}" add docs/public/staged.md
  printf '// staged unrelated\npackage bar\n' >"${repo_dir}/src/bar.go"
  git_in "${repo_dir}" add src/bar.go
  run_verifier "${repo_dir}" "${out}" --staged
  assert_contains "[fake-uv] mkdocs build SUCCESS" "${out}"
}

# 9. Staged mode skips when no staged docs/navigation files exist.
test_staged_mode_no_docs_skips() {
  local repo_dir="${tmp_root}/staged-skip"
  local out="${tmp_root}/staged-skip.out"
  git_init "${repo_dir}"
  printf '// only source change\npackage baz\n' >"${repo_dir}/src/baz.go"
  git_in "${repo_dir}" add src/baz.go
  run_verifier "${repo_dir}" "${out}" --staged
  assert_contains "no staged docs/navigation/project-guidance files" "${out}"
  assert_not_contains "[fake-uv]" "${out}"
}

# 10. .opencode/agent/*.md changes trigger the gate.
test_opencode_agent_triggers_build() {
  local repo_dir="${tmp_root}/trigger-opencode"
  local out="${tmp_root}/trigger-opencode.out"
  git_init "${repo_dir}"
  mkdir -p "${repo_dir}/.opencode/agent"
  printf '# Agent doc' >"${repo_dir}/.opencode/agent/test-eshu.md"
  git_in "${repo_dir}" add .opencode/agent/test-eshu.md
  git_in "${repo_dir}" commit -q -m "docs: update opencode agent"
  run_verifier "${repo_dir}" "${out}"
  assert_contains "[fake-uv] mkdocs build SUCCESS" "${out}"
}

# 11. .agents/skills/**/*.md changes trigger the gate.
test_agents_skills_triggers_build() {
  local repo_dir="${tmp_root}/trigger-skills"
  local out="${tmp_root}/trigger-skills.out"
  git_init "${repo_dir}"
  mkdir -p "${repo_dir}/.agents/skills"
  printf '# Skill doc' >"${repo_dir}/.agents/skills/my-skill.md"
  git_in "${repo_dir}" add .agents/skills/my-skill.md
  git_in "${repo_dir}" commit -q -m "docs: update skill"
  run_verifier "${repo_dir}" "${out}"
  assert_contains "[fake-uv] mkdocs build SUCCESS" "${out}"
}

# 12. Branch-mode deletion of a docs file triggers the gate.
test_branch_deletion_triggers_build() {
  local repo_dir="${tmp_root}/branch-deletion"
  local out="${tmp_root}/branch-deletion.out"
  git_init "${repo_dir}"
  git_in "${repo_dir}" rm docs/public/index.md
  git_in "${repo_dir}" commit -q -m "docs: remove index page"
  run_verifier "${repo_dir}" "${out}"
  assert_contains "[fake-uv] mkdocs build SUCCESS" "${out}"
}

# 13. Staged-mode deletion of a docs file triggers the gate.
test_staged_deletion_triggers_build() {
  local repo_dir="${tmp_root}/staged-deletion"
  local out="${tmp_root}/staged-deletion.out"
  git_init "${repo_dir}"
  git_in "${repo_dir}" rm docs/public/index.md
  run_verifier "${repo_dir}" "${out}" --staged
  assert_contains "[fake-uv] mkdocs build SUCCESS" "${out}"
}

# ─── Run ────────────────────────────────────────────────────────────

test_no_docs_changes_skips
test_changed_docs_triggers_build
test_changed_readme_triggers_build
test_changed_agents_triggers_build
test_changed_claude_triggers_build
test_failure_contains_exact_command
test_no_merge_base_leak
test_staged_mode_respects_docs_changes
test_staged_mode_no_docs_skips
test_opencode_agent_triggers_build
test_agents_skills_triggers_build
test_branch_deletion_triggers_build
test_staged_deletion_triggers_build

echo "test-docs-build: all tests passed"
