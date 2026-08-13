// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"math"
	"strings"
)

// This file holds the two bounded orphan-sweep reads (S1 candidates, S2
// connected keys) and the store helpers that run them. Neither statement
// contains a relationship-existence predicate: S1 has no relationship clause
// at all, and S2 is the one relationship primitive proven reliable on both
// pinned NornicDB backends -- a concrete relationship-variable MATCH anchored
// on caller-supplied keys. orphan_sweep.go computes orphan/connected status as
// a Go-side anti-join over these two reads' results.

// orphanSweepCandidate is one S1 row: a node eligible for orphan sweeping,
// with its current marker state (nil when unmarked).
type orphanSweepCandidate struct {
	key        orphanSweepKey
	observedAt *int64
}

// orphanSweepKeyExpr renders the read expression for one identity property.
// Every property after the first goes through coalesce(..., ”) because it may
// be absent on a node projected by an older release: a canonical Module written
// before the (name, lang) cutover settled its language with
// `SET m.lang = coalesce(m.lang, row.language)`, which removes the property
// outright when the row carried no language. On the pinned backend a bare
// `n.lang > $cursor_1` never matches such a node, so it would sit outside every
// page forever -- the same under-deletion the composite key exists to remove.
// The leading property is always present (it is the MERGE anchor) and is left
// bare so single-property labels keep byte-identical Cypher.
func orphanSweepKeyExpr(property string, position int) string {
	if position == 0 {
		return "n." + property
	}
	return fmt.Sprintf("coalesce(n.%s, '')", property)
}

// BuildCandidateOrphanNodesQuery builds the S1 read: every node carrying this
// label's evidence_source predicate, with its identity key and current
// orphan marker. It contains no relationship predicate; connectivity is
// resolved separately by BuildConnectedKeysQuery. The LIMIT bounds how many
// candidates one cycle considers -- a label with more matching nodes than
// limit converges over multiple cycles rather than in one pass.
//
// The read is ordered by the identity key and takes only keys strictly greater
// than the caller's cursor, so a label with more matching nodes than limit is
// paged deterministically across cycles instead of re-reading the same
// arbitrary window. That guarantees forward progress past a window that happens
// to be entirely connected: the cursor advances every cycle regardless of
// orphan state, and orphan_sweep.go wraps it back to the label start once a
// short page signals the end.
//
// Module's key is (name, lang), so its cursor is a tuple and the predicate is
// the standard keyset comparison -- past the leading property, or equal there
// and past the next. A name-only cursor would resume strictly after a name and
// skip whichever of its languages fell on the far side of a page boundary.
// The comparison is expressed as separate property comparisons rather than
// ORDER BY over the projected aliases: on the pinned backend
// eshu-nornicdb-pr290:3722b483c02c, `ORDER BY <return alias>` is not honored,
// while ordering the WITH-projected values is (verified through the Bolt
// driver, five rows across three names paged two at a time).
func BuildCandidateOrphanNodesQuery(label OrphanSweepLabel, limit int, cursor orphanSweepKey) (Statement, bool) {
	match, ok := orphanLabelMatch(label)
	if !ok {
		return Statement{}, false
	}
	properties, ok := orphanSweepIdentityProperties(label)
	if !ok {
		return Statement{}, false
	}
	parameters := map[string]any{
		"limit": normalizePositiveInt(limit, defaultOrphanSweepCountLimit),
	}

	if len(properties) == 1 {
		key := properties[0]
		parameters["cursor"] = cursorValue(cursor, 0)
		return Statement{
			Operation: OperationCanonicalRetract,
			Cypher: fmt.Sprintf(`MATCH (n:%s)
WHERE %s
  AND n.%s > $cursor
RETURN n.%s AS key, n.eshu_orphan_observed_at_unix AS observed_at
ORDER BY n.%s
LIMIT $limit`, match, orphanSweepNodeGuard(label), key, key, key),
			Parameters: parameters,
		}, true
	}

	disjuncts := make([]string, 0, len(properties))
	projections := make([]string, 0, len(properties)+1)
	aliases := make([]string, 0, len(properties))
	for i, property := range properties {
		parameters[fmt.Sprintf("cursor_%d", i)] = cursorValue(cursor, i)
		terms := make([]string, 0, i+1)
		for j := 0; j < i; j++ {
			terms = append(terms, fmt.Sprintf("%s = $cursor_%d", orphanSweepKeyExpr(properties[j], j), j))
		}
		terms = append(terms, fmt.Sprintf("%s > $cursor_%d", orphanSweepKeyExpr(property, i), i))
		disjunct := strings.Join(terms, " AND ")
		if i > 0 {
			disjunct = "(" + disjunct + ")"
		}
		disjuncts = append(disjuncts, disjunct)
		alias := fmt.Sprintf("key_%d", i)
		aliases = append(aliases, alias)
		projections = append(projections, fmt.Sprintf("%s AS %s", orphanSweepKeyExpr(property, i), alias))
	}
	projections = append(projections, "n.eshu_orphan_observed_at_unix AS observed_at")

	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher: fmt.Sprintf(`MATCH (n:%s)
WHERE %s
  AND (%s)
WITH %s
RETURN %s, observed_at
ORDER BY %s
LIMIT $limit`,
			match,
			orphanSweepNodeGuard(label),
			strings.Join(disjuncts, " OR "),
			strings.Join(projections, ", "),
			strings.Join(aliases, ", "),
			strings.Join(aliases, ", ")),
		Parameters: parameters,
	}, true
}

