#!/usr/bin/env bash
# Merge-tree cases for scripts/test-pre-push.sh (#7111 F5: the floor tests the
# merge, not the head). Sourced, never executed: it relies on the caller's
# `fail`, `temp_root`, and `script`, and on `rg` and `git` on PATH.
#
# Each case builds a throwaway repository where refs/remotes/origin/main and
# HEAD diverge from a shared baseline, then runs the REAL pre-push driver with
# a fake `go`. The fake models one compile rule — a call to p.Old() needs a
# `func Old(` somewhere in the tree it runs in — so a head that builds alone
# but not merged with main fails exactly where a real compiler would: in the
# merged tree, never in the head tree.
# shellcheck disable=SC2154  # fail, temp_root, script, repo_root: the caller's.

# write_merge_fake_go installs the compile-modelling `go` stand-in as <bin>/go.
write_merge_fake_go() {
	cp "${repo_root}/scripts/lib/test-pre-push-merge-fake-go.sh" "$1/go"
	chmod +x "$1/go"
}

# merge_fixture_commit stages everything and commits with hooks disabled.
merge_fixture_commit() {
	local fixture="$1" message="$2"
	git -C "${fixture}" -c core.hooksPath=/dev/null add -A
	git -C "${fixture}" -c core.hooksPath=/dev/null -c user.name=Test \
		-c user.email=test@example.invalid commit -qm "${message}"
}

# build_merge_fixture <name> <scenario> creates a repo whose origin/main and
# HEAD both descend from one baseline. Scenarios:
#   conflict  main and head edit the same line of go/internal/p/def.go
#   renamed   main renames p.Old to p.New; head adds a new caller of p.Old
#   clean     main adds an unrelated package; head adds a caller of p.Old
#   contained HEAD already contains origin/main (the merge is HEAD itself)
#   docsonly  main adds a package; head edits only a non-Go file
#   sdkonly   go.mod replaces a module with ../sdk in a block; main adds a Go
#             package; head edits only sdk/
#   sdkquoted the same, with a quoted single-line `replace ... => "../sdk"`
build_merge_fixture() {
	local name="$1" scenario="$2" fixture baseline
	fixture="${temp_root}/${name}"
	rm -rf "${fixture}"
	mkdir -p "${fixture}/scripts/dev" "${fixture}/scripts/lib" "${fixture}/bin" \
		"${fixture}/go/internal/p"
	cp "${script}" "${fixture}/scripts/dev/pre-push.sh"
	cp "${repo_root}"/scripts/lib/pre-pr-*.sh "${fixture}/scripts/lib/"
	cp "${repo_root}"/scripts/lib/pre-push-*.sh "${fixture}/scripts/lib/" 2>/dev/null || true
	write_merge_fake_go "${fixture}/bin"
	printf '#!/usr/bin/env bash\nexit 0\n' > "${fixture}/scripts/dev/precommit-go.sh"
	printf '#!/usr/bin/env bash\nexit 0\n' > "${fixture}/scripts/dev/run-selected-gates.sh"
	printf '#!/usr/bin/env bash\nexit 0\n' > "${fixture}/scripts/verify-docs-contradiction.sh"
	chmod +x "${fixture}/scripts/dev/precommit-go.sh" "${fixture}/scripts/dev/run-selected-gates.sh" \
		"${fixture}/scripts/verify-docs-contradiction.sh"
	printf 'module example.com/m\n\ngo 1.22\n' > "${fixture}/go/go.mod"
	case "${scenario}" in
		sdkonly) printf 'replace (\n\texample.com/sdk v1.0.0 => ../sdk/\n)\n' >> "${fixture}/go/go.mod" ;;
		sdkquoted) printf 'replace example.com/sdk => "../sdk"\n' >> "${fixture}/go/go.mod" ;;
	esac
	mkdir -p "${fixture}/sdk"
	printf 'package sdk\n' > "${fixture}/sdk/lib.go"
	printf 'package p\n\nfunc Old() {}\n' > "${fixture}/go/internal/p/def.go"
	git -C "${fixture}" init -q -b feature
	merge_fixture_commit "${fixture}" baseline
	baseline="$(git -C "${fixture}" rev-parse HEAD)"

	# main's side, recorded as origin/main, then back to the baseline.
	case "${scenario}" in
		conflict) printf 'package p\n\nfunc Old() { _ = 2 }\n' > "${fixture}/go/internal/p/def.go" ;;
		renamed) printf 'package p\n\nfunc New() {}\n' > "${fixture}/go/internal/p/def.go" ;;
		clean | contained | docsonly | sdkonly | sdkquoted)
			mkdir -p "${fixture}/go/internal/s"
			printf 'package s\n' > "${fixture}/go/internal/s/s.go"
			;;
	esac
	merge_fixture_commit "${fixture}" "main side"
	git -C "${fixture}" update-ref refs/remotes/origin/main HEAD
	[[ "${scenario}" == "contained" ]] || git -C "${fixture}" reset -q --hard "${baseline}"

	# the head's side.
	case "${scenario}" in
		conflict) printf 'package p\n\nfunc Old() { _ = 3 }\n' > "${fixture}/go/internal/p/def.go" ;;
		renamed | clean | contained)
			mkdir -p "${fixture}/go/internal/r"
			printf 'package r\n\nimport "example.com/m/internal/p"\n\nfunc F() { p.Old() }\n' \
				> "${fixture}/go/internal/r/call.go"
			;;
		docsonly) printf 'docs\n' > "${fixture}/NOTES.md" ;;
		sdkonly | sdkquoted) printf 'package sdk\n\nfunc Changed() {}\n' > "${fixture}/sdk/lib.go" ;;
	esac
	merge_fixture_commit "${fixture}" "head side"
	printf '%s\n' "${fixture}"
}

