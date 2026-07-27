#!/usr/bin/env bash
#
# verify-docs-only-ci-skip.sh — static mirror for the docs-only CI carve-out.
#
# A docs-only PR (docs/**, root markdown, mkdocs config) must skip the heavy Go
# lanes while the cheap due-diligence gates — and the docs-relevant guards that
# happen to live inside otherwise-heavy workflows — still run. That behavior
# lives in .github/workflows YAML and is only exercised by GitHub Actions, so
# this script is the local guard that the WIRING is intact. It fails closed if a
# future edit removes a path filter, drops a heavy job's `changes` gate, lets the
# go-race umbrella reject a legitimately-skipped matrix, or — the subtle one the
# review caught — silences a docs-relevant guard (Trivy's secret scan, the
# capability `-mode docs` check) by gating the wrong job.
#
# It does not re-implement dorny/paths-filter's matcher (CI owns the actual path
# evaluation); it asserts the structure that makes the skip correct, complete,
# and required-check-safe. Runs in the always-on docs-helm-hygiene job.
#
# It also guards the #5818 regression class directly: test.yml, security-scan.yml,
# and mcp-schema-drift.yml each carry their own copy of the same dorny/paths-filter
# `code` block, and #5818 burned ~118 runner-minutes on a five-markdown-file PR
# because `!*.md` is ROOT-ANCHORED (it never matched the nested
# `.agents/skills/**/*.md` files that PR touched), so `code` evaluated true and
# every heavy Go lane ran. Two checks below catch that class going forward:
#   1. the three copies of the `code` filter must stay byte-identical (a fix
#      applied to only one copy is exactly how #5818-shaped drift starts);
#   2. no registry gate whose CI job is one of these workflows' code-gated jobs
#      may declare a trigger path that the filter's negations would swallow
#      (the filter's negation style over-covers safely in the other direction,
#      so this is a one-way superset check).
set -uo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
wf="${repo_root}/.github/workflows"
registry="${repo_root}/specs/ci-gates.v1.yaml"
fail=0

ok()  { printf 'ok - %s\n' "$1"; }
bad() { printf 'not ok - %s\n' "$1"; fail=1; }
has() { rg -qF -- "$2" "$1"; }

# job_block <file> <job> — the YAML lines of one top-level job, header through the
# line before the next 2-space job key.
job_block() { awk -v j="  $2:" '$0==j{f=1;print;next} f&&/^  [A-Za-z]/{exit} f{print}' "$1"; }
# job_gated <file> <job> — true if the job carries a `needs: changes` code gate.
job_gated()    { job_block "$1" "$2" | rg -qF 'needs: changes'; }
job_alwayson() { ! job_gated "$1" "$2"; }

# code_filter_block <file> — the dorny/paths-filter `code:` key and its `- '...'`
# negation list, verbatim (comments excluded), for byte-identity comparison
# across the three workflows that duplicate this filter.
code_filter_block() {
	awk '
		/^[[:space:]]*code:[[:space:]]*$/ { grab = 1; print; next }
		grab {
			if ($0 !~ /^[[:space:]]*- /) { exit }
			print
			next
		}
	' "$1"
}

