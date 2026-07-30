// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

const (
	containerImageIdentityFactKind       = "reducer_container_image_identity"
	containerImageIdentityFormatImageRef = "image_ref_v2"
)

// ContainerImageIdentityTransaction is the narrow atomic write surface used by
// the identity writer for outcome-independent publications followed by cleanup
// of unreachable legacy outcome-keyed rows.
type ContainerImageIdentityTransaction interface {
	workloadIdentityExecer
	Commit() error
	Rollback() error
}

// ContainerImageIdentityBeginner opens the identity writer's publication and
// legacy-cleanup transaction.
type ContainerImageIdentityBeginner interface {
	BeginContainerImageIdentityTx(context.Context) (ContainerImageIdentityTransaction, error)
}

// ContainerImageIdentityCutoverLookup reads the durable format-transition
// marker that makes the bounded steady-state cleanup safe without reacquiring
// the rolling-upgrade lock.
type ContainerImageIdentityCutoverLookup interface {
	ContainerImageIdentityCutoverExists(context.Context, string, string) (bool, error)
}

// PostgresContainerImageIdentityWriter persists image-reference-keyed identity
// decisions into the shared fact store.
type PostgresContainerImageIdentityWriter struct {
	DB            workloadIdentityExecer
	Beginner      ContainerImageIdentityBeginner
	CutoverLookup ContainerImageIdentityCutoverLookup
	Now           func() time.Time
}

// WriteContainerImageIdentityDecisions stores canonical image identity
// decisions and fenced tombstones for evaluated demotions. Weak, missing,
// ambiguous, or stale outcomes stay diagnostic reducer output unless the
// retirement planner proves the reference was evaluated authoritatively.
//
// The fact ID is stable by logical image identity: scope, generation, and image
// reference. Outcome is payload, not identity. A reclassification therefore
// collides on the same primary key, where the shared insert's fencing guard
// rejects an older evidence read. A demotion writes a tombstone at that same
// key, preserving the durable fence so a stalled older pass cannot resurrect
// the retired row after the fresher pass commits.
//
// Legacy outcome-keyed rows are deleted only after the new derivation makes
// those keys unreachable to every future writer. Publication and that one-way
// cleanup share a transaction so readers never observe both formats from a
// completed pass.
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
	publications := planContainerImageIdentityPublications(write)
	collectorKind := reducerFactCollectorKind(write.SourceSystem)
	rows := make([]reducerFactRow, 0, len(publications))
	canonicalWrites := 0
	retirementAttempts := 0
	for _, publication := range publications {
		decision := publication.decision
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
			IsTombstone:      publication.tombstone,
			Payload:          string(payloadJSON),
			FencingToken:     fencingToken,
		})
		if publication.tombstone {
			retirementAttempts++
		} else {
			canonicalWrites++
		}
	}
	exec := w.DB
	var tx ContainerImageIdentityTransaction
	rollbackNeeded := false
	legacyRowsDeleted := 0
	if len(write.LegacyFactIDs) > 0 {
		cutoverComplete := false
		if w.CutoverLookup != nil {
			var err error
			cutoverComplete, err = w.CutoverLookup.ContainerImageIdentityCutoverExists(
				ctx,
				write.ScopeID,
				write.GenerationID,
			)
			if err != nil {
				return ContainerImageIdentityWriteResult{}, fmt.Errorf(
					"read container image identity cutover: %w",
					err,
				)
			}
		}
		if cutoverComplete && len(rows) <= reducerFactBatchSize {
			var err error
			legacyRowsDeleted, err = execContainerImageIdentityPublicationsAndCleanup(
				ctx,
				w.DB,
				rows,
				write.LegacyFactIDs,
				write.ScopeID,
				write.GenerationID,
				fencingToken,
			)
			if err != nil {
				return ContainerImageIdentityWriteResult{}, err
			}
			return containerImageIdentityWriteResult(
				canonicalWrites,
				retirementAttempts,
				legacyRowsDeleted,
			), nil
		}
		if w.Beginner == nil {
			return ContainerImageIdentityWriteResult{}, fmt.Errorf(
				"container image identity transaction beginner is required for legacy cleanup",
			)
		}
		var err error
		tx, err = w.Beginner.BeginContainerImageIdentityTx(ctx)
		if err != nil {
			return ContainerImageIdentityWriteResult{}, fmt.Errorf(
				"begin container image identity write: %w",
				err,
			)
		}
		exec = tx
		rollbackNeeded = true
		defer func() {
			if rollbackNeeded {
				_ = tx.Rollback()
			}
		}()
		if !cutoverComplete {
			if err := execContainerImageIdentityCutoverFence(
				ctx,
				exec,
				write.ScopeID,
				write.GenerationID,
			); err != nil {
				return ContainerImageIdentityWriteResult{}, err
			}
		}
		legacyRowsDeleted, err = execContainerImageIdentityPublicationsAndCleanup(
			ctx,
			exec,
			rows,
			write.LegacyFactIDs,
			write.ScopeID,
			write.GenerationID,
			fencingToken,
		)
		if err != nil {
			return ContainerImageIdentityWriteResult{}, err
		}
	} else if err := reducerBatchInsertFacts(ctx, exec, rows); err != nil {
		return ContainerImageIdentityWriteResult{}, fmt.Errorf("write container image identity fact: %w", err)
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return ContainerImageIdentityWriteResult{}, fmt.Errorf(
				"commit container image identity write: %w",
				err,
			)
		}
		rollbackNeeded = false
	}
	return containerImageIdentityWriteResult(
		canonicalWrites,
		retirementAttempts,
		legacyRowsDeleted,
	), nil
}

