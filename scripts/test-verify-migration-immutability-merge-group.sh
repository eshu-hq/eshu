#!/usr/bin/env bash
# Exercises the merge-queue checkout shape: GITHUB_BASE_REF is absent, HEAD is
# a shallow synthetic queue position, and origin/main is present but initially
# has no visible common ancestor with that position. Positions two and three
# therefore need a bounded deepen before the migration immutability verifier
# can safely inspect the full queued range.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-migration-immutability.sh"
migrations_dir="go/internal/storage/postgres/migrations"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT
out_file="${tmp_root}/verifier.out"
err_file="${tmp_root}/verifier.err"

init_queue_remote() {
  local name="$1"
  local mutate_position_two="${2:-false}"
  local remote="${tmp_root}/${name}.git"
  local writer="${tmp_root}/${name}-writer"

  git init -q --bare "${remote}"
  git init -q "${writer}"
  git -C "${writer}" config user.email "test@example.invalid"
  git -C "${writer}" config user.name "Eshu Test"
  git -C "${writer}" branch -M main
  git -C "${writer}" remote add origin "${remote}"
  mkdir -p "${writer}/${migrations_dir}" "${writer}/queue"
  printf -- '-- 001 shipped migration\nCREATE TABLE widgets (id INT);\n' \
    >"${writer}/${migrations_dir}/001_widgets.sql"
  printf 'package queue\n' >"${writer}/queue/source.go"
  git -C "${writer}" add .
  git -C "${writer}" commit -q -m 'main: shipped migration'
  git -C "${writer}" push -q origin main
  git -C "${remote}" symbolic-ref HEAD refs/heads/main

  git -C "${writer}" checkout -q -b queue/p1
  printf 'package queue\n\n// queue position one\n' >"${writer}/queue/source.go"
  git -C "${writer}" add queue/source.go
  git -C "${writer}" commit -q -m 'queue: position one'
  git -C "${writer}" push -q origin queue/p1

  git -C "${writer}" checkout -q -b queue/p2
  if [ "${mutate_position_two}" = "true" ]; then
    printf -- '-- forbidden mutation in queue position two\n' \
      >>"${writer}/${migrations_dir}/001_widgets.sql"
    git -C "${writer}" add "${migrations_dir}/001_widgets.sql"
    git -C "${writer}" commit -q -m 'queue: position two edits shipped migration'
  else
    printf 'package queue\n\n// queue position two\n' >"${writer}/queue/source.go"
    git -C "${writer}" add queue/source.go
    git -C "${writer}" commit -q -m 'queue: position two'
  fi
  git -C "${writer}" push -q origin queue/p2

  git -C "${writer}" checkout -q -b queue/p3
  printf 'package queue\n\n// queue position three\n' >"${writer}/queue/source.go"
  git -C "${writer}" add queue/source.go
  git -C "${writer}" commit -q -m 'queue: position three'
  git -C "${writer}" push -q origin queue/p3
  printf '%s\n' "${remote}"
}

shallow_queue_checkout() {
  local remote="$1"
  local position="$2"
  local checkout_name="${3:-${position}}"
  local checkout="${tmp_root}/checkout-${checkout_name}-$(basename "${remote}" .git)"

  git clone -q --depth 2 --branch "queue/${position}" "file://${remote}" "${checkout}"
  # This is the base ref that actions/checkout makes available to the verifier.
  # The separate depth-1 fetch deliberately leaves the actual merge base outside
  # the queue position's shallow history for positions two and three.
  git -C "${checkout}" fetch -q --depth 1 origin main:refs/remotes/origin/main
  if [ "$(git -C "${checkout}" rev-parse --is-shallow-repository)" != "true" ]; then
    printf 'expected shallow merge-group fixture for %s\n' "${position}" >&2
    exit 1
  fi
  if git -C "${checkout}" merge-base origin/main HEAD >/dev/null 2>&1; then
    if [ "${position}" != "p1" ]; then
      printf 'expected depth-2 fixture for %s to hide the merge base\n' "${position}" >&2
      exit 1
    fi
  elif [ "${position}" = "p1" ]; then
    printf 'expected queue position one to retain its merge base\n' >&2
    exit 1
  fi
  printf '%s\n' "${checkout}"
}