# registry_gated_triggers <workflow> <job...> — every `triggers:` path entry in
# specs/ci-gates.v1.yaml for a gate whose `ci.workflow` is <workflow> and whose
# `ci.job` is one of the given (exact, unquoted) job names. Job filtering matters:
# a registry gate can name test.yml/security-scan.yml/mcp-schema-drift.yml as its
# `ci.workflow` while actually running in an ALWAYS-ON job of that workflow
# (docs-helm-hygiene, Trivy filesystem scan, Verify capability inventory) — those
# gates are correctly unaffected by the `code` filter and must not be checked
# against it.
registry_gated_triggers() {
	local target_wf="$1"
	shift
	local jobs_csv="" j
	for j in "$@"; do jobs_csv="${jobs_csv}|${j}"; done
	jobs_csv="${jobs_csv#|}"
	awk -v wf="${target_wf}" -v jobs="${jobs_csv}" '
		function job_matches(j,    n, arr, i) {
			n = split(jobs, arr, "|")
			for (i = 1; i <= n; i++) if (arr[i] == j) return 1
			return 0
		}
		function flush() {
			if (cur_wf == wf && job_matches(cur_job)) {
				n = split(cur_triggers, arr, "\n")
				for (i = 1; i <= n; i++) if (arr[i] != "") print arr[i]
			}
		}
		/^  - id: / {
			flush()
			cur_wf = ""; cur_job = ""; cur_triggers = ""; in_ci = 0; in_trig = 0
			next
		}
		/^    ci:$/ { in_ci = 1; in_trig = 0; next }
		in_ci && /^      workflow: / { cur_wf = $0; sub(/^      workflow: */, "", cur_wf); next }
		in_ci && /^      job: / {
			cur_job = $0; sub(/^      job: */, "", cur_job); gsub(/"/, "", cur_job); in_ci = 0; next
		}
		/^    triggers:$/ { in_trig = 1; next }
		in_trig && /^      - / {
			t = $0; sub(/^      - */, "", t); gsub(/"/, "", t)
			cur_triggers = cur_triggers t "\n"
			next
		}
		in_trig && !/^      - / { in_trig = 0 }
		END { flush() }
	' "${registry}"
}

# code_filter_negations <file> — the negation glob strings (leading '!'
# stripped) in <file>'s code: filter block, in the order they appear, skipping
# the base '**' positive entry. Parsed from the actual filter text rather than
# a fixed list, so a negation ADDED to the filter later (e.g. a future
# '!go/internal/query/**') is picked up automatically — a hard-coded case
# statement here is exactly the shape of bug this script exists to catch: it
# would silently stop checking new negations the day one is added, which is
# worse than not checking at all because it still prints "ok".
code_filter_negations() {
	code_filter_block "$1" | awk '
		/^[[:space:]]*- /{
			line = $0
			sub(/^[[:space:]]*- /, "", line)
			gsub(/^'"'"'|'"'"'$/, "", line)
			if (line == "**") next
			if (substr(line, 1, 1) == "!") print substr(line, 2)
		}
	'
}

# trigger_root <trigger> — the longest literal path prefix of a trigger glob,
# with any trailing "/**" or "/**/*.ext" wildcard tail stripped. Empty when the
# trigger itself starts with a wildcard (e.g. a bare "*.md"), since such a
# trigger has no fixed directory to compare against a directory-recursive
# negation.
trigger_root() {
	local t="$1"
	case "${t}" in
	*/\*\*) printf '%s' "${t%/\*\*}" ;;
	*/\*\*/\**) printf '%s' "${t%%/\*\*/*}" ;;
	*[*?]*) printf '' ;;
	*) printf '%s' "${t}" ;;
	esac
}

