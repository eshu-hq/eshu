#!/usr/bin/env bash
# deployable_unit_edges convergence-retry loop (#5993), split out of
# ifa_deployable_unit_live.sh (the P6 review's return-code and drain-label
# fixes to this loop pushed that file back over the repository's 500-line
# cap) to keep both files under it. Sourced by verify-ifa-determinism.sh and
# verify-ifa-fault-injection.sh alongside ifa_deployable_unit_live.sh, which
# the caller owns strict mode, logging, and ifa_det_pg for -- same convention
# as that file's own header.
#
# No sourcing order is required relative to ifa_deployable_unit_live.sh.
# ifa_deployable_unit_live_converge_edges below calls
# ifa_deployable_unit_live_run_maintenance_pass and
# ifa_deployable_unit_live_assert, both defined there, but bash resolves a
# function call at INVOCATION time, not at source time, and this file's only
# top-level statement is the `:=` default just below -- it does not call
# either function itself. Both files are always fully sourced by the time any
# cell actually runs, regardless of which is sourced first.

# ifa_deployable_unit_live_init_maintenance_scratch creates and exports the
# two scratch directories ifa_deployable_unit_live_run_maintenance_pass
# reuses for every maintenance pass in the caller's run (#6149 -- see that
# function's STABILIZED header note in ifa_deployable_unit_live.sh for why a
# fresh `mktemp -d` per call broke the fault-injection digest comparison).
# Called once per driver script, right after work_dir is created, nested
# under it so the driver's existing EXIT-trap cleanup covers these too.
# Idempotent (skips if already set) so a stray second call is harmless.
#
# PHANTOM-REPO FINDING (second digest fix, same #6149 live run that proved
# the path-stability fix above): a genuinely EMPTY ESHU_FILESYSTEM_ROOT is
# NOT inert. DiscoverFilesystemRepositoryIDs (go/internal/collector/
# git_selection_discovery.go) recurses via discoverRepoRootsWithGitPriority,
# and repositoryRootLikeFromEntries's fallback -- `return childDirectories ==
# 0` -- reports a directory with ZERO entries as repository-root-like, so an
# empty root is discovered as ONE repository (repoID "."), not zero. This
# reproduced directly with collector.DiscoverFilesystemRepositoryIDs against
# a bare os.MkdirTemp root, unrelated to any other directory's location,
# proving it is emptiness that triggers it, NOT which directory sits next to
# or under which. Moving DEPLOYABLE_UNIT_MAINTENANCE_SCRATCH_ROOT and
# _SCRATCH_REPOS_DIR to unrelated parents would not have fixed it -- the
# phantom Repository node's local_path (ReposDir/basename(FilesystemRoot))
# looked like a nesting bug but is a filesystemRepoPaths computed string, not
# evidence of where the directories actually sit. This is the real collector
# behavior, working as coded, not a layout defect in this file -- confirmed
# a finding, not a bug to route around by rearranging paths.
#
# Fix, scoped to this harness: seed the root with one hidden MARKER
# DIRECTORY (not a file -- the distinction is load-bearing, see the mkdir
# line below). Corrected mechanism (#6149 review; an earlier version of this
# comment had it backwards): repositoryRootLikeFromEntries counts EVERY
# directory entry toward childDirectories BEFORE it ever looks at the name --
# `if entry.IsDir() { childDirectories++; continue }` runs first, and the
# dot-prefix check after it only ever applies to the FILE branch. A hidden
# marker DIRECTORY still increments childDirectories, which is what flips
# the empty-root fallback (`return childDirectories == 0`) to false; a hidden
# marker FILE would be skipped by that dot-prefix check without incrementing
# anything, leaving childDirectories at 0 and the phantom-repo bug intact.
# The dot-prefix also makes discoverRepoRootsWithGitPriority's own recursion
# loop skip descending into the marker, but that is a separate, secondary
# property -- it keeps the marker from being evaluated as its own candidate
# repo, not what saves the outer root; the directory-counting above is.
# Verified directly against collector.DiscoverFilesystemRepositoryIDs: a bare
# empty temp dir returns `["."]`; the same dir with one
# `.eshu-no-repos-marker` SUBDIRECTORY returns `[]`.
#
# Args: work_dir
ifa_deployable_unit_live_init_maintenance_scratch() {
	local work_dir="$1"
	[[ -n "${DEPLOYABLE_UNIT_MAINTENANCE_SCRATCH_ROOT:-}" ]] && return 0
	export DEPLOYABLE_UNIT_MAINTENANCE_SCRATCH_ROOT="${work_dir}/deployable-unit-maintenance-scratch-root"
	export DEPLOYABLE_UNIT_MAINTENANCE_SCRATCH_REPOS_DIR="${work_dir}/deployable-unit-maintenance-scratch-repos"
	# .eshu-no-repos-marker MUST be a directory, not a file (`mkdir`, never
	# `touch`) -- see this function's header. A file would be skipped by
	# discovery's dot-prefix check without ever counting toward
	# childDirectories, silently reintroducing the phantom-repo bug this
	# marker exists to prevent.
	mkdir -p "${DEPLOYABLE_UNIT_MAINTENANCE_SCRATCH_ROOT}/.eshu-no-repos-marker" "${DEPLOYABLE_UNIT_MAINTENANCE_SCRATCH_REPOS_DIR}"
}

