#!/usr/bin/env bash
# Static structural test for verify-ifa-determinism.sh. The matrix itself
# needs Docker + a built toolchain and takes ~30-45 minutes (three fresh
# Postgres + NornicDB stacks, sequential), so this mirror validates the
# contract that cannot silently drift: the script parses, sets strict mode,
# reuses an isolated Compose project + non-default ports distinct from every
# sibling verify-ifa-*.sh script, drives N ∈ {1, 2, 4}, tears down and
# rebuilds a FRESH stack between every cell, asserts the drive actually
# enqueued work before draining, drains via the same B-12 residual bound
# verify-ifa-replay-drive.sh proves, canonicalizes the graph at each cell,
# asserts all three digests are byte-identical, prints the full-dump diff on
# a mismatch instead of hiding it, and tears down its own stack on exit. This
# is the credential-free lane CI runs per PR; the full Docker matrix runs on
# demand/nightly, not on every PR.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/verify-ifa-determinism.sh"
lib="${repo_root}/scripts/lib/ifa_determinism_common.sh"
lifecycle_lib="${repo_root}/scripts/lib/ifa_determinism_lifecycle.sh"
delta_lib="${repo_root}/scripts/lib/ifa_sql_delta_live.sh"
code_call_lib="${repo_root}/scripts/lib/ifa_code_call_live.sh"
documentation_lib="${repo_root}/scripts/lib/ifa_documentation_live.sh"
deployable_unit_lib="${repo_root}/scripts/lib/ifa_deployable_unit_live.sh"
deployable_unit_diagnostics_lib="${repo_root}/scripts/lib/ifa_deployable_unit_live_diagnostics.sh"
deployable_unit_converge_lib="${repo_root}/scripts/lib/ifa_deployable_unit_live_converge.sh"
rationale_lib="${repo_root}/scripts/lib/ifa_rationale_live.sh"
codeowners_lib="${repo_root}/scripts/lib/ifa_codeowners_live.sh"
submodule_pin_lib="${repo_root}/scripts/lib/ifa_submodule_pin_live.sh"
fixtures_lib="${repo_root}/scripts/lib/ifa_family_fixtures.sh"
require_helpers_lib="${repo_root}/scripts/lib/test-ifa-determinism-require-helpers.sh"
family_cases_lib="${repo_root}/scripts/lib/test-ifa-determinism-family-cases.sh"
registry_lockstep_cases_lib="${repo_root}/scripts/lib/test-ifa-determinism-registry-lockstep-cases.sh"
family_registry_pins_lib="${repo_root}/scripts/lib/test-ifa-family-registry-derived-pins-cases.sh"
# registry_family_lib (ifa_family_registry.sh) is used by both
# test-ifa-determinism-family-cases.sh (the shared-cell drive/assert loop's
# totality check) and test-ifa-family-registry-derived-pins-cases.sh (its own
# internal source + totality check), so it is declared once here at
# top level rather than locally in either consumer.
registry_family_lib="${repo_root}/scripts/lib/ifa_family_registry.sh"
workflow="${repo_root}/.github/workflows/ifa-determinism-gate.yml"
registry="${repo_root}/specs/ci-gates.v1.yaml"

