#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-frontend-format-changed.sh"

if [[ ! -x "${verifier}" ]]; then
  echo "test-frontend-format: missing executable verifier at ${verifier}" >&2
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
  printf '{"private":true}\n' >"${repo_dir}/package.json"
  git_in "${repo_dir}" add package.json
  git_in "${repo_dir}" commit -q -m "initial"
  git_in "${repo_dir}" update-ref refs/remotes/origin/main HEAD
  git_in "${repo_dir}" switch -q -c feature
}

fake_prettier() {
  local bin_dir="$1"
  mkdir -p "${bin_dir}"
  cat >"${bin_dir}/prettier" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail
for arg in "$@"; do
  [[ "${arg}" == "--check" ]] && continue
  if [[ -f "${arg}" ]] && rg -q 'UNFORMATTED' "${arg}"; then
    echo "fake-prettier: ${arg} is unformatted" >&2
    exit 1
  fi
done
exit 0
SCRIPT
  chmod +x "${bin_dir}/prettier"
}

run_verifier() {
  local repo_dir="$1"
  local output_file="$2"
  shift 2
  local prettier_dir="${tmp_root}/fake-bin"
  fake_prettier "${prettier_dir}"
  (
    cd "${repo_dir}"
    env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
      ESHU_PRETTIER_BIN="${prettier_dir}/prettier" "${verifier}" "$@"
  ) >"${output_file}" 2>&1
}

assert_contains() {
  local needle="$1"
  local file="$2"
  if ! rg -q --fixed-strings "${needle}" "${file}"; then
    echo "test-frontend-format: expected ${file} to contain: ${needle}" >&2
    cat "${file}" >&2
    exit 1
  fi
}

test_no_changed_js_ts_files_skips() {
  local repo_dir="${tmp_root}/skip"
  local out="${tmp_root}/skip.out"
  git_init "${repo_dir}"
  printf 'notes\n' >"${repo_dir}/README.txt"
  git_in "${repo_dir}" add README.txt
  git_in "${repo_dir}" commit -q -m "docs"
  run_verifier "${repo_dir}" "${out}"
  assert_contains "format: no changed JS/TS files" "${out}"
}

test_changed_unformatted_js_ts_fails() {
  local repo_dir="${tmp_root}/unformatted"
  local out="${tmp_root}/unformatted.out"
  git_init "${repo_dir}"
  mkdir -p "${repo_dir}/apps/console/src"
  printf 'const value = "UNFORMATTED";\n' >"${repo_dir}/apps/console/src/bad.ts"
  git_in "${repo_dir}" add apps/console/src/bad.ts
  git_in "${repo_dir}" commit -q -m "bad format"
  if run_verifier "${repo_dir}" "${out}"; then
    echo "test-frontend-format: expected unformatted file to fail" >&2
    cat "${out}" >&2
    exit 1
  fi
  assert_contains "fake-prettier:" "${out}"
}

test_changed_formatted_js_ts_passes() {
  local repo_dir="${tmp_root}/formatted"
  local out="${tmp_root}/formatted.out"
  git_init "${repo_dir}"
  mkdir -p "${repo_dir}/src"
  printf 'const value = "formatted";\n' >"${repo_dir}/src/good.ts"
  git_in "${repo_dir}" add src/good.ts
  git_in "${repo_dir}" commit -q -m "good format"
  run_verifier "${repo_dir}" "${out}"
  assert_contains "format: checking prettier on 1 changed files" "${out}"
}

test_no_merge_base_uses_two_dot_fallback() {
  local repo_dir="${tmp_root}/no-merge-base"
  local out="${tmp_root}/no-merge-base.out"
  git_init "${repo_dir}"
  mkdir -p "${repo_dir}/apps/console/src"
  printf 'const value = "formatted";\n' >"${repo_dir}/apps/console/src/good.tsx"
  git_in "${repo_dir}" add apps/console/src/good.tsx
  git_in "${repo_dir}" commit -q -m "feature format"
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
  assert_contains "format: no merge base" "${out}"
  assert_contains "format: checking prettier on 1 changed files" "${out}"
  if rg -q --fixed-strings "fatal: origin/main...HEAD: no merge base" "${out}"; then
    echo "test-frontend-format: verifier leaked the triple-dot merge-base fatal" >&2
    cat "${out}" >&2
    exit 1
  fi
}

test_staged_no_frontend_js_ts_files_skips() {
  local repo_dir="${tmp_root}/staged-skip"
  local out="${tmp_root}/staged-skip.out"
  git_init "${repo_dir}"
  printf 'notes\n' >"${repo_dir}/README.txt"
  git_in "${repo_dir}" add README.txt
  run_verifier "${repo_dir}" "${out}" --staged
  assert_contains "format: no staged JS/TS files" "${out}"
}

test_staged_unformatted_js_ts_fails() {
  local repo_dir="${tmp_root}/staged-unformatted"
  local out="${tmp_root}/staged-unformatted.out"
  git_init "${repo_dir}"
  mkdir -p "${repo_dir}/apps/console/src"
  printf 'const value = "UNFORMATTED";\n' >"${repo_dir}/apps/console/src/bad.ts"
  git_in "${repo_dir}" add apps/console/src/bad.ts
  if run_verifier "${repo_dir}" "${out}" --staged; then
    echo "test-frontend-format: expected staged unformatted file to fail" >&2
    cat "${out}" >&2
    exit 1
  fi
  assert_contains "fake-prettier:" "${out}"
}

test_staged_formatted_js_ts_passes() {
  local repo_dir="${tmp_root}/staged-formatted"
  local out="${tmp_root}/staged-formatted.out"
  git_init "${repo_dir}"
  mkdir -p "${repo_dir}/src"
  printf 'const value = "formatted";\n' >"${repo_dir}/src/good.ts"
  git_in "${repo_dir}" add src/good.ts
  run_verifier "${repo_dir}" "${out}" --staged
  assert_contains "format: checking prettier on 1 staged files" "${out}"
}

test_no_changed_js_ts_files_skips
test_changed_unformatted_js_ts_fails
test_changed_formatted_js_ts_passes
test_no_merge_base_uses_two_dot_fallback
test_staged_no_frontend_js_ts_files_skips
test_staged_unformatted_js_ts_fails
test_staged_formatted_js_ts_passes

echo "test-frontend-format: all tests passed"
