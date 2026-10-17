#!/usr/bin/env bash
# gosec arms for scripts/dev/precommit-go.sh (sourced, never executed directly).
#
# Three subcommands share one fail-closed runner, run_gosec:
#   gosec <files...>   changed Go files from the pre-push hook (changed packages)
#   gosec-all          the whole module (./...), the same scope as security-scan.yml
#   gosec-changed      derives its own package list from the merge-base diff and
#                      is what the registry row `gosec-changed` runs in
#                      `make pre-push`, `make pre-pr` and `make security-preflight`
#
# FAIL CLOSED (#8335). These arms used to send gosec's output to /dev/null, run
# with -no-fail and read the SARIF with `jq ... || echo 0`, so gosec exiting 0
# with no file, an empty file or non-JSON counted as zero findings. The pinned
# gosec also exits 0 for a package with a type error, writing no results and
# logging "skipping SSA analysis". run_gosec reports "I did not scan" as a
# failure on every one of those paths; scripts/test-precommit-go-gosec.sh pins
# each of them.
#
# The caller (precommit-go.sh) provides: repo_root, go_dir, worktree_cache_dir,
# ensure_gosec, collect_dirs (fills the global `dirs`), note and die.

# gosec_log is read by gosec_fail to print the tail of the scan log.
gosec_log=""

# gosec_fail prints a message plus the log tail and exits with the given code.
gosec_fail() {
	local code="$1" msg="$2"
	printf '%s\n' "${msg}" >&2
	if [[ -n "${gosec_log}" && -s "${gosec_log}" ]]; then
		printf 'last lines of %s:\n' "${gosec_log}" >&2
		tail -n 20 "${gosec_log}" >&2
	fi
	exit "${code}"
}

# run_gosec <label> <package...> runs the pinned gosec from go/ over the given
# package patterns and exits non-zero unless it produced a complete, parsable,
# non-empty, error-free result with no findings.
run_gosec() {
	local label="$1" bin out status=0 n_pkgs files n_err n_find n_dirs
	shift
	n_pkgs=$#
	command -v jq >/dev/null 2>&1 || die "gosec: jq is required to read gosec results"
	bin="$(ensure_gosec)"
	out="${worktree_cache_dir}/gosec-${label}.json"
	gosec_log="${worktree_cache_dir}/gosec-${label}.log"
	rm -f "${out}" "${gosec_log}"
	# -fmt=json carries `Golang errors` and `Stats`; SARIF carries neither.
	( cd "${go_dir}" && "${bin}" -severity=low -confidence=low -no-fail \
		-fmt=json -out "${out}" "$@" >"${gosec_log}" 2>&1 ) || status=$?
	if [[ "${status}" -eq 143 ]]; then
		gosec_fail 143 "gosec exited 143 (SIGTERM) before writing results; rerun make pre-push"
	elif [[ "${status}" -ne 0 ]]; then
		gosec_fail "${status}" "gosec exited ${status} before writing results"
	fi
	if [[ ! -s "${out}" ]] || ! jq -e '.Issues' "${out}" >/dev/null 2>&1; then
		gosec_fail 1 "gosec wrote no parsable result (${out})"
	fi
	files="$(jq -r '.Stats.files // 0' "${out}")"
	if [[ "${files}" -le 0 ]]; then
		gosec_fail 1 "gosec scanned 0 files for ${n_pkgs} package pattern(s); refusing to report clean"
	fi
	n_err="$(jq -r '(."Golang errors" // {}) | length' "${out}")"
	if [[ "${n_err}" -ne 0 ]]; then
		jq -r '."Golang errors" | to_entries[] | .key as $f | .value[] | "  \($f):\(.line):\(.column) \(.error)"' "${out}" >&2
		gosec_fail 1 "gosec reported Go errors, so the packages were not fully analyzed"
	fi
	n_find="$(jq -r '.Issues | length' "${out}")"
	if [[ "${n_find}" -ne 0 ]]; then
		jq -r --arg p "${go_dir}/" '.Issues[] | "  \(.rule_id) \(.file | ltrimstr($p)):\(.line)"' "${out}" >&2
		n_dirs="$(jq -r --arg p "${go_dir}/" '[.Issues[].file | ltrimstr($p) | sub("/[^/]*$"; "")] | unique | length' "${out}")"
		gosec_fail 1 "gosec: ${n_find} finding(s) in ${n_dirs} package(s) — fix or annotate with a leading // #nosec <RULE> -- <reason>"
	fi
	rm -f "${gosec_log}"
	printf 'gosec (%s): %s file(s) scanned, no findings\n' "${label}" "${files}"
}