// cursorValue returns the cursor's value at position, or the empty string when
// the cursor is unset (start of the label). The empty string sorts before every
// real value, which is what makes an unset cursor mean "from the beginning".
func cursorValue(cursor orphanSweepKey, position int) string {
	if position >= len(cursor) {
		return ""
	}
	return cursor[position]
}

// BuildConnectedKeysQuery builds the S2 read: for each supplied identity key,
// whether that node currently has any relationship. This is the anti-join's
// only relationship predicate, and the only shape proven reliable on both
// pinned NornicDB backends: a concrete relationship variable (`-[r]-`) in a
// MATCH anchored on a caller-supplied key, never a negated or counted
// pattern-existence predicate.
//
// The UNWIND binding variable is deliberately named candidate_key rather than
// key: reusing the RETURN alias name for the UNWIND variable silently returns
// zero rows on the pinned NornicDB backends instead of erroring (see
// docs/public/reference/nornicdb-pitfalls.md).
func BuildConnectedKeysQuery(label OrphanSweepLabel, keys []orphanSweepKey) (Statement, bool) {
	match, ok := orphanLabelMatch(label)
	if !ok {
		return Statement{}, false
	}
	properties, ok := orphanSweepIdentityProperties(label)
	if !ok {
		return Statement{}, false
	}
	// For a label whose identity key is not unique across node classes (Module:
	// name is shared between canonical imports and semantic entities), restrict
	// the connectivity check to the class this sweep owns, so a connected
	// same-name node of the OTHER class does not mask a true orphan.
	class := orphanSweepClassPredicate(label)

	if len(properties) == 1 {
		key := properties[0]
		classWhere := ""
		if class != "" {
			classWhere = "\nWHERE " + class
		}
		return Statement{
			Operation: OperationCanonicalRetract,
			Cypher: fmt.Sprintf(`UNWIND $keys AS candidate_key
MATCH (n:%s {%s: candidate_key})-[r]-(m)%s
RETURN DISTINCT n.%s AS key`, match, key, classWhere, key),
			Parameters: map[string]any{
				"keys": singlePropertyKeyParams(keys),
			},
		}, true
	}

	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher: fmt.Sprintf(`UNWIND $keys AS candidate_key
MATCH (n:%s {%s: candidate_key.key_0})-[r]-(m)
WHERE %s
RETURN DISTINCT %s`,
			match,
			properties[0],
			strings.Join(compositeKeyPredicates(properties, class), "\n  AND "),
			strings.Join(compositeKeyProjections(properties), ", ")),
		Parameters: map[string]any{
			"keys": compositeKeyParams(properties, keys),
		},
	}, true
}

// singlePropertyKeyParams flattens single-property keys back to the plain
// string list the pre-composite parameter shape used, so nothing changes on the
// wire for those labels.
func singlePropertyKeyParams(keys []orphanSweepKey) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, cursorValue(key, 0))
	}
	return out
}

// compositeKeyParams renders keys as the UNWIND row shape a composite-key
// statement binds: one map per key with key_0, key_1, ... entries.
func compositeKeyParams(properties []string, keys []orphanSweepKey) []map[string]any {
	out := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		row := make(map[string]any, len(properties))
		for i := range properties {
			row[fmt.Sprintf("key_%d", i)] = cursorValue(key, i)
		}
		out = append(out, row)
	}
	return out
}

// compositeKeyPredicates returns the WHERE terms binding every identity
// property after the anchor, plus the class predicate when the label has one.
// The anchor property is bound inline in the MATCH pattern so the write still
// resolves through that property's schema index instead of scanning the label.
func compositeKeyPredicates(properties []string, class string) []string {
	terms := make([]string, 0, len(properties))
	if class != "" {
		terms = append(terms, class)
	}
	for i := 1; i < len(properties); i++ {
		terms = append(terms, fmt.Sprintf("%s = candidate_key.key_%d", orphanSweepKeyExpr(properties[i], i), i))
	}
	return terms
}

// compositeKeyProjections returns the RETURN expressions that read a composite
// key back out, aliased key_0, key_1, ... to match the UNWIND row shape.
func compositeKeyProjections(properties []string) []string {
	out := make([]string, 0, len(properties))
	for i, property := range properties {
		out = append(out, fmt.Sprintf("%s AS key_%d", orphanSweepKeyExpr(property, i), i))
	}
	return out
}

