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

# write_merge_fake_go writes the compile-modelling `go` stand-in to <bin>/go.
write_merge_fake_go() {
	local bin="$1"
	cat > "${bin}/go" <<'FAKEGO'
#!/usr/bin/env bash
printf 'go %s cwd=%s\n' "$*" "${PWD}" >> "${DRIVER_ARGS_LOG}"
case "${1:-}" in
	build | vet) ;;
	*) exit 0 ;;
esac
if rg -q --glob '*.go' 'p\.Old\(' . && ! rg -q --glob '*.go' '^func Old\(' .; then
	printf 'internal/r/call.go:5:12: undefined: p.Old\n' >&2
	exit 1
fi
exit 0
FAKEGO
	chmod +x "${bin}/go"
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
	printf 'package p\n\nfunc Old() {}\n' > "${fixture}/go/internal/p/def.go"
	git -C "${fixture}" init -q -b feature
	merge_fixture_commit "${fixture}" baseline
	baseline="$(git -C "${fixture}" rev-parse HEAD)"

	# main's side, recorded as origin/main, then back to the baseline.
	case "${scenario}" in
		conflict) printf 'package p\n\nfunc Old() { _ = 2 }\n' > "${fixture}/go/internal/p/def.go" ;;
		renamed) printf 'package p\n\nfunc New() {}\n' > "${fixture}/go/internal/p/def.go" ;;
		clean | contained | docsonly)
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
