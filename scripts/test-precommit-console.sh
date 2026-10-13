#!/usr/bin/env bash
# test-precommit-console.sh — hermetic self-test for the console pre-push
# helper (scripts/dev/precommit-console.sh), using throwaway mktemp repos.
#
# Run with:
#   bash scripts/test-precommit-console.sh
#
# ESHU_TEST_VERIFIER overrides the helper under test (default: the helper in
# this checkout). Use it to run the cells against an older copy of the script
# and confirm they fail there.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${ESHU_TEST_VERIFIER:-${repo_root}/scripts/dev/precommit-console.sh}"

if [[ ! -x "${helper}" ]]; then
  echo "test-precommit-console: missing executable helper at ${helper}" >&2
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
  mkdir -p "${repo_dir}/scripts/dev"
  cp "${helper}" "${repo_dir}/scripts/dev/precommit-console.sh"
  chmod +x "${repo_dir}/scripts/dev/precommit-console.sh"
  git_in "${repo_dir}" init -q -b main
  git_in "${repo_dir}" config user.email "test@example.invalid"
  git_in "${repo_dir}" config user.name "Eshu Test"
  git_in "${repo_dir}" config core.hooksPath /dev/null
  printf '{"private":true}\n' >"${repo_dir}/package.json"
  git_in "${repo_dir}" add package.json scripts/dev/precommit-console.sh
  git_in "${repo_dir}" commit -q -m "initial"
  git_in "${repo_dir}" update-ref refs/remotes/origin/main HEAD
  git_in "${repo_dir}" switch -q -c feature
}

fake_node_tools() {
  local bin_dir="$1"
  mkdir -p "${bin_dir}"
  cat >"${bin_dir}/npx" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail
printf 'fake-npx:%s\n' "$*"
SCRIPT
  cat >"${bin_dir}/npm" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail
printf 'fake-npm:%s\n' "$*"
SCRIPT
  chmod +x "${bin_dir}/npx" "${bin_dir}/npm"
}

run_helper() {
  local repo_dir="$1"
  local output_file="$2"
  local bin_dir="${tmp_root}/fake-bin"
  fake_node_tools "${bin_dir}"
  (
    cd "${repo_dir}"
    env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
      PATH="${bin_dir}:${PATH}" scripts/dev/precommit-console.sh e2e
  ) >"${output_file}" 2>&1
}

assert_contains() {
  local needle="$1"
  local file="$2"
  if ! rg -q --fixed-strings "${needle}" "${file}"; then
    echo "test-precommit-console: expected ${file} to contain: ${needle}" >&2
    sed -n '1,160p' "${file}" >&2
    exit 1
  fi
}

test_no_console_changes_skips_without_node_modules() {
  local repo_dir="${tmp_root}/skip"
  local out="${tmp_root}/skip.out"
  git_init "${repo_dir}"
  printf 'notes\n' >"${repo_dir}/README.txt"
  git_in "${repo_dir}" add README.txt
  git_in "${repo_dir}" commit -q -m "docs"
  run_helper "${repo_dir}" "${out}"
  assert_contains "precommit-console: no apps/console changes in this branch; skipping console gate." "${out}"
}

test_console_change_requires_node_modules() {
  local repo_dir="${tmp_root}/missing-node-modules"
  local out="${tmp_root}/missing-node-modules.out"
  git_init "${repo_dir}"
  mkdir -p "${repo_dir}/apps/console/src"
  printf 'export const page = true;\n' >"${repo_dir}/apps/console/src/page.ts"
  git_in "${repo_dir}" add apps/console/src/page.ts
  git_in "${repo_dir}" commit -q -m "console page"
  if run_helper "${repo_dir}" "${out}"; then
    echo "test-precommit-console: expected missing node_modules to fail" >&2
    sed -n '1,160p' "${out}" >&2
    exit 1
  fi
  assert_contains "precommit-console: node_modules is missing or incomplete." "${out}"
}

test_console_change_runs_e2e_with_dependencies() {
  local repo_dir="${tmp_root}/runs"
  local out="${tmp_root}/runs.out"
  git_init "${repo_dir}"
  mkdir -p "${repo_dir}/apps/console/src" "${repo_dir}/node_modules/playwright" "${repo_dir}/node_modules/vite"
  printf 'export const page = true;\n' >"${repo_dir}/apps/console/src/page.ts"
  git_in "${repo_dir}" add apps/console/src/page.ts
  git_in "${repo_dir}" commit -q -m "console page"
  run_helper "${repo_dir}" "${out}"
  assert_contains "fake-npx:playwright install chromium" "${out}"
  assert_contains "fake-npm:run console:e2e:mock" "${out}"
  assert_contains "precommit-console: console gate passed." "${out}"
}

