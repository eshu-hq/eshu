// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// decisions. Weak, missing, ambiguous, or stale tag outcomes stay diagnostic
// reducer output until a stronger source can prove digest identity.
//
// The fact id is stable by decision identity, so a retry that reaches the same
// classification upserts the same rows, and the insert's fencing guard keeps a
// pass that read STALE evidence from overwriting a fresher pass's payload on
// that shared fact id (reducerFactBatchInsertQuery).
//
// What this write is NOT is generation-authoritative. The identity embeds
// `outcome` and `image_ref`, so a replay that RE-CLASSIFIES an image lands under
// a new fact id beside the old one, and a replay that demotes an image out of
// the canonical outcomes produces no row to upsert over the stale one at all.
// Both leave a superseded decision live for the same active generation, which
// PostgresContainerImageIdentityStore.ListContainerImageIdentities serves — it
// has no DISTINCT ON, GROUP BY, or per-digest latest-wins. Closing that needs a
// retire pass whose deletes are safe against the OCI collector's bounded
// degradation (a soft-failed config blob and a truncated tag list both shrink a
// generation with no registry-side assertion), which is tracked as #5854.
func (w PostgresContainerImageIdentityWriter) WriteContainerImageIdentityDecisions(
	ctx context.Context,
	write ContainerImageIdentityWrite,
) (ContainerImageIdentityWriteResult, error) {
	if w.DB == nil {
		return ContainerImageIdentityWriteResult{}, fmt.Errorf("container image identity database is required")
	}
	// Checked before any statement is issued: an unfenced row must never reach
	// the database, because a row resting at the fact_records default of 0 makes
	// the insert's conflict guard inert for every later pass.
	if err := validateContainerImageIdentityFence(write); err != nil {
		return ContainerImageIdentityWriteResult{}, err
	}

	now := reducerWriterNow(w.Now)
	// Stamped on the INSERT, which is the only statement that stamps it. See
	// reducerFactBatchInsertQuery for why a row at 0 defeats its own guard.
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
	return ContainerImageIdentityWriteResult{
		CanonicalWrites: len(decisions),
		EvidenceSummary: fmt.Sprintf("wrote container image identity decisions %d", len(decisions)),
	}, nil
}

// errContainerImageIdentityMissingEvidenceAsOf is returned when a write reaches
// the writer without the evidence-read watermark the durable row is stamped
// with.
var errContainerImageIdentityMissingEvidenceAsOf = errors.New(
	"container image identity write requires evidence_as_of: the durable row has no watermark to be stamped with",
)

// containerImageIdentityFencingToken renders the write's evidence-read watermark
// as the BIGINT fact_records.fencing_token carries.
//
// Microsecond resolution matches Postgres' own timestamp resolution and leaves
// int64 headroom for ~294,000 years, so no saturation handling is needed.
//
// The token is a wall-clock microsecond reading, so it is monotonic across
// reopens and retries without needing a durable counter — unlike the queue's
// attempt_count, which the reopen-succeeded statement deliberately resets to 0
// and which therefore cannot rank a reopened replay against the run it is
// repairing. Two reducer processes read their own clocks, so the ordering is
// only as good as NTP between them; the hazard window is a whole lease duration,
// which is orders of magnitude larger than realistic host clock skew.
func containerImageIdentityFencingToken(write ContainerImageIdentityWrite) int64 {
	return write.EvidenceAsOf.UTC().UnixMicro()
}

// validateContainerImageIdentityFence rejects a write with no evidence-read
// watermark.
//
// This is deliberately a hard error rather than a defaulted value. A zero
// EvidenceAsOf does not yield token 0; containerImageIdentityFencingToken runs
// time.Time{} through UnixMicro, and year 1 is -62135596800000000 microseconds
// from the Unix epoch. Every row the domain wrote would then carry that same
// floor value, so the insert's
// `fact_records.fencing_token <= EXCLUDED.fencing_token` guard would compare the
// floor against itself and admit every later pass unconditionally: the domain
// would look fenced while behaving like the six writers that never opted in.
// Defaulting the watermark to the writer's own clock would be worse, because
// write time ranks a stalled worker highest — the exact inversion the watermark
// exists to prevent.
func validateContainerImageIdentityFence(write ContainerImageIdentityWrite) error {
	if write.EvidenceAsOf.IsZero() {
		return errContainerImageIdentityMissingEvidenceAsOf
	}
	return nil
}

// containerImageIdentityEvidenceAsOf reads the handler's clock for the
// evidence-read watermark, falling back to the process clock when the handler
// left Now unset.
func containerImageIdentityEvidenceAsOf(now func() time.Time) time.Time {
	return reducerWriterNow(now)
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