run_merge_fixture() {
	local fixture="$1" status=0
	: > "${fixture}.args"
	DRIVER_ARGS_LOG="${fixture}.args" PATH="${fixture}/bin:${PATH}" \
		bash "${fixture}/scripts/dev/pre-push.sh" > "${fixture}.log" 2>&1 || status=$?
	printf '%s' "${status}"
}

# merged_go_calls prints the fake-go invocations that ran in the merged tree,
# which pre-push materializes under the worktree's git dir, never the worktree.
merged_go_calls() {
	rg -- 'cwd=.*eshu-pre-push-merge' "$1.args" || true
}

# ── Case E: the head conflicts with origin/main → fail closed, name the file,
# and build nothing (there is no merged tree to test).
fixture="$(build_merge_fixture case-e conflict)"
status="$(run_merge_fixture "${fixture}")"
[[ "${status}" != "0" ]] || { cat "${fixture}.log" >&2; fail "case E: a conflicting merge must fail closed, got exit 0"; }
rg -q -- 'does not merge cleanly' "${fixture}.log" || { cat "${fixture}.log" >&2; fail "case E: failure must say the head does not merge cleanly"; }
rg -q -- 'go/internal/p/def.go' "${fixture}.log" || fail "case E: failure must name the conflicting path"
[[ -z "$(merged_go_calls "${fixture}")" ]] || fail "case E: nothing may be built for a conflicted merge"

# ── Case F: the head builds alone but main renamed a symbol it calls → the
# head-scoped build passes and the merged-tree build fails the run (#7053).
fixture="$(build_merge_fixture case-f renamed)"
status="$(run_merge_fixture "${fixture}")"
[[ "${status}" != "0" ]] || { cat "${fixture}.log" >&2; fail "case F: a head that breaks merged with main must fail, got exit 0"; }
rg -q -- 'undefined: p.Old' "${fixture}.log" || { cat "${fixture}.log" >&2; fail "case F: the merged-tree compile error must be shown"; }
rg -q -- '^go (build|vet) .*cwd=.*eshu-pre-push-merge' "${fixture}.args" || { cat "${fixture}.args" >&2; fail "case F: go build/vet never ran in the merged tree"; }
rg -q -- '^FAIL  .*merge' "${fixture}.log" || { cat "${fixture}.log" >&2; fail "case F: the summary must name the failed merge step"; }
rg -q -- '^PASS  gofumpt \+ lint \+ build \+ vet' "${fixture}.log" || fail "case F: the head-scoped build/vet must still pass on its own"