# gosec_keep_existing_dirs drops entries of the global `dirs` whose directory no
# longer exists (a commit that deletes a whole package leaves nothing to scan).
gosec_keep_existing_dirs() {
	local kept=() d
	for d in ${dirs[@]+"${dirs[@]}"}; do
		[[ -d "${go_dir}/${d#./}" ]] && kept+=("${d}")
	done
	dirs=(${kept[@]+"${kept[@]}"})
}

# gosec_scan_dirs runs run_gosec over the global `dirs`, or reports nothing to
# scan. Zero packages is the only case where gosec is deliberately not invoked.
gosec_scan_dirs() {
	local label="$1" empty_msg="$2" pkgs=() d
	if [[ ${#dirs[@]} -eq 0 ]]; then
		printf '%s\n' "${empty_msg}"
		return 0
	fi
	for d in "${dirs[@]}"; do pkgs+=("${d}/..."); done
	run_gosec "${label}" "${pkgs[@]}"
}

# gosec_arm_files implements `gosec <files...>`: the pre-push hook passes the
# changed Go files and gosec scans their packages.
gosec_arm_files() {
	collect_dirs "$@"
	gosec_keep_existing_dirs
	gosec_scan_dirs gosec "gosec: no changed Go packages; nothing to scan"
}

# gosec_arm_changed implements `gosec-changed`. It diffs against the merge-base
# with ESHU_PRE_PUSH_BASE (default origin/main), adds staged and unstaged work,
# and falls back to the whole module when a file that decides how gosec runs
# changed. An unresolvable base or failed git command exits 2, never a quiet
# narrowing to HEAD~1 (the precedent is scripts/dev/pre-push.sh).
gosec_arm_changed() {
	local base="${ESHU_PRE_PUSH_BASE:-origin/main}" mb committed staged unstaged p gofiles=()
	if ! git -C "${repo_root}" rev-parse --verify "${base}^{commit}" >/dev/null 2>&1 \
		|| ! mb="$(git -C "${repo_root}" merge-base "${base}" HEAD 2>/dev/null)"; then
		printf 'gosec-changed: base %s does not resolve; set ESHU_PRE_PUSH_BASE or fetch origin main\n' "${base}" >&2
		exit 2
	fi
	committed="$(git -C "${repo_root}" diff --name-only --diff-filter=ACMR "${mb}" HEAD)" \
		|| { printf 'gosec-changed: git diff against %s failed\n' "${base}" >&2; exit 2; }
	staged="$(git -C "${repo_root}" diff --name-only --cached)" \
		|| { printf 'gosec-changed: git diff --cached failed\n' >&2; exit 2; }
	unstaged="$(git -C "${repo_root}" diff --name-only)" \
		|| { printf 'gosec-changed: git diff failed\n' >&2; exit 2; }
	while IFS= read -r p; do
		[[ -n "${p}" ]] || continue
		case "${p}" in
			go/go.mod | go/go.sum | scripts/dev/precommit-go.sh | scripts/lib/precommit-go-gosec.sh \
				| scripts/ci/go-install-retry.sh | scripts/ci/go-mod-download-retry.sh \
				| .github/workflows/security-scan.yml)
				printf 'gosec-changed: whole-module scan, %s changed\n' "${p}"
				run_gosec gosec-changed ./...
				return 0
				;;
			go/*.go) gofiles+=("${p}") ;;
		esac
	done < <(printf '%s\n%s\n%s\n' "${committed}" "${staged}" "${unstaged}")
	collect_dirs ${gofiles[@]+"${gofiles[@]}"}
	gosec_keep_existing_dirs
	gosec_scan_dirs gosec-changed "gosec-changed: no changed Go packages vs ${base}; nothing to scan"
}
