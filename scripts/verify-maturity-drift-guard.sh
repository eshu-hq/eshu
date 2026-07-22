#!/usr/bin/env bash
#
# verify-maturity-drift-guard.sh - fails when the "Real-Repo Validation" /
# "End-to-End Indexing" columns of docs/public/languages/support-maturity.md
# drift from the live golden-corpus gate's actual "supported" bar (#5400,
# spun off from #5336's manual hand-grading of the same matrix).
#
# #5336 defined the bar and hand-graded the matrix once. Without automation
# the grading rots the next time scripts/verify-golden-corpus-gate.sh's
# corpus_fixtures array or testdata/golden/e2e-20repo-snapshot.json (the B-12
# snapshot) changes -- exactly how the matrix drifted into over-credit the
# first time. This gate mechanically re-derives the bar and diffs it against
# the committed matrix on every run.
#
# ALGORITHM
#
# The bar (support-maturity.md's own "Grade Definitions" section): a language
# is "supported" iff its fixture is BOTH (a) staged in `corpus_fixtures` in
# scripts/verify-golden-corpus-gate.sh AND (b) asserted by at least one
# language-attributed `required_correlations`/`query_shapes` entry in the B-12
# snapshot. This script derives that intersection with no hardcoded language
# list -- every name comes from a file already checked into the repo:
#
#   1. corpus_fixtures: parsed straight out of the `corpus_fixtures=( ... )`
#      bash array in scripts/verify-golden-corpus-gate.sh.
#   2. Ledger language keys: parsed straight out of the `language:` field of
#      every entry in specs/language-feature-parity-ledger.v1.yaml (the
#      existing canonical registry the parser-relationship-kit gate already
#      treats as the source of truth for language identity).
#   3. Matrix rows: parsed straight out of the "Language Feature Parity
#      Ledger" table in docs/public/languages/support-maturity.md. A row is
#      only evaluated (see LIMITATION below) when its "Query Surfacing"
#      column is not "-" -- a "-" row has explicitly opted out of the
#      supported/fixture-backed ladder (Kubernetes, Helm, ArgoCD, ... are
#      config/framework rows graded "-" across the board, never
#      "fixture-backed"), so drift-checking it would be comparing against a
#      state the row was never meant to occupy.
#   4. Fixture -> language attribution:
#        - A "<key>_comprehensive" fixture names its language directly (the
#          suffix strip IS the ledger key: go_comprehensive -> go).
#        - Every other staged fixture (the app-shaped "real repo" fixtures:
#          lib-common, orders-api, api-svc, ...) is attributed by scanning
#          its tree for the source extensions registered in
#          scripts/lib/maturity-drift-guard-language-extensions.sh (a
#          "programming language" extension registry, not a support-status
#          list -- it never changes what counts as supported, only what
#          language a fixture's source files are written in).
#   5. Row display name -> ledger key: a fixed transliteration (lowercase,
#      strip whitespace, "+" -> "p", "#" -> "sharp") that reproduces every
#      ledger key for every row this gate evaluates (verified: c, cpp,
#      csharp, dart, elixir, go, groovy, haskell, java, javascript, kotlin,
#      perl, php, python, ruby, rust, scala, sql, swift, terraform,
#      terragrunt, typescript, typescriptjsx -- the 23 rows whose Query
#      Surfacing column is not "-"). A row whose transliteration does not
#      resolve to a real ledger key is a parse/rename bug, not a grade to
#      skip, so it fails closed rather than being silently ignored.
#   6. B-12 language-attribution test (fails closed on either signal):
#        - STRUCTURED: the B-12 snapshot's `graph.required_correlations` /
#          `query_shapes` subtree, recursively scanned, contains an
#          `arguments.language` (or equivalent) field literally equal to the
#          ledger key (e.g. "language": "go").
#        - TEXTUAL: that same subtree contains the literal name of one of
#          the SINGLE-LANGUAGE fixtures attributed to this language as a
#          fixed-string substring (e.g. "go_comprehensive", "orders-api").
#      A language is "live-supported" iff it has at least one staged fixture
#      AND clears either signal.
#
#      The TEXTUAL signal is fixture-scoped, so it is only trustworthy for a
#      fixture that attributes to exactly ONE language. A POLYGLOT fixture
#      (one whose source tree carries files for more than one ledger
#      language -- e.g. both .go and .py) attributes to every one of those
#      languages via step 4, so a bare fixture-name mention in the B-12 blob
#      cannot tell WHICH of them the evidence actually targets. Trusting the
#      TEXTUAL match there would mark every attributed language live off a
#      single mention. So the TEXTUAL signal is only consulted for a fixture
#      attributed to exactly one language; a polyglot fixture's languages
#      must each clear the language-scoped STRUCTURED signal independently.
#      (The current corpus is all single-language app fixtures plus
#      "_comprehensive" fixtures, so no language relies on this today, but
#      the guard keeps a future polyglot fixture from silently over-crediting
#      the matrix -- exactly the drift class this gate exists to catch.)
#
# A row's Real-Repo Validation and End-to-End Indexing columns are compared
# against the single derived live-supported boolean and MUST both read
# "supported" iff live-supported is true. Drift fails in EITHER direction:
#   - live-supported but a column is not "supported" (a fixture/B-12 change
#     entered the live set but the matrix was never promoted).
#   - not live-supported but a column reads "supported" (a fixture/B-12
#     change left the live set -- removed from corpus_fixtures, or its B-12
#     evidence was deleted -- but the matrix was never demoted).
# A column reading "real-repo-validated" (currently zero members repo-wide,
# per the doc) is treated as "not supported" for this comparison -- promoting
# that grade is a human dogfood-artifact decision this gate does not model,
# but demoting FROM "supported" is still checked the same way.
#
# LIMITATION: rows with Query Surfacing == "-" are never evaluated (see #3
# above) -- promoting one of those rows onto the supported/fixture-backed
# ladder for the first time is a human decision (the row must first gain a
# Query Surfacing claim), not something this gate can derive.
#
# FAILS CLOSED: a missing input file, unparseable snapshot JSON, an empty
# corpus_fixtures/ledger/matrix-row extraction, a staged fixture directory
# that does not exist on disk, or a matrix row whose name does not
# transliterate to a real ledger key all `die` rather than silently passing.
#
# Usage: bash scripts/verify-maturity-drift-guard.sh
set -euo pipefail

