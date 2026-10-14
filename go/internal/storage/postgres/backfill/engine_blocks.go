// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// loadCatalogBlock loads the repository catalog as the pass's catalog_load
// block. The block span covers the whole loader call, so row iteration and JSON
// decode are inside it, not only the first-row query that postgres.query spans
// stop at. rows is the fact rows scanned, entries the deduplicated catalog
// entries kept, and decode_seconds the time spent decoding payloads. A nil
// LoadCatalog means the pass runs against an empty catalog and reports no block.
func (s Store) loadCatalogBlock(ctx context.Context) ([]relationships.CatalogEntry, error) {
	if s.LoadCatalog == nil {
		return nil, nil
	}
	block := startBlockFromContext(ctx, BlockCatalogLoad)
	stats := &catalogLoadStats{}
	catalog, _, err := s.LoadCatalog(withCatalogLoadStats(ctx, stats), s.DB)
	block.End(err,
		IntAttr("rows", int(stats.rows)),
		IntAttr("entries", len(catalog)),
		FloatAttr("decode_seconds", stats.decode.Seconds()),
	)
	return catalog, err
}

// discoverEvidenceBlock runs evidence discovery, dedupe, and the per-source
// repository grouping as the pass's discover block. It returns the evidence
// grouped by source repository and the number of evidence facts kept. Facts
// missing a source or target repository are dropped, exactly as before.
func discoverEvidenceBlock(
	ctx context.Context,
	activeFacts []facts.Envelope,
	catalog []relationships.CatalogEntry,
) (map[string][]relationships.EvidenceFact, int64) {
	block := startBlockFromContext(ctx, BlockDiscover)
	discoveredEvidence := relationships.DedupeEvidenceFacts(
		relationships.DiscoverEvidence(activeFacts, catalog),
	)

	var totalEvidence int64
	evidenceBySourceRepo := make(map[string][]relationships.EvidenceFact)
	for _, fact := range discoveredEvidence {
		if strings.TrimSpace(fact.SourceRepoID) == "" || strings.TrimSpace(fact.TargetRepoID) == "" {
			continue
		}
		evidenceBySourceRepo[fact.SourceRepoID] = append(evidenceBySourceRepo[fact.SourceRepoID], fact)
		totalEvidence++
	}
	block.End(nil, IntAttr("facts_in", len(activeFacts)), IntAttr("evidence_out", int(totalEvidence)))
	return evidenceBySourceRepo, totalEvidence
}

// endFanInBlock ends the fan-in block. skipped is derived only on success: a
// failed fan-in cannot say how many partitions it declined, so the attribute
// is withheld rather than reported wrong. argocdProbeSeconds is the summed
// per-partition ArgoCD probe time across the fan-in workers, reported on both
// outcomes because a failed fan-in's probe cost is still real.
func endFanInBlock(block *BlockSpan, err error, partitions, published int, argocdProbeSeconds float64) {
	if err != nil {
		block.End(err,
			IntAttr("partitions", partitions),
			IntAttr("published", published),
			FloatAttr("argocd_probe_seconds", argocdProbeSeconds),
		)
		return
	}
	block.End(nil,
		IntAttr("partitions", partitions),
		IntAttr("published", published),
		IntAttr("skipped", partitions-published),
		FloatAttr("argocd_probe_seconds", argocdProbeSeconds),
	)
}

// sumInsertedRows totals the per-partition evidence rows a pass inserted.
func sumInsertedRows(insertedRows map[ScopeGenerationPartition]int64) int64 {
	var total int64
	for _, inserted := range insertedRows {
		total += inserted
	}
	return total
}
