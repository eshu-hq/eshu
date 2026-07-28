// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

const containerImageIdentityFactKind = "reducer_container_image_identity"

// PostgresContainerImageIdentityWriter persists digest-keyed image identity
// decisions into the shared fact store.
type PostgresContainerImageIdentityWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WriteContainerImageIdentityDecisions stores only canonical image identity
// decisions, then retires every other decision this domain holds for the same
// (scope, generation). Weak, missing, ambiguous, or stale tag outcomes stay
// diagnostic reducer output until a stronger source can prove digest identity.
//
// The fact id is stable by decision identity, so a retry that reaches the same
// classification upserts the same rows. That alone is NOT enough for a replay
// whose classification CHANGED — the identity embeds `outcome` and `image_ref`,
// so a re-classified decision lands under a new fact id, and a decision demoted
// out of the canonical set produces no row to upsert at all. The retire pass is
// what makes the write generation-authoritative: after it, the durable
// decisions for this scope generation are exactly the canonical set, no more.
// See containerImageIdentityRetireQuery for the two replay cases this closes.
func (w PostgresContainerImageIdentityWriter) WriteContainerImageIdentityDecisions(
	ctx context.Context,
	write ContainerImageIdentityWrite,
) (ContainerImageIdentityWriteResult, error) {
	if w.DB == nil {
		return ContainerImageIdentityWriteResult{}, fmt.Errorf("container image identity database is required")
	}
	// Checked before any statement is issued: an unfenced retire must never
	// reach the database, not even after a successful insert.
	if err := validateContainerImageIdentityFence(write); err != nil {
		return ContainerImageIdentityWriteResult{}, err
	}

	now := reducerWriterNow(w.Now)
	// Stamped on the INSERT, not only by the retire's CTE. A row left at the
	// table default 0 between those two statements is durable, visible, and
	// deletable by any concurrent stalled worker's fenced retire, because 0 is at
	// or below every token. See reducerFactBatchInsertQuery.
	fencingToken := containerImageIdentityFencingToken(write)
	decisions := containerImageIdentityCanonicalDecisions(write.Decisions)
	collectorKind := reducerFactCollectorKind(write.SourceSystem)
	rows := make([]reducerFactRow, 0, len(decisions))
	for _, decision := range decisions {
		canonicalID := canonicalContainerImageIdentityID(write, decision)
		payloadJSON, err := json.Marshal(containerImageIdentityPayload(write, decision, canonicalID))
		if err != nil {
			return ContainerImageIdentityWriteResult{}, fmt.Errorf("marshal container image identity payload: %w", err)
		}
		rows = append(rows, reducerFactRow{
			FactID:           containerImageIdentityFactID(write, decision),
			ScopeID:          write.ScopeID,
			GenerationID:     write.GenerationID,
			FactKind:         containerImageIdentityFactKind,
			StableFactKey:    containerImageIdentityStableFactKey(write, decision),
			CollectorKind:    collectorKind,
			SourceConfidence: facts.SourceConfidenceInferred,
			SourceSystem:     write.SourceSystem,
			SourceFactKey:    write.IntentID,
			ObservedAt:       now,
			IngestedAt:       now,
			Payload:          string(payloadJSON),
			FencingToken:     fencingToken,
		})
	}
	// Bounded chunked bulk insert: canonical decisions are upserted in
	// O(N/batchSize) round-trips rather than one ExecContext per decision.
	if err := reducerBatchInsertFacts(ctx, w.DB, rows); err != nil {
		return ContainerImageIdentityWriteResult{}, fmt.Errorf("write container image identity fact: %w", err)
	}
	// Retire AFTER the insert, never before. This ordering buys two things, and
	// it is worth being precise about which, because it does NOT buy atomicity:
	//
	//   - a failed insert leaves the previous generation's decisions in place
	//     rather than clearing them and then writing nothing;
	//   - no reader ever sees this scope generation with ZERO decisions, which
	//     retire-first would expose for the width of the insert.
	//
	// What it does not close: the insert and the retire are two separate
	// autocommit statements on the same connection, with no enclosing
	// transaction. Between them the corrected decision and the superseded one are
	// BOTH durable and both active, so a reader landing in that window sees two
	// contradictory decisions for one image — the same shape the retire exists to
	// remove, just briefly instead of permanently. Closing that needs the insert
	// and the retire to share a transaction, which this writer cannot do today:
	// it holds a bare execer, not a transaction handle, and the batched insert
	// may itself span several statements. The window is bounded by one round-trip
	// and it resolves without intervention, so it is a documented read-skew
	// window, not a lost update.
	retired, err := w.retireSupersededDecisions(ctx, write, rows)
	if err != nil {
		return ContainerImageIdentityWriteResult{}, err
	}

	blindRetire := len(decisions) == 0 && retired > 0
	// The partition shrank: rows left it with no replacement. An ordinary
	// re-classification retires at most one superseded row per image it rewrites,
	// so it can never exceed its own write count; exceeding it is the demotion
	// shape — and a demotion is exactly what an evidence-visibility gap
	// counterfeits. blindRetire is the TOTAL case of that; this is the partial one
	// it cannot see, where a pass reads the cross-scope OCI facts for some images
	// in the generation and not others.
	partialRetire := retired > len(decisions) && !blindRetire
	if blindRetire {
		// Loud on purpose. An empty canonical set is the correct answer for a
		// genuine demotion, but classifyContainerImageRef returns the same
		// `unresolved` outcome when the cross-scope registry observations simply
		// were not visible to this pass, and from here those two are identical.
		// The fencing token cannot separate them either — the blind pass read its
		// (empty) evidence LAST, so it ranks highest. Before the retire existed
		// such a pass was a harmless no-op; now it clears the partition, and
		// nothing re-triggers the domain afterwards. This is the operator's
		// handle on it.
		slog.Warn(
			"container image identity retired prior decisions with no canonical write",
			"domain", string(DomainContainerImageIdentity),
			"intent_id", write.IntentID,
			"scope_id", write.ScopeID,
			"generation_id", write.GenerationID,
			"retired", retired,
			"evidence_as_of", write.EvidenceAsOf.UTC(),
		)
	}
	if partialRetire {
		// One line per pass, and never both: blindRetire already covers the total
		// case, so an operator sees exactly one signal for one partition shrink.
		// canonical_writes is logged beside retired because the shrink is the
		// RELATION between them — retired= alone has no baseline to be read
		// against, which is why the count on its own was not a signal.
		slog.Warn(
			"container image identity retired more decisions than it wrote",
			"domain", string(DomainContainerImageIdentity),
			"intent_id", write.IntentID,
			"scope_id", write.ScopeID,
			"generation_id", write.GenerationID,
			"retired", retired,
			"canonical_writes", len(decisions),
			"evidence_as_of", write.EvidenceAsOf.UTC(),
		)
	}

	return ContainerImageIdentityWriteResult{
		CanonicalWrites:               len(decisions),
		Retired:                       retired,
		RetiredWithoutCanonicalWrites: blindRetire,
		RetiredMoreThanWritten:        retired > len(decisions),
		EvidenceSummary: fmt.Sprintf(
			"wrote container image identity decisions %d retired=%d retired_without_canonical_writes=%t "+
				"retired_more_than_written=%t",
			len(decisions), retired, blindRetire, retired > len(decisions),
		),
	}, nil
}