fail() { printf 'test-verify-ifa-determinism: %s\n' "$*" >&2; exit 1; }

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "verify-ifa-determinism.sh must be executable"
[[ -f "${lib}" ]] || fail "missing ${lib}"
[[ -f "${lifecycle_lib}" ]] || fail "missing ${lifecycle_lib}"
[[ -f "${delta_lib}" ]] || fail "missing ${delta_lib}"
[[ -f "${code_call_lib}" ]] || fail "missing ${code_call_lib}"
[[ -f "${documentation_lib}" ]] || fail "missing ${documentation_lib}"
[[ -f "${deployable_unit_lib}" ]] || fail "missing ${deployable_unit_lib}"
[[ -f "${deployable_unit_diagnostics_lib}" ]] || fail "missing ${deployable_unit_diagnostics_lib}"
[[ -f "${deployable_unit_converge_lib}" ]] || fail "missing ${deployable_unit_converge_lib}"
[[ -f "${rationale_lib}" ]] || fail "missing ${rationale_lib}"
[[ -f "${fixtures_lib}" ]] || fail "missing ${fixtures_lib}"
[[ -f "${require_helpers_lib}" ]] || fail "missing ${require_helpers_lib}"
[[ -f "${family_cases_lib}" ]] || fail "missing ${family_cases_lib}"
[[ -f "${registry_lockstep_cases_lib}" ]] || fail "missing ${registry_lockstep_cases_lib}"
[[ -f "${family_registry_pins_lib}" ]] || fail "missing ${family_registry_pins_lib}"
[[ -f "${registry_family_lib}" ]] || fail "missing ${registry_family_lib}"
[[ -f "${workflow}" ]] || fail "missing ${workflow}"
[[ -f "${registry}" ]] || fail "missing ${registry}"

# Both files parse under bash -n.
bash -n "${script}" || fail "verify-ifa-determinism.sh has a syntax error"
bash -n "${lib}" || fail "ifa_determinism_common.sh has a syntax error"
bash -n "${lifecycle_lib}" || fail "ifa_determinism_lifecycle.sh has a syntax error"
bash -n "${delta_lib}" || fail "ifa_sql_delta_live.sh has a syntax error"
bash -n "${code_call_lib}" || fail "ifa_code_call_live.sh has a syntax error"
bash -n "${documentation_lib}" || fail "ifa_documentation_live.sh has a syntax error"
bash -n "${deployable_unit_lib}" || fail "ifa_deployable_unit_live.sh has a syntax error"
bash -n "${deployable_unit_diagnostics_lib}" || fail "ifa_deployable_unit_live_diagnostics.sh has a syntax error"
bash -n "${deployable_unit_converge_lib}" || fail "ifa_deployable_unit_live_converge.sh has a syntax error"
bash -n "${rationale_lib}" || fail "ifa_rationale_live.sh has a syntax error"
bash -n "${fixtures_lib}" || fail "ifa_family_fixtures.sh has a syntax error"
bash -n "${require_helpers_lib}" || fail "test-ifa-determinism-require-helpers.sh has a syntax error"
bash -n "${family_cases_lib}" || fail "test-ifa-determinism-family-cases.sh has a syntax error"
bash -n "${registry_lockstep_cases_lib}" || fail "test-ifa-determinism-registry-lockstep-cases.sh has a syntax error"
bash -n "${family_registry_pins_lib}" || fail "test-ifa-family-registry-derived-pins-cases.sh has a syntax error"
bash -n "${registry_family_lib}" || fail "ifa_family_registry.sh has a syntax error"
# This mirror needs the same guard as its fault-injection sibling. The fault
# side has always asserted on both itself (test-verify-ifa-fault-injection.sh
# BASH_SOURCE[0]) and the gate script under test (verify-ifa-fault-injection.sh
# ${script}); this determinism mirror asserted only on the gate script below,
# so test-verify-ifa-determinism.sh itself could drift over the cap with
# nothing to catch it -- which is exactly how it reached 508 lines before this
# split. `filecap-all` does not close the hole either -- it walks
# `git ls-files 'go/*.go'` and never sees shell.
[[ "$(wc -l <"${BASH_SOURCE[0]}" | tr -d '[:space:]')" -lt 500 ]] \
	|| fail "test-verify-ifa-determinism.sh must stay under 500 lines"
[[ "$(wc -l <"${script}" | tr -d '[:space:]')" -lt 500 ]] \
	|| fail "verify-ifa-determinism.sh must stay under 500 lines"