# ifa_deployable_unit_live_converge_bound is the number of bootstrap-index
# maintenance + drain cycles ifa_deployable_unit_live_converge_edges will run
# looking for deployable_unit_edges' one-edge exact set before giving up.
# Overridable by the environment. Default mirrors
# golden-corpus-maintenance-drains.sh's own three-cycle choice for a DEEPER
# (three-link) chain: this family's chain is two links deep
# (ifa_deployable_unit_live_run_maintenance_pass's header in
# ifa_deployable_unit_live.sh), so 3 buys the same one-cycle margin over the
# 2 that header says should suffice.
: "${ifa_deployable_unit_live_converge_bound:=3}"

# ifa_deployable_unit_live_converge_edges runs additional bootstrap-index
# maintenance + drain cycles, re-checking deployable_unit_edges' exact edge
# set after each one, until it converges or ifa_deployable_unit_live_converge_bound
# is exhausted. Callers already ran and drained ONE maintenance pass, and
# their own ifa_deployable_unit_live_assert already failed once -- this
# function picks up from there. Do not call it before that first attempt, and
# do not call it after a first attempt that already succeeded.
#
# WHY A BOUND, NOT A FIXED "RUN TWICE" (#5993 review). Origin's
# ingestion_reopen_correlation.go documents deployable_unit_correlation as
# having "no readiness retry of its own": it correlates nothing on the pass
# before deployment_mapping's own cross-repo resolution commits the resolved
# DEPLOYS_FROM relationship it reads, and catches up on "a later pass". That
# is an eventual-consistency contract -- the edge appears within N passes --
# not a fixed one-pass-behind guarantee. A hard-coded second pass happens to
# work today and would flake later if the ordering is ever genuinely
# concurrent rather than reliably one-pass-behind. A bound asserts what the
# contract actually promises and still fails non-vacuously if the edge never
# appears at all.
#
# TEMPORAL OBSERVABLE GAP (deferred, not solved here -- carried to a
# follow-up, not expanded into this loop). Every probe available here,
# including this one, can only read POST-DRAIN state: it cannot tell you
# whether deployable_unit_correlation's own attempt ran before or after
# deployment_mapping's resolved-relationship commit, only that, after N full
# passes, the edge either exists or does not. Catching a genuinely concurrent
# (not reliably one-pass-behind) race needs an ORDERING signal --
# correlation's attempt/completion time compared against the resolved row's
# commit time -- not a count, a scoped count, or a bounded retry: none of
# those can observe an event's position in time after the fact. A `resolved
# DEPLOYS_FROM count` diagnostic (however scoped) answers "does the row
# exist", never "did correlation see it in time" -- do not read either as
# proof correlation had material to work with.
#
# EACH RETRY PASS RESETS attempt_count TO 0 (found live in CI, #6149): the
# maintenance pass this loop runs on every retry reopens every succeeded row
# crossScopeCorrelationReopenDomains covers, including a row a fault cell's
# OWN kill-and-reclaim already recovered -- ReopenSucceeded
# (go/internal/storage/postgres/reducer_queue_replay.go) sets attempt_count
# = 0 unconditionally on reopen, "the start of a fresh repair cycle" by its
# own doc comment. Any counter-based assertion (attempt_count-derived retry
# proof, e.g. ifa_fault_assert_retried_above) MUST be captured BEFORE this
# loop runs, not after: capturing after reads whatever the last reopen reset
# it to, not the evidence the recovery actually produced. This is not a rare
# edge case for the deployable-unit family specifically -- it converges on
# pass 2 as its NORMAL path, so a counter read after this loop engages reads
# the reset far more often than it reads the original evidence.
#
# Args: pass_label_prefix bin_dir log_dir expected_edges drain_cmd...
# drain_cmd is invoked once per extra pass as "$@" followed by ONE appended
# argument, "${pass_label_prefix}-pass${extra_pass}" -- e.g.
# `run_drain_gate baseline_deployable_unit baseline_deployable_unit-pass2`
# for the fault-injection cells (run_drain_gate reads only its first
# argument, so the appended label is inert there) or
# `ifa_deployable_unit_live_drain_retry "$bin_dir" "$log_dir" "$drain_timeout"
# primary-pass2` for the standalone determinism-gate cell, whose wrapper
# turns that label into a drain label unique per retry (see
# ifa_deployable_unit_live_drain_retry's own header, in
# ifa_deployable_unit_live.sh, for why a constant label there used to
# silently truncate the wrong pass's logs). Each caller's own drain
# convention stays intact; this loop only adds the retry-and-recheck around
# it, plus the one extra argument every drain_cmd must tolerate.
#
# Return codes distinguish WHY this returned non-zero, since "did not
# converge" and "a retry pass itself crashed" are different defects that used
# to share one caller-facing die() message (a crashed bootstrap-index read as
# an eventual-consistency failure):
#   0 - converged within the bound.
#   1 - the bound was exhausted without converging (genuine non-convergence;
#       the sub-steps below all succeeded on every retry).
#   2 - a maintenance pass ITSELF failed on a retry (bootstrap-index crashed
#       or errored; see the maintenance-pass log this prints above).
#   3 - a retry's drain_cmd ITSELF failed (see the drain log above).
ifa_deployable_unit_live_converge_edges() {
	local pass_label_prefix="$1" bin_dir="$2" log_dir="$3" expected_edges="$4"
	shift 4
	local extra_pass pass_label
	for extra_pass in $(seq 2 "${ifa_deployable_unit_live_converge_bound}"); do
		pass_label="${pass_label_prefix}-pass${extra_pass}"
		printf 'deployable_unit_edges: convergence retry %s/%s (eventual consistency: deployable_unit_correlation has no readiness retry of its own; see ingestion_reopen_correlation.go)\n' \
			"${extra_pass}" "${ifa_deployable_unit_live_converge_bound}"
		if ! ifa_deployable_unit_live_run_maintenance_pass "${pass_label}" "${bin_dir}" "${log_dir}"; then
			echo "deployable_unit_edges: convergence retry ${extra_pass}/${ifa_deployable_unit_live_converge_bound} aborted -- the bootstrap-index maintenance pass itself failed; this is a crash, not an eventual-consistency timeout" >&2
			return 2
		fi
		if ! "$@" "${pass_label}"; then
			echo "deployable_unit_edges: convergence retry ${extra_pass}/${ifa_deployable_unit_live_converge_bound} aborted -- the retry's drain itself failed; this is a drain defect, not an eventual-consistency timeout" >&2
			return 3
		fi
		if ifa_deployable_unit_live_assert "${bin_dir}" "${expected_edges}"; then
			printf 'deployable_unit_edges: converged on maintenance pass %s/%s\n' "${extra_pass}" "${ifa_deployable_unit_live_converge_bound}"
			return 0
		fi
	done
	echo "deployable_unit_edges: edge set did not converge to the expected set within ${ifa_deployable_unit_live_converge_bound} maintenance passes -- eventual consistency did not converge, not a one-shot admission failure" >&2
	return 1
}
# ifa_deployable_unit_live_drain_retry / ifa_deployable_unit_live_drain live
# here (moved out of ifa_deployable_unit_live.sh to keep that file
# under the repository's 500-line cap): the primary drain, its
# uniquely-labelled convergence retries, and the pre-maintenance
# quiescence mode (#6184) share one home with the converge loop that
# calls them.

