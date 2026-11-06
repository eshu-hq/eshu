// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factimpact

// MaxCVEIDsPerStatement is the most ids any key probe on IndexVulnerabilityCVEV2
// binds per statement, whatever its kinds (#9994). A longer list runs as several
// statements in the load's one snapshot.
//
// Before migration 281 the planner had no statistics for payload->>'cve_id'
// across fact_records, so it charged every id the default 0.5% of the kinds'
// rows: below 200 ids it summed those shares, which reach 86% at 172 ids,
// and on a custom plan the key probe then cost more than walking the whole
// advisory index. From 200 ids it switched to a smaller estimate and the
// probe won again, until the same crossing at 389 ids. QA measured the walk
// at 172 to 199 ids and from 389. It ran past a 30 s limit on every list
// timed, at 600 ids and up.
//
// The fall at 200 is arithmetic. The crossing is a cost comparison that moves with
// the correlation stored for the advisory index's expression, so an ANALYZE can move
// 172. This bound is measured against today's statistics and is not a guarantee.
//
// A list's last statement can be any length up to this bound, so the bound has to
// sit below 172, not merely plan well itself: 240 plans on the key probe and a
// 180-id tail does not. At 120 the walk costs 1.41 times the key probe, and every
// shorter length costs less. Do not raise this without planning every length up to
// the new value, under both plan modes at page costs 1.1 and 4, on QA or on a
// heap-proportioned fixture (docs/internal/evidence/9994-cve-key-probe-chunking.md).
//
// Migration 281 now carries extended statistics on this expression (#10050),
// which hold the probe on its index past the walk locally — but the estimate
// stays ~10x under actual and QA-scale re-derivation is owed, so this bound
// stays until that proof lands
// (docs/internal/evidence/10050-cve-key-probe-statistics.md).
const MaxCVEIDsPerStatement = 120