test_console_delete_runs_e2e_with_dependencies() {
  local repo_dir="${tmp_root}/delete-runs"
  local out="${tmp_root}/delete-runs.out"
  git_init "${repo_dir}"
  mkdir -p "${repo_dir}/apps/console/src" "${repo_dir}/node_modules/playwright" "${repo_dir}/node_modules/vite"
  printf 'export const page = true;\n' >"${repo_dir}/apps/console/src/page.ts"
  git_in "${repo_dir}" add apps/console/src/page.ts
  git_in "${repo_dir}" commit -q -m "console page"
  git_in "${repo_dir}" update-ref refs/remotes/origin/main HEAD
  rm "${repo_dir}/apps/console/src/page.ts"
  git_in "${repo_dir}" add apps/console/src/page.ts
  git_in "${repo_dir}" commit -q -m "delete console page"
  run_helper "${repo_dir}" "${out}"
  assert_contains "fake-npx:playwright install chromium" "${out}"
  assert_contains "fake-npm:run console:e2e:mock" "${out}"
  assert_contains "precommit-console: console gate passed." "${out}"
}

# ─── Shared-store depth guard (#8032) ───────────────────────────────
# The helper runs at every pre-push (always_run), before its "no console
# changes" skip. It must never turn a full clone shallow: every worktree of a
# clone shares one .git/shallow, so a depth fetch from any worktree breaks
# history-dependent gates in all the others. These cells use a local bare repo
# as origin (file:// so --depth is honoured); nothing touches a real remote.

# make_depth_fixture NAME [clone flags...]: bare origin with 60 commits on
# main, a clone of it on branch feature with the helper copied in untracked,
# and refs/remotes/origin/main left in place.
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
  mkdir -p "${clone}/scripts/dev"
  cp "${helper}" "${clone}/scripts/dev/precommit-console.sh"
  chmod +x "${clone}/scripts/dev/precommit-console.sh"
  printf '%s\n' "${clone}"
}

assert_equals() {
  local want="$1"
  local got="$2"
  local what="$3"
  if [[ "${want}" != "${got}" ]]; then
    echo "test-precommit-console: ${what}: want '${want}', got '${got}'" >&2
    exit 1
  fi
}

# Full clone, base ref unresolvable: the helper may fetch, but must not shallow.
test_full_clone_missing_base_stays_full() {
  local clone out
  clone="$(make_depth_fixture full-missing)"
  out="${tmp_root}/full-missing.out"
  git_in "${clone}" update-ref -d refs/remotes/origin/main
  run_helper "${clone}" "${out}"
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
  run_helper "${clone}" "${out}"
  assert_equals "false" "$(git_in "${clone}" rev-parse --is-shallow-repository)" \
    "full clone must stay full when the base resolves"
  if [[ -e "$(git_in "${clone}" rev-parse --absolute-git-dir)/FETCH_HEAD" ]]; then
    echo "test-precommit-console: a resolvable base ref must not trigger a fetch" >&2
    exit 1
  fi
}

# Shallow clone (the CI fetch-depth case): the depth-50 fetch still happens.
test_shallow_clone_missing_base_fetches_with_depth() {
  local clone out
  clone="$(make_depth_fixture shallow-missing --depth=1)"
  out="${tmp_root}/shallow-missing.out"
  git_in "${clone}" update-ref -d refs/remotes/origin/main
  run_helper "${clone}" "${out}"
  assert_equals "true" "$(git_in "${clone}" rev-parse --is-shallow-repository)" \
    "shallow clone stays shallow"
  assert_equals "50" "$(git_in "${clone}" rev-list --count refs/remotes/origin/main)" \
    "shallow clone must keep the depth-50 base fetch"
}

test_no_console_changes_skips_without_node_modules
test_console_change_requires_node_modules
test_console_change_runs_e2e_with_dependencies
test_console_delete_runs_e2e_with_dependencies
test_full_clone_missing_base_stays_full
test_full_clone_resolvable_base_skips_fetch
test_shallow_clone_missing_base_fetches_with_depth

echo "test-precommit-console: all tests passed"
