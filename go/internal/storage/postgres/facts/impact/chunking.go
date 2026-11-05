// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factimpact

// MaxCVEIDsPerStatement is the most ids any key probe on IndexVulnerabilityCVEV2
// binds per statement, whatever its kinds (#9994). A longer list runs as several
// statements in the load's one snapshot.
//
// The planner has no statistics for payload->>'cve_id' across fact_records, so it
// charges every id the default 0.5% of the kinds' rows. Below 200 ids it sums those
// shares, which reaches 86% at 172 ids, and on a custom plan the key probe then
// costs more than walking the whole advisory index. From 200 ids it switches to a
// smaller estimate and the probe wins again, until the same crossing at 389 ids. QA
// measured the walk at 172 to 199 ids and from 389. It ran past a 30 s limit on every
// list timed, at 600 ids and up.
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
const MaxCVEIDsPerStatement = 120
