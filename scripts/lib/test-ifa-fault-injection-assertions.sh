#!/usr/bin/env bash
# shellcheck disable=SC2154  # Sourced test helper reads parent-owned paths.
# File-scoped assertion helpers for scripts/test-verify-ifa-fault-injection.sh.
# The parent verifier owns strict mode, fail(), and all target path variables.

# require pins a needle ANYWHERE in the gate, comments included. TEN call sites
# still use it, and every one deliberately binds FRAMING -- overview text,
# rationale, and the inventory comments that enumerate which cells a proof
# covers. Those exist only as prose, so routing them through require_code could
# never pass.
#
# The rest bind code and use require_code. The exact number is deliberately
# not spelled here: it drifted twice (45 vs 44) because a prose count in a
# comment has no gate, which is the same trap three public docs hit with
# "twenty-one cells". The split was established
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
	rg --line-regexp --quiet -- "${needle}" "${script}" || fail "missing ${label}: ${needle}"
}
require_fixture() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${fixtures_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (fixtures lib): ${needle}, or it survives only inside a comment"
}
require_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${fault_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (lib): ${needle}, or it survives only inside a comment"
}
# require_framing pins a needle that binds FRAMING -- rationale, overview or
# inventory prose -- in a named file. Its counterparts (require_lib,
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
# list -- missed 266 pins in one review round and 52 in the next, and nothing in
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
assert_pin_helpers_bind_code() {
	local f name body allow found=0
	local -a prose=(require require_framing)
	# Structurally comment-immune, but not via the matcher or --line-regexp:
	# require_delivery_cells_multiline pins a WHOLE multi-line invocation with
	# `rg -U`, braces included, so commenting any inner line breaks the match.
	# Listed by name with that reason rather than loosening the rule for every
	# `-U` pin, since -U alone proves nothing about comments.
	local -a structural=(require_delivery_cells_multiline)
	for f in "${repo_root}"/scripts/test-verify-ifa-*.sh "${repo_root}"/scripts/lib/test-ifa-*.sh; do
		[[ -f "${f}" ]] || continue
		while IFS= read -r name; do
			found=$((found + 1))
			allow=0
			for p in "${prose[@]}" "${structural[@]}"; do
				[[ "${name}" == "${p}" ]] && allow=1
			done
			[[ "${allow}" -eq 1 ]] && continue
			body="$(rg -U --only-matching "(?s)^${name}\\(\\) \\{.*?^\\}" "${f}" || true)"
			[[ -n "${body}" ]] \
				|| fail "assert_pin_helpers_bind_code: could not read the body of ${name}() in ${f##*/}"
			# --line-regexp is comment-immune BY CONSTRUCTION: it pins a whole
			# line, and a commented-out line is not that line. It is the stronger
			# form, so it counts as binding code without going through the matcher.
			[[ "${body}" == *_count_code_matches* || "${body}" == *--line-regexp* ]] \
				|| fail "${name}() in ${f##*/} pins with a plain rg and is neither line-anchored nor on the prose allowlist -- a comment quoting its needle would satisfy it, so a commented-out call site would pass"
		done < <(rg --only-matching --replace '$1' -- '^(require[a-z_]*)\(\) \{' "${f}" || true)
	done
	[[ "${found}" -ge 25 ]] \
		|| fail "assert_pin_helpers_bind_code found only ${found} pin helper(s); the discovery glob has collapsed and this gate is checking nothing"
	printf 'pin-helper bind check: %s helper(s)\n' "${found}"
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
	done
	# Floor: this derivation can resolve to NOTHING (an empty `for` word list is not
	# an error), so one pattern edit would silently skip every lib where the 32
	# explicit `bash -n` lines it replaced each cost only one. Hand-written, below
	# the current count, never derived from the expression it guards.
	[[ "${syntax_checked}" -ge 35 ]] \
		|| fail "syntax check covered only ${syntax_checked} lib(s); the *_lib derivation has collapsed and nothing is being parsed"
}
assert_no_private_data() {
	local private_pattern f v
	local -a targets
	# Every alternative below brackets one character, so the pattern does not
	# contain its own literals. A bracketed single-character class matches exactly
	# the same text as the bare character, so detection is unchanged.
	#
	# This is required now that the scan covers every declared *_lib, including
	# this file. Spelling any of these tokens plainly in this function -- even in
	# prose explaining them -- makes the scanner flag itself on every run. That
	# happened twice while writing this comment, which is why it now describes the
	# tokens rather than quoting them.
	#
	# The hazard was latent before, not absent: the literal used to live in the
	# mirror, whose ${script} points at the GATE, so the mirror never scanned
	# itself and never noticed.
	private_pattern='gh[p]_|github_pa[t]_|glpa[t]-|AKI[A]|ASI[A]|xo[x][baprs]-|arn:aw[s]:|(^|[^0-9])[0-9]{12}([^0-9]|$)|/[U]sers/|/[h]ome/[a-z]'

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
		if rg --pcre2 --quiet -- "${private_pattern}" "${f}"; then
			fail "$(basename "${f}") looks like it contains private data"
		fi
	done
	printf 'private-data scan: %s file(s) scanned\n' "${#targets[@]}"
}
_ifa_count_code_matches() {
	local needle="$1" file="$2" n=0 line stripped code
	while IFS= read -r line || [[ -n "${line}" ]]; do
		stripped="${line#"${line%%[![:space:]]*}"}"
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
		# shellcheck does not flag it, `bash -n` passes, and no gate runs shellcheck
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
