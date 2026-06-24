// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"strings"
)

// BuildCodeImportRepoEdgeRefreshIntents returns one retract-only intent per
// consumer repository that appears in the file facts of this generation but
// for which no import resolves to an owning repository (owner gone, owners
// index empty, or all imports are intra-repo/stdlib/unresolved/self), and
// which therefore projects no upsert edge.
//
// Without these refresh intents a consumer that held a projection/code-imports
// DEPENDS_ON edge in a prior generation would keep that stale edge forever:
// BuildCodeImportRepoDependencyIntents emits nothing for the consumer, so the
// shared repo-dependency lane never reprocesses it and the prior DEPENDS_ON
// edge remains graph-visible. The refresh intent reuses the same stable
// acceptance identity (scope and scope-only source-run id) as the upsert path,
// so it lands on the same acceptance unit the prior edge wrote and the lane's
// per-consumer refresh-first reconstruction retracts the now-unsupported
// code-import edges.
//
// Each refresh row carries codeImportEvidenceSource so the lane retracts only
// projection/code-imports edges and leaves resolver/cross-repo or other-source
// edges for the same consumer untouched. A consumer that produces at least one
// distinct-owner upsert is excluded: its upsert intent already drives the
// refresh-first reconstruction for this evidence source. A consumer whose only
// resolved import is a self-reference (consumer == owner) is still retracted;
// if it held a real cross-repo code-import edge in a prior generation that
// edge must be removed now (mirrors #3598 package-consumption retract logic).
func BuildCodeImportRepoEdgeRefreshIntents(
	input CodeImportRepoDependencyInput,
) []SharedProjectionIntentRow {
	// covered tracks consumers that produced at least one distinct-owner upsert.
	covered := make(map[string]struct{})
	candidateOrder := make([]string, 0)
	candidates := make(map[string]struct{})

	for _, envelope := range input.FileEnvelopes {
		if envelope.FactKind != factKindFile {
			continue
		}
		consumerRepoID := strings.TrimSpace(payloadStr(envelope.Payload, "repo_id"))
		if consumerRepoID == "" {
			continue
		}
		language := strings.TrimSpace(payloadStr(envelope.Payload, "language"))
		fileData, ok := envelope.Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}

		if _, seen := candidates[consumerRepoID]; !seen {
			candidates[consumerRepoID] = struct{}{}
			candidateOrder = append(candidateOrder, consumerRepoID)
		}

		if _, alreadyCovered := covered[consumerRepoID]; alreadyCovered {
			continue
		}
		for _, entry := range mapSlice(fileData["imports"]) {
			source := codeImportEntrySource(entry)
			if source == "" {
				continue
			}
			ecosystem, coordinate := normalizeImportSource(source, language)
			if ecosystem == "" || coordinate == "" {
				continue
			}
			ownerRepoID := matchImportCoordinateToOwner(ecosystem, coordinate, input.Owners)
			if ownerRepoID != "" && ownerRepoID != consumerRepoID {
				covered[consumerRepoID] = struct{}{}
				break
			}
		}
	}

	sort.Strings(candidateOrder)
	rows := make([]SharedProjectionIntentRow, 0, len(candidateOrder))
	for _, consumerRepoID := range candidateOrder {
		if _, ok := covered[consumerRepoID]; ok {
			continue
		}
		rows = append(rows, buildCodeImportRepoEdgeRefreshIntent(input, consumerRepoID))
	}
	if len(rows) == 0 {
		return nil
	}
	return rows
}

// buildCodeImportRepoEdgeRefreshIntent builds one retract-only intent that
// triggers the shared repo-dependency lane to drop a consumer's
// projection/code-imports edges. It carries no target_repo_id: the lane
// retracts every code-imports edge owned by the consumer acceptance unit for
// this evidence source.
func buildCodeImportRepoEdgeRefreshIntent(
	input CodeImportRepoDependencyInput,
	consumerRepoID string,
) SharedProjectionIntentRow {
	payload := map[string]any{
		"action":          "retract",
		"repo_id":         consumerRepoID,
		"evidence_source": codeImportEvidenceSource,
		"generation_id":   input.GenerationID,
	}
	return BuildSharedProjectionIntent(SharedProjectionIntentInput{
		ProjectionDomain: DomainRepoDependency,
		PartitionKey:     fmt.Sprintf("retract:repo:%s|%s", consumerRepoID, codeImportEvidenceSource),
		ScopeID:          input.ScopeID,
		AcceptanceUnitID: consumerRepoID,
		RepositoryID:     consumerRepoID,
		SourceRunID:      strings.TrimSpace(input.SourceRunID),
		GenerationID:     input.GenerationID,
		Payload:          payload,
		CreatedAt:        input.CreatedAt,
	})
}
