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

# The partial-analysis ledger (scripts/lib/gosec-partial-packages.tsv, #8339)
# records packages the pinned gosec cannot fully type-check: it still parses
# every file and runs the AST rules, but skips the SSA analyzers. Go errors in a
# ledgered package are disclosed, not failed; Go errors anywhere else fail, and a
# ledger row that no longer errors fails as stale so the ledger only shrinks.
# ESHU_GOSEC_PARTIAL_LEDGER overrides the path for scripts/test-precommit-go-gosec.sh.
gosec_ledger_file() {
	printf '%s' "${ESHU_GOSEC_PARTIAL_LEDGER:-${repo_root}/scripts/lib/gosec-partial-packages.tsv}"
}

# gosec_ledger_rows prints "<dir>\t<issue>" per ledger row and "BAD\t<line>" for
# a malformed one (not 4 tab fields, a dir that is empty, absolute, under go/ or
# containing .., or an issue that is not #<number>). A missing ledger is empty.
gosec_ledger_rows() {
	local file
	file="$(gosec_ledger_file)"
	[[ -f "${file}" ]] || return 0
	awk -F'\t' '
		/^[[:space:]]*#/ || /^[[:space:]]*$/ { next }
		NF != 4 || $1 == "" || $1 ~ /^\// || $1 ~ /^go\// || $1 ~ /\.\./ || $2 !~ /^#[0-9]+$/ || $3 == "" || $4 == "" {
			print "BAD\t" NR; next
		}
		{ print $1 "\t" $2 }' "${file}"
}

# gosec_error_dirs prints one package dir (relative to go/) per Go error in the
# gosec JSON. A file key maps to its dir; the "" key carries compiler output, so
# each `<dir>/<file>.go:<line>:<col>:` line maps to its dir. A "" entry with no
# such line is "(unattributed)" and so never matches a ledger row.
gosec_error_dirs() {
	jq -r --arg p "${go_dir}/" '
		(."Golang errors" // {}) | to_entries[] |
		if .key == "" then
			([.value[].error | split("\n")[] | capture("^(?<f>[^ :]+\\.go):[0-9]+:[0-9]+:")? | .f | sub("/[^/]*$"; "")]) as $ds
			| (if ($ds | length) == 0 then ["(unattributed)"] else $ds end)[]
		else
			.value[] as $e | (.key | ltrimstr($p) | sub("/[^/]*$"; ""))
		end' "$1"
}

# gosec_in_scope <dir> <pattern...> reports whether a requested ./... or
# ./<prefix>/... pattern covers the package dir.
gosec_in_scope() {
	local dir="$1" pat prefix
	shift
	for pat in "$@"; do
		pat="${pat#./}"
		case "${pat}" in
			...) return 0 ;;
			*/...)
				prefix="${pat%/...}"
				[[ "${dir}" == "${prefix}" || "${dir}" == "${prefix}/"* ]] && return 0
				;;
		esac
	done
	return 1
}

# gosec_ledger_check <label> <result.json> <pattern...> applies the ledger to the
# result's Go errors. It exits 1 on a malformed ledger row, on Go errors outside
# the ledger (naming those dirs first) and on a stale in-scope ledger row.
gosec_ledger_check() {
	local label="$1" out="$2" rows bad counted cnt dir issue missing="" known="" r
	shift 2
	rows="$(gosec_ledger_rows)"
	bad="$(printf '%s\n' "${rows}" | awk -F'\t' '$1 == "BAD" { print $2; exit }')"
	if [[ -n "${bad}" ]]; then
		gosec_fail 1 "gosec: malformed ledger row at line ${bad} of $(gosec_ledger_file)"
	fi
	counted="$(gosec_error_dirs "${out}" | sort | uniq -c)"
	while read -r cnt dir; do
		[[ -n "${dir}" ]] || continue
		issue="$(printf '%s\n' "${rows}" | awk -F'\t' -v d="${dir}" '$1 == d { print $2; exit }')"
		if [[ -z "${issue}" ]]; then
			missing="${missing} ${dir}"
		else
			known="${known}${label}|${issue}|${dir}|${cnt}"$'\n'
		fi
	done < <(printf '%s\n' "${counted}")
	if [[ -n "${missing}" ]]; then
		printf 'gosec: Go errors in package(s) not on the partial-analysis ledger:%s\n' "${missing}" >&2
		jq -r '."Golang errors" | to_entries[] | .key as $f | .value[] | "  \($f):\(.line):\(.column) \(.error)"' "${out}" >&2
		gosec_fail 1 "gosec reported Go errors, so the packages were not fully analyzed"
	fi
	while IFS='|' read -r label issue dir cnt; do
		[[ -n "${dir}" ]] || continue
		printf 'gosec (%s): partial analysis (known, %s): %s -- %s Go errors, SSA analyzers skipped; see scripts/lib/gosec-partial-packages.tsv\n' \
			"${label}" "${issue}" "${dir}" "${cnt}" >&2
	done < <(printf '%s' "${known}")
	while IFS=$'\t' read -r r issue; do
		[[ -n "${r}" ]] || continue
		if gosec_in_scope "${r}" "$@" && ! printf '%s\n' "${counted}" | awk -v d="${r}" '$2 == d { f = 1 } END { exit !f }'; then
			gosec_fail 1 "gosec: ledger row ${r} is stale (no Go errors this run); remove it from scripts/lib/gosec-partial-packages.tsv"
		fi
	done < <(printf '%s\n' "${rows}")
}

# run_gosec <label> <package...> runs the pinned gosec from go/ over the given
# package patterns and exits non-zero unless it produced a complete, parsable,
# non-empty, error-free result with no findings.
run_gosec() {
	local label="$1" bin out status=0 n_pkgs files n_find n_dirs
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
	gosec_ledger_check "${label}" "${out}" "$@"
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