# ── Case G: a clean merge that compiles passes, runs whole-module go vet in the
# merged tree, and leaves the worktree's own status untouched.
fixture="$(build_merge_fixture case-g clean)"
status="$(run_merge_fixture "${fixture}")"
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case G: a clean compiling merge must pass, got ${status}"; }
merged_go_calls "${fixture}" | rg -q -- '^go vet \./\.\.\. ' || { cat "${fixture}.args" >&2; fail "case G: merged tree was not vetted whole-module"; }
[[ -z "$(git -C "${fixture}" status --porcelain)" ]] || fail "case G: the merged tree must not appear in the worktree's status"
rg -q -- "^go test -race -count=1 -timeout 900s \./internal/r cwd=${fixture}/go\$" "${fixture}.args" || { cat "${fixture}.args" >&2; fail "case G: the changed package was not race-tested"; }
rg -q -- 'merge tree [0-9a-f]{40}' "${fixture}.log" || fail "case G: the log must name the merge tree id it tested"

# ── Case H: HEAD already contains origin/main → the merge IS HEAD; the step
# says so and skips the duplicate build instead of silently passing.
fixture="$(build_merge_fixture case-h contained)"
status="$(run_merge_fixture "${fixture}")"
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case H: expected exit 0, got ${status}"; }
rg -q -- 'already contains' "${fixture}.log" || { cat "${fixture}.log" >&2; fail "case H: the skip must be explicit"; }
[[ -z "$(merged_go_calls "${fixture}")" ]] || fail "case H: no duplicate merged-tree build when merge == HEAD"

# ── Case I: no origin/main at all → fail closed before any step (exit 2),
# never a silent head-only pass.
fixture="$(build_merge_fixture case-i clean)"
git -C "${fixture}" update-ref -d refs/remotes/origin/main
status="$(run_merge_fixture "${fixture}")"
[[ "${status}" == "2" ]] || { cat "${fixture}.log" >&2; fail "case I: a missing origin/main must fail closed with exit 2, got ${status}"; }
[[ -z "$(merged_go_calls "${fixture}")" ]] || fail "case I: nothing may run in a merged tree without a base"

# ── Case J: the head changes no Go input (go/ or a go.mod local replace dir),
# so the merged Go inputs equal main's, which main's own CI already vetted.
fixture="$(build_merge_fixture case-j docsonly)"
status="$(run_merge_fixture "${fixture}")"
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case J: expected exit 0, got ${status}"; }
rg -q -- 'Go inputs are identical' "${fixture}.log" || { cat "${fixture}.log" >&2; fail "case J: the no-Go-input skip must be explicit"; }
[[ -z "$(merged_go_calls "${fixture}")" ]] || fail "case J: nothing to vet when the merged Go inputs equal the base's"
rg -q -- '^go test -race' "${fixture}.args" && fail "case J: no race run without a changed Go package"
true

# ── Case K: HEAD moves while the merge is vetted (a concurrent commit or
# amend). The summary must name the HEAD that was merged and vetted, not
# whatever HEAD is when the summary prints.
fixture="$(build_merge_fixture case-k clean)"
vetted_head="$(git -C "${fixture}" rev-parse --short=12 HEAD)"
: > "${fixture}.args"
status=0
FAKE_GO_MOVE_HEAD_REPO="${fixture}" DRIVER_ARGS_LOG="${fixture}.args" PATH="${fixture}/bin:${PATH}" \
	bash "${fixture}/scripts/dev/pre-push.sh" > "${fixture}.log" 2>&1 || status=$?
[[ "$(git -C "${fixture}" rev-parse --short=12 HEAD)" != "${vetted_head}" ]] || fail "case K: the fake go did not move HEAD"
rg -q -- "^merge tree: [0-9a-f]{40} \\(HEAD ${vetted_head} \\+ origin/main " "${fixture}.log" || \
	{ rg -- '^merge tree' "${fixture}.log" >&2; fail "case K: the summary must name the vetted HEAD ${vetted_head}"; }

