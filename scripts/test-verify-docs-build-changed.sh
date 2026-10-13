#!/usr/bin/env bash
# test-verify-docs-build-changed.sh — hermetic self-test for the
# docs-build-changed verifier, using a fake `uv` shim so the suite
# needs no real MkDocs/Material/pymdown dependency.
#
# Run with:
#   bash scripts/test-verify-docs-build-changed.sh
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${ESHU_TEST_VERIFIER:-${repo_root}/scripts/verify-docs-build-changed.sh}"

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
  # Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
  # the entire heredoc body to a pipe before forking the reader, and macOS's
  # 512-byte pipe buffer deadlocks on any body over that size (#5074).
  cat "${repo_root}/scripts/lib/test-verify-docs-build-changed-fake-uv.sh" >"${bin_dir}/uv"
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

# run_verifier_timeout runs the verifier with a stalled fake-uv build under an
# explicit timeout bound, so the #7983 timeout diagnostics have something to
# fire on. Prints nothing itself; the caller asserts on the exit status and
# the output file.
run_verifier_timeout() {
  local repo_dir="$1"
  local output_file="$2"
  local timeout_secs="$3"
  local lock_dir="$4"
  local fake_bin="${tmp_root}/fake-bin"
  fake_uv "${fake_bin}"
  (
    cd "${repo_dir}"
    env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
      PATH="${fake_bin}:${PATH}" \
      ESHU_FAKE_UV_SLEEP=30 \
      "ESHU_DOCS_BUILD_TIMEOUT_SECONDS=${timeout_secs}" \
      "ESHU_LIVE_GATE_LOCK_DIR=${lock_dir}" \
      ESHU_VERIFY_DOCS_BRANCH="" \
      "${verifier}"
  ) >"${output_file}" 2>&1
}

# 14. A build that overruns its timeout fails LOUD: non-zero exit, the bound
#     in the message, and the live-gate holder named (#7983 AC1/AC2).
test_build_timeout_names_lock_holder() {
  local repo_dir="${tmp_root}/timeout-holder"
  local out="${tmp_root}/timeout-holder.out"
  git_init "${repo_dir}"
  printf '# Updated page\n\nContent.\n' >"${repo_dir}/docs/public/index.md"
  git_in "${repo_dir}" add docs/public/index.md
  git_in "${repo_dir}" commit -q -m "docs: update"
  local lock_home="${tmp_root}/timeout-lock-home"
  mkdir -p "${lock_home}"
  ln -s "424242:999:/busy/sibling-worktree" "${lock_home}/eshu-live-gate.lock"
  if run_verifier_timeout "${repo_dir}" "${out}" 2 "${lock_home}"; then
    echo "test-docs-build: expected verifier to fail when the build overruns its timeout" >&2
    cat "${out}" >&2
    exit 1
  fi
  assert_contains "TIMED OUT after 2s" "${out}"
  assert_contains "424242" "${out}"
  assert_contains "/busy/sibling-worktree" "${out}"
  assert_contains "live-gate lock is HELD" "${out}"
}

# 15. The same timeout with no holder says so, so a stall without contention
#     is not misread as a mutex wait (#7983: the mutex theory was disproven).
test_build_timeout_reports_free_lock() {
  local repo_dir="${tmp_root}/timeout-free"
  local out="${tmp_root}/timeout-free.out"
  git_init "${repo_dir}"
  printf '# Updated page\n\nContent.\n' >"${repo_dir}/docs/public/index.md"
  git_in "${repo_dir}" add docs/public/index.md
  git_in "${repo_dir}" commit -q -m "docs: update"
  local lock_home="${tmp_root}/timeout-free-lock-home"
  mkdir -p "${lock_home}"
  if run_verifier_timeout "${repo_dir}" "${out}" 2 "${lock_home}"; then
    echo "test-docs-build: expected verifier to fail when the build overruns its timeout" >&2
    cat "${out}" >&2
    exit 1
  fi
  assert_contains "TIMED OUT after 2s" "${out}"
  assert_contains "live-gate lock is not held" "${out}"
}

# 16. A non-numeric or out-of-range timeout bound fails closed before the
#     build starts, never silently unbounded.
test_invalid_timeout_config_fails_closed() {
  local repo_dir="${tmp_root}/timeout-config"
  local out="${tmp_root}/timeout-config.out"
  git_init "${repo_dir}"
  printf '# Updated page\n\nContent.\n' >"${repo_dir}/docs/public/index.md"
  git_in "${repo_dir}" add docs/public/index.md
  git_in "${repo_dir}" commit -q -m "docs: update"
  local lock_home="${tmp_root}/timeout-config-lock-home"
  mkdir -p "${lock_home}"
  for bad in "banana" "0"; do
    if run_verifier_timeout "${repo_dir}" "${out}" "${bad}" "${lock_home}"; then
      echo "test-docs-build: expected verifier to fail closed on timeout config '${bad}'" >&2
      cat "${out}" >&2
      exit 1
    fi
    assert_contains "must be an integer from 1 through 3600" "${out}"
  done
}