# ifa_deployable_unit_live_drain_retry adapts ifa_deployable_unit_live_drain
# to the drain_cmd calling convention ifa_deployable_unit_live_converge_edges
# invokes ("$@" plus one appended pass-label argument, see that function's doc
# comment): it turns the appended per-retry pass label into a UNIQUE drain
# label ("post-${pass_label}") instead of the constant "post"
# ifa_deployable_unit_live_run_standalone_cell used before this existed. Only
# the standalone determinism-gate cell needs this -- the fault-injection
# cells pass run_drain_gate, which polls an already-running projector/reducer
# rather than starting fresh ones per retry, so it never truncates a log
# (see this function's own header two functions below for the truncation this
# fixes).

ifa_deployable_unit_live_drain_retry() {
	local bin_dir="$1" log_dir="$2" drain_timeout="$3" pass_label="$4"
	ifa_deployable_unit_live_drain "post-${pass_label}" "${bin_dir}" "${log_dir}" "${drain_timeout}"
}

# ifa_deployable_unit_live_drain runs projector + reducer in the background
# and polls the gate to the B-12 residual bound, exactly like every other
# Ifá live cell's drain step. label distinguishes the primary drain (before
# the maintenance pass) from the post-maintenance drain in the logs.
#
# label MUST be unique per invocation within a cell: ifa_det_start_bg opens
# its log file with `>` (truncate), so calling this twice with the same label
# -- e.g. the constant "post" on every convergence retry, the bug this
# comment now documents -- overwrites reducer-deployable-unit-${label}.log /
# projector-deployable-unit-${label}.log each time. On failure only the LAST
# call's log survives; the pass where the family should have converged is
# gone. ifa_deployable_unit_live_drain_retry above is how convergence retries
# get a unique label instead of reusing this one.
ifa_deployable_unit_live_drain() {
	local label="$1" bin_dir="$2" log_dir="$3" drain_timeout="$4"
	local projector_pid reducer_pid pre_flag=()
	# The "pre" drain runs before the maintenance pass opens the fail-closed
	# readiness gates (#6184), so it waits for quiescence -- no live work
	# left -- instead of convergence. Every other label stays strict.
	if [[ "${label}" == "pre" ]]; then
		pre_flag=(-drain-allow-readiness-deferred)
	fi
	printf '\n=== deployable_unit_edges (%s): drain projector + reducer ===\n' "${label}"
	ifa_det_start_bg "${log_dir}" "projector-deployable-unit-${label}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-deployable-unit-${label}" reducer_pid "${bin_dir}/eshu-reducer"
	if ! "${bin_dir}/eshu-golden-corpus-gate" \
		-phase=drains \
		-snapshot=testdata/golden/e2e-20repo-snapshot.json \
		-drain-timeout="${drain_timeout}" "${pre_flag[@]:-}"; then
		tail -30 "${log_dir}/reducer-deployable-unit-${label}.log" || true
		tail -30 "${log_dir}/projector-deployable-unit-${label}.log" || true
		echo "deployable_unit_edges (${label}): drain did not reach the snapshot's residual bound within ${drain_timeout}" >&2
		return 1
	fi
	kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true
}