// readCandidateOrphanNodes runs BuildCandidateOrphanNodesQuery and decodes
// its rows.
func (s *OrphanSweepStore) readCandidateOrphanNodes(
	ctx context.Context,
	label OrphanSweepLabel,
	limit int,
	cursor orphanSweepKey,
) ([]orphanSweepCandidate, error) {
	stmt, ok := BuildCandidateOrphanNodesQuery(label, limit, cursor)
	if !ok {
		return nil, fmt.Errorf("unsupported orphan sweep label %q", label)
	}
	properties, _ := orphanSweepIdentityProperties(label)
	rows, err := s.Reader.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return nil, fmt.Errorf("read candidate orphan nodes for %s: %w", label, err)
	}
	out := make([]orphanSweepCandidate, 0, len(rows))
	for _, row := range rows {
		key, err := decodeOrphanSweepKeyRow(properties, row)
		if err != nil {
			return nil, fmt.Errorf("read candidate orphan nodes for %s: %w", label, err)
		}
		candidate := orphanSweepCandidate{key: key}
		if observedAt, ok := int64Count(row["observed_at"]); ok {
			candidate.observedAt = &observedAt
		}
		out = append(out, candidate)
	}
	return out, nil
}

// decodeOrphanSweepKeyRow reads one identity key out of a result row: the `key`
// column for a single-property label, or key_0, key_1, ... for a composite one.
//
// Only the anchor value is required to be non-empty. Every later property is
// projected through coalesce(..., ”), so an empty string there is a real
// value -- a Module whose language could not be determined owns its own node
// and must be swept under its own key, not skipped.
func decodeOrphanSweepKeyRow(properties []string, row map[string]any) (orphanSweepKey, error) {
	if len(properties) == 1 {
		value, ok := row["key"].(string)
		if !ok || value == "" {
			return nil, fmt.Errorf("unexpected key type %T", row["key"])
		}
		return orphanSweepKey{value}, nil
	}
	key := make(orphanSweepKey, 0, len(properties))
	for i := range properties {
		column := fmt.Sprintf("key_%d", i)
		value, ok := row[column].(string)
		if !ok {
			if row[column] == nil && i > 0 {
				value = ""
			} else {
				return nil, fmt.Errorf("unexpected %s type %T", column, row[column])
			}
		}
		if i == 0 && value == "" {
			return nil, fmt.Errorf("empty anchor key %s", column)
		}
		key = append(key, value)
	}
	return key, nil
}

// readConnectedKeys runs BuildConnectedKeysQuery for keys and decodes its
// rows, splitting keys into defaultOrphanSweepConnectedKeysChunkSize-sized
// round trips (see that constant's doc comment for why). It returns (nil,
// nil) without any round trip when keys is empty.
func (s *OrphanSweepStore) readConnectedKeys(
	ctx context.Context,
	label OrphanSweepLabel,
	keys []orphanSweepKey,
) ([]orphanSweepKey, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	if len(keys) <= defaultOrphanSweepConnectedKeysChunkSize {
		return s.readConnectedKeysChunk(ctx, label, keys)
	}

	seen := make(map[string]bool, len(keys))
	out := make([]orphanSweepKey, 0, len(keys))
	for start := 0; start < len(keys); start += defaultOrphanSweepConnectedKeysChunkSize {
		end := start + defaultOrphanSweepConnectedKeysChunkSize
		if end > len(keys) {
			end = len(keys)
		}
		chunk, err := s.readConnectedKeysChunk(ctx, label, keys[start:end])
		if err != nil {
			return nil, err
		}
		for _, k := range chunk {
			encoded := k.encode()
			if !seen[encoded] {
				seen[encoded] = true
				out = append(out, k)
			}
		}
	}
	return out, nil
}

// readConnectedKeysChunk runs BuildConnectedKeysQuery for exactly one
// round trip's worth of keys (never more than
// defaultOrphanSweepConnectedKeysChunkSize) and decodes its rows.
func (s *OrphanSweepStore) readConnectedKeysChunk(
	ctx context.Context,
	label OrphanSweepLabel,
	keys []orphanSweepKey,
) ([]orphanSweepKey, error) {
	stmt, ok := BuildConnectedKeysQuery(label, keys)
	if !ok {
		return nil, fmt.Errorf("unsupported orphan sweep label %q", label)
	}
	properties, _ := orphanSweepIdentityProperties(label)
	rows, err := s.Reader.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return nil, fmt.Errorf("read connected keys for %s: %w", label, err)
	}
	out := make([]orphanSweepKey, 0, len(rows))
	for _, row := range rows {
		key, err := decodeOrphanSweepKeyRow(properties, row)
		if err != nil {
			return nil, fmt.Errorf("read connected keys for %s: %w", label, err)
		}
		out = append(out, key)
	}
	return out, nil
}

func int64Count(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		if typed < 0 || typed > math.MaxInt64 || math.Trunc(typed) != typed {
			return 0, false
		}
		return int64(typed), true
	default:
		return 0, false
	}
}
