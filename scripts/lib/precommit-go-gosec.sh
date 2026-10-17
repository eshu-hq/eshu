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
# FAIL CLOSED (#8335). These arms used to send gosec's output to /dev/null, run
# with -no-fail and read the SARIF with `jq ... || echo 0`, so gosec exiting 0
# with no file, an empty file or non-JSON counted as zero findings. The pinned
# gosec also exits 0 for a package with a type error, writing no results and
# logging "skipping SSA analysis". run_gosec reports "I did not scan" as a
# failure on every one of those paths; scripts/test-precommit-go-gosec.sh pins
# each of them.
#
# EXPLICIT PACKAGE DIRS AND THE ALIAS (#8339). go/packages splits an oversized
# file-pattern list and loses imports, which made gosec silently skip the SSA
# analyzers on go/internal/storage/postgres. run_gosec therefore expands every
# pattern with `go list`, passes each package as an explicit ./<dir>/ (never
# ./...), and routes a package over gosec_pattern_limit bytes through a short
# symlink alias go/_g<pid> that it removes on exit. It checks that gosec scanned
# exactly the GoFiles and CgoFiles `go list` reports (tests are excluded by
# design, as gosec does by default), so a short or empty scan cannot read clean.
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
		tail -n 20 "${gosec_log}" | gosec_unalias >&2
	fi
	exit "${code}"
}

# x/tools go/packages (defaultDriver, safeArgMax) splits a pattern list into
# `go list` runs of at most this many bytes, each pattern counting len+1. gosec
# passes one pattern per file, so a package whose file patterns exceed it is
# loaded in several runs that share the ID command-line-arguments, and the merge
# keeps only the first run's imports (#8339): gosec then type-checks the package
# with missing imports and skips its SSA analyzers (G115 G118 G405 G406 G407
# G602). Verified by A-control in scripts/test-precommit-go-gosec.sh against
# x/tools v0.45.0; re-verify on an x/tools bump.
gosec_pattern_limit=16383
gosec_aliases=()
gosec_alias_dirs=()
gosec_alias_made=""

# gosec_cleanup removes the alias symlinks this process created (EXIT trap).
gosec_cleanup() {
	local a
	for a in ${gosec_aliases[@]+"${gosec_aliases[@]}"}; do
		[[ -L "${go_dir}/${a}" ]] && rm -f "${go_dir}/${a}"
	done
	return 0
}

