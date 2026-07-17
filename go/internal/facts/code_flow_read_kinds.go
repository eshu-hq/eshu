// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

// CodeFlowReadFactKinds returns the canonical, ordered set of fact kinds the
// cumulative-active code-flow read selects and its repo-anchored partial index
// covers. It is the single source of truth that keeps three sites in lockstep:
//
//   - query.listActiveCodeFlowFactsSQL's literal `fact_kind IN (...)` conjunct,
//     which unlocks the partial index under a generic prepared plan (#5280);
//   - the fact_records_code_flow_repo_idx partial `WHERE fact_kind IN (...)`
//     predicate, which only accelerates the kinds named in it; and
//   - query.codeFlowFactKinds, whose union across every CodeFlowKind is exactly
//     this set (the per-read $1 subset is always drawn from here).
//
// Adding a code-flow fact kind here forces the lockstep guard tests in the
// query and postgres packages to fail until both SQL sites cover it, so the
// index can never silently miss a kind the read queries (which would over-fetch
// that kind through the old all-scope heap filter while the write path still
// paid the index's maintenance cost). Returns a fresh slice so callers cannot
// mutate the canonical order.
func CodeFlowReadFactKinds() []string {
	return []string{
		CodeTaintEvidenceFactKind,
		CodeInterprocEvidenceFactKind,
		CodeDataflowFunctionFactKind,
	}
}
