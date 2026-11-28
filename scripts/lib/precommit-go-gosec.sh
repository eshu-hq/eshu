#!/usr/bin/env bash
# gosec arms for scripts/dev/precommit-go.sh (sourced, never executed directly).
#
# Three subcommands share one fail-closed runner, run_gosec:
#   gosec <files...>   changed Go files from the pre-push hook (changed packages)
#   gosec-all          the whole module, the same packages as security-scan.yml
#   gosec-changed      derives its own package list from the merge-base diff and
#                      is what the registry row `gosec-changed` runs in
#                      `make pre-push`, `make pre-pr` and `make security-preflight`
#
# SCOPE: PRODUCTION CODE ONLY (#8341). Every arm and the CI gosec job run gosec
# without -tests, so `_test.go` files are excluded by design: the gate and the
# Security tab never see them. A hidden finding class in test code is the price
# of that choice, so #8341 triaged the sixteen G115/G602 findings that a
# tests-on scan found in go/internal/storage/postgres. To scan tests too, run
# golangci-lint with only gosec enabled and run.tests on (see
# docs/public/reference/local-testing/pre-commit-hooks.md).
#
# FAIL CLOSED (#8335). These arms used to send gosec's output to /dev/null, run
# with -no-fail and read the SARIF with `jq ... || echo 0`, so gosec exiting 0
# with no file, an empty file or non-JSON counted as zero findings. The pinned
# gosec also exits 0 for a package with a type error, writing no results and
# logging "skipping SSA analysis". run_gosec reports "I did not scan" as a
# failure on every one of those paths; scripts/test-precommit-go-gosec.sh pins
# each of them.
#
# EXPLICIT PACKAGE DIRS, LOADED BY DIRECTORY (#8339, #11241). gosec v2.27.1
# passed one go/packages pattern per file, and x/tools splits a pattern list at
# 16,383 bytes into several `go list` runs that share one package ID and keep only
# the first run's imports: a package over that budget (go/internal/storage/postgres
# at 473 files) was type-checked with missing imports and its SSA analyzers
# (G115 G118 G405 G406 G407 G602) were skipped. The pinned gosec, a pseudo-version
# that carries securego/gosec#1803 and #1806, loads module packages by directory,
# so there is no per-file pattern budget and no alias. run_gosec expands every
# pattern with `go list` and passes each package as an explicit ./<dir>/ (never
# ./...). It checks that gosec scanned exactly the GoFiles and CgoFiles `go list`
# reports (tests are excluded by design, as gosec does by default), so a short or
# empty scan cannot read clean. Move to the first gosec release that contains
# both commits when one ships (#11243).
#
# The caller (precommit-go.sh) provides: repo_root, go_dir, worktree_cache_dir,
# ensure_gosec, collect_dirs (fills the global `dirs`), ensure_nested_roots
# (fills the global nested_roots), note and die.

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