func containerImageIdentityWriteResult(
	canonicalWrites int,
	retirementAttempts int,
	legacyRowsDeleted int,
) ContainerImageIdentityWriteResult {
	return ContainerImageIdentityWriteResult{
		CanonicalWrites:    canonicalWrites,
		RetirementAttempts: retirementAttempts,
		LegacyRowsDeleted:  legacyRowsDeleted,
		EvidenceSummary: fmt.Sprintf(
			"wrote container image identity decisions %d attempted tombstone publications %d legacy rows deleted %d",
			canonicalWrites,
			retirementAttempts,
			legacyRowsDeleted,
		),
	}
}

type containerImageIdentityPublication struct {
	decision  ContainerImageIdentityDecision
	tombstone bool
}

func planContainerImageIdentityPublications(
	write ContainerImageIdentityWrite,
) []containerImageIdentityPublication {
	byFactID := make(map[string]containerImageIdentityPublication)
	for _, decision := range containerImageIdentityCanonicalDecisions(write.Decisions) {
		publication := containerImageIdentityPublication{decision: decision}
		factID := containerImageIdentityFactID(write, decision)
		if current, ok := byFactID[factID]; !ok ||
			preferContainerImageIdentityPublication(publication, current) {
			byFactID[factID] = publication
		}
	}
	for _, decision := range write.TombstoneDecisions {
		publication := containerImageIdentityPublication{
			decision:  decision,
			tombstone: true,
		}
		factID := containerImageIdentityFactID(write, decision)
		if current, ok := byFactID[factID]; !ok ||
			preferContainerImageIdentityPublication(publication, current) {
			byFactID[factID] = publication
		}
	}

	factIDs := make([]string, 0, len(byFactID))
	for factID := range byFactID {
		factIDs = append(factIDs, factID)
	}
	sort.Strings(factIDs)
	publications := make([]containerImageIdentityPublication, 0, len(factIDs))
	for _, factID := range factIDs {
		publications = append(publications, byFactID[factID])
	}
	return publications
}

func preferContainerImageIdentityPublication(
	candidate containerImageIdentityPublication,
	current containerImageIdentityPublication,
) bool {
	if candidate.tombstone != current.tombstone {
		return !candidate.tombstone
	}
	candidateRank := containerImageIdentityPublicationRank(candidate.decision.Outcome)
	currentRank := containerImageIdentityPublicationRank(current.decision.Outcome)
	if candidateRank != currentRank {
		return candidateRank > currentRank
	}
	return containerImageIdentityDecisionSortKey(candidate.decision) <
		containerImageIdentityDecisionSortKey(current.decision)
}

func containerImageIdentityPublicationRank(outcome ContainerImageIdentityOutcome) int {
	switch outcome {
	case ContainerImageIdentityExactDigest:
		return 2
	case ContainerImageIdentityTagResolved:
		return 1
	default:
		return 0
	}
}

func containerImageIdentityDecisionSortKey(decision ContainerImageIdentityDecision) string {
	return strings.Join([]string{
		strings.TrimSpace(decision.ImageRef),
		string(decision.Outcome),
		strings.TrimSpace(decision.Digest),
		strings.TrimSpace(decision.RepositoryID),
		strings.TrimSpace(decision.SourceRevision),
		strings.TrimSpace(decision.Reason),
	}, "\x00")
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
	}
}

func legacyContainerImageIdentityFactID(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
) string {
	identity := containerImageIdentityIdentity(write, decision)
	identity["outcome"] = string(decision.Outcome)
	return containerImageIdentityFactKind + ":" + facts.StableID(
		containerImageIdentityFactKind,
		identity,
	)
}

func containerImageIdentityPayload(
	write ContainerImageIdentityWrite,
	decision ContainerImageIdentityDecision,
	canonicalID string,
) map[string]any {
	return map[string]any{
		"identity_format":            containerImageIdentityFormatImageRef,
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
