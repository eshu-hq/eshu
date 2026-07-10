// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perfcontract

import "time"

// LocalEnvelope is the typed view of one row of the local performance envelope
// table (local-performance-envelope.md). Durations are the documented upper
// bounds. Two fields — dead-code scan and reducer bulk-write — need an active
// repo / 50K-fact load and so are operator-gated, not hermetically measurable.
type LocalEnvelope struct {
	Profile               string
	ColdStart             time.Duration
	WarmRestart           time.Duration
	SingleFileReindex     time.Duration
	PrimaryQueryP95       time.Duration // symbol lookup (lightweight) / call-chain (authoritative)
	DeadCodeScan          time.Duration // authoritative only; 0 for lightweight
	ReducerBulkWriteBatch time.Duration // authoritative only; 0 for lightweight
}

// LocalLightweight returns the documented local_lightweight envelope.
func LocalLightweight() LocalEnvelope {
	return LocalEnvelope{
		Profile:           "local_lightweight",
		ColdStart:         5 * time.Second,
		WarmRestart:       2 * time.Second,
		SingleFileReindex: 2 * time.Second,
		PrimaryQueryP95:   500 * time.Millisecond,
	}
}

// LocalAuthoritative returns the documented local_authoritative envelope.
func LocalAuthoritative() LocalEnvelope {
	return LocalEnvelope{
		Profile:               "local_authoritative",
		ColdStart:             15 * time.Second,
		WarmRestart:           5 * time.Second,
		SingleFileReindex:     5 * time.Second,
		PrimaryQueryP95:       2 * time.Second,
		DeadCodeScan:          10 * time.Second,
		ReducerBulkWriteBatch: 10 * time.Second,
	}
}

// localEnvelopeThresholds binds the envelope numbers to the exact phrases in
// local-performance-envelope.md. The phrases are the table cells' wording; the
// lockstep test fails if any drifts.
func localEnvelopeThresholds() []Threshold {
	const doc = DocLocalEnvelope
	return []Threshold{
		// local_lightweight row.
		{Name: "lightweight_cold_start", Doc: doc, Phrase: "cold start under `5s`", Token: "5s", Value: 5, Unit: "s", Enforcement: EnforcementOperatorGated},
		{Name: "lightweight_warm_restart", Doc: doc, Phrase: "warm restart under `2s`", Token: "2s", Value: 2, Unit: "s", Enforcement: EnforcementOperatorGated},
		{Name: "lightweight_symbol_lookup_p95", Doc: doc, Phrase: "exact symbol lookup p95 under `500ms`", Token: "500ms", Value: 500, Unit: "ms", Enforcement: EnforcementOperatorGated},
		{Name: "lightweight_content_search_p95", Doc: doc, Phrase: "content search p95 under `800ms`", Token: "800ms", Value: 800, Unit: "ms", Enforcement: EnforcementOperatorGated},
		{Name: "lightweight_complexity_query_p95", Doc: doc, Phrase: "complexity query p95 under `1500ms`", Token: "1500ms", Value: 1500, Unit: "ms", Enforcement: EnforcementOperatorGated},
		{Name: "lightweight_single_file_reindex", Doc: doc, Phrase: "single-file reindex to visible search update under `2s`", Token: "2s", Value: 2, Unit: "s", Enforcement: EnforcementOperatorGated},
		// local_authoritative row.
		{Name: "authoritative_cold_start", Doc: doc, Phrase: "cold start under `15s`", Token: "15s", Value: 15, Unit: "s", Enforcement: EnforcementOperatorGated},
		{Name: "authoritative_warm_restart", Doc: doc, Phrase: "warm restart under `5s`", Token: "5s", Value: 5, Unit: "s", Enforcement: EnforcementOperatorGated},
		{Name: "authoritative_call_chain_p95", Doc: doc, Phrase: "transitive caller and call-chain p95 under `2s`", Token: "2s", Value: 2, Unit: "s", Enforcement: EnforcementOperatorGated},
		{Name: "authoritative_dead_code_scan", Doc: doc, Phrase: "active-repo dead-code scan under `10s`", Token: "10s", Value: 10, Unit: "s", Enforcement: EnforcementOperatorGated},
		{Name: "authoritative_reducer_bulk_write", Doc: doc, Phrase: "reducer bulk write batch under `10s` for `50K` facts", Token: "10s", Value: 10, Unit: "s", Enforcement: EnforcementOperatorGated},
		{Name: "authoritative_single_file_reindex", Doc: doc, Phrase: "single-file reindex to visible graph update under `5s`", Token: "5s", Value: 5, Unit: "s", Enforcement: EnforcementOperatorGated},
		// `make prove` (issue #4397, P4) credential-free common path budget: the
		// Ifá contract-layer test, both determinism/dead-letter-matrix hermetic
		// structural mirrors, and the `ifa coverage` reconcile. The Docker matrix
		// (Layer 2) is excluded — its wall time varies by machine/Docker state and
		// is reported informationally, never budgeted.
		{Name: "prove_common_path_wall_time", Doc: doc, Phrase: "the `make prove` credential-free common path stays under `5s`", Token: "5s", Value: 5, Unit: "s", Enforcement: EnforcementOperatorGated},
	}
}
