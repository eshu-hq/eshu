// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"math"
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
	key        string
	observedAt *int64
}

// BuildCandidateOrphanNodesQuery builds the S1 read: every node carrying this
// label's evidence_source predicate, with its identity key and current
// orphan marker. It contains no relationship predicate; connectivity is
// resolved separately by BuildConnectedKeysQuery. The LIMIT bounds how many
// candidates one cycle considers -- a label with more matching nodes than
// limit converges over multiple cycles rather than in one pass.
func BuildCandidateOrphanNodesQuery(label OrphanSweepLabel, limit int, cursor string) (Statement, bool) {
	match, ok := orphanLabelMatch(label)
	if !ok {
		return Statement{}, false
	}
	key, ok := orphanSweepIdentityKey(label)
	if !ok {
		return Statement{}, false
	}
	// ORDER BY the identity key and take only keys strictly greater than the
	// caller's cursor so a label with more matching nodes than limit is paged
	// deterministically across cycles instead of re-reading the same arbitrary
	// window. This guarantees forward progress past a window that happens to be
	// entirely connected: the cursor advances every cycle regardless of orphan
	// state, and orphan_sweep.go wraps the cursor back to "" once a short page
	// signals the end of the label.
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher: fmt.Sprintf(`MATCH (n:%s)
WHERE %s
  AND n.%s > $cursor
RETURN n.%s AS key, n.eshu_orphan_observed_at_unix AS observed_at
ORDER BY n.%s
LIMIT $limit`, match, orphanSweepNodeGuard(label), key, key, key),
		Parameters: map[string]any{
			"limit":  normalizePositiveInt(limit, defaultOrphanSweepCountLimit),
			"cursor": cursor,
		},
	}, true
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
func BuildConnectedKeysQuery(label OrphanSweepLabel, keys []string) (Statement, bool) {
	match, ok := orphanLabelMatch(label)
	if !ok {
		return Statement{}, false
	}
	key, ok := orphanSweepIdentityKey(label)
	if !ok {
		return Statement{}, false
	}
	// For a label whose identity key is not unique across node classes (Module:
	// name is shared between canonical imports and semantic entities), restrict
	// the connectivity check to the class this sweep owns, so a connected
	// same-name node of the OTHER class does not mask a true orphan.
	classWhere := ""
	if class := orphanSweepClassPredicate(label); class != "" {
		classWhere = "\nWHERE " + class
	}
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher: fmt.Sprintf(`UNWIND $keys AS candidate_key
MATCH (n:%s {%s: candidate_key})-[r]-(m)%s
RETURN DISTINCT n.%s AS key`, match, key, classWhere, key),
		Parameters: map[string]any{
			"keys": keys,
		},
	}, true
}

// readCandidateOrphanNodes runs BuildCandidateOrphanNodesQuery and decodes
// its rows.
func (s *OrphanSweepStore) readCandidateOrphanNodes(
	ctx context.Context,
	label OrphanSweepLabel,
	limit int,
	cursor string,
) ([]orphanSweepCandidate, error) {
	stmt, ok := BuildCandidateOrphanNodesQuery(label, limit, cursor)
	if !ok {
		return nil, fmt.Errorf("unsupported orphan sweep label %q", label)
	}
	rows, err := s.Reader.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return nil, fmt.Errorf("read candidate orphan nodes for %s: %w", label, err)
	}
	out := make([]orphanSweepCandidate, 0, len(rows))
	for _, row := range rows {
		key, ok := row["key"].(string)
		if !ok || key == "" {
			return nil, fmt.Errorf("read candidate orphan nodes for %s: unexpected key type %T", label, row["key"])
		}
		candidate := orphanSweepCandidate{key: key}
		if observedAt, ok := int64Count(row["observed_at"]); ok {
			candidate.observedAt = &observedAt
		}
		out = append(out, candidate)
	}
	return out, nil
}

// readConnectedKeys runs BuildConnectedKeysQuery for keys and decodes its
// rows, splitting keys into defaultOrphanSweepConnectedKeysChunkSize-sized
// round trips (see that constant's doc comment for why). It returns (nil,
// nil) without any round trip when keys is empty.
func (s *OrphanSweepStore) readConnectedKeys(
	ctx context.Context,
	label OrphanSweepLabel,
	keys []string,
) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	if len(keys) <= defaultOrphanSweepConnectedKeysChunkSize {
		return s.readConnectedKeysChunk(ctx, label, keys)
	}

	seen := make(map[string]bool, len(keys))
	out := make([]string, 0, len(keys))
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
			if !seen[k] {
				seen[k] = true
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
	keys []string,
) ([]string, error) {
	stmt, ok := BuildConnectedKeysQuery(label, keys)
	if !ok {
		return nil, fmt.Errorf("unsupported orphan sweep label %q", label)
	}
	rows, err := s.Reader.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return nil, fmt.Errorf("read connected keys for %s: %w", label, err)
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		key, ok := row["key"].(string)
		if !ok || key == "" {
			return nil, fmt.Errorf("read connected keys for %s: unexpected key type %T", label, row["key"])
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
