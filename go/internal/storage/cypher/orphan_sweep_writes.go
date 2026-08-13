// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"strings"
)

// This file holds the three key-anchored orphan-sweep writes (S3 clear, S4
// mark, S5 sweep). Each takes the exact key list orphan_sweep.go computed
// from the S1/S2 anti-join and applies via UNWIND $keys AS candidate_key
// MATCH (n:Label {key: candidate_key}) -- never a relationship-existence
// predicate, dynamic label, or DETACH DELETE.
//
// A label whose identity is more than one property (Module: name plus lang)
// binds its anchor property inline in the MATCH pattern, so the write still
// resolves through that property's schema index, and binds every remaining
// property in the WHERE clause. Binding only the anchor would make each write
// touch every same-named node in the class -- clearing, marking, or deleting a
// Go module because its Python namesake was the orphan.

// buildKeyAnchoredOrphanStatement assembles one key-anchored write: the shared
// UNWIND/MATCH prologue for the label's identity, the caller's extra WHERE
// terms, and the mutation clause. Keeping the three writes on one builder is
// what keeps their identity binding impossible to get right in one place and
// wrong in another.
func buildKeyAnchoredOrphanStatement(
	label OrphanSweepLabel,
	keys []orphanSweepKey,
	extraWhere []string,
	mutation string,
	parameters map[string]any,
) (Statement, bool) {
	match, ok := orphanLabelMatch(label)
	if !ok {
		return Statement{}, false
	}
	properties, ok := orphanSweepIdentityProperties(label)
	if !ok {
		return Statement{}, false
	}

	params := map[string]any{}
	for name, value := range parameters {
		params[name] = value
	}

	anchor := "candidate_key"
	where := []string{orphanSweepNodeGuard(label)}
	if len(properties) == 1 {
		params["keys"] = singlePropertyKeyParams(keys)
	} else {
		anchor = "candidate_key.key_0"
		params["keys"] = compositeKeyParams(properties, keys)
		for i := 1; i < len(properties); i++ {
			where = append(where,
				fmt.Sprintf("%s = candidate_key.key_%d", orphanSweepKeyExpr(properties[i], i), i))
		}
	}
	where = append(where, extraWhere...)

	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher: fmt.Sprintf(`UNWIND $keys AS candidate_key
MATCH (n:%s {%s: %s})
WHERE %s
%s`, match, properties[0], anchor, strings.Join(where, "\n  AND "), mutation),
		Parameters: params,
	}, true
}

// BuildClearOrphanMarkerStatement builds the S3 write: clears the orphan
// marker from every supplied key. Callers must only pass keys already known
// to be both marked and currently connected (marked ∩ connected).
func BuildClearOrphanMarkerStatement(label OrphanSweepLabel, keys []orphanSweepKey) (Statement, bool) {
	return buildKeyAnchoredOrphanStatement(label, keys,
		[]string{"n.eshu_orphan_observed_at_unix IS NOT NULL"},
		"REMOVE n.eshu_orphan_observed_at_unix",
		nil)
}

// BuildMarkOrphanNodesStatement builds the S4 write: stamps the orphan
// marker on every supplied key. Callers must only pass keys already known to
// be both orphaned and currently unmarked (orphans ∩ unmarked), bounded
// to the policy batch limit.
func BuildMarkOrphanNodesStatement(label OrphanSweepLabel, keys []orphanSweepKey, observedAtUnix int64) (Statement, bool) {
	return buildKeyAnchoredOrphanStatement(label, keys,
		[]string{"n.eshu_orphan_observed_at_unix IS NULL"},
		"SET n.eshu_orphan_observed_at_unix = $observed_at_unix",
		map[string]any{"observed_at_unix": observedAtUnix})
}

// BuildSweepOrphanNodesStatement builds the S5 write: deletes every supplied
// key without DETACH DELETE. Callers pass keys already known to be orphaned,
// marked, aged past the TTL cutoff, and re-verified disconnected by the TOCTOU
// guard immediately before this statement is issued. The write re-applies the
// ownership/class guard, the full identity binding, and the marker+age
// predicate so a key that changed ownership (e.g. re-created by canonical
// projection) or lost its marker between the read and this delete is skipped,
// not deleted.
func BuildSweepOrphanNodesStatement(label OrphanSweepLabel, keys []orphanSweepKey, cutoffUnix int64) (Statement, bool) {
	return buildKeyAnchoredOrphanStatement(label, keys,
		[]string{
			"n.eshu_orphan_observed_at_unix IS NOT NULL",
			"n.eshu_orphan_observed_at_unix <= $cutoff_unix",
		},
		"DELETE n",
		map[string]any{"cutoff_unix": cutoffUnix})
}