# gosec_new_alias <real-dir> creates go/_g<pid> (or a lettered variant) as a
# relative symlink to the package dir and sets gosec_alias_made to its name (no
# command substitution: it must update gosec_aliases in the calling shell). The underscore keeps
# it out of ./..., go vet and golangci-lint; the name is per process, and ln -s
# fails on an existing name, so a concurrent run never shares or deletes ours.
gosec_new_alias() {
	local dir="$1" base="_g$(($$ % 10000))" cand letters=bcdefghijklmnopqrstuvwxyz n=0
	cand="${base}"
	until ln -s "${dir}" "${go_dir}/${cand}" 2>/dev/null; do
		n=$((n + 1))
		[[ ${n} -le ${#letters} ]] || gosec_fail 1 "gosec: could not create an alias under ${go_dir}"
		cand="${base}${letters:$((n - 1)):1}"
	done
	gosec_aliases+=("${cand}")
	gosec_alias_dirs+=("${dir}")
	gosec_alias_made="${cand}"
}

# gosec_unalias maps alias dirs in stdin back to the real package dirs.
gosec_unalias() {
	local args=() i=0
	while [[ ${i} -lt ${#gosec_aliases[@]} ]]; do
		args+=(-e "s#${gosec_aliases[${i}]}/#${gosec_alias_dirs[${i}]}/#g")
		i=$((i + 1))
	done
	if [[ ${#args[@]} -eq 0 ]]; then cat; else sed "${args[@]}"; fi
}

# run_gosec <label> <pattern...> runs the pinned gosec from go/ and exits
# non-zero unless it produced a complete, parsable, error-free result that
# scanned exactly the files `go list` reports, with no findings. The repo's gosec
# runs with tests excluded (gosec's default), so the expected files are the
# package GoFiles and CgoFiles, never _test.go files.
#
# Patterns are expanded with `go list` and every package is passed to gosec as an
# explicit ./<dir>/, never ./...: that keeps gosec's file set equal to go list's
# (gosec's own ./... walk also enters testdata dirs, which Go ignores) and lets
# the loop below route an oversized package through a short alias.
run_gosec() {
	local label="$1" bin out status=0 files expected=0 n_pkgs=0 n_err n_find n_dirs p
	local listing line dir files_str rel nf bytes abytes f alias pat final=() whole=0
	shift
	for p in "$@"; do
		case "${p}" in
			/*) gosec_fail 1 "gosec: absolute pattern ${p} refused; patterns are relative to go/" ;;
			./...) whole=1 ;;
		esac
	done
	command -v jq >/dev/null 2>&1 || die "gosec: jq is required to read gosec results"
	[[ -f "${go_dir}/go.mod" ]] || gosec_fail 1 "gosec: must run from the module root go/ (gosec resolves patterns from the CWD but lists from the module root)"
	listing="$(cd "${go_dir}" && go list -e -find -f '{{.Dir}}|{{join .GoFiles " "}} {{join .CgoFiles " "}}' "$@")" \
		|| gosec_fail 1 "gosec: go list failed for: $*"
	trap gosec_cleanup EXIT
	while IFS='|' read -r dir files_str; do
		[[ -n "${dir}" ]] || continue
		local farr=(${files_str})
		nf=${#farr[@]}
		[[ ${nf} -gt 0 ]] || continue
		rel="${dir#"${go_dir}"/}"
		[[ "${dir}" == "${go_dir}" ]] && rel="."
		expected=$((expected + nf))
		n_pkgs=$((n_pkgs + 1))
		bytes=0
		abytes=0
		alias=""
		for f in "${farr[@]}"; do
			if [[ "${rel}" == . ]]; then bytes=$((bytes + ${#f} + 1)); else bytes=$((bytes + ${#rel} + ${#f} + 2)); fi
		done
		pat="./${rel}/"
		[[ "${rel}" == . ]] && pat="."
		if [[ ${bytes} -gt ${gosec_pattern_limit} ]]; then
			gosec_new_alias "${rel}"
			alias="${gosec_alias_made}"
			for f in "${farr[@]}"; do abytes=$((abytes + ${#alias} + ${#f} + 2)); done
			if [[ ${abytes} -gt ${gosec_pattern_limit} ]]; then
				gosec_fail 1 "gosec: ${rel} exceeds the go/packages pattern limit even via alias (${abytes} > ${gosec_pattern_limit}); split the package (#8339)"
			fi
			printf 'gosec: %s scanned via alias %s (pattern bytes %s > %s; alias headroom %s bytes; #8339)\n' \
				"${rel}" "${alias}" "${bytes}" "${gosec_pattern_limit}" "$((gosec_pattern_limit - abytes))"
			pat="./${alias}/"
		fi
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
	rm -f "${out}" "${gosec_log}"
	# -fmt=json carries `Golang errors` and `Stats`; SARIF carries neither.
	( cd "${go_dir}" || exit 97
	  [[ -f go.mod && "$(pwd -P)" == "$(cd "${go_dir}" && pwd -P)" ]] || exit 97
	  "${bin}" -severity=low -confidence=low -no-fail -fmt=json -out "${out}" "${final[@]}" >"${gosec_log}" 2>&1 ) || status=$?
	if [[ "${status}" -eq 97 ]]; then
		gosec_fail 1 "gosec: must run from the module root go/ (gosec resolves patterns from the CWD but lists from the module root)"
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
		jq -r '."Golang errors" | to_entries[] | .key as $f | .value[] | "  \($f):\(.line):\(.column) \(.error)"' "${out}" | gosec_unalias >&2
		gosec_fail 1 "gosec reported Go errors, so the packages were not fully analyzed"
	fi
	n_find="$(jq -r '.Issues | length' "${out}")"
	if [[ "${n_find}" -ne 0 ]]; then
		jq -r --arg p "${go_dir}/" '.Issues[] | "  \(.rule_id) \(.file | ltrimstr($p)):\(.line)"' "${out}" | gosec_unalias >&2
		n_dirs="$(jq -r --arg p "${go_dir}/" '[.Issues[].file | ltrimstr($p) | sub("/[^/]*$"; "")] | unique | length' "${out}")"
		gosec_fail 1 "gosec: ${n_find} finding(s) in ${n_dirs} package(s) — fix or annotate with a leading // #nosec <RULE> -- <reason>"
	fi
	rm -f "${gosec_log}"
	printf 'gosec (%s): %s file(s) scanned in %s package(s), no findings\n' "${label}" "${files}" "${n_pkgs}"
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