# _ifa_det_count_code_matches counts lines of ${2} where ${1} appears in the CODE
# portion -- before any `#`. Lines whose first non-whitespace character is `#`
# are skipped. Mirrors scripts/lib/test-ifa-fault-injection-assertions.sh's
# helper of the same shape, for the same reason: the bare `rg --fixed-strings`
# form below was satisfied by a COMMENT quoting its needle. Proven on this very
# file's pins -- prefixing both occurrences of the shared_cell guard with
# `# DISABLED: ` left this mirror green while the drive loop stopped skipping
# non-shared_cell families. Truncating at `#` also stops the `true  # was: X`
# shape. A `#` inside a quoted string can only make a pin RED, never pass.
# Whole-line form, sharing the comment and heredoc rules. `rg --line-regexp` is
# comment-immune but NOT heredoc-immune -- a heredoc line equal to the needle
# satisfies a whole-line regex, and the behavioural probe caught exactly that.
_ifa_det_count_code_lines_exact() {
	local needle="$1" file="$2" n=0 line stripped heredoc=""
	while IFS= read -r line || [[ -n "${line}" ]]; do
		stripped="${line#"${line%%[![:space:]]*}"}"
		if [[ -n "${heredoc}" ]]; then
			[[ "${stripped}" == "${heredoc}" ]] && heredoc=""
			continue
		fi
		if [[ "${line}" =~ \<\<-?[[:space:]]*[\'\"]?([A-Za-z_][A-Za-z0-9_]*)[\'\"]?[[:space:]]*$ ]]; then
			heredoc="${BASH_REMATCH[1]}"
		fi
		[[ "${stripped}" == "#"* ]] && continue
		[[ "${stripped}" == "${needle}" ]] && n=$((n + 1))
	done < "${file}"
	printf '%s\n' "${n}"
}
_ifa_det_count_code_matches() {
	local needle="$1" file="$2" n=0 line stripped code heredoc=""
	while IFS= read -r line || [[ -n "${line}" ]]; do
		stripped="${line#"${line%%[![:space:]]*}"}"
		# Heredoc bodies are data, not code: a real call moved into a dead heredoc
		# was proven to satisfy every pin while never executing.
		if [[ -n "${heredoc}" ]]; then
			[[ "${stripped}" == "${heredoc}" ]] && heredoc=""
			continue
		fi
		if [[ "${line}" =~ \<\<-?[[:space:]]*[\'\"]?([A-Za-z_][A-Za-z0-9_]*)[\'\"]?[[:space:]]*$ ]]; then
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
		# shellcheck does not flag it, `bash -n` passes, and no gate runs shellcheck
		# on these scripts, so nothing else would have caught it.
		code="${line%%[[:space:]\;\|\&\(\)\<\>\`]#*}"
		[[ "${code}" == *"${needle}"* ]] && n=$((n + 1))
	done < "${file}"
	printf '%s\n' "${n}"
}
# require pins a needle ANYWHERE in the gate, comments included. Exactly THREE
# call sites still use it, and all three deliberately bind FRAMING that exists
# only as prose: the contention ledger-regression rationale, the combined
# baseline drive inventory, and the optional contention drive inventory. Do NOT
# route those through require_code -- a prose pin demanding the text be code can
# never pass.
#
# Everything else binds code and uses require_code. An earlier version of this
# comment claimed only one pin bound framing; a review counted 66 of 69 call
# sites still binding code through this weaker form, and proved one live:
# commenting out `trap ifa_det_cleanup EXIT` left `require "exit trap"`
# satisfied. The split was established empirically -- every call site was moved
# to require_code and only the three that genuinely could not pass were moved
# back.
require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || fail "missing ${label}: ${needle}"
}
# require_code pins a needle that must be LIVE CODE, not prose about code. Use it
# for anything asserting the gate DOES something; use require above only for
# framing. Proven necessary: prefixing both occurrences of the shared_cell guard
# with `# DISABLED: ` left this mirror green while the drive loop stopped
# skipping non-shared_cell families.
require_code() {
	local label="$1" needle="$2"
	[[ "$(_ifa_det_count_code_matches "${needle}" "${script}")" -ge 1 ]] \
		|| fail "missing ${label}, or it survives only inside a comment: ${needle}"
}
# require_fixture asserts a needle that lives in the shared family-fixtures lib
# the gate sources, not in the gate script: the committed cassette and
# expected-set paths plus their fail-fast existence guards. Kept separate from
# require() so moving anything ELSE out of the gate script still fails.
# require_line pins a WHOLE line, so a needle that also appears inside a comment
# cannot satisfy it. Strict mode needs this: the gate script names
# `set -euo pipefail` in its bash>=4.4 header comment as well as running it, so
# the fixed-strings form still passed with the real line deleted.
require_line() {
	local label="$1" needle="$2"
	[[ "$(_ifa_det_count_code_lines_exact "${needle}" "${script}")" -ge 1 ]] \
		|| fail "missing ${label}, or it survives only inside a comment or heredoc: ${needle}"
}
require_fixture() {
	local label="$1" needle="$2"
	[[ "$(_ifa_det_count_code_matches "${needle}" "${fixtures_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (fixtures lib): ${needle}, or it survives only inside a comment"
}
# Per-family require_*_lib() helpers (require_lib, require_lifecycle_lib,
# require_delta_lib, require_code_call_lib, require_documentation_lib,
# require_deployable_unit_lib, require_rationale_lib, require_codeowners_lib,
# require_submodule_pin_lib) live in a sourced module so this structural
# verifier stays below 500 lines (mirroring the fault-injection sibling's
# per-mechanism case-module split). They call _ifa_det_count_code_matches
# above and the *_lib path variables declared at the top of this file.
# shellcheck source=scripts/lib/test-ifa-determinism-require-helpers.sh
source "${require_helpers_lib}"

# Strict mode and self-cleanup.
require_line "strict mode" "set -euo pipefail"
require_code "exit trap" "trap ifa_det_cleanup EXIT"
# The bash>=4.4 precondition guard MUST stay: under bash 3.2 a nounset abort is
# masked by the exit trap above as a false PASS. Pin the exact check so a
# refactor cannot silently drop it.
require_code "bash>=4.4 guard (masking-safe)" "requires bash >= 4.4"
require_code "sources shared lib" "scripts/lib/ifa_determinism_common.sh"
require_code "sources lifecycle lib" "scripts/lib/ifa_determinism_lifecycle.sh"
require_code "sources SQL delta-live lib" "scripts/lib/ifa_sql_delta_live.sh"
require_code "sources code-call live lib" "scripts/lib/ifa_code_call_live.sh"
require_code "sources documentation live lib" "scripts/lib/ifa_documentation_live.sh"
require_code "sources deployable-unit live lib" "scripts/lib/ifa_deployable_unit_live.sh"
require_code "sources deployable-unit diagnostics lib" "scripts/lib/ifa_deployable_unit_live_diagnostics.sh"
require_code "sources deployable-unit converge lib" "scripts/lib/ifa_deployable_unit_live_converge.sh"
require_code "sources rationale live lib" "scripts/lib/ifa_rationale_live.sh"
# Background pids must be recorded in the PARENT shell (printf -v in the lib),
# or the cleanup trap reaps nothing on a failure path and leaks host processes.
require_lib "parent-shell pid capture" "printf -v"
# Failure must surface the host-binary logs before the work dir is removed.
require_lifecycle_lib "failure log dump" "host binary logs (failure)"
require_code "--no-compose flag" "--no-compose"
require_code "--keep flag" "--keep"

# Isolation: a Compose project name and non-default ports distinct from every
# sibling verify-ifa-*.sh script and verify-golden-corpus-gate.sh's own
# defaults, so a run of this script cannot collide with any of them.
require_code "isolated compose project default" 'DETERMINISM_COMPOSE_PROJECT:=eshu-ifa-determinism-$$'
require_code "compose -p flag on up" '-p "${DETERMINISM_COMPOSE_PROJECT}"'
for reserved in \
	'ESHU_POSTGRES_PORT:=15432' 'NEO4J_BOLT_PORT:=7687' 'NEO4J_HTTP_PORT:=7474' \
	'ESHU_POSTGRES_PORT:=15532' 'NEO4J_BOLT_PORT:=7788' 'NEO4J_HTTP_PORT:=7575' \
	'ESHU_POSTGRES_PORT:=15635' 'NEO4J_BOLT_PORT:=7792' 'NEO4J_HTTP_PORT:=7679'; do
	if rg --fixed-strings --quiet -- "${reserved}" "${script}"; then
		fail "must not reuse a sibling verify-ifa-*.sh / verify-golden-corpus-gate.sh default port: ${reserved}"
	fi
done
# The port overrides MUST be exported, not just set: docker-compose.yaml's
# "ports" mapping interpolates them from the environment `docker compose`
# inherits, not from this script's own shell variables.
require_code "exported Postgres port override" 'export ESHU_POSTGRES_PORT='
require_code "exported Neo4j bolt port override" 'export NEO4J_BOLT_PORT='
require_code "exported Neo4j http port override" 'export NEO4J_HTTP_PORT='

# The determinism matrix itself: N in {1, 2, 4}, driven through the same
# cassette every sibling Ifá P3 script uses.
require_code "worker-count matrix N in {1,2,4}" "worker_counts=(1 2 4)"
require_code "demo-org cassette" "testdata/cassettes/gcpcloud/supply-chain-demo.json"
require_code "drive verb invocation" 'eshu-ifa" drive -cassette'
require_code "ifa binary build" "ifa_det_build_bin \"\${bin_dir}\" ifa"
require_code "projector drain" "eshu-projector"
require_code "reducer drain" "eshu-reducer"
require_code "gate binary" "eshu-golden-corpus-gate"
require_code "drains phase" "-phase=drains"
require_code "snapshot contract" "testdata/golden/e2e-20repo-snapshot.json"

# Synth-multiscope cassette (issue #4396 slice 6b): generated ONCE before the
# cell loop via `ifa synth-cassette`, then driven into every cell via a
# SECOND `ifa drive` call alongside the unmodified demo-org cassette — this is
# what makes -workers N non-inert (a single-scope cassette gives the driver
# exactly one work unit for any N).
require_code "synth-cassette verb invocation" '"${bin_dir}/eshu-ifa" synth-cassette'
require_code "synth-cassette seed flag" "-seed \"\${SYNTH_MULTISCOPE_SEED}\""
require_code "synth-cassette projects flag" "-projects \"\${SYNTH_MULTISCOPE_PROJECTS}\""
require_code "synth-cassette resources flag" "-resources \"\${SYNTH_MULTISCOPE_RESOURCES}\""
require_code "synth-cassette generated before the cell loop" "synth_cassette=\"\${work_dir}/synth-multiscope.json\""
require_code "second drive invocation into the same cell" 'eshu-ifa" drive -cassette "${synth_cassette}" -workers "${n}"'
require_code "combined-graph digest framing" "demo-org + synth-multiscope + SQL family + code-call family"

# Per-family static structural cases (SQL relationship, code_calls,
# deployable_unit_edges, rationale_edges, documentation_edges) live in a
# sourced case module so this structural verifier stays below 500 lines
# (mirroring the fault-injection sibling's per-family case-module split).
# shellcheck source=scripts/lib/test-ifa-determinism-family-cases.sh
source "${family_cases_lib}"
run_ifa_determinism_family_cases

# CI-gate registry/workflow lockstep cases (the real ci-gates registry
# matcher, not a text grep) live in a sourced case module so this structural
# verifier stays below 500 lines (mirroring the fault-injection sibling's
# per-mechanism case-module split).
# shellcheck source=scripts/lib/test-ifa-determinism-registry-lockstep-cases.sh
source "${registry_lockstep_cases_lib}"
run_ifa_determinism_registry_lockstep_cases

# Family-registry derived-pins cases (#6147 PR-0): independent, hand-derived
# blocker_kind/wait_stage/wait_key pins for every family scripts/lib/ifa_family_registry.sh
# declares, proven against the reducer handler source rather than copied back
# out of the registry -- so a wrong registry declaration is actually caught,
# not vacuously restated. Lives in its own module (owned separately from this
# split) so its expected-value derivation stays independent of whoever writes
# the registry; wired here the same way as the two case modules above.
# registry_family_lib is a top-level path variable (declared above, shared
# with test-ifa-determinism-family-cases.sh's own registry totality check);
# this module still validates it itself before sourcing it (see
# run_ifa_family_registry_pins_cases's own ${registry_family_lib} guard
# below) rather than trusting the caller silently.
# shellcheck source=scripts/lib/test-ifa-family-registry-derived-pins-cases.sh
source "${family_registry_pins_lib}"
run_ifa_family_registry_pins_cases

# #5007 contention cassette (opt-in --contention): the overlapping-identity
# fixture whose K scopes share one CloudResource uid set, so the cross-scope
# writers contend and the owner ledger must keep the digest identical across
# N=1/2/4. Generated via `ifa synth-cassette -overlap -divergent` and driven as
# an optional seventh drive, behind --contention so it cannot alter the default matrix.
require_code "--contention flag" "--contention"
require_code "contention overlap generation" '-overlap -divergent'
require_code "contention seed flag" "-seed \"\${SYNTH_CONTENTION_SEED}\""
require_code "contention projects flag" "-projects \"\${SYNTH_CONTENTION_PROJECTS}\""
require_code "contention cassette generated once" 'contention_cassette="${work_dir}/contention.json"'
require_code "optional seventh drive of the contention cassette" 'eshu-ifa" drive -cassette "${contention_cassette}" -workers "${n}"'
require "contention ledger-regression framing" "graph-level contention"
contention_contract="$(rg -U --pcre2 --only-matching '(?ms)^# #5007 contention cassette.*?^require "contention ledger-regression framing"[^\n]*$' "${BASH_SOURCE[0]}")"
if [[ "${contention_contract}" == *"THIRD drive"* \
	|| "${contention_contract}" == *"third drive"* ]]; then
	fail "contention contract still describes the pre-family third-drive position"
fi

# Populated-then-drained guard per cell: a 0/0 reading before anything was
# ever enqueued would pass on a vacuous drain.
require_code "drive-populated guard" "vacuous drain proof"
require "combined baseline drive inventory" "After the six baseline drives"
require "optional contention drive inventory" "the optional seventh contention drive"
if rg --fixed-strings --quiet -- "Fourth drive" "${script}" \
	|| rg --fixed-strings --quiet -- "Prove both drives" "${script}"; then
	fail "determinism verifier still describes the pre-family drive count"
fi
require_code "fact_work_items populated check" "SELECT count(*) FROM fact_work_items;"

# Fresh-DB-per-cell: every cell must tear its OWN stack down before the next
# cell starts, not only once at the very end — this is what makes each N a
# genuinely independent, fresh-database run rather than an incremental replay
# onto the previous cell's data.
require_code "per-cell fresh-stack teardown" "fresh stack for the next cell"
require_code "per-cell down -v inside the loop" 'docker compose -p "${DETERMINISM_COMPOSE_PROJECT}" -f "${compose_file}" down -v'

# Graph-truth capture and the digest-equality assertion.
require_code "graph-dump full-bytes capture" "graph-dump -out"
require_code "graph-dump digest capture" "graph-dump -digest"
require_code "digest storage per N" "digests[\${n}]="
require_code "digest mismatch detection" "MISMATCH:"
require_code "full-bytes diff on divergence" "diff -u"
require_code "failure-artifact framing" "failure artifact"
# Pinned to the DIE plus the divergence-specific text, not the bare phrase.
# "graph-determinism matrix FAILED" alone matches three places -- a comment,
# the --teeth branch, and the real assertion -- so any two of them satisfied
# it and downgrading the real `die` to `log`, message unchanged, kept the
# mirror green. That is precisely the "do NOT normalize this away" drift the
# asserted line itself warns against.
require_code "hard failure on divergence" '|| die "graph-determinism matrix FAILED: digests diverged across worker counts'
require_code "no-normalize-away directive" "do NOT lower N, retry, or otherwise normalize this away"

# Per-cell wall time is reported.
require_code "per-cell wall time capture" "cell_end - cell_start"
require_code "wall time in PASS reporting" "wall=%ss"

# The drain must be polled by the gate binary, not slept.
if rg --quiet --pcre2 'sleep\s+\$\{?GATE_DRAIN' "${script}"; then
	fail "drain must be polled by the gate, not slept"
fi

# --teeth (#4396 slice 6): the acceptance clause's negative-path proof that
# the matrix catches a deliberately non-idempotent write, built behind a Go
# build tag so it never ships in a normal/CI/production binary.
require_code "--teeth flag" "--teeth"
require_code "teeth build tag" "ifadeterminismteeth"
require_code "teeth threads tags through every build call" 'ifa_det_build_bin "${bin_dir}" reducer "${build_tags}"'
require_code "teeth caught framing" "TEETH: CAUGHT"
require_code "teeth-not-caught is its own failure" "TEETH FAILED"
require_code "teeth still forbids lowering N" "lower N, retry, or otherwise normalize this away"
require_lib "build_bin accepts an optional tags argument" 'local bin_dir="$1" cmd="$2" tags="${3:-}"'
require_lib "tags become -tags args only when non-empty" 'tag_args=(-tags "${tags}")'

# The build-tag-gated fault itself must exist exactly where the script's own
# doc says it does, and must not be reachable without the tag.
teeth_reducer_on="${repo_root}/go/internal/reducer/gcp_resource_materialization_teeth.go"
teeth_reducer_off="${repo_root}/go/internal/reducer/gcp_resource_materialization_teeth_off.go"
teeth_cypher_on="${repo_root}/go/internal/storage/cypher/cloud_resource_node_writer_teeth.go"
teeth_cypher_off="${repo_root}/go/internal/storage/cypher/cloud_resource_node_writer_teeth_off.go"
for f in "${teeth_reducer_on}" "${teeth_reducer_off}" "${teeth_cypher_on}" "${teeth_cypher_off}"; do
	[[ -f "${f}" ]] || fail "missing teeth build-tag file: ${f}"
done
rg --fixed-strings --quiet -- '//go:build ifadeterminismteeth' "${teeth_reducer_on}" \
	|| fail "${teeth_reducer_on} must carry the ifadeterminismteeth build tag"
rg --fixed-strings --quiet -- '//go:build !ifadeterminismteeth' "${teeth_reducer_off}" \
	|| fail "${teeth_reducer_off} must carry the !ifadeterminismteeth build tag"
rg --fixed-strings --quiet -- '//go:build ifadeterminismteeth' "${teeth_cypher_on}" \
	|| fail "${teeth_cypher_on} must carry the ifadeterminismteeth build tag"
rg --fixed-strings --quiet -- '//go:build !ifadeterminismteeth' "${teeth_cypher_off}" \
	|| fail "${teeth_cypher_off} must carry the !ifadeterminismteeth build tag"

# No private data: hostnames, IPs, cloud account IDs, keys, internal paths.
#
# DERIVED, not hand-typed. The previous form named five targets out of the
# fourteen *_lib variables this mirror declares, and missed every one this
# branch adds -- codeowners, fixtures, family-cases, registry-lockstep-cases,
# pins and registry-family. A hand-typed scan does not grow when the tree does,
# which is the same defect the sibling fault mirror carried until it was
# derived; this is that fix applied here.
#
# The pattern brackets one character per alternative so it does not match its
# own definition now that the scan covers this file too. A bracketed
# single-character class matches exactly the text the bare character does, so
# detection is unchanged -- verified alternative by alternative.
private_pattern='gh[p]_|github_pa[t]_|glpa[t]-|AKI[A]|ASI[A]|xo[x][baprs]-|arn:aw[s]:|(^|[^0-9])[0-9]{12}([^0-9]|$)|/[U]sers/|/[h]ome/[a-z]'
declare -a private_targets=("${script}" "${BASH_SOURCE[0]}")
while IFS= read -r private_lib_var; do
	private_targets+=("${!private_lib_var}")
done < <(compgen -v | rg '_lib$' | sort)
# The registry, its rows and its hand-derived pins are not bound to *_lib vars,
# so the derivation cannot see them. Globbed, so a seventh row is covered the
# day it lands.
for private_target in "${repo_root}"/scripts/lib/ifa_family_registry.sh \
	"${repo_root}"/scripts/lib/ifa_family_registry/rows/*.sh \
	"${repo_root}"/scripts/lib/ifa_family_registry_pins/*.sh; do
	[[ -e "${private_target}" ]] && private_targets+=("${private_target}")
done
# EXACTLY TWO: the glob block itself, and this pin's own line. An at-least-one
# form is useless here -- a pin whose needle lives in the same file is always
# satisfied by itself, which is how the previous version stayed green with the
# whole glob block deleted. Counting is what makes an in-file pin able to fail.
[[ "$(_ifa_det_count_code_matches 'ifa_family_registry_pins/*.sh' "${BASH_SOURCE[0]}")" -eq 2 ]] \
	|| fail "the determinism private-data scan no longer globs the registry rows and pins (expected the glob block plus this pin line)"
# Floor against a collapsed derivation: if the *_lib expression stops matching,
# the loop silently scans almost nothing and passes. Hand-written, below the
# current count, never derived from the expression it guards.
[[ "${#private_targets[@]}" -ge 20 ]] \
	|| fail "private-data scan covers only ${#private_targets[@]} file(s); the derivation has collapsed"
# The glob block above is otherwise bound only by the floor, and only by a 3-file
# margin -- three more *_lib declarations and deleting it would stop reddening,
# which is the exact silent revert the sibling mirror was fixed for. Pin it
# directly so the margin stops mattering.
for private_target in "${private_targets[@]}"; do
	[[ -f "${private_target}" ]] \
		|| fail "private-data scan target ${private_target} does not exist -- a scan that skips a missing file proves nothing"
	if rg --pcre2 --quiet -- "${private_pattern}" "${private_target}"; then
		fail "$(basename "${private_target}") looks like it contains private data"
	fi
done
printf 'private-data scan: %s file(s) scanned\n' "${#private_targets[@]}"

# shellcheck source=scripts/lib/test-ifa-determinism-pin-behaviour-cases.sh
source "${repo_root}/scripts/lib/test-ifa-determinism-pin-behaviour-cases.sh"
run_ifa_determinism_pin_behaviour_cases
# Pin this gate's OWN call site: deleting the three lines above would otherwise
# remove the whole behavioural pin check with this mirror still green, and the
# sourced->triggered drift check cannot see a module that stops being sourced.
# EXACTLY THREE (call, this needle, the name in the message below) because an
# in-file pin is always satisfied by its own line.
[[ "$(_ifa_det_count_code_matches 'run_ifa_determinism_pin_behaviour_cases' "${BASH_SOURCE[0]}")" -eq 3 ]] \
	|| fail "this mirror no longer calls run_ifa_determinism_pin_behaviour_cases (expected the call, this pin, and the name in this message) -- nothing would check that its pin helpers bind code"

printf 'test-verify-ifa-determinism: pass\n'