repo_root="${ESHU_MATURITY_DRIFT_GUARD_REPO_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
script_dir="$(cd "$(dirname "$0")" && pwd)"
gate_script="${repo_root}/scripts/verify-golden-corpus-gate.sh"
fixture_inventory="${repo_root}/scripts/lib/golden-corpus-fixtures.sh"
snapshot_path="${repo_root}/testdata/golden/e2e-20repo-snapshot.json"
ledger_path="${repo_root}/specs/language-feature-parity-ledger.v1.yaml"
matrix_path="${repo_root}/docs/public/languages/support-maturity.md"
fixtures_root="${repo_root}/tests/fixtures/ecosystems"

# The minimum number of evaluated matrix rows expected in the real repo tree
# (23 today). Guards against a regex/glob regression silently zeroing the
# table scan and turning every run into a vacuous pass. Intentionally well
# below the real count so adding/removing a handful of rows never trips it;
# it exists to catch a total parse failure, not to track the exact count.
row_floor="${ESHU_MATURITY_DRIFT_GUARD_ROW_FLOOR:-10}"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

log() {
	printf 'verify-maturity-drift-guard: %s\n' "$*" >&2
}

die() {
	log "$*"
	exit 1
}

usage() {
	printf 'usage: %s\n' "${0##*/}"
	printf '  derive the live-gate supported set and diff it against %s\n' \
		"${matrix_path#"${repo_root}"/}"
}

command -v rg >/dev/null 2>&1 || die "missing required tool: rg"
command -v jq >/dev/null 2>&1 || die "missing required tool: jq"

# shellcheck source=scripts/lib/maturity-drift-guard-language-extensions.sh
. "${script_dir}/lib/maturity-drift-guard-language-extensions.sh"