// retireSupersededDecisions deletes every container image identity decision for
// this write's (scope, generation) that the current execution did not just
// write — but only rows whose evidence was no fresher than this write's — and
// returns how many rows it deleted.
//
// The returned count is not incidental. The retire destroys durable decisions,
// and the instrumented ExecContext wrapper records only that a statement ran,
// never what it removed; without this number nothing reports how many decisions
// a pass destroyed, and the blind-retire case (see
// WriteContainerImageIdentityDecisions) could not be detected at all.
//
// keepFactIDs is built from the same rows handed to the insert rather than
// re-derived from write.Decisions, so the keep-set can never disagree with what
// was persisted. Repeated fact ids are harmless — the insert collapses them
// last-write-wins, and neither `= ANY` nor `<> ALL` is affected by duplicates in
// the array. An empty decision set yields an empty keep-set, which retires every
// prior decision for the generation at or below this write's token: the intended
// behavior for a generation whose images have all been demoted out of the
// canonical outcomes (see containerImageIdentityRetireQuery).
func (w PostgresContainerImageIdentityWriter) retireSupersededDecisions(
	ctx context.Context,
	write ContainerImageIdentityWrite,
	rows []reducerFactRow,
) (int, error) {
	keepFactIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		keepFactIDs = append(keepFactIDs, row.FactID)
	}
	result, err := w.DB.ExecContext(
		ctx,
		containerImageIdentityRetireQuery,
		containerImageIdentityFactKind,
		write.ScopeID,
		write.GenerationID,
		keepFactIDs,
		containerImageIdentityFencingToken(write),
	)
	if err != nil {
		return 0, fmt.Errorf("retire superseded container image identity decisions: %w", err)
	}
	retired := 0
	if result != nil {
		if affected, affErr := result.RowsAffected(); affErr == nil && affected > 0 {
			retired = int(affected)
		}
	}
	return retired, nil
}