# negation_swallows_trigger <negation> <trigger> — true if EVERY path matching
# <trigger> is excluded by the single <negation> glob (leading '!' already
# stripped). Handles the four negation shapes the code: filter actually uses
# (see the in-file filter comment): a directory-recursive "<dir>/**", a
# directory-recursive + extension-anchored hybrid "<dir>/**/*.<ext>" (e.g.
# ".github/**/*.md" — dorny/paths-filter matches paths with picomatch, whose
# "**" matches zero or more path segments including none, so this shape
# matches "<dir>/file.<ext>" through "<dir>/a/b/c/file.<ext>"; verified against
# picomatch's globbing-features docs, not assumed), a bare root-anchored
# extension pattern like "*.md" (dorny/paths-filter treats a slash-free
# pattern as top-level-only — the #5818 root cause), and an exact literal
# path. This is not a general glob-intersection solver — it is exactly precise
# enough to catch a registry trigger the filter's real negations would
# swallow, including a negation the filter does not carry today.
#
# Two further shapes are deliberately NOT handled: a "**" in the middle of a
# pattern with no extension anchor (e.g. "<dir>/**/sub/**") and a character
# class trigger or negation (e.g. "<dir>/[a-z]*"). Both need a real glob
# intersection to decide "every matching path" safely, and a wrong "swallowed"
# verdict here would silently stop flagging a real #5818-class drift — worse
# than the current false "not swallowed", which only means this checker missed
# a shape (loud in review) rather than approving a bad one. Neither shape
# appears in the live filter today.
negation_swallows_trigger() {
	local n="$1" t="$2"

	# Exact literal match: neither side carries a glob metacharacter.
	if [[ "${n}" != *[*?]* && "${t}" == "${n}" ]]; then
		return 0
	fi

	# Directory-recursive negation "<dir>/**" swallows any trigger rooted at
	# or under <dir>.
	case "${n}" in
	*/\*\*)
		local ndir troot
		ndir="${n%/\*\*}"
		troot="$(trigger_root "${t}")"
		if [[ -n "${troot}" ]] && { [[ "${troot}" == "${ndir}" ]] || [[ "${troot}" == "${ndir}/"* ]]; }; then
			return 0
		fi
		;;
	esac

	# Directory-recursive + extension-anchored hybrid negation
	# "<dir>/**/*.<ext>" swallows a trigger rooted at or under <dir> only when
	# EVERY path that trigger can match also carries the same extension: a
	# literal (glob-free) trigger ending in "<ext>", or a trigger that is
	# itself this same hybrid shape (same tail "**/*.<ext>") rooted at or
	# under <dir>. A trigger like "<dir>/**" is NOT swallowed here — it can
	# match files of any extension, so only some of its paths are excluded.
	case "${n}" in
	*/\*\*/\**)
		local ndir nsuffix troot
		ndir="${n%%/\*\*/*}"
		nsuffix="${n#*/\*\*/}"
		troot="$(trigger_root "${t}")"
		if [[ -n "${troot}" ]] && { [[ "${troot}" == "${ndir}" ]] || [[ "${troot}" == "${ndir}/"* ]]; }; then
			if [[ "${t}" != *[*?]* && "${t}" == *"${nsuffix#\*}" ]]; then
				return 0
			fi
			case "${t}" in
			*/\*\*/"${nsuffix}")
				return 0
				;;
			esac
		fi
		;;
	esac

	# Bare root-anchored extension negation (no "/", e.g. "*.md") swallows the
	# bare pattern itself, or a literal top-level trigger sharing the suffix.
	if [[ "${n}" != */* && "${n}" == \**.* ]]; then
		local suffix="${n#\*}"
		if [[ "${t}" == "${n}" ]]; then
			return 0
		fi
		if [[ "${t}" != */* && "${t}" != *[*?]* && "${t}" == *"${suffix}" ]]; then
			return 0
		fi
	fi

	return 1
}

# trigger_is_swallowed_by_negations <trigger> <negation...> — true if EVERY
# path matching <trigger> would be excluded by at least one of the given
# negation globs, meaning a gate relying on that trigger would never see
# `code == 'true'`.
trigger_is_swallowed_by_negations() {
	local t="$1"
	shift
	local n
	for n in "$@"; do
		if negation_swallows_trigger "${n}" "${t}"; then
			return 0
		fi
	done
	return 1
}

# --- build.yml: pure binary build, no docs-relevant job — skip the whole run.
# .agents/** is asserted explicitly (not just docs/**, *.md): before this gate
# ratcheted it in, docs/public/reference/local-testing.md claimed build.yml
# skipped an .agents/**-only PR while its paths-ignore never listed the
# pattern — nothing here checked that claim, so it silently drifted false. ---
b="${wf}/build.yml"
if has "${b}" 'paths-ignore:' && has "${b}" "- 'docs/**'" && has "${b}" "- '*.md'" && has "${b}" "- '.agents/**'"; then
	ok "build.yml skips docs-only PRs (paths-ignore: docs/**, *.md, .agents/**)"
