#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${ESHU_TEST_VERIFIER:-${repo_root}/scripts/verify-frontend-format-changed.sh}"

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

run_verifier_for_main_push() {
  local repo_dir="$1"
  local output_file="$2"
  local prettier_dir="${tmp_root}/fake-bin"
  fake_prettier "${prettier_dir}"
  (
    cd "${repo_dir}"
    env -u GIT_DIR -u GIT_WORK_TREE -u GIT_INDEX_FILE -u GIT_COMMON_DIR \
      GITHUB_EVENT_NAME=push GITHUB_REF_NAME=main \
      ESHU_PRETTIER_BIN="${prettier_dir}/prettier" "${verifier}"
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

test_main_push_checks_previous_commit_when_origin_main_is_head() {
  local repo_dir="${tmp_root}/main-push"
  local out="${tmp_root}/main-push.out"
  git_init "${repo_dir}"
  git_in "${repo_dir}" switch -q main
  mkdir -p "${repo_dir}/src"
  printf 'const value = "UNFORMATTED";\n' >"${repo_dir}/src/bad.ts"
  git_in "${repo_dir}" add src/bad.ts
  git_in "${repo_dir}" commit -q -m "bad format on main"
  git_in "${repo_dir}" update-ref refs/remotes/origin/main HEAD
  if run_verifier_for_main_push "${repo_dir}" "${out}"; then
    echo "test-frontend-format: expected main push unformatted file to fail" >&2
    cat "${out}" >&2
    exit 1
  fi
  assert_contains "fake-prettier:" "${out}"
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
    echo "test-frontend-format: ${what}: want '${want}', got '${got}'" >&2
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
  if [[ -e "$(git_in "${clone}" rev-parse --absolute-git-dir)/FETCH_HEAD" ]]; then
    echo "test-frontend-format: a resolvable base ref must not trigger a fetch" >&2
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

test_no_changed_js_ts_files_skips
test_changed_unformatted_js_ts_fails
test_changed_formatted_js_ts_passes
test_no_merge_base_uses_two_dot_fallback
test_main_push_checks_previous_commit_when_origin_main_is_head
test_staged_no_frontend_js_ts_files_skips
test_staged_unformatted_js_ts_fails
test_staged_formatted_js_ts_passes
test_full_clone_missing_base_stays_full
test_full_clone_resolvable_base_skips_fetch
test_shallow_clone_missing_base_fetches_with_depth

echo "test-frontend-format: all tests passed"