trim_cell() {
	local value="$1"
	value="${value#"${value%%[![:space:]]*}"}"
	value="${value%"${value##*[![:space:]]}"}"
	value="${value//\`/}"
	printf '%s' "${value}"
}

# transliterate_language_name maps a matrix row's display name to the ledger
# key it must resolve to (see algorithm step 5 above).
transliterate_language_name() {
	local name="$1"
	name="${name//+/p}"
	name="${name//#/sharp}"
	name="$(printf '%s' "${name}" | tr '[:upper:]' '[:lower:]')"
	name="$(printf '%s' "${name}" | tr -d '[:space:]')"
	name="$(printf '%s' "${name}" | tr -cd 'a-z0-9')"
	printf '%s' "${name}"
}

# extract_corpus_fixtures prints one fixture name per line from the
# `corpus_fixtures=( ... )` bash array in the golden gate orchestrator or its
# sourced fixture inventory. Keeping the orchestrator under the file cap must
# not make maturity grading vacuous.
extract_corpus_fixtures() {
	local sources=("${gate_script}")
	if [[ -f "${fixture_inventory}" ]]; then
		sources+=("${fixture_inventory}")
	fi
	awk '
    /^corpus_fixtures=\(/ { in_block = 1; next }
    in_block && /^\)/ { in_block = 0; next }
    in_block {
      line = $0
      sub(/#.*/, "", line)
      gsub(/^[ \t]+|[ \t]+$/, "", line)
      if (line != "") print line
    }
  ' "${sources[@]}"
}

# extract_ledger_languages prints one ledger key per line from every
# `- language: <key>` entry in specs/language-feature-parity-ledger.v1.yaml.
extract_ledger_languages() {
	rg -o '^\s*-\s*language:\s*(\S+)' -r '$1' "${ledger_path}" | LC_ALL=C sort -u
}

# extract_matrix_rows prints one "<name>\t<query_surfacing>\t<real_repo>\t<e2e>"
# line per row of the "Language Feature Parity Ledger" table (identified by
# its unique 9-column shape: NF==11 after splitting on "|"), trimmed and with
# the header/separator rows dropped.
extract_matrix_rows() {
	awk -F'|' '
    NF != 11 { next }
    {
      name = $2; qs = $8; rr = $9; e2e = $10
      gsub(/^[ \t]+|[ \t]+$/, "", name)
      gsub(/^[ \t]+|[ \t]+$/, "", qs)
      gsub(/^[ \t]+|[ \t]+$/, "", rr)
      gsub(/^[ \t]+|[ \t]+$/, "", e2e)
      gsub(/`/, "", name)
      if (name == "" || name == "Parser") next
      if (name ~ /^-+$/) next
      print name "\t" qs "\t" rr "\t" e2e
    }
  ' "${matrix_path}"
}

# fixture_language_candidates prints every ledger-key candidate a corpus
# fixture attributes to (algorithm step 4). Zero, one, or (for a
# multi-extension app-shaped fixture) more than one candidate may print;
# callers filter against the real ledger key set.
fixture_language_candidates() {
	local fixture="$1"
	case "${fixture}" in
	*_comprehensive)
		printf '%s\n' "${fixture%_comprehensive}"
		return 0
		;;
	esac
	local dir="${fixtures_root}/${fixture}"
	[[ -d "${dir}" ]] || return 0
	local entry ext key
	for entry in "${MATURITY_DRIFT_GUARD_EXT_LANG[@]}"; do
		ext="${entry%%:*}"
		key="${entry#*:}"
		if [[ -n "$(rg --files -g "*.${ext}" "${dir}" 2>/dev/null | head -1)" ]]; then
			printf '%s\n' "${key}"
		fi
	done
}

main() {
	case "${1:-}" in
	"") ;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		log "unknown argument: $1"
		usage >&2
		exit 2
		;;
	esac

	[[ -f "${gate_script}" ]] || die "golden-corpus gate script not found: ${gate_script}"
	[[ -f "${snapshot_path}" ]] || die "B-12 snapshot not found: ${snapshot_path}"
	[[ -f "${ledger_path}" ]] || die "language feature parity ledger not found: ${ledger_path}"
	[[ -f "${matrix_path}" ]] || die "support-maturity.md not found: ${matrix_path}"
	[[ -d "${fixtures_root}" ]] || die "fixtures root not found: ${fixtures_root}"

	jq empty "${snapshot_path}" 2>/dev/null || die "B-12 snapshot is not valid JSON: ${snapshot_path}"

	local fixtures_file="${tmp_dir}/fixtures.txt"
	extract_corpus_fixtures >"${fixtures_file}"
	local fixture_count
	fixture_count="$(awk 'NF' "${fixtures_file}" | wc -l | tr -d ' ')"
	[[ "${fixture_count}" -gt 0 ]] || die "parsed zero corpus_fixtures entries from ${gate_script} and sourced inventories (regex regression?)"

	local fixture
	while IFS= read -r fixture; do
		[[ -z "${fixture}" ]] && continue
		[[ -d "${fixtures_root}/${fixture}" ]] || die "staged corpus fixture not found on disk: ${fixtures_root}/${fixture}"
	done <"${fixtures_file}"

	local ledger_file="${tmp_dir}/ledger.txt"
	extract_ledger_languages >"${ledger_file}"
	local ledger_count
	ledger_count="$(awk 'NF' "${ledger_file}" | wc -l | tr -d ' ')"
	[[ "${ledger_count}" -gt 0 ]] || die "parsed zero language ledger keys from ${ledger_path} (regex regression?)"

	# pairs_file: one "<ledger_key> <fixture>" line per attribution, filtered
	# down to candidates that are real ledger keys (algorithm step 4).
	local pairs_file="${tmp_dir}/pairs.txt"
	: >"${pairs_file}"
	local cand
	while IFS= read -r fixture; do
		[[ -z "${fixture}" ]] && continue
		while IFS= read -r cand; do
			[[ -z "${cand}" ]] && continue
			if rg -qx -- "${cand}" "${ledger_file}"; then
				printf '%s %s\n' "${cand}" "${fixture}" >>"${pairs_file}"
			fi
		done < <(fixture_language_candidates "${fixture}")
	done <"${fixtures_file}"
	# Dedup so a fixture that attributes to one key via multiple extensions
	# (e.g. .cpp + .hpp both -> cpp) counts as a single "<key> <fixture>"
	# pair, keeping the polyglot key-count below accurate.
	LC_ALL=C sort -u -o "${pairs_file}" "${pairs_file}"

	# B-12 attribution signals, scoped to the graph.required_correlations +
	# query_shapes subtree (algorithm step 6).
	local blob_file="${tmp_dir}/blob.json"
	jq -c '{required_correlations: .graph.required_correlations, query_shapes: .query_shapes}' \
		"${snapshot_path}" >"${blob_file}"
	local structured_file="${tmp_dir}/structured-languages.txt"
	jq -r '[.. | objects | .language? // empty] | unique | .[]' "${blob_file}" >"${structured_file}"

	local rows_file="${tmp_dir}/rows.txt"
	extract_matrix_rows >"${rows_file}"
	local total_rows
	total_rows="$(awk 'NF' "${rows_file}" | wc -l | tr -d ' ')"
	[[ "${total_rows}" -gt 0 ]] || die "parsed zero rows from the matrix table in ${matrix_path} (regex regression?)"

	local failed=0 evaluated=0 live_supported_count=0
	local name qs rr e2e expected_key attributed_fixtures live_supported
	while IFS=$'\t' read -r name qs rr e2e; do
		[[ -z "${name}" ]] && continue
		[[ "$(trim_cell "${qs}")" == "-" ]] && continue
		evaluated=$((evaluated + 1))

		expected_key="$(transliterate_language_name "${name}")"
		if ! rg -qx -- "${expected_key}" "${ledger_file}"; then
			die "matrix row '${name}' transliterates to '${expected_key}', which is not a known language ledger key -- renamed row or ledger drift, cannot verify"
		fi

		attributed_fixtures="$(awk -v k="${expected_key}" '$1 == k { print $2 }' "${pairs_file}" | LC_ALL=C sort -u)"
		live_supported=false
		if [[ -n "${attributed_fixtures}" ]]; then
			if rg -qx -- "${expected_key}" "${structured_file}"; then
				# STRUCTURED signal: language-scoped, always precise.
				live_supported=true
			else
				# TEXTUAL signal: fixture-scoped, only trusted for a fixture
				# that attributes to exactly one language (algorithm step 6).
				local f key_count
				while IFS= read -r f; do
					[[ -z "${f}" ]] && continue
					key_count="$(awk -v ff="${f}" '$2 == ff { print $1 }' "${pairs_file}" | LC_ALL=C sort -u | wc -l | tr -d ' ')"
					if [[ "${key_count}" -ne 1 ]]; then
						continue
					fi
					if rg -q -F -- "${f}" "${blob_file}"; then
						live_supported=true
						break
					fi
				done <<<"${attributed_fixtures}"
			fi
		fi

		if [[ "${live_supported}" == "true" ]]; then
			live_supported_count=$((live_supported_count + 1))
			if [[ "${rr}" != "supported" || "${e2e}" != "supported" ]]; then
				log "DRIFT (under-graded): '${name}' is live-supported (fixture staged + B-12 attributed via key '${expected_key}') but Real-Repo Validation='${rr}' / End-to-End Indexing='${e2e}' -- promote both columns to 'supported'"
				failed=1
			fi
		else
			if [[ "${rr}" == "supported" || "${e2e}" == "supported" ]]; then
				log "DRIFT (over-graded): '${name}' is NOT live-supported (no staged fixture, or its B-12 evidence is gone) but Real-Repo Validation='${rr}' / End-to-End Indexing='${e2e}' -- demote the 'supported' column(s)"
				failed=1
			fi
		fi
	done <"${rows_file}"

	if [[ "${evaluated}" -lt "${row_floor}" ]]; then
		die "evaluated only ${evaluated} matrix row(s), below the floor of ${row_floor} (regex regression?)"
	fi

	if [[ "${failed}" -ne 0 ]]; then
		return 1
	fi

	log "OK: ${evaluated} language row(s) checked, ${live_supported_count} live-supported, 0 drift"
	return 0
}

main "$@"