else
	bad "build.yml has a pull_request paths-ignore covering docs/**, *.md, and .agents/**"
fi

# --- security-scan.yml: keep the secret scan on, gate the Go scanners. ---
s="${wf}/security-scan.yml"
if has "${s}" 'dorny/paths-filter' && job_block "${s}" changes | rg -qF 'code: ${{ steps.filter.outputs.code }}'; then
	ok "security-scan.yml has a changes job exporting the code filter"
else
	bad "security-scan.yml has a changes job exporting code"
fi
if job_alwayson "${s}" trivy-fs; then
	ok "security-scan.yml keeps trivy-fs (secret + IaC scan) always-on for docs-only PRs"
else
	bad "security-scan.yml trivy-fs must NOT be code-gated (it is the only secret scan)"
fi
for j in govulncheck gosec nancy; do
	if job_gated "${s}" "${j}"; then ok "security-scan.yml ${j} (Go scanner) is code-gated"
	else bad "security-scan.yml ${j} carries needs:changes"; fi
done

# --- mcp-schema-drift.yml: keep the docs guard on, gate the Go drift jobs. ---
m="${wf}/mcp-schema-drift.yml"
if has "${m}" 'dorny/paths-filter' && job_block "${m}" changes | rg -qF 'code: ${{ steps.filter.outputs.code }}'; then
	ok "mcp-schema-drift.yml has a changes job exporting the code filter"
else
	bad "mcp-schema-drift.yml has a changes job exporting code"
fi
if job_alwayson "${m}" capability-verify; then
	ok "mcp-schema-drift.yml keeps capability-verify (-mode docs guard) always-on for docs-only PRs"
else
	bad "mcp-schema-drift.yml capability-verify must NOT be code-gated (its -mode docs is the only docs guard)"
fi
for j in mcp-tool-count mcp-test-suite; do
	if job_gated "${m}" "${j}"; then ok "mcp-schema-drift.yml ${j} (Go drift job) is code-gated"
	else bad "mcp-schema-drift.yml ${j} carries needs:changes"; fi
done

# --- test.yml: heavy Go lanes gated; docs build stays always-on. ---
t="${wf}/test.yml"
if has "${t}" 'code: ${{ steps.filter.outputs.code }}' && has "${t}" 'dorny/paths-filter'; then
	ok "test.yml has a changes job exporting the code filter"
else
	bad "test.yml exposes a changes.outputs.code from dorny/paths-filter"
fi
needs_n="$(rg -cF 'needs: changes' "${t}" || true)"; needs_n="${needs_n:-0}"
guard_n="$(rg -cF "github.event_name != 'pull_request' || needs.changes.outputs.code == 'true'" "${t}" || true)"; guard_n="${guard_n:-0}"
if [[ "${needs_n}" -ge 3 && "${guard_n}" -ge 3 ]]; then
	ok "all 3 heavy test.yml jobs gate on the changes job (needs×${needs_n}, guard×${guard_n})"
else
	bad "verify-contracts, go-core and go-race each carry needs:changes + the event-guarded if (needs×${needs_n}, guard×${guard_n}, want ≥3 each)"
fi
if job_alwayson "${t}" docs-helm-hygiene; then
	ok "test.yml keeps docs-helm-hygiene (docs build) always-on for docs-only PRs"
else
	bad "test.yml docs-helm-hygiene must NOT be code-gated (it is the docs build)"
