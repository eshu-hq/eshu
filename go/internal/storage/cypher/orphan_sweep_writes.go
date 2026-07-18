// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "fmt"

// This file holds the three key-anchored orphan-sweep writes (S3 clear, S4
// mark, S5 sweep). Each takes the exact key list orphan_sweep.go computed
// from the S1/S2 anti-join and applies via UNWIND $keys AS candidate_key
// MATCH (n:Label {key: candidate_key}) -- never a relationship-existence
// predicate, dynamic label, or DETACH DELETE.

// BuildClearOrphanMarkerStatement builds the S3 write: clears the orphan
// marker from every supplied key. Callers must only pass keys already known
// to be both marked and currently connected (marked ∩ connected).
func BuildClearOrphanMarkerStatement(label OrphanSweepLabel, keys []string) (Statement, bool) {
	match, ok := orphanLabelMatch(label)
	if !ok {
		return Statement{}, false
	}
	key, ok := orphanSweepIdentityKey(label)
	if !ok {
		return Statement{}, false
	}
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher: fmt.Sprintf(`UNWIND $keys AS candidate_key
MATCH (n:%s {%s: candidate_key})
WHERE %s
  AND n.eshu_orphan_observed_at_unix IS NOT NULL
REMOVE n.eshu_orphan_observed_at_unix`, match, key, orphanSweepNodeGuard(label)),
		Parameters: map[string]any{
			"keys": keys,
		},
	}, true
}

// BuildMarkOrphanNodesStatement builds the S4 write: stamps the orphan
// marker on every supplied key. Callers must only pass keys already known to
// be both orphaned and currently unmarked (orphans ∩ unmarked), bounded
// to the policy batch limit.
func BuildMarkOrphanNodesStatement(label OrphanSweepLabel, keys []string, observedAtUnix int64) (Statement, bool) {
	match, ok := orphanLabelMatch(label)
	if !ok {
		return Statement{}, false
	}
	key, ok := orphanSweepIdentityKey(label)
	if !ok {
		return Statement{}, false
	}
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher: fmt.Sprintf(`UNWIND $keys AS candidate_key
MATCH (n:%s {%s: candidate_key})
WHERE %s
  AND n.eshu_orphan_observed_at_unix IS NULL
SET n.eshu_orphan_observed_at_unix = $observed_at_unix`, match, key, orphanSweepNodeGuard(label)),
		Parameters: map[string]any{
			"keys":             keys,
			"observed_at_unix": observedAtUnix,
		},
	}, true
}

// BuildSweepOrphanNodesStatement builds the S5 write: deletes every supplied
// key without DETACH DELETE. Callers pass keys already known to be orphaned,
// marked, aged past the TTL cutoff, and re-verified disconnected by the TOCTOU
// guard immediately before this statement is issued. The write re-applies the
// ownership/class guard and the marker+age predicate so a key that changed
// ownership (e.g. re-created by canonical projection) or lost its marker
// between the read and this delete is skipped, not deleted.
func BuildSweepOrphanNodesStatement(label OrphanSweepLabel, keys []string, cutoffUnix int64) (Statement, bool) {
	match, ok := orphanLabelMatch(label)
	if !ok {
		return Statement{}, false
	}
	key, ok := orphanSweepIdentityKey(label)
	if !ok {
		return Statement{}, false
	}
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher: fmt.Sprintf(`UNWIND $keys AS candidate_key
MATCH (n:%s {%s: candidate_key})
WHERE %s
  AND n.eshu_orphan_observed_at_unix IS NOT NULL
  AND n.eshu_orphan_observed_at_unix <= $cutoff_unix
DELETE n`, match, key, orphanSweepNodeGuard(label)),
		Parameters: map[string]any{
			"keys":        keys,
			"cutoff_unix": cutoffUnix,
		},
	}, true
}
