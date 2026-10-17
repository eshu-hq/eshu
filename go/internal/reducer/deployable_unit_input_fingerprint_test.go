// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// fingerprintedDeployableUnitLoader is a resolved loader whose fused fenced
// read also returns the input fingerprint the store computed in the same
// statement snapshot (#8283).
type fingerprintedDeployableUnitLoader struct {
	stubDeployableUnitResolvedLoader
	complete    bool
	fingerprint string
	fusedCalls  int
	ownGen      string
	repoIDs     []string
}

func (l *fingerprintedDeployableUnitLoader) GetResolvedRelationshipsForReposWithCorpusFenceAndInputFingerprint(
	_ context.Context,
	ownGenerationID string,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, bool, string, error) {
	l.fusedCalls++
	l.ownGen = ownGenerationID
	l.repoIDs = repoIDs
	if l.err != nil {
		return nil, false, "", l.err
	}
	if !l.complete {
		return nil, false, "", nil
	}
	return l.resolved, true, l.fingerprint, nil
}

type recordedFingerprint struct {
	workItemID  string
	repoIDs     []string
	fingerprint string
}

// recordingFingerprintWriter records every fingerprint the handler stores and
// the edge-write count at that moment, so a test can prove the row is written
// after the graph work it vouches for.
type recordingFingerprintWriter struct {
	records         []recordedFingerprint
	edgeWritesAtRow []int
	edgeWriter      *recordingDeployableUnitEdgeWriter
	err             error
}

func (w *recordingFingerprintWriter) RecordDeployableUnitInputFingerprint(
	_ context.Context,
	workItemID string,
	repoIDs []string,
	fingerprint string,
) error {
	if w.err != nil {
		return w.err
	}
	w.records = append(w.records, recordedFingerprint{workItemID, slices.Clone(repoIDs), fingerprint})
	if w.edgeWriter != nil {
		w.edgeWritesAtRow = append(w.edgeWritesAtRow, len(w.edgeWriter.writeCalls))
	}
	return nil
}

func admittedDeploymentEvidence() []relationships.ResolvedRelationship {
	return []relationships.ResolvedRelationship{{
		SourceRepoID:     "repo-deployments",
		TargetRepoID:     "repo-edge-api",
		RelationshipType: relationships.RelDeploysFrom,
		Confidence:       0.94,
		Details: map[string]any{
			"evidence_kinds": []string{string(relationships.EvidenceKindArgoCDAppSource)},
		},
	}}
}

// TestDeployableUnitCorrelationRecordsInputFingerprintAfterGraphWrites pins
// the write side of the reopen gate: on success the handler stores the
// fingerprint the fused read returned, keyed on the work item id, with the
// candidate repository ids the read used, and only after the edge writes.
func TestDeployableUnitCorrelationRecordsInputFingerprintAfterGraphWrites(t *testing.T) {
	t.Parallel()

	edgeWriter := &recordingDeployableUnitEdgeWriter{}
	writer := &recordingFingerprintWriter{edgeWriter: edgeWriter}
	loader := &fingerprintedDeployableUnitLoader{
		complete: true, fingerprint: "fp-snapshot-1", stubDeployableUnitResolvedLoader: stubDeployableUnitResolvedLoader{
			resolved: admittedDeploymentEvidence(),
		},
	}
	handler := DeployableUnitCorrelationHandler{
		FactLoader:             &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader:         loader,
		PhasePublisher:         &recordingGraphProjectionPhasePublisher{},
		EdgeWriter:             edgeWriter,
		InputFingerprintWriter: writer,
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("repo:repo-edge-api"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got.Status != ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want succeeded", got.Status)
	}
	if loader.fusedCalls != 1 || loader.ownGen != "generation-1" || !slices.Equal(loader.repoIDs, []string{"repo-edge-api"}) {
		t.Fatalf("fused read calls=%d ownGen=%q repoIDs=%v, want 1 call for generation-1 over [repo-edge-api]",
			loader.fusedCalls, loader.ownGen, loader.repoIDs)
	}
	if len(writer.records) != 1 {
		t.Fatalf("fingerprint records = %d, want exactly 1", len(writer.records))
	}
	rec := writer.records[0]
	if rec.workItemID != "intent-1" || rec.fingerprint != "fp-snapshot-1" || !slices.Equal(rec.repoIDs, []string{"repo-edge-api"}) {
		t.Fatalf("recorded = %+v, want work item intent-1, fingerprint fp-snapshot-1, repo ids [repo-edge-api]", rec)
	}
	if len(edgeWriter.writeCalls) == 0 {
		t.Fatal("edge writer saw no write; the fixture must reach the edge path for the ordering proof")
	}
	if writer.edgeWritesAtRow[0] != len(edgeWriter.writeCalls) {
		t.Fatalf("fingerprint stored before the edge writes finished (%d of %d)",
			writer.edgeWritesAtRow[0], len(edgeWriter.writeCalls))
	}
}

// TestDeployableUnitCorrelationRecordsInputFingerprintOnNoEvidenceSuccess
// covers the second success exit: a candidate that evaluates to no admitted
// unit still succeeded on this read snapshot, so its fingerprint is stored too.
func TestDeployableUnitCorrelationRecordsInputFingerprintOnNoEvidenceSuccess(t *testing.T) {
	t.Parallel()

	writer := &recordingFingerprintWriter{}
	loader := &fingerprintedDeployableUnitLoader{complete: true, fingerprint: "fp-none"}
	handler := DeployableUnitCorrelationHandler{
		FactLoader:             &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader:         loader,
		PhasePublisher:         &recordingGraphProjectionPhasePublisher{},
		InputFingerprintWriter: writer,
	}

	if _, err := handler.Handle(context.Background(), deployableUnitIntent("repo:repo-edge-api")); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if len(writer.records) != 1 || writer.records[0].fingerprint != "fp-none" {
		t.Fatalf("records = %+v, want one record with fp-none", writer.records)
	}
}

// TestDeployableUnitCorrelationDoesNotRecordFingerprintOnDeferral keeps the
// row honest: a deferred or failed run stored nothing, so the item cannot be
// skipped on a result it never produced.
func TestDeployableUnitCorrelationDoesNotRecordFingerprintOnDeferral(t *testing.T) {
	t.Parallel()

	writer := &recordingFingerprintWriter{}
	loader := &fingerprintedDeployableUnitLoader{complete: false}
	handler := DeployableUnitCorrelationHandler{
		FactLoader:             &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader:         loader,
		InputFingerprintWriter: writer,
	}

	_, err := handler.Handle(context.Background(), deployableUnitIntent("repo:repo-edge-api"))
	if err == nil {
		t.Fatal("Handle() error = nil, want the corpus-fence deferral")
	}
	if len(writer.records) != 0 {
		t.Fatalf("records after a deferral = %+v, want none", writer.records)
	}
}

// TestDeployableUnitCorrelationFingerprintWriteFailureFailsTheItem makes the
// failure loud: the bookkeeping write error fails the attempt (the queue
// retries, the graph writes are idempotent) instead of being swallowed.
func TestDeployableUnitCorrelationFingerprintWriteFailureFailsTheItem(t *testing.T) {
	t.Parallel()

	writer := &recordingFingerprintWriter{err: errors.New("fingerprint store down")}
	handler := DeployableUnitCorrelationHandler{
		FactLoader:             &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader:         &fingerprintedDeployableUnitLoader{complete: true, fingerprint: "fp"},
		PhasePublisher:         &recordingGraphProjectionPhasePublisher{},
		InputFingerprintWriter: writer,
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("repo:repo-edge-api"))
	if err == nil || !strings.Contains(err.Error(), "fingerprint store down") {
		t.Fatalf("Handle() = (%+v, %v), want an error wrapping the writer failure", got, err)
	}
	if got.Status == ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q after a failed fingerprint write, want not succeeded", got.Status)
	}
}