expect_pass() {
  local checkout="$1"
  if ! env -u ESHU_MIGRATION_IMMUTABILITY_BASE -u GITHUB_BASE_REF \
    ESHU_MIGRATION_IMMUTABILITY_REPO_ROOT="${checkout}" \
    "${verifier}" >"${out_file}" 2>"${err_file}"; then
    printf 'expected verifier to pass for shallow queue checkout %s, got:\n' "${checkout}" >&2
    sed -n '1,120p' "${err_file}" >&2
    exit 1
  fi
}

expect_fail() {
  local checkout="$1"
  local want="$2"
  if env -u ESHU_MIGRATION_IMMUTABILITY_BASE -u GITHUB_BASE_REF \
    ESHU_MIGRATION_IMMUTABILITY_REPO_ROOT="${checkout}" \
    "${verifier}" >"${out_file}" 2>"${err_file}"; then
    printf 'expected verifier to fail for shallow queue checkout %s\n' "${checkout}" >&2
    exit 1
  fi
  if ! rg -q --fixed-strings -- "${want}" "${err_file}"; then
    printf 'expected verifier stderr to contain %q, got:\n' "${want}" >&2
    sed -n '1,120p' "${err_file}" >&2
    exit 1
  fi
}

legacy_masked_fetch_merge_base() {
  # This is the pre-fix resolution loop. It is intentionally test-only: its
  # `|| true` around a failed explicit base fetch is the false-green behavior
  # this fixture must distinguish from the shipped verifier.
  local checkout="$1"
  local ref="origin/main"
  local mb
  local depth=100

  if mb="$(git -C "${checkout}" merge-base "${ref}" HEAD 2>/dev/null)"; then
    printf '%s\n' "${mb}"
    return 0
  fi
  while [ "${depth}" -le 3200 ]; do
    git -C "${checkout}" fetch --no-tags --deepen="${depth}" >/dev/null 2>&1 || true
    git -C "${checkout}" fetch --no-tags --update-shallow --deepen="${depth}" origin \
      main:refs/remotes/origin/main >/dev/null 2>&1 || true
    if mb="$(git -C "${checkout}" merge-base "${ref}" HEAD 2>/dev/null)"; then
      printf '%s\n' "${mb}"
      return 0
    fi
    depth=$((depth * 4))
  done
  return 1
}

make_explicit_base_fetch_failure_wrapper() {
  local wrapper_dir="$1"
  mkdir -p "${wrapper_dir}"
  printf '%s\n' \
    '#!/usr/bin/env bash' \
    'set -euo pipefail' \
    'has_fetch=false' \
    'has_origin=false' \
    'has_main_refspec=false' \
    'for arg in "$@"; do' \
    '  [ "${arg}" = "fetch" ] && has_fetch=true' \
    '  [ "${arg}" = "origin" ] && has_origin=true' \
    '  [ "${arg}" = "main:refs/remotes/origin/main" ] && has_main_refspec=true' \
    'done' \
    'if [ "${has_fetch}" = true ] && [ "${has_origin}" = true ] && [ "${has_main_refspec}" = true ]; then' \
    '  printf "blocked explicit main fetch: %s\\n" "$*" >>"${ESHU_GIT_WRAPPER_LOG}"' \
    '  exit 1' \
    'fi' \
    'exec "${ESHU_REAL_GIT}" "$@"' >"${wrapper_dir}/git"
  chmod +x "${wrapper_dir}/git"
}

clean_remote="$(init_queue_remote clean-queue)"
for position in p1 p2 p3; do
  expect_pass "$(shallow_queue_checkout "${clean_remote}" "${position}")"
done