fi
# --- test.yml: both umbrella gates (go-race-complete #5757, go-core-complete
# #5814) are the names branch protection points at, so each must hold the same
# three-part contract: always reports, accepts a genuine docs-only skip as pass,
# and REFUSES to accept that skip when the `changes` gate itself failed.
#
# That last part is the subtle one. GitHub implicitly prepends `success()` to a
# job `if:` that does not name a status function, so a `changes` job that fails
# or is cancelled marks its dependants `skipped` — not `failure`. An umbrella
# keying only on its own dependency's result would report GREEN having built and
# tested nothing. Each umbrella therefore needs `changes` as an explicit
# dependency plus a check on its result.
#
# Every assertion below is scoped with `job_block` to the specific job. A
# file-wide `rg` would be satisfied by the OTHER umbrella's copy of the same
# string and silently stop testing the job it names.
for umbrella in go-race-complete go-core-complete; do
	case "${umbrella}" in
	go-race-complete) dep="go-race" ;;
	go-core-complete) dep="go-core" ;;
	esac
	block="$(job_block "${t}" "${umbrella}")"
	if rg -qF "needs: [changes, ${dep}]" <<<"${block}"; then
		ok "${umbrella} depends on both changes and ${dep}"
	else
		bad "${umbrella} must declare needs: [changes, ${dep}]"
	fi
	if rg -qF 'if: ${{ always() }}' <<<"${block}"; then
		ok "${umbrella} always reports (if: \${{ always() }})"
	else
		bad "${umbrella} must carry if: \${{ always() }} so it always reports"
	fi
	if rg -qF '!= "skipped"' <<<"${block}"; then
		ok "${umbrella} accepts a skipped ${dep} as pass (required-check-safe)"
	else
		bad "${umbrella} treats result==skipped as pass"
	fi
	# Assert the guard STRUCTURALLY: the comparison AND a non-zero exit inside
	# the same `if ... fi`. Two weaker forms were tried and both were vacuous:
	#   - matching `needs.changes.result` matched the `changes_result=...`
	#     ASSIGNMENT, so deleting the whole conditional stayed green;
	#   - matching the comparison alone left "keep the comparison and the
	#     ::error:: echo, drop the `exit 1`" green — and that mutation still
	#     exits 0, so the umbrella reports success after a failed `changes`.
	# A bare `exit 1` search over the whole job block is no good either: the
	# SECOND guard (the lane-result check) carries its own `exit 1` and would
	# satisfy it. So slice out just this guard's body and look inside it.
	guard_body="$(awk '/"\$\{changes_result\}" != "success"/{f=1} f{print} f&&/^[[:space:]]*fi[[:space:]]*$/{exit}' <<<"${block}")"
	if [[ -n "${guard_body}" ]] && rg -q '^\s*exit [1-9]' <<<"${guard_body}"; then
		ok "${umbrella} fails when the changes gate itself failed (guard exits non-zero)"
	else
		bad "${umbrella} changes_result guard must compare != \"success\" AND exit non-zero inside that same if-block"
	fi
done

# --- #5818 regression guard 1: the three `code` filter copies must stay
# byte-identical. #5818 happened because the fix (excluding a nested markdown
# tree) only needed to land once conceptually, but the filter is duplicated
# byte-for-byte three times; a future edit to only one copy is exactly this
# drift. ---
block_test="$(code_filter_block "${t}")"
block_sec="$(code_filter_block "${s}")"
block_mcp="$(code_filter_block "${m}")"
if [[ -z "${block_test}" || -z "${block_sec}" || -z "${block_mcp}" ]]; then
	bad "could not extract a code: filter block from test.yml, security-scan.yml, and mcp-schema-drift.yml"
elif [[ "${block_test}" == "${block_sec}" && "${block_sec}" == "${block_mcp}" ]]; then
	ok "test.yml, security-scan.yml, and mcp-schema-drift.yml carry byte-identical code: filters"
else
	bad "the code: filter has drifted between test.yml, security-scan.yml, and mcp-schema-drift.yml (#5818 regression class) — diff:"
	diff <(printf '%s\n' "${block_test}") <(printf '%s\n' "${block_sec}") >&2 || true
	diff <(printf '%s\n' "${block_test}") <(printf '%s\n' "${block_mcp}") >&2 || true
fi

