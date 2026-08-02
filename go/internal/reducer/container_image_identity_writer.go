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

// ContainerImageIdentityLegacyCleanupLookup proves whether a completed cutover
// has no held legacy-format rows left to retire.
type ContainerImageIdentityLegacyCleanupLookup interface {
	ContainerImageIdentityLegacyCleanupComplete(context.Context, string, string) (bool, error)
}

// ContainerImageIdentityClaimedExecer runs a statement that locks and verifies
// the exact active claim epoch before returning its legacy cleanup count.
type ContainerImageIdentityClaimedExecer interface {
	ExecContainerImageIdentityClaimed(
		context.Context,
		string,
		...any,
	) (deleted int, claimValid bool, err error)
	// ExecContainerImageIdentityClaimedAdmission runs one exact-claim statement
	// that ALSO carries a leading container_image_identity_write_admission CAS
	// (#5874), and reports both the claim verdict and whether the admission CAS
	// itself succeeded. Used only by the completed-cutover, single-round-trip
	// write path (execContainerImageIdentityCompletedCutoverWrite): that path
	// has no separate transaction statement to run the admission check as, so
	// the check is woven into the same combined statement instead. See
	// containerImageIdentityCompletedCutoverWriteQuery's doc comment.
	ExecContainerImageIdentityClaimedAdmission(
		context.Context,
		string,
		...any,
	) (deleted int, admitted bool, claimValid bool, err error)
}

// PostgresContainerImageIdentityWriter persists image-reference-keyed identity
// decisions into the shared fact store.
type PostgresContainerImageIdentityWriter struct {
	DB                  workloadIdentityExecer
	Beginner            ContainerImageIdentityBeginner
	CutoverLookup       ContainerImageIdentityCutoverLookup
	LegacyCleanupLookup ContainerImageIdentityLegacyCleanupLookup
	ClaimedExecer       ContainerImageIdentityClaimedExecer
	Now                 func() time.Time
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
// those keys unreachable to every future writer. Publication and eligible
// one-way cleanup share a transaction. A completeness warning can deliberately
// hold a legacy row while the same completed pass publishes stronger v2 truth;
// readers may therefore observe both formats until the warning clears and a
// later pass retires the held row.
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
	// FencingToken (#5874) is the value actually stamped on rows and the
	// admission watermark; a zero value here means a caller forgot to wire
	// ContainerImageIdentityFencingTokenIssuer, which must fail loudly rather
	// than silently defaulting. See validateContainerImageIdentityFencingToken.
	if err := validateContainerImageIdentityFencingToken(write.FencingToken); err != nil {
		return ContainerImageIdentityWriteResult{}, err
	}

	now := reducerWriterNow(w.Now)
	// Stamped on the INSERT, which is the only statement that stamps it. See
	// reducerFactBatchInsertQuery for why a row at 0 defeats its own guard.
	fencingToken := containerImageIdentityFencingToken(write)
	rows, canonicalWrites, retirementAttempts, err := buildContainerImageIdentityRows(write, now, fencingToken)
	if err != nil {
		return ContainerImageIdentityWriteResult{}, err
	}
	if (len(rows) > 0 || len(write.LegacyFactIDs) > 0) && write.ClaimEpoch <= 0 {
		return ContainerImageIdentityWriteResult{}, errors.New(
			"container image identity claim_epoch must be positive for v2 publication or legacy cleanup",
		)
	}
	legacyRowsDeleted, err := w.writeContainerImageIdentityRows(
		ctx,
		write,
		rows,
		fencingToken,
		now,
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

func buildContainerImageIdentityRows(
	write ContainerImageIdentityWrite,
	now time.Time,
	fencingToken int64,
) ([]reducerFactRow, int, int, error) {
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
			return nil, 0, 0, fmt.Errorf("marshal container image identity payload: %w", err)
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
	return rows, canonicalWrites, retirementAttempts, nil
}

// errContainerImageIdentityMissingEvidenceAsOf is returned when a write reaches
// the writer without the evidence-read watermark the durable row is stamped
// with.
var errContainerImageIdentityMissingEvidenceAsOf = errors.New(
	"container image identity write requires evidence_as_of: the durable row has no watermark to be stamped with",
)

// containerImageIdentityFencingToken renders the write's database-issued
// fencing token as the BIGINT fact_records.fencing_token carries.
//
// #5874: this used to be write.EvidenceAsOf.UTC().UnixMicro() -- the REDUCER
// HOST'S wall clock. With modest clock skew between reducer replicas, a
// worker that started LATER on a fast-clock host could carry a SMALLER token
// than a worker that started EARLIER on a correct clock, so the conflict
// guard (reducerFactBatchInsertQuery, fact_records.fencing_token <=
// EXCLUDED.fencing_token) could admit a genuinely staler pass over a fresher
// one -- the same inversion #5848/#5875 P1 already fixed for the sibling
// aws_cloud_runtime_drift domain (container_image_identity_admission.go's
// containerImageIdentityAdmissionQuery doc comment has the full argument).
//
// write.FencingToken is issued by ContainerImageIdentityFencingTokenIssuer
// (a Postgres sequence, container_image_identity_fencing_token_seq, migration
// 093), drawn in ContainerImageIdentityHandler.Handle at the SAME point
// EvidenceAsOf used to be captured -- before the first fact load, not at
// write-commit time. Every reducer replica issues nextval() against the SAME
// shared Postgres instance, so the value reflects real invocation order,
// immune to any individual host's clock. It remains monotonic across reopens
// and retries the same way the wall clock was, without needing a durable
// per-domain counter that resets: unlike the queue's attempt_count, which the
// reopen-succeeded statement deliberately resets to 0 and which therefore
// cannot rank a reopened replay against the run it is repairing, nextval()
// never returns a value already issued.
func containerImageIdentityFencingToken(write ContainerImageIdentityWrite) int64 {
	return write.FencingToken
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
