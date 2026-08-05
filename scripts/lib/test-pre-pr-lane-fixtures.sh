#!/usr/bin/env bash
# test-pre-pr-lane-fixtures.sh — environment, assertions, and throwaway
# repositories for scripts/lib/test-pre-pr-lane.sh (#5721).
#
# Sourced by that suite, never executed. It exists because the suite crossed the
# repo's 500-line cap: the cases are the interesting part and belong in one
# readable file, so the scaffolding they all share moved here.
#
# Expects ${suite_root} (the repo root) to be set by the caller, and leaves
# behind:
#   failures / cases_run   counters the assertions maintain
#   scratch                temp tree for fixtures, reaped on EXIT
#   assert_eq / assert_contains / fail / skip_case
#   init_state             pre_pr_git_state_init plus cleanup bookkeeping
#   running_as_root        guard for the cases that depend on mode bits
#   new_repo / orphan_repo / fake_root   the fixtures themselves

# The fixtures below build throwaway repositories with `git init` and
# `git commit`, and git reads the developer's ~/.gitconfig in every one of them.
# A global `commit.gpgsign = true` makes those commits ask for a signing key
# they will not get; a global `core.hooksPath` pointing at a rejecting
# pre-commit hook stops them outright. Either one reds the suite, and
# `make pre-pr` runs it on every run and fails the run when it reds -- so a
# contributor who signs their commits would be unable to push anything, and the
# summary line would blame the classifier rather than their git config. Pin all
# three knobs: GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM name the files, and
# GIT_CONFIG_NOSYSTEM covers the git versions that honour it instead. Do not
# remove this -- the fixtures are about the lane wiring, not about how anyone
# has configured git.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1

# shellcheck disable=SC2154  # suite_root is set by the sourcing suite.
for _lib in pre-pr-docs-fastpath pre-pr-lane; do
	if [[ ! -f "${suite_root}/scripts/lib/${_lib}.sh" ]]; then
		echo "test-pre-pr-lane: missing library at ${suite_root}/scripts/lib/${_lib}.sh" >&2
		exit 1
	fi
	# shellcheck source=/dev/null
	source "${suite_root}/scripts/lib/${_lib}.sh"
done

failures=0
cases_run=0
scratch="$(mktemp -d)"

# Every pre_pr_git_state_init makes its own `mktemp -d` OUTSIDE ${scratch} --
# that is the library's job, not the suite's -- so the trap below cannot reach
# them and each run used to leave one behind per init. Record them as they are
# made and reap them with everything else.
state_dirs=()

# A case in the suite drops write permission on a directory on purpose; put it
# back before removing the tree, or the cleanup leaves the directory behind.
cleanup() {
	chmod -R u+rwX "${scratch}" >/dev/null 2>&1
	rm -rf "${scratch}"
	if [[ ${#state_dirs[@]} -gt 0 ]]; then
		chmod -R u+rwX "${state_dirs[@]}" >/dev/null 2>&1
		rm -rf "${state_dirs[@]}"
	fi
}
trap cleanup EXIT

# init_state — pre_pr_git_state_init plus the bookkeeping the trap needs. Cases
# call this instead of the library function directly.
init_state() {
	pre_pr_git_state_init
	[[ -n "${pre_pr_state_dir}" ]] && state_dirs+=("${pre_pr_state_dir}")
	return 0
}

fail() {
	echo "FAIL $*" >&2
	failures=$((failures + 1))
}

# skip_case <case-name> <reason> — announce a case that cannot run here, so a
# short run reads as deliberate rather than as a case that quietly vanished.
skip_case() {
	echo "SKIP $1 ($2)"
}

# Root ignores the permission bits on a directory it does not own, so a
# `chmod 500` case asserts a broken channel on a directory root can still write
# and the case reds for a reason that has nothing to do with the code. `make
# pre-pr` fails the whole run when this suite reds, which in a root container
# would block every push, so those cases announce themselves and stand down.
running_as_root() { [[ "$(id -u)" -eq 0 ]]; }

# assert_eq <case-name> <want> <got>
assert_eq() {
	local name="$1" want="$2" got="$3"
	cases_run=$((cases_run + 1))
	if [[ "${got}" != "${want}" ]]; then
		fail "${name}: want '${want}', got '${got}'"
		return
	fi
	echo "PASS ${name}"
}

# assert_contains <case-name> <needle> <haystack>
assert_contains() {
	local name="$1" needle="$2" haystack="$3"
	cases_run=$((cases_run + 1))
	if [[ "${haystack}" != *"${needle}"* ]]; then
		fail "${name}: want output containing '${needle}', got '${haystack}'"
		return
	fi
	echo "PASS ${name}"
}

# assert_not_contains <case-name> <needle> <haystack>
assert_not_contains() {
	local name="$1" needle="$2" haystack="$3"
	cases_run=$((cases_run + 1))
	if [[ "${haystack}" == *"${needle}"* ]]; then
		fail "${name}: want output NOT containing '${needle}', got '${haystack}'"
		return
	fi
	echo "PASS ${name}"
}

# new_repo <name> — throwaway repository with one commit on `main`, plus an
# origin/main ref pointing at it and a `feature` branch one docs commit ahead.
new_repo() {
	local dir="${scratch}/$1"
	mkdir -p "${dir}/docs"
	git init -q -b main "${dir}"
	git -C "${dir}" config user.email test@example.invalid
	git -C "${dir}" config user.name test
	: >"${dir}/README.md"
	git -C "${dir}" add README.md
	git -C "${dir}" commit -qm first
	git -C "${dir}" update-ref refs/remotes/origin/main HEAD
	git -C "${dir}" checkout -q -b feature
	: >"${dir}/docs/page.md"
	git -C "${dir}" add docs/page.md
	git -C "${dir}" commit -qm docs
	printf '%s\n' "${dir}"
}

# orphan_repo <name> — origin/main resolves but shares no history with HEAD, so
# `git diff origin/main...HEAD` exits 128 with "fatal: no merge base" and prints
# nothing. This is the shape that used to read as a clean tree.
orphan_repo() {
	local dir
	dir="$(new_repo "$1")"
	git -C "${dir}" checkout -q --orphan detached
	git -C "${dir}" rm -q -rf . >/dev/null 2>&1
	mkdir -p "${dir}/docs"
	: >"${dir}/docs/page.md"
	git -C "${dir}" add docs/page.md
	git -C "${dir}" commit -qm orphan
	printf '%s\n' "${dir}"
}

# fake_root <name> <docs-suite-rc> <lane-suite-rc> — a repo-shaped directory
# holding stub suites, so the self-check's own pass/fail handling is tested
# without running (or recursing into) the real suites.
fake_root() {
	local dir="${scratch}/$1" docs_rc="$2" lane_rc="$3"
	mkdir -p "${dir}/scripts/lib"
	printf '#!/usr/bin/env bash\nexit %s\n' "${docs_rc}" >"${dir}/scripts/lib/test-pre-pr-docs-fastpath.sh"
	printf '#!/usr/bin/env bash\nexit %s\n' "${lane_rc}" >"${dir}/scripts/lib/test-pre-pr-lane.sh"
	printf '%s\n' "${dir}"
}