# --- #5818 regression guard 2: no gate whose CI job is actually code-gated may
# rely on a trigger path the filter's negations would swallow. The filter
# over-covers safely in the other direction (unfiltered code paths tripping a
# gate that does not need them just runs an extra gate), so this is a one-way
# superset check, not full glob equivalence.
#
# gated_jobs_for <workflow> — the exact `ci.job` names in <workflow> that carry
# the `needs: changes` + event-guarded `if` (as opposed to an always-on job like
# docs-helm-hygiene, Trivy filesystem scan, or Verify capability inventory).
# A plain case statement (not an associative array) so this stays bash-3.2-safe
# like the rest of this script — no `declare -A` version floor to police. ---
gated_jobs_for() {
	case "$1" in
	test.yml) printf '%s\n' "go-core|verify-contracts|go-race-complete" ;;
	security-scan.yml) printf '%s\n' "gosec (Go static analysis)|govulncheck (Go vulnerability database)|nancy (Sonatype dep CVE + license scan)" ;;
	mcp-schema-drift.yml) printf '%s\n' "Verify ReadOnlyTools count|Full MCP test suite" ;;
	esac
}
# Negations are parsed from test.yml's own code: filter (this check runs
# regardless of whether guard 1 above passed, so it evaluates against ONE
# concrete filter and lets a genuine drift between the three copies surface
# only as guard 1's failure, not a double report here).
code_negations=()
while IFS= read -r neg; do
	[[ -z "${neg}" ]] && continue
	code_negations+=("${neg}")
done < <(code_filter_negations "${t}")
trigger_fail=0
for workflow_name in test.yml security-scan.yml mcp-schema-drift.yml; do
	IFS='|' read -r -a job_names <<<"$(gated_jobs_for "${workflow_name}")"
	wf_trigger_count=0
	while IFS= read -r trig; do
		[[ -z "${trig}" ]] && continue
		wf_trigger_count=$((wf_trigger_count + 1))
		if trigger_is_swallowed_by_negations "${trig}" "${code_negations[@]}"; then
			bad "${workflow_name}: a code-gated job's registry trigger '${trig}' is swallowed by the code filter's own negations — that gate can never fire from a PR touching only that path"
			trigger_fail=1
		fi
	done < <(registry_gated_triggers "${workflow_name}" "${job_names[@]}")
	# Report the per-workflow trigger count explicitly rather than folding it
	# into the single pass/fail line below. A workflow whose code-gated jobs
	# (per gated_jobs_for) never appear as a registry gate's `ci.job` — true
	# for mcp-schema-drift.yml today, see #5841 P2 — checks zero triggers and
	# would otherwise read as uniform coverage across all three workflows.
	if [[ "${wf_trigger_count}" -eq 0 ]]; then
		# Join with an explicit delimiter set AT USE TIME. The `IFS='|'` on the
		# `read` above is scoped to that one command, so by here IFS is back to
		# its default space -- and these job names CONTAIN spaces, so "${a[*]}"
		# would render them as one unreadable run-on string.
		joined_jobs="$(
			IFS=','
			printf '%s' "${job_names[*]}"
		)"
		joined_jobs="${joined_jobs//,/, }"
		ok "${workflow_name}: 0 triggers checked — no registry gate names one of its code-gated jobs (${joined_jobs}) as ci.job"
	else
		ok "${workflow_name}: checked ${wf_trigger_count} code-gated registry trigger(s) against the code filter's negations"
	fi
done
if [[ "${trigger_fail}" -eq 0 ]]; then
	ok "no code-gated registry trigger (test.yml/security-scan.yml/mcp-schema-drift.yml) is swallowed by the code filter's negations"
fi

if [[ "${fail}" -ne 0 ]]; then
	printf '\nverify-docs-only-ci-skip: docs-only CI carve-out wiring drifted — see failures above.\n' >&2
	exit 1
fi
printf '\nverify-docs-only-ci-skip: docs-only CI carve-out wiring intact.\n'
