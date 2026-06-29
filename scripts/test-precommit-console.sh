#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${repo_root}/scripts/dev/precommit-console.sh"

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

test_no_console_changes_skips_without_node_modules
test_console_change_requires_node_modules
test_console_change_runs_e2e_with_dependencies

echo "test-precommit-console: all tests passed"