# run_gosec <label> <pattern...> runs the pinned gosec from the module root and
# exits non-zero unless it produced a complete, parsable, error-free result that
# scanned exactly the files `go list` reports, with no findings. The repo's gosec
# runs with tests excluded (gosec's default), so the expected files are the
# package GoFiles and CgoFiles, never _test.go files.
#
# Patterns are expanded with `go list` and every package is passed to gosec as an
# explicit ./<dir>/, never ./...: that keeps gosec's file set equal to go list's
# (gosec's own ./... walk also enters testdata dirs, which Go ignores).
#
# The module root is ${gosec_mod_dir:-${go_dir}}: the main go/ module by
# default, or one nested module root (see run_gosec_all) when the caller scans
# packages the parent module cannot resolve.
run_gosec() {
	local label="$1" bin out status=0 files expected=0 n_pkgs=0 n_err n_find n_dirs p
	local listing dir files_str rel nf pat final=() whole=0
	local mod_dir="${gosec_mod_dir:-${go_dir}}"
	shift
	for p in "$@"; do
		case "${p}" in
			/*) gosec_fail 1 "gosec: absolute pattern ${p} refused; patterns are relative to the module root" ;;
			./...) whole=1 ;;
		esac
	done
	command -v jq >/dev/null 2>&1 || die "gosec: jq is required to read gosec results"
	[[ -f "${mod_dir}/go.mod" ]] || gosec_fail 1 "gosec: must run from the module root ${mod_dir} (gosec resolves patterns from the CWD but lists from the module root)"
	listing="$(cd "${mod_dir}" && go list -e -find -f '{{.Dir}}|{{join .GoFiles " "}} {{join .CgoFiles " "}}' "$@")" \
		|| gosec_fail 1 "gosec: go list failed for: $*"
	while IFS='|' read -r dir files_str; do
		[[ -n "${dir}" ]] || continue
		local farr=(${files_str})
		nf=${#farr[@]}
		[[ ${nf} -gt 0 ]] || continue
		rel="${dir#"${mod_dir}"/}"
		[[ "${dir}" == "${mod_dir}" ]] && rel="."
		expected=$((expected + nf))
		n_pkgs=$((n_pkgs + 1))
		pat="./${rel}/"
		[[ "${rel}" == . ]] && pat="."
		final+=("${pat}")
	done < <(printf '%s\n' "${listing}")
	if [[ ${n_pkgs} -eq 0 ]]; then
		[[ ${whole} -eq 0 ]] || gosec_fail 1 "gosec: go list ./... found no packages with Go files"
		printf 'gosec (%s): no Go packages in %s; nothing to scan\n' "${label}" "$*"
		return 0
	fi
	bin="$(ensure_gosec)"
	out="${worktree_cache_dir}/gosec-${label}.json"
	gosec_log="${worktree_cache_dir}/gosec-${label}.log"
	local sarif_out=""
	if [[ -n "${GOSEC_SARIF_OUT:-}" ]]; then
		sarif_out="${GOSEC_SARIF_OUT}"
		case "${sarif_out}" in /*) ;; *) sarif_out="${repo_root}/${sarif_out}" ;; esac
	fi
	rm -f "${out}" "${gosec_log}" ${sarif_out:+"${sarif_out}"}
	# -fmt=json carries `Golang errors` and `Stats`; SARIF carries neither.
	( cd "${mod_dir}" || exit 97
	  [[ -f go.mod && "$(pwd -P)" == "$(cd "${mod_dir}" && pwd -P)" ]] || exit 97
	  "${bin}" -severity=low -confidence=low -no-fail -fmt=json -out "${out}" "${final[@]}" >"${gosec_log}" 2>&1 ) || status=$?
	if [[ "${status}" -eq 97 ]]; then
		gosec_fail 1 "gosec: must run from the module root ${mod_dir} (gosec resolves patterns from the CWD but lists from the module root)"
	elif [[ "${status}" -eq 143 ]]; then
		gosec_fail 143 "gosec exited 143 (SIGTERM) before writing results; rerun make pre-push"
	elif [[ "${status}" -ne 0 ]]; then
		gosec_fail "${status}" "gosec exited ${status} before writing results"
	fi
	if [[ ! -s "${out}" ]] || ! jq -e '.Issues' "${out}" >/dev/null 2>&1; then
		gosec_fail 1 "gosec wrote no parsable result (${out})"
	fi
	files="$(jq -r '.Stats.files // 0' "${out}")"
	if [[ "${files}" -ne "${expected}" ]]; then
		gosec_fail 1 "gosec scanned ${files} of ${expected} expected files for ${n_pkgs} package(s); refusing to report clean"
	fi
	n_err="$(jq -r '(."Golang errors" // {}) | length' "${out}")"
	if [[ "${n_err}" -ne 0 ]]; then
		jq -r '."Golang errors" | to_entries[] | .key as $f | .value[] | "  \($f):\(.line):\(.column) \(.error)"' "${out}" >&2
		gosec_fail 1 "gosec reported Go errors, so the packages were not fully analyzed"
	fi
	if [[ -n "${sarif_out}" ]]; then
		python3 "${script_root:-${repo_root}}/scripts/lib/gosec-json-to-sarif.py" \
			--input "${out}" --output "${sarif_out}" --base-dir "${repo_root}" \
			|| gosec_fail 1 "gosec: failed to write SARIF report to ${sarif_out}"
	fi
	n_find="$(jq -r '.Issues | length' "${out}")"
	if [[ "${n_find}" -ne 0 ]]; then
		jq -r --arg p "${go_dir}/" '.Issues[] | "  \(.rule_id) \(.file | ltrimstr($p)):\(.line)"' "${out}" >&2
		n_dirs="$(jq -r --arg p "${go_dir}/" '[.Issues[].file | ltrimstr($p) | sub("/[^/]*$"; "")] | unique | length' "${out}")"
		gosec_fail 1 "gosec: ${n_find} finding(s) in ${n_dirs} package(s) — fix or annotate with a leading // #nosec <RULE> -- <reason>"
	fi
	rm -f "${gosec_log}"
	printf 'gosec (%s): %s file(s) scanned in %s package(s), no findings\n' "${label}" "${files}" "${n_pkgs}"
}

# run_gosec_all <label> runs run_gosec over the main go/ module (./...) and then
# over every nested module root (./... from inside that root). A nested run uses
# a "<label>-<basename>" label so its JSON/log files do not clobber the
# main-module result; any module's failure fails the arm.
run_gosec_all() {
	local label="$1" nroot base
	run_gosec "${label}" ./...
	for nroot in ${nested_roots[@]+"${nested_roots[@]}"}; do
		base="${nroot##*/}"
		gosec_mod_dir="${repo_root}/${nroot}"
		run_gosec "${label}-${base}" ./...
		unset gosec_mod_dir
	done
}