# The mutation is in position two, but the verifier is run at position three:
# the regression must inspect the entire queued range rather than only HEAD~1.
violating_remote="$(init_queue_remote violating-queue true)"
expect_fail "$(shallow_queue_checkout "${violating_remote}" p2)" "001_widgets.sql was modified"
expect_fail "$(shallow_queue_checkout "${violating_remote}" p3)" "001_widgets.sql was modified"

# The base is reachable in the stale local checkout but no longer represents
# remote main. The PATH wrapper fails only the explicit base fetch; anonymous
# deepening still succeeds and reaches the stale base. The prior masked-fetch
# loop would therefore pass falsely, while the shipped verifier must fail with
# its precise failed-deepen diagnostic.
moving_remote="$(init_queue_remote moving-main)"
moving_current_checkout="$(shallow_queue_checkout "${moving_remote}" p3)"
moving_legacy_checkout="$(shallow_queue_checkout "${moving_remote}" p3 p3-legacy)"
stale_main="$(git -C "${moving_current_checkout}" rev-parse origin/main)"
moving_writer="${tmp_root}/moving-main-rewriter"
git clone -q "file://${moving_remote}" "${moving_writer}"
git -C "${moving_writer}" config user.email "test@example.invalid"
git -C "${moving_writer}" config user.name "Eshu Test"
git -C "${moving_writer}" checkout -q --orphan replacement-main
printf 'replacement main with unrelated history\n' >"${moving_writer}/replacement.txt"
git -C "${moving_writer}" add replacement.txt
git -C "${moving_writer}" commit -q -m 'replacement main'
git -C "${moving_writer}" push -q --force origin HEAD:main
remote_main="$(git -C "${moving_remote}" rev-parse refs/heads/main)"
if [ "${stale_main}" = "${remote_main}" ]; then
  printf 'expected remote main to move away from the stale reachable base\n' >&2
  exit 1
fi

wrapper_dir="${tmp_root}/fail-explicit-base-fetch"
wrapper_log="${tmp_root}/fail-explicit-base-fetch.log"
real_git="$(command -v git)"
make_explicit_base_fetch_failure_wrapper "${wrapper_dir}"
if env -u ESHU_MIGRATION_IMMUTABILITY_BASE -u GITHUB_BASE_REF \
  PATH="${wrapper_dir}:${PATH}" \
  ESHU_GIT_WRAPPER_LOG="${wrapper_log}" \
  ESHU_MIGRATION_IMMUTABILITY_REPO_ROOT="${moving_current_checkout}" \
  ESHU_REAL_GIT="${real_git}" \
  "${verifier}" >"${out_file}" 2>"${err_file}"; then
  printf 'expected verifier to fail when the explicit main fetch fails\n' >&2
  exit 1
fi
if ! rg -q --fixed-strings -- "failed to deepen origin/main" "${err_file}"; then
  printf 'expected failed explicit base fetch diagnostic, got:\n' >&2
  sed -n '1,120p' "${err_file}" >&2
  exit 1
fi

if ! legacy_base="$(PATH="${wrapper_dir}:${PATH}" \
  ESHU_GIT_WRAPPER_LOG="${wrapper_log}" \
  ESHU_REAL_GIT="${real_git}" \
  legacy_masked_fetch_merge_base "${moving_legacy_checkout}")"; then
  printf 'expected the prior masked-fetch loop to produce its false-green base\n' >&2
  exit 1
fi
if [ "${legacy_base}" != "${stale_main}" ]; then
  printf 'expected prior masked-fetch loop to reuse stale base %s, got %s\n' \
    "${stale_main}" "${legacy_base}" >&2
  exit 1
fi
if ! rg -q --fixed-strings -- "blocked explicit main fetch" "${wrapper_log}"; then
  printf 'expected wrapper to block only the explicit base fetch\n' >&2
  exit 1
fi

printf 'test-verify-migration-immutability-merge-group: all scenarios passed\n'