// TestDeployableUnitCorrelationWithoutFingerprintSupportStaysFailOpen covers
// the loaders that cannot return a fingerprint (the unfenced stub, a nil
// writer, an item with no candidates): the handler succeeds, writes nothing,
// and the missing row reopens the item on the next pass.
func TestDeployableUnitCorrelationWithoutFingerprintSupportStaysFailOpen(t *testing.T) {
	t.Parallel()

	t.Run("loader without the fingerprint read", func(t *testing.T) {
		t.Parallel()
		writer := &recordingFingerprintWriter{}
		handler := DeployableUnitCorrelationHandler{
			FactLoader:             &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
			ResolvedLoader:         &stubDeployableUnitResolvedLoader{},
			PhasePublisher:         &recordingGraphProjectionPhasePublisher{},
			InputFingerprintWriter: writer,
		}
		if _, err := handler.Handle(context.Background(), deployableUnitIntent("repo:repo-edge-api")); err != nil {
			t.Fatalf("Handle() error = %v, want nil", err)
		}
		if len(writer.records) != 0 {
			t.Fatalf("records = %+v, want none without a fingerprint to store", writer.records)
		}
	})
	t.Run("nil writer", func(t *testing.T) {
		t.Parallel()
		handler := DeployableUnitCorrelationHandler{
			FactLoader:     &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
			ResolvedLoader: &fingerprintedDeployableUnitLoader{complete: true, fingerprint: "fp"},
			PhasePublisher: &recordingGraphProjectionPhasePublisher{},
		}
		if _, err := handler.Handle(context.Background(), deployableUnitIntent("repo:repo-edge-api")); err != nil {
			t.Fatalf("Handle() error = %v, want nil with no writer wired", err)
		}
	})
	t.Run("no candidates", func(t *testing.T) {
		t.Parallel()
		writer := &recordingFingerprintWriter{}
		loader := &fingerprintedDeployableUnitLoader{complete: true, fingerprint: "fp"}
		handler := DeployableUnitCorrelationHandler{
			FactLoader:             &stubDeployableUnitFactLoader{},
			ResolvedLoader:         loader,
			PhasePublisher:         &recordingGraphProjectionPhasePublisher{},
			InputFingerprintWriter: writer,
		}
		if _, err := handler.Handle(context.Background(), deployableUnitIntent("repo:repo-edge-api")); err != nil {
			t.Fatalf("Handle() error = %v, want nil", err)
		}
		if loader.fusedCalls != 0 || len(writer.records) != 0 {
			t.Fatalf("no-candidate item: fused calls=%d records=%d, want 0 and 0", loader.fusedCalls, len(writer.records))
		}
	})
}