# assert_vetted_merged_tree fails unless <fixture> ran `go vet ./...` in the
# merged tree and did not skip it as "no Go inputs changed".
assert_vetted_merged_tree() {
	local fixture="$1" label="$2"
	merged_go_calls "${fixture}" | rg -q -- '^go vet \./\.\.\. ' || { cat "${fixture}.log" "${fixture}.args" >&2; fail "${label}: the merged tree was not vetted"; }
	! rg -q -- 'Go inputs are identical' "${fixture}.log" || fail "${label}: the vet was skipped as if no Go input changed"
}

# ── Case L: the head changes ONLY a directory named by a go.mod local replace
# (sdk/), so go/ equals main's. The Go inputs are derived from go.mod, not a
# hand list: an sdk-only change must still be vetted, in a `replace ( ... )`
# block with a version and trailing slash, and in a quoted single-line form.
for scenario in sdkonly sdkquoted; do
	fixture="$(build_merge_fixture "case-l-${scenario}" "${scenario}")"
	status="$(run_merge_fixture "${fixture}")"
	[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case L (${scenario}): expected exit 0, got ${status}"; }
	assert_vetted_merged_tree "${fixture}" "case L (${scenario})"
done

# The pid lock is the symlink <git-dir>/eshu-pre-push-merge.lock -> <holder pid>.
merge_lock_path() { printf '%s/.git/eshu-pre-push-merge.lock' "$1"; }

# seed_merge_lock <fixture> <pid> leaves a lock held by <pid>.
seed_merge_lock() {
	mkdir -p "$1/.git/eshu-pre-push-merge"
	ln -s "$2" "$(merge_lock_path "$1")"
}

# ── Case M: two pre-push runs in one worktree share one merged-tree directory.
# A live holder fails the second run closed with a clear message and vets
# nothing; a dead holder's lock is taken over and released afterwards.
sleep 60 &
live_pid=$!
fixture="$(build_merge_fixture case-m-live clean)"
seed_merge_lock "${fixture}" "${live_pid}"
status="$(run_merge_fixture "${fixture}")"
kill "${live_pid}" 2>/dev/null || true
wait "${live_pid}" 2>/dev/null || true
[[ "${status}" != "0" ]] || { cat "${fixture}.log" >&2; fail "case M: a live lock holder must fail the run closed, got exit 0"; }
rg -q -- "another pre-push \\(pid ${live_pid}\\) is using" "${fixture}.log" || { cat "${fixture}.log" >&2; fail "case M: the failure must name the live holder"; }
[[ -z "$(merged_go_calls "${fixture}")" ]] || fail "case M: nothing may be vetted while another run holds the tree"

( : ) &
dead_pid=$!
wait "${dead_pid}"
fixture="$(build_merge_fixture case-m-dead clean)"
seed_merge_lock "${fixture}" "${dead_pid}"
status="$(run_merge_fixture "${fixture}")"
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case M: a dead holder's lock must be taken over, got exit ${status}"; }
assert_vetted_merged_tree "${fixture}" "case M (dead holder)"
[[ ! -e "$(merge_lock_path "${fixture}")" && ! -L "$(merge_lock_path "${fixture}")" ]] || fail "case M: the lock must be released after the run"

# ── Case N: ESHU_PRE_PUSH_BASE replaces origin/main as the merge base. main
# renamed p.Old, so against origin/main this head fails (case F); against the
# baseline commit (an ancestor of the head) the merge is HEAD itself.
fixture="$(build_merge_fixture case-n renamed)"
alt_base="$(git -C "${fixture}" rev-parse HEAD~1)"
status="$(ESHU_PRE_PUSH_BASE="${alt_base}" run_merge_fixture "${fixture}")"
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case N: ESHU_PRE_PUSH_BASE must replace origin/main as the merge base, got exit ${status}"; }
rg -q -- "already contains ${alt_base}" "${fixture}.log" || { cat "${fixture}.log" >&2; fail "case N: the merge must be computed against the override base"; }

# ── Case O: a SIGKILLed run leaves the merged tree's index.lock behind; the
# next run must clear it instead of failing every run until removed by hand.
fixture="$(build_merge_fixture case-o clean)"
mkdir -p "${fixture}/.git/eshu-pre-push-merge"
: > "${fixture}/.git/eshu-pre-push-merge/index.lock"
status="$(run_merge_fixture "${fixture}")"
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case O: a stale index.lock must not wedge the merge step, got exit ${status}"; }
assert_vetted_merged_tree "${fixture}" "case O"

# ── Case P: the race step prints how many packages it will race, and warns
# once that count passes ESHU_PRE_PUSH_RACE_WARN_PACKAGES (default 20).
fixture="$(build_merge_fixture case-p clean)"
status="$(run_merge_fixture "${fixture}")"
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case P: expected exit 0, got ${status}"; }
! rg -q -- 'exceeds ESHU_PRE_PUSH_RACE_WARN_PACKAGES' "${fixture}.log" || fail "case P: one package must not warn at the default threshold"
fixture="$(build_merge_fixture case-p-warn clean)"
status="$(ESHU_PRE_PUSH_RACE_WARN_PACKAGES=0 run_merge_fixture "${fixture}")"
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case P: a warning must not fail the run, got ${status}"; }
rg -q -- 'race: 1 changed package\(s\) exceeds ESHU_PRE_PUSH_RACE_WARN_PACKAGES=0' "${fixture}.log" || { cat "${fixture}.log" >&2; fail "case P: the race step must warn past the threshold"; }

# ── Case Q: takeover is atomic. Two runs see the same dead holder; one of them
# takes the lock, and the other must not delete the fresh lock it now finds.
# `kill` is the last call between reading the holder and taking the lock, so a
# stand-in there replaces the dead holder's lock with a live run's, the way a
# faster racer would.
lock_dir="$(mktemp -d "${temp_root}/case-q.XXXXXX")"
sleep 60 &
live_pid=$!
( : ) &
dead_pid=$!
wait "${dead_pid}"
ln -s "${dead_pid}" "${lock_dir}/x.lock"
status=0
bash -c '
	source "$1"
	lock="$2"; racer="$3"
	kill() { rm -f "${lock}"; ln -s "${racer}" "${lock}"; return 1; }
	pre_push_merge_lock "${lock}"
' _ "${repo_root}/scripts/lib/pre-push-merge.sh" "${lock_dir}/x.lock" "${live_pid}" 2>"${lock_dir}/err" || status=$?
[[ "${status}" != "0" ]] || { kill "${live_pid}" 2>/dev/null; fail "case Q: a run that lost the takeover race must fail closed"; }
[[ "$(readlink "${lock_dir}/x.lock")" == "${live_pid}" ]] || { kill "${live_pid}" 2>/dev/null; fail "case Q: the winner's lock must survive the losing run"; }
rg -q -- "another pre-push \\(pid ${live_pid}\\) took" "${lock_dir}/err" || { kill "${live_pid}" 2>/dev/null; fail "case Q: the loser must name the run that took the lock"; }
kill "${live_pid}" 2>/dev/null || true
wait "${live_pid}" 2>/dev/null || true

# ── Case R: a lock directory left by an earlier revision (a pid file inside)
# is honoured when its holder is live and cleared when it is dead, never
# treated as acquired by linking inside it.
mkdir "${lock_dir}/legacy.lock"
printf '%s\n' "${dead_pid}" > "${lock_dir}/legacy.lock/pid"
bash -c 'source "$1"; pre_push_merge_lock "$2"' _ "${repo_root}/scripts/lib/pre-push-merge.sh" "${lock_dir}/legacy.lock" 2>/dev/null || \
	fail "case R: a dead legacy directory lock must be cleared"
[[ -L "${lock_dir}/legacy.lock" ]] || fail "case R: the lock must now be a pid link"