# is_nested_modfile succeeds when the repo-relative path is a go.mod or go.sum
# directly inside a nested module root: such a change alters that module's
# build list, so gosec-changed widens to the whole-module scan.
is_nested_modfile() {
	local p="$1" r
	ensure_nested_roots
	for r in ${nested_roots[@]+"${nested_roots[@]}"}; do
		if [[ "${p}" == "${r}/go.mod" || "${p}" == "${r}/go.sum" ]]; then
			return 0
		fi
	done
	return 1
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
# Nested-module packages run from inside their own module root with a
# "<label>-<basename>" label: the parent module's `go list` resolves them to
# zero files, which used to read as a clean scan.
gosec_scan_dirs() {
	local label="$1" empty_msg="$2" spec mod pat base
	local main_pkgs=() d
	if [[ ${#dirs[@]} -eq 0 ]]; then
		printf '%s\n' "${empty_msg}"
		return 0
	fi
	partition_dirs
	for d in ${main_dirs[@]+"${main_dirs[@]}"}; do main_pkgs+=("${d}/..."); done
	if [[ ${#main_pkgs[@]} -gt 0 ]]; then
		run_gosec "${label}" "${main_pkgs[@]}"
	fi
	for spec in ${nested_runs[@]+"${nested_runs[@]}"}; do
		mod="${spec%%|*}"
		pat="${spec#*|}"
		if [[ "${pat}" != "." ]]; then
			pat="${pat}/..."
		fi
		base="${mod##*/}"
		gosec_mod_dir="${repo_root}/${mod}"
		run_gosec "${label}-${base}" "${pat}"
		unset gosec_mod_dir
	done
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
		if is_nested_modfile "${p}"; then
			printf 'gosec-changed: whole-module scan, %s changed\n' "${p}"
			run_gosec_all gosec-changed
			return 0
		fi
		case "${p}" in
			go/go.mod | go/go.sum | scripts/dev/precommit-go.sh | scripts/lib/precommit-go-gosec.sh \
				| scripts/lib/gosec-json-to-sarif.py | scripts/ci/go-install-retry.sh \
				| scripts/ci/go-mod-download-retry.sh | .github/workflows/security-scan.yml)
				printf 'gosec-changed: whole-module scan, %s changed\n' "${p}"
				run_gosec_all gosec-changed
				return 0
				;;
			go/*.go) gofiles+=("${p}") ;;
		esac
	done < <(printf '%s\n%s\n%s\n' "${committed}" "${staged}" "${unstaged}")
	collect_dirs ${gofiles[@]+"${gofiles[@]}"}
	gosec_keep_existing_dirs
	gosec_scan_dirs gosec-changed "gosec-changed: no changed Go packages vs ${base}; nothing to scan"
}
