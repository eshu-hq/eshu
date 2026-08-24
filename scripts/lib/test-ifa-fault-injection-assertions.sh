#!/usr/bin/env bash
# shellcheck disable=SC2154  # Sourced test helper reads parent-owned paths.
# File-scoped assertion helpers for scripts/test-verify-ifa-fault-injection.sh.
# The parent verifier owns strict mode, fail(), and all target path variables.

# require pins a needle ANYWHERE in the gate, comments included. The call sites
# that still use it all deliberately bind FRAMING -- overview text, rationale,
# and the inventory comments that enumerate which cells a proof covers. Those
# exist only as prose, so routing them through require_code could never pass.
#
# Neither that number nor the require_code one is spelled here. The require_code
# count drifted twice (45 vs 44) because a prose count in a comment has no gate,
# which is the same trap three public docs hit with "thirty cells" -- and the
# count on this side drifted the same way, sitting at "TEN" through several
# rounds while the real figure was eight (#6161). The split was established
# empirically, not by reading labels: every call site was converted, the mirror
# run, and only the ones that genuinely could not pass were moved back.
require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || fail "missing ${label}: ${needle}"
}
# require_fixture asserts a needle that lives in the shared family-fixtures lib
# the gate sources, not in the gate script: the committed cassette and
# expected-set paths plus their fail-fast existence guards. Kept separate from
# require() so moving anything ELSE out of the gate script still fails.
# require_line pins a WHOLE line, so a needle that also appears inside a comment
# cannot satisfy it. Strict mode needs this: the gate script names
# `set -euo pipefail` in its bash>=4.4 header comment as well as running it, so
# the fixed-strings form still passed with the real line deleted.
# require_code pins a needle that must be LIVE CODE in the gate, not prose about
# it. Use it for anything asserting the gate DOES something; keep require for the
# pins that deliberately bind framing. The determinism mirror carries the same
# split, established the same way: convert everything, move back only what
# genuinely cannot pass.
require_code() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${script}")" -ge 1 ]] \
		|| fail "missing ${label}, or it survives only inside a comment: ${needle}"
}
require_line() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_lines_exact "${needle}" "${script}")" -ge 1 ]] \
		|| fail "missing ${label}, or it survives only inside a comment or heredoc: ${needle}"
}
require_fixture() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${fixtures_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (fixtures lib): ${needle}, or it survives only inside a comment"
}
# require_catalog pins a needle that lives in the numbered cell catalog,
# docs/internal/ifa-fault-cell-catalog.md, rather than in the gate script. The
# catalog was a comment block inside verify-ifa-fault-injection.sh until that
# script hit its 500-line cap; require() is deliberately strict about the gate
# script so that moving anything else out still fails, and this is the narrow,
# named exception for the one block that did move -- the same shape as
# require_fixture above. It is a plain fixed-string match because the catalog is
# entirely prose: _ifa_count_code_matches skips comment lines and would find
# nothing there.
require_catalog() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${cell_catalog_doc}" \
		|| fail "missing ${label} (cell catalog): ${needle}"
}
require_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${fault_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (lib): ${needle}, or it survives only inside a comment"
}
# require_framing pins a needle that binds non-executable TEXT in a named file:
# rationale, overview and inventory prose, and also DATA the script emits -- the
# JSON kind strings written through a heredoc are pinned here for that reason,
# not because they are documentation. Its counterparts (require_lib,
# require_cells, ...) all bind CODE through the code-portion matcher, so a
# prose needle can never satisfy them. The target file is passed explicitly so
# the exception is visible at the call site rather than hidden in a helper name.
#
# Every use is a deliberate statement that the thing being pinned is
# documentation, not behaviour. If a pin here could name live code instead, it
# should -- prose pins survive a commented-out implementation by design.
# require_shard_lib binds live code in the shard lib (flag parsing, partitioning).
require_shard_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${shard_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (shard lib), or it survives only inside a comment: ${needle}"
}
require_framing() {
	local label="$1" needle="$2" file="$3"
	rg --fixed-strings --quiet -- "${needle}" "${file}" \
		|| fail "missing ${label} (framing, ${file##*/}): ${needle}"
}
require_driver() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${driver_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (driver lib): ${needle}, or it survives only inside a comment"
}
require_cells() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${cells_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (cells lib): ${needle}, or it survives only inside a comment"
}
require_sql_cells() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${sql_cells_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (sql cells lib): ${needle}, or it survives only inside a comment"
}
require_delivery_cells() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${delivery_cells_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (delivery cells lib): ${needle}, or it survives only inside a comment"
}
require_code_call_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${code_call_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (code-call lib): ${needle}, or it survives only inside a comment"
}
require_code_call_cells() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${code_call_cells_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (code-call cells lib): ${needle}, or it survives only inside a comment"
}
# _ifa_count_code_matches counts lines of ${2} where ${1} appears in the CODE
# portion -- the part before any `#`. Lines whose first non-whitespace character
# is `#` are skipped outright.
#
# Both halves are load-bearing, and each was defeated in review before it was
# added. Skipping only leading-`#` lines left `true  # was: <needle>` counting as
# a live call, so commenting out every call site that way passed with ZERO real
# invocations. Truncating at the first `#` closes that, and also stops a needle
# quoted inside a trailing comment, a here-doc line, or a disabled continuation
# from standing in for the code it describes.
#
# Direction of the residual imprecision is deliberate: a `#` inside a quoted
# string truncates the code portion early, so a needle after it reads as absent
# and the pin REDS. That is a false alarm a human resolves in seconds. The
# earlier form of this comment claimed that direction was "never a false pass",
# which was wrong while the cut was whitespace-only -- `:;#<needle>` passed. It
# holds for the metacharacter set below; it is a property of THAT set, not a
# guarantee, so widen the set rather than restate the claim if another
# comment-introducing context turns up. Counting lines rather than matches is the same trade -- two needles
# on one line read 1, which can only over-report absence.
#
# require_line above pins whole lines for the same reason, and its docstring is
# this lesson from an earlier incident. It was in this file when the weaker form
# was written here; read the siblings before adding a pin helper.
# assert_no_private_data scans the gate plus EVERY *_lib it declares for
# hostnames, IPs, cloud account IDs, keys and internal paths.
#
# The file list is DERIVED from the declarations, not hand-typed. The hand-typed
# version listed 36 of 42 *_lib vars; the six it missed included this file, and
# it silently lost the library the 500-line split created until a reviewer
# caught it. A hand-typed list does not grow when the tree does.
#
# Lives here rather than in the mirror because that file sits against the
# 500-line cap and this is a whole coherent unit -- the same reason, and the
# same shape, as the other extractions this branch made.
# assert_libs_parse runs `bash -n` over every declared *_lib and floors the
# count. Lives here rather than in the mirror for the same reason the
# private-data scan does: that file sits against the 500-line cap, and a scan
# plus its floor is one coherent unit.
# assert_pin_helpers_bind_code is a META-GATE: it asserts that every pin helper
# in either mirror routes through a code-portion matcher, unless it is on the
# hand-written prose allowlist below.
#
# It exists because the alternative -- converting helpers from a hand-enumerated
# list -- missed pins in two consecutive review rounds, and nothing in
# the tree noticed either time. A list that must be kept complete by hand is the
# same defect class these mirrors exist to catch, one level up. This gate makes
# a forgotten helper a RED instead of a finding.
#
# PROSE_PIN_HELPERS is deliberately hand-written and deliberately short. Adding a
# name here is a claim that the helper binds documentation, not behaviour, and
# every current entry was verified by measuring code-portion matches = 0 for all
# of its needles. If you add one, do that measurement first -- a pin aimed at the
# wrong FILE also measures zero, and that is how --no-compose came to be
# misfiled as prose while its parser went unpinned.
# _ifa_pin_probe_run executes one pin helper against one synthetic target file.
# Every *_lib-shaped variable (and ${script}) is repointed at that file inside a
# subshell, so whichever target the helper happens to read, it reads the probe.
# fail() is redefined to exit rather than abort the whole mirror.
_ifa_pin_probe_run() {
	local fn="$1" target="$2"
	shift 2
	(
		fail() { exit 7; }
		local probe_var=""
		while IFS= read -r probe_var; do
			printf -v "${probe_var}" '%s' "${target}"
		done < <(compgen -v | rg '_lib$|^lib$|^script$')
		"${fn}" "pin-probe" "$@"
	) >/dev/null 2>&1
}
# Helpers that deliberately bind PROSE, and are therefore exempt from the
# behavioural probe below. Every entry is a claim that the pin's subject is
# documentation, not behaviour. Verify by measuring code-portion matches = 0 for
# all of its needles BEFORE adding one -- a pin aimed at the wrong FILE also
# measures zero, which is how --no-compose came to be filed as prose while its
# parser went unpinned.
#
# require_delivery_cells_multiline is here for a different reason: it pins a
# whole multi-line invocation with `rg -U`, braces included, so commenting any
# inner line breaks the match. It is comment-immune by construction, but it
# cannot pass a single-line probe.
# require_catalog binds prose in docs/internal/ifa-fault-cell-catalog.md, the
# numbered cell list that moved out of the gate script when it hit its 500-line
# cap (#6212). Both of its needles were measured against the precondition above
# before it was added here: each matches the catalog exactly once, each matches
# the gate script zero times, and the catalog has zero code-portion lines because
# it is entirely markdown. The wrong-FILE trap the comment warns about is what
# the first of those three measurements rules out -- a pin aimed at nothing also
# reports zero code matches.
IFA_PIN_PROSE_HELPERS="require require_framing require_delivery_cells_multiline require_catalog"
assert_pin_helpers_bind_code() {
	# BEHAVIOURAL, not textual. Earlier versions of this gate discovered helpers
	# with a regex and judged them by substring-matching their bodies. Both halves
	# were defeated in review: `require_x ()`, `function require_x`, a leading tab
	# and a digit in the name all escaped discovery, and a body that merely
	# MENTIONED the matcher in a comment -- or called it and discarded the result --
	# passed the body check. That is the same "a comment satisfies a pin" defect
	# this whole mechanism exists to end, rebuilt one level up inside the gate
	# meant to end it.
	#
	# So nothing here reads code. Bash enumerates its own functions, and each
	# helper is EXECUTED against a synthetic target where its needle appears only
	# in a comment, then only inside a heredoc. A helper that passes either probe
	# is not binding code, whatever it is implemented with. Spelling, formatting,
	# comments about the matcher, and ignoring the matcher's return value all stop
	# mattering, because none of them survives being run.
	local probe_dir needle fn rc checked=0
	needle='__ifa_pin_probe_needle__'
	probe_dir="$(mktemp -d -t ifa-pin-probe.XXXXXX)"
	printf '#!/usr/bin/env bash\n# %s\n:\n' "${needle}" >"${probe_dir}/comment_only.sh"
	printf '#!/usr/bin/env bash\n: <<%sIFAEOF%s\n%s\nIFAEOF\n:\n' "'" "'" "${needle}" >"${probe_dir}/heredoc_only.sh"; printf '#!/usr/bin/env bash\n: <<%sIFAEOF%s >/dev/null\n%s\nIFAEOF\n:\n' "'" "'" "${needle}" >"${probe_dir}/heredoc_redirect_only.sh"  # trailing-redirection heredoc; packed for the 500-line cap
	printf '#!/usr/bin/env bash\n%s\n' "${needle}" >"${probe_dir}/real_code.sh"

	while IFS= read -r fn; do
		case " ${IFA_PIN_PROSE_HELPERS} " in *" ${fn} "*) continue ;; esac
		checked=$((checked + 1))
		# Arity is discovered by trial, not declared: require_generic_cells_count
		# takes an expected COUNT as a third argument, and a 2-arg probe would fail
		# on real code and be misread as "binding". Probe with the shape that
		# actually passes on live code, then use that same shape for the negatives.
		local -a extra=()
		if ! _ifa_pin_probe_run "${fn}" "${probe_dir}/real_code.sh" "${needle}"; then
			extra=(1)
			_ifa_pin_probe_run "${fn}" "${probe_dir}/real_code.sh" "${needle}" "${extra[@]}" \
				|| fail "${fn}() rejected a needle that IS live code under both call shapes -- the probe cannot distinguish binding from broken, so its comment result proves nothing"
		fi
		local probe
		for probe in comment_only heredoc_only heredoc_redirect_only; do
			# Every *_lib-shaped variable is repointed at the probe file, so whichever
			# target this helper happens to read, it reads the probe.
			rc=0
			_ifa_pin_probe_run "${fn}" "${probe_dir}/${probe}.sh" "${needle}" "${extra[@]}" || rc=$?
			[[ "${rc}" -ne 0 ]] \
				|| fail "${fn}() accepted a needle that appears only in a ${probe//_/ } -- it is not binding code, so a commented-out or dead call site would satisfy every pin that uses it"
		done
	done < <(compgen -A function | rg '^require' | sort)

	rm -rf "${probe_dir}"
	[[ "${checked}" -ge 20 ]] \
		|| fail "pin-helper behaviour check exercised only ${checked} helper(s); discovery has collapsed and this gate is checking nothing"
	printf 'pin-helper behaviour check: %s helper(s) executed against comment, heredoc and trailing-redirection heredoc probes\n' "${checked}"
}
assert_libs_parse() {
	local lib_var lib_path syntax_checked
	syntax_checked=0
	for lib_var in $(compgen -v | rg '_lib$' | sort); do
		lib_path="${!lib_var}"
		syntax_checked=$((syntax_checked + 1))
		# A missing file FAILS rather than being skipped. The earlier `|| continue`
		# carried a justification -- "a few *_lib names hold fragments or
		# directories rather than scripts" -- that is not true of this file: every
		# *_lib variable resolves to an existing script. What the skip actually did
		# was hide a deletion: ${fixtures_lib} is the one *_lib not also named in
		# the existence loop above, so renaming scripts/lib/ifa_family_fixtures.sh
		# would have left this mirror green, where before the loop existed
		# `bash -n "${fixtures_lib}"` failed loudly on it.
		[[ -f "${lib_path}" ]] \
			|| fail "${lib_var} points at ${lib_path}, which does not exist -- a renamed or deleted lib must fail here, not be skipped"
		bash -n "${lib_path}" || fail "${lib_path##*/} has a syntax error"
		# The 500-line cap used to be asserted on only the mirror itself and the
		# gate script under test -- four files total across both live-gate
		# mirrors. Every scripts/lib/test-ifa-*-cases.sh case module went
		# unchecked, which is exactly how two of them crossed the cap silently in
		# one review round. Folded into this SAME derived loop rather than a
		# second hand-typed list, so a new case module is covered the day its
		# *_lib variable is declared, not the day someone remembers to add it here.
		[[ "$(wc -l <"${lib_path}" | tr -d '[:space:]')" -lt 500 ]] \
			|| fail "${lib_path##*/} must stay under 500 lines"
	done
	# Floor: this derivation can resolve to NOTHING (an empty `for` word list is not
	# an error), so one pattern edit would silently skip every lib, where the
	# explicit `bash -n` lines it replaced could only ever lose one at a time.
	# Hand-written, below the current count, never derived from the expression
	# it guards. Re-derived after binding the six previously-literal-path case
	# modules (repo-dependency, workload-dependency, submodule-pin, codeowners,
	# marker, plus the already-bound repo-dependency-lease sibling) to *_lib
	# vars: `compgen -v | rg '_lib$'` against the mirror now resolves 50 names
	# (verified by sourcing the mirror's own var-declaration lines in a
	# subshell and counting the result), up from the ~37 this floor of 35 was
	# originally set against.
	[[ "${syntax_checked}" -ge 45 ]] \
		|| fail "syntax check covered only ${syntax_checked} lib(s); the *_lib derivation has collapsed and nothing is being parsed"
}
assert_no_private_data() {
	local private_pattern f v rc
	local -a targets
	# The pattern and its positive control live in ifa_private_data_pattern.sh,
	# which asserts one planted token per alternative still matches before it
	# hands the pattern over. It was a literal here and an identical literal in
	# the determinism scan module, bound by nothing: replacing it with anything
	# unmatchable left this gate at exit 0, still printing its file count
	# (#6161). It ASSIGNS rather than prints, because a `fail` inside a command
	# substitution exits only the subshell and the caller would scan on regardless.
	ifa_private_data_pattern private_pattern

	# Built as an ARRAY, not an unquoted word list: a repo path containing a
	# space or a glob character would otherwise split or expand and mis-scan.
	targets=("$@")
	while IFS= read -r v; do
		targets+=("${!v}")
	done < <(compgen -v | rg '_lib$' | sort)
	# The registry, its row files and its hand-derived pins are the files this
	# branch adds most of, and NONE of them is bound to a *_lib variable, so the
	# derivation above cannot see them. Globbed explicitly rather than named, so
	# a seventh row file is covered the day it lands.
	for f in "${repo_root}"/scripts/lib/ifa_family_registry.sh \
		"${repo_root}"/scripts/lib/ifa_family_registry/rows/*.sh \
		"${repo_root}"/scripts/lib/ifa_family_registry_pins/*.sh; do
		[[ -e "${f}" ]] && targets+=("${f}")
	done

	# A FLOOR on what was actually scanned. Without it the derivation is bound by
	# nothing: changing `rg '_lib$'` to a pattern that matches no variable silently
	# reduced the scan from 44 files to 1 and the mirror still passed, because an
	# empty command substitution in a word list is not an error. The number is
	# hand-written and deliberately below the current count -- it is a floor
	# against collapse, not a pin on the exact set, so adding a lib does not
	# require editing it. It must never be derived from the same expression it
	# guards.
	[[ "${#targets[@]}" -ge 40 ]] \
		|| fail "assert_no_private_data scanned only ${#targets[@]} file(s); the *_lib derivation has collapsed and the scan is no longer covering the tree"

	for f in "${targets[@]}"; do
		[[ -f "${f}" ]] || fail "assert_no_private_data: ${f} does not exist -- a scan that skips a missing file proves nothing"
		# rc is CAPTURED, not tested through `if`: rg exits 2 on a pattern it
		# cannot compile, `if` reads that as "no match", and `set -e` does not
		# apply inside an `if` condition, so one uncompilable pattern made every
		# file read as clean and this gate passed (#6161).
		rc=0
		rg --pcre2 --quiet -- "${private_pattern}" "${f}" || rc=$?
		[[ "${rc}" -ne 0 ]] || fail "$(basename "${f}") looks like it contains private data"
		[[ "${rc}" -eq 1 ]] \
			|| fail "the private-data scan could not run over $(basename "${f}") (rg exit ${rc}); a scanner that cannot run must never read as clean"
	done
	printf 'private-data scan: %s file(s) scanned\n' "${#targets[@]}"
}
# _ifa_count_code_lines_exact is the whole-line form, sharing the same comment
# and heredoc skipping. require_line used `rg --line-regexp`, which is immune to
# comments but NOT to heredocs -- a heredoc line equal to the needle satisfies a
# whole-line regex, and the behavioural probe caught exactly that. Routing it
# here keeps one model of "what counts as code" instead of two.
_ifa_count_code_lines_exact() {
	local needle="$1" file="$2" n=0 line stripped heredoc=""
	while IFS= read -r line || [[ -n "${line}" ]]; do
		stripped="${line#"${line%%[![:space:]]*}"}"
		if [[ -n "${heredoc}" ]]; then
			[[ "${stripped}" == "${heredoc}" ]] && heredoc=""
			continue
		fi
		if [[ "${line}" =~ \<\<-?[[:space:]]*[\'\"]?([A-Za-z_][A-Za-z0-9_]*)[\'\"]?[[:space:]]*([0-9]*[\<\>\|\;\&\)].*)?$ ]]; then
			heredoc="${BASH_REMATCH[1]}"
		fi
		[[ "${stripped}" == "#"* ]] && continue
		[[ "${stripped}" == "${needle}" ]] && n=$((n + 1))
	done < "${file}"
	printf '%s\n' "${n}"
}
_ifa_count_code_matches() {
	local needle="$1" file="$2" n=0 line stripped code heredoc=""
	while IFS= read -r line || [[ -n "${line}" ]]; do
		stripped="${line#"${line%%[![:space:]]*}"}"
		# Inside a heredoc nothing is code -- a needle there is data, and a real
		# call moved into a dead heredoc was proven to satisfy every pin while
		# never executing. Delimiter tracking only, no attempt to model quoting.
		if [[ -n "${heredoc}" ]]; then
			[[ "${stripped}" == "${heredoc}" ]] && heredoc=""
			continue
		fi
		if [[ "${line}" =~ \<\<-?[[:space:]]*[\'\"]?([A-Za-z_][A-Za-z0-9_]*)[\'\"]?[[:space:]]*([0-9]*[\<\>\|\;\&\)].*)?$ ]]; then
			heredoc="${BASH_REMATCH[1]}"
		fi
		[[ "${stripped}" == "#"* ]] && continue
		# Truncate at a `#` that STARTS A WORD (preceded by whitespace), which is
		# what shell treats as a comment. A blanket `%%#*` also cut at `${#arr[@]}`
		# and `${var#prefix}`, making any line using those unpinnable -- it silently
		# broke the floor pin added this round, which is exactly the kind of guard
		# that must stay pinnable.
		# Bash starts a comment at `#` after ANY unquoted metacharacter, not only
		# whitespace: `;` `|` `&` `(` `)` `<` `>` all do it. Cutting only at
		# whitespace-then-`#` let `:;#trap ifa_det_cleanup EXIT` read as live code,
		# which reproduced on HEAD the exact defect the previous round closed --
		# the shell checker does not flag it, `bash -n` passes, and no gate runs it
		# on these scripts, so nothing else would have caught it.
		code="${line%%[[:space:]\;\|\&\(\)\<\>\`]#*}"
		[[ "${code}" == *"${needle}"* ]] && n=$((n + 1))
	done < "${file}"
	printf '%s\n' "${n}"
}
require_generic_cells() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${generic_cells_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (generic cells lib), or it survives only inside a comment: ${needle}"
}
# require_generic_cells_count pins a needle that must appear an EXACT number of
# times, not merely at least once. `rg --quiet` proves >=1 and stops looking, so
# a needle occurring at N distinct call sites is satisfied by any one of them
# surviving -- deleting the other N-1 leaves the mirror green. That is not
# hypothetical: the assert-edges invocation appears at BOTH the kill-worker body
# and the fail-graph-write cell, and deleting only the fail-graph-write one left
# this mirror at exit 0 while cell_failgraphwrite_code_calls and
# cell_failgraphwrite_rationale silently stopped asserting their family's exact
# edge set. The expected count is hand-written by the caller and deliberately NOT
# derived from the file under test -- a count read out of the artifact it checks
# proves only that the artifact equals itself.
# require_documentation_barrier_count is the exact-count form of
# require_documentation_barrier. The ACK-barrier lock-identity predicates
# ("NOT barrier.granted", the two-key objsubid, the per-database bindings) each
# appear in THREE OR FOUR sibling pg_locks queries, and an -ge 1 pin is satisfied
# by any one survivor: dropping "NOT barrier.granted" from a single query widened
# it from ungranted-waiters to all locks and the mirror stayed green (#6161).
# Each of those predicates narrows the join to the exact lock under test, so
# losing one from one query is a correctness break in a concurrency proof.
# The *_count wrappers route through one inner so adding another costs a line,
# not a block -- this file is near the 500-line cap. Each exists because its
# needle has several code occurrences that ALL do work, which `-ge 1` cannot
# express: it is satisfied by whichever survives (#6161). `_ifa_require_count_in`
# is deliberately not named require*, so the probe exercises the real wrappers.
_ifa_require_count_in() {
	local label="$1" needle="$2" want="$3" file="$4" got
	got="$(_ifa_count_code_matches "${needle}" "${file}")"
	[[ "${got}" == "${want}" ]] \
		|| fail "${label}: expected ${want} code occurrence(s) in ${file##*/}, found ${got} -- each one does work, so a -ge 1 pin stays green when one of them is deleted: ${needle}"
}
require_cells_count() { _ifa_require_count_in "$1" "$2" "$3" "${cells_lib}"; }
require_delivery_cells_count() { _ifa_require_count_in "$1" "$2" "$3" "${delivery_cells_lib}"; }
require_documentation_barrier_setup_count() { _ifa_require_count_in "$1" "$2" "$3" "${documentation_barrier_setup_lib}"; }
require_documentation_barrier_count() {
	local label="$1" needle="$2" want="$3" got
	got="$(_ifa_count_code_matches "${needle}" "${documentation_barrier_lib}")"
	[[ "${got}" == "${want}" ]] \
		|| fail "${label}: expected this predicate in ${want} barrier quer(y/ies) in ${documentation_barrier_lib##*/}, found ${got} -- a predicate dropped from one query silently widens it: ${needle}"
}
require_generic_cells_count() {
	local label="$1" needle="$2" want="$3" got
	got="$(_ifa_count_code_matches "${needle}" "${generic_cells_lib}")"
	[[ "${got}" == "${want}" ]] \
		|| fail "${label}: expected ${want} call site(s) of this invocation in ${generic_cells_lib##*/}, found ${got} -- a deleted call site is a proof that silently stopped running: ${needle}"
}
# require_generic_baseline pins the contents of the generic BASELINE cell, which
# lives in its own file since the 500-line split. Without this, that file is
# pinned by nothing at all: require_generic_cells reads only generic_cells_lib,
# so the split moved the baseline cell's assertions somewhere no pin could see
# them. Proven at the time: deleting the baseline cell's own _ifa_generic_assert_edges
# call left this mirror at exit 0, silently permitting the very thing that cell's
# comment says it exists to prevent -- a fixture that materializes nothing
# establishing an empty baseline every recovery cell then matches.
require_generic_baseline() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${generic_baseline_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (generic baseline cell), or it survives only inside a comment: ${needle}"
}
require_documentation_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${documentation_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (documentation lib): ${needle}, or it survives only inside a comment"
}
require_documentation_cells() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${documentation_cells_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (documentation cells lib): ${needle}, or it survives only inside a comment"
}
require_documentation_barrier() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${documentation_barrier_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (documentation ACK barrier lib): ${needle}, or it survives only inside a comment"
}
require_documentation_barrier_setup() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${documentation_barrier_setup_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (documentation ACK barrier setup lib): ${needle}, or it survives only inside a comment"
}

# Deleting only the function name from a continued call leaves its arguments
# behind, so multiline call assertions use -U and bind the whole invocation.
require_delivery_cells_multiline() {
	local label="$1" needle="$2"
	rg -U --fixed-strings --quiet -- "${needle}" "${delivery_cells_lib}" || fail "missing ${label} (delivery cells lib, multiline): ${needle}"
}
