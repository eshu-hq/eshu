// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
)

// verifySemanticEntityMatchIdentities reconciles every admitted semantic row
// against the node identity its upsert statement will address (#8359). A
// MATCH-miss no-ops silently with a success status, so the writer reports
// per-label attempted-vs-matched counts and the unmatched uids; the handler
// WARNs on them without failing the work item (projector-to-reducer ordering
// is not proven total, so a miss can be a race rather than a defect — the
// hard-error question is tracked separately).
//
// The check is pure Go over admitted rows: no backend read, no statement
// change. A row counts as matched when its statement cannot miss — a
// node-creating write lands by construction, and a MATCH-owned label the
// projector does not re-derive still MATCHes the incoming uid — or, for a
// MATCH-owned label the projector re-derives, when the row's uid equals the
// exact content.CanonicalEntityID derivation the projector computes. A row
// no statement can even be built for counts as attempted but unmatched: it
// is the same admitted-but-not-materialized defect class.
func verifySemanticEntityMatchIdentities(
	rows []semantic.EntityRow,
	canonicalNodeRows bool,
) (attempted, matched map[string]int, unmatched []string) {
	attempted = make(map[string]int)
	matched = make(map[string]int)
	for _, row := range rows {
		attempted[row.EntityType]++
		if !semanticRowMatchVerified(row, canonicalNodeRows) {
			unmatched = append(unmatched, row.EntityID)
			continue
		}
		matched[row.EntityType]++
	}
	return attempted, matched, unmatched
}

// semanticRowMatchVerified reports whether one admitted row's upsert
// statement addresses a node identity that exists by construction or by
// derivation agreement. See verifySemanticEntityMatchIdentities.
func semanticRowMatchVerified(row semantic.EntityRow, canonicalNodeRows bool) bool {
	if _, ok := buildSemanticEntityRowMap(row); !ok {
		return false
	}
	if !canonicalNodeRows || !semanticEntityCanonicalNodeOwnedLabel(row.EntityType) {
		return true
	}
	label, mapped := canonical.EntityTypeLabel(row.EntityType)
	if !mapped || label == "" {
		return false
	}
	if !canonical.DiscardsIncomingEntityID(row.EntityType) {
		return true
	}
	return content.CanonicalEntityID(
		row.RepoID,
		row.RelativePath,
		row.EntityType,
		row.EntityName,
		row.StartLine,
	) == row.EntityID
}