# 17. With no timeout backend on PATH, the hook fails closed instead of
#     running unbounded. The masked PATH keeps only what the verifier needs
#     before the backend check: bash (env shebang), git (trigger detection),
#     and the fake uv (uv presence check).
test_no_timeout_backend_fails_closed() {
  local repo_dir="${tmp_root}/timeout-no-backend"
  local out="${tmp_root}/timeout-no-backend.out"
  git_init "${repo_dir}"
  printf '# Updated page\n\nContent.\n' >"${repo_dir}/docs/public/index.md"
  git_in "${repo_dir}" add docs/public/index.md
  git_in "${repo_dir}" commit -q -m "docs: update"
  local mask_bin="${tmp_root}/timeout-no-backend-bin"
  mkdir -p "${mask_bin}"
  ln -sf "$(command -v bash)" "${mask_bin}/bash"
  ln -sf "$(command -v git)" "${mask_bin}/git"
  fake_uv "${mask_bin}"
  if (
    cd "${repo_dir}"
    env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
      PATH="${mask_bin}" \
      ESHU_VERIFY_DOCS_BRANCH="" \
      "${verifier}"
  ) >"${out}" 2>&1; then
    echo "test-docs-build: expected verifier to fail closed with no timeout backend" >&2
    cat "${out}" >&2
    exit 1
  fi
  assert_contains "no timeout backend" "${out}"
}

# ─── Shared-store depth guard (#8032) ───────────────────────────────
# A gate script must never turn a full clone shallow: every worktree of the
# clone shares one .git/shallow, so a depth fetch from any worktree breaks
# history-dependent gates in all the others. These cells use a local bare repo
# as origin (file:// so --depth is honoured); nothing touches a real remote.

# make_depth_fixture NAME [clone flags...]: bare origin with 60 commits on
# main, a clone of it on branch feature, local main deleted, and
# refs/remotes/origin/main left in place.
make_depth_fixture() {
  local name="$1"
  shift
  local origin="${tmp_root}/${name}-origin.git"
  local seed="${tmp_root}/${name}-seed"
  local clone="${tmp_root}/${name}-clone"
  local i
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
    git init -q --bare -b main "${origin}"
  mkdir -p "${seed}"
  git_in "${seed}" init -q -b main
  git_in "${seed}" config user.email "test@example.invalid"
  git_in "${seed}" config user.name "Eshu Test"
  git_in "${seed}" config core.hooksPath /dev/null
  printf 'seed\n' >"${seed}/seed.txt"
  git_in "${seed}" add seed.txt
  git_in "${seed}" commit -q -m "seed"
  for i in $(seq 1 59); do
    git_in "${seed}" commit -q --allow-empty -m "c${i}"
  done
  git_in "${seed}" push -q "file://${origin}" main
  env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
    git clone -q "$@" "file://${origin}" "${clone}"
  git_in "${clone}" config user.email "test@example.invalid"
  git_in "${clone}" config user.name "Eshu Test"
  git_in "${clone}" config core.hooksPath /dev/null
  git_in "${clone}" switch -q -c feature
  git_in "${clone}" branch -q -D main
  printf '%s\n' "${clone}"
}

assert_equals() {
  local want="$1"
  local got="$2"
  local what="$3"
  if [[ "${want}" != "${got}" ]]; then
    echo "test-docs-build: ${what}: want '${want}', got '${got}'" >&2
    exit 1
  fi
}

# Full clone, base ref unresolvable: the gate may fetch, but must not shallow.
test_full_clone_missing_base_stays_full() {
  local clone out
  clone="$(make_depth_fixture full-missing)"
  out="${tmp_root}/full-missing.out"
  git_in "${clone}" update-ref -d refs/remotes/origin/main
  GITHUB_BASE_REF=main run_verifier "${clone}" "${out}"
  assert_equals "false" "$(git_in "${clone}" rev-parse --is-shallow-repository)" \
    "full clone must stay full after a base fetch"
  assert_equals "60" "$(git_in "${clone}" rev-list --count refs/remotes/origin/main)" \
    "base fetch must bring the full base history"
}

# Full clone, base ref already resolves: no fetch at all.
test_full_clone_resolvable_base_skips_fetch() {
  local clone out
  clone="$(make_depth_fixture full-resolved)"
  out="${tmp_root}/full-resolved.out"
  GITHUB_BASE_REF=main run_verifier "${clone}" "${out}"
  assert_equals "false" "$(git_in "${clone}" rev-parse --is-shallow-repository)" \
    "full clone must stay full when the base resolves"
  if [[ -e "$(git_in "${clone}" rev-parse --git-path FETCH_HEAD)" ]]; then
    echo "test-docs-build: a resolvable base ref must not trigger a fetch" >&2
    exit 1
  fi
}

# Shallow clone (the CI fetch-depth case): the depth-50 fetch still happens.
test_shallow_clone_missing_base_fetches_with_depth() {
  local clone out
  clone="$(make_depth_fixture shallow-missing --depth=1)"
  out="${tmp_root}/shallow-missing.out"
  git_in "${clone}" update-ref -d refs/remotes/origin/main
  GITHUB_BASE_REF=main run_verifier "${clone}" "${out}"
  assert_equals "true" "$(git_in "${clone}" rev-parse --is-shallow-repository)" \
    "shallow clone stays shallow"
  assert_equals "50" "$(git_in "${clone}" rev-list --count refs/remotes/origin/main)" \
    "shallow clone must keep the depth-50 base fetch"
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
test_build_timeout_names_lock_holder
test_build_timeout_reports_free_lock
test_invalid_timeout_config_fails_closed
test_no_timeout_backend_fails_closed
test_full_clone_missing_base_stays_full
test_full_clone_resolvable_base_skips_fetch
test_shallow_clone_missing_base_fetches_with_depth

echo "test-docs-build: all tests passed"