func containerImageIdentityFactID(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
) string {
	return containerImageIdentityFactKind + ":" + facts.StableID(
		containerImageIdentityFactKind,
		containerImageIdentityIdentity(write, decision),
	)
}

func containerImageIdentityStableFactKey(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
) string {
	identity := containerImageIdentityIdentity(write, decision)
	return strings.Join([]string{
		"container_image_identity",
		strings.TrimSpace(fmt.Sprint(identity["scope_id"])),
		strings.TrimSpace(fmt.Sprint(identity["generation_id"])),
		strings.TrimSpace(fmt.Sprint(identity["image_ref"])),
		strings.TrimSpace(fmt.Sprint(identity["outcome"])),
	}, ":")
}

func canonicalContainerImageIdentityID(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
) string {
	return "canonical:" + containerImageIdentityStableFactKey(write, decision)
}

func containerImageIdentityIdentity(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
) map[string]any {
	return map[string]any{
		"scope_id":      strings.TrimSpace(write.ScopeID),
		"generation_id": strings.TrimSpace(write.GenerationID),
		"image_ref":     strings.TrimSpace(decision.ImageRef),
		"outcome":       string(decision.Outcome),
	}
}

func containerImageIdentityPayload(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
	canonicalID string,
) map[string]any {
	return map[string]any{
		"reducer_domain":             string(DomainContainerImageIdentity),
		"intent_id":                  write.IntentID,
		"scope_id":                   write.ScopeID,
		"generation_id":              write.GenerationID,
		"source_system":              write.SourceSystem,
		"cause":                      write.Cause,
		"image_ref":                  decision.ImageRef,
		"digest":                     decision.Digest,
		"repository_id":              decision.RepositoryID,
		"source_revision":            strings.TrimSpace(decision.SourceRevision),
		"source_revision_provenance": strings.TrimSpace(decision.SourceRevisionProvenance),
		"source_repository_ids": uniqueSortedStrings(
			decision.SourceRepositoryIDs,
		),
		// build_provenance_repository_ids persists the strong-evidence-only
		// subset of SourceRepositoryIDs (an OCI config source label, a CI run,
		// or verified SLSA provenance -- never a mere deploy/scope reference).
		// The supply-chain-impact consumer (singleSupplyChainImageSourceRepositoryID,
		// #5801) ranks this field ahead of the broader source_repository_ids so a
		// label-derived repository is not treated as ambiguous merely because a
		// weaker scope anchor also names a different repository for the same
		// image.
		"build_provenance_repository_ids": uniqueSortedStrings(
			decision.BuildProvenanceRepositoryIDs,
		),
		"workload_ids":      uniqueSortedStrings(decision.WorkloadIDs),
		"service_ids":       uniqueSortedStrings(decision.ServiceIDs),
		"outcome":           string(decision.Outcome),
		"reason":            decision.Reason,
		"canonical_id":      canonicalID,
		"canonical_writes":  decision.CanonicalWrites,
		"evidence_fact_ids": uniqueSortedStrings(decision.EvidenceFactIDs),
		"identity_strength": decision.IdentityStrength,
		"publication_kind":  containerImageIdentityFactKind,
		"source_layers": []string{
			string(truth.LayerSourceDeclaration),
			string(truth.LayerObservedResource),
		},
	}
}
