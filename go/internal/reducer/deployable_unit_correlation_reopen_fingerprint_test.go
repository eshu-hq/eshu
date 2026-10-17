// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// The #8283 success-write contract of the deployable_unit_correlation
// handler: the reopen fingerprint is recorded once, after every graph write,
// with the value the fenced read returned, by the exact claim that ran; no
// deferral and no failed write records one.

// fingerprintedResolvedLoader serves the fenced by-repos read with the input
// fingerprint the production store returns from the same statement.
type fingerprintedResolvedLoader struct {
	resolved    []relationships.ResolvedRelationship
	complete    bool
	fingerprint reducercontract.ResolvedReadFingerprint
	reads       int
}

func (l *fingerprintedResolvedLoader) GetResolvedRelationships(
	context.Context, string,
) ([]relationships.ResolvedRelationship, error) {
	return nil, nil
}

func (l *fingerprintedResolvedLoader) GetResolvedRelationshipsForReposWithCorpusFence(
	context.Context, []string,
) ([]relationships.ResolvedRelationship, bool, error) {
	return l.resolved, l.complete, nil
}

func (l *fingerprintedResolvedLoader) GetFingerprintedResolvedRelationshipsForReposWithCorpusFence(
	context.Context, []string,
) ([]relationships.ResolvedRelationship, bool, reducercontract.ResolvedReadFingerprint, error) {
	l.reads++
	return l.resolved, l.complete, l.fingerprint, nil
}

// fingerprintRecord is one recorded success write.
type fingerprintRecord struct {
	workItemID  string
	claimedAt   time.Time
	fingerprint reducercontract.ResolvedReadFingerprint
}

type recordingReopenFingerprints struct {
	records []fingerprintRecord
	err     error
}

func (r *recordingReopenFingerprints) RecordDeployableUnitReopenFingerprint(
	_ context.Context, workItemID string, claimedAt time.Time, fingerprint reducercontract.ResolvedReadFingerprint,
) error {
	if r.err != nil {
		return r.err
	}
	r.records = append(r.records, fingerprintRecord{workItemID, claimedAt, fingerprint})
	return nil
}

// erroringFingerprintEdgeWriter fails every edge retract and write.
type erroringFingerprintEdgeWriter struct{}

func (erroringFingerprintEdgeWriter) RetractEdges(context.Context, string, []SharedProjectionIntentRow, string) error {
	return errors.New("graph retract failed")
}

func (erroringFingerprintEdgeWriter) WriteEdges(
	context.Context, string, []SharedProjectionIntentRow, string,
) (SharedProjectionWriteReport, error) {
	return SharedProjectionWriteReport{}, errors.New("graph write failed")
}

func claimedDeployableUnitIntent(claimedAt time.Time) Intent {
	intent := deployableUnitIntent("repo:repo-edge-api")
	intent.ClaimedAt = &claimedAt
	return intent
}

func edgeAPIDeploysFrom() []relationships.ResolvedRelationship {
	return []relationships.ResolvedRelationship{{
		SourceRepoID:     "repo-deployments",
		TargetRepoID:     "repo-edge-api",
		RelationshipType: relationships.RelDeploysFrom,
		Confidence:       0.94,
	}}
}

func TestDeployableUnitCorrelationRecordsReadFingerprintOnSuccess(t *testing.T) {
	t.Parallel()

	claimedAt := time.Date(2026, time.October, 16, 23, 0, 0, 0, time.UTC)
	read := reducercontract.ResolvedReadFingerprint{RepoIDs: []string{"repo-edge-api"}, Value: "read-snapshot"}
	recorder := &recordingReopenFingerprints{}
	handler := DeployableUnitCorrelationHandler{
		FactLoader:                &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader:            &fingerprintedResolvedLoader{resolved: edgeAPIDeploysFrom(), complete: true, fingerprint: read},
		EdgeWriter:                &recordingDeployableUnitEdgeWriter{},
		ReopenFingerprintRecorder: recorder,
	}

	got, err := handler.Handle(context.Background(), claimedDeployableUnitIntent(claimedAt))
	if err != nil || got.Status != ResultStatusSucceeded {
		t.Fatalf("Handle() = %+v, %v; want succeeded", got, err)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("fingerprint records = %d, want exactly 1 on success", len(recorder.records))
	}
	rec := recorder.records[0]
	if rec.workItemID != "intent-1" || !rec.claimedAt.Equal(claimedAt) {
		t.Fatalf("fingerprint record claim = %q@%s, want intent-1@%s", rec.workItemID, rec.claimedAt, claimedAt)
	}
	if rec.fingerprint.Value != read.Value || len(rec.fingerprint.RepoIDs) != 1 || rec.fingerprint.RepoIDs[0] != "repo-edge-api" {
		t.Fatalf("recorded fingerprint = %+v, want the fenced read's %+v", rec.fingerprint, read)
	}
}

// A run with no workload candidate reads no resolved input, so its output
// cannot change with resolution: it records an empty repository set.
func TestDeployableUnitCorrelationRecordsEmptyInputWithoutCandidates(t *testing.T) {
	t.Parallel()

	loader := &fingerprintedResolvedLoader{complete: true}
	recorder := &recordingReopenFingerprints{}
	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{
			envelopes: deployableUnitCorrelationEnvelopes("repo-edge-api", "edge-api", nil),
		},
		ResolvedLoader:            loader,
		EdgeWriter:                &recordingDeployableUnitEdgeWriter{},
		ReopenFingerprintRecorder: recorder,
	}

	if _, err := handler.Handle(context.Background(), claimedDeployableUnitIntent(time.Now().UTC())); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(recorder.records) != 1 || len(recorder.records[0].fingerprint.RepoIDs) != 0 ||
		recorder.records[0].fingerprint.Value != reducercontract.EmptyResolvedReadFingerprint {
		t.Fatalf("fingerprint records = %+v, want one empty repository set with the empty-input sentinel", recorder.records)
	}
	if loader.reads != 0 {
		t.Fatalf("fenced resolved reads = %d, want 0 without a candidate", loader.reads)
	}
}

func TestDeployableUnitCorrelationRecordsNoFingerprintWithoutSuccess(t *testing.T) {
	t.Parallel()

	read := reducercontract.ResolvedReadFingerprint{RepoIDs: []string{"repo-edge-api"}, Value: "read-snapshot"}
	for _, tc := range []struct {
		name    string
		wantErr bool
		mutate  func(*DeployableUnitCorrelationHandler, *Intent)
	}{
		{"own_gate_closed", true, func(h *DeployableUnitCorrelationHandler, _ *Intent) {
			h.ResolutionActiveLookup = stubResolutionActiveLookup(map[string]bool{})
		}},
		{"canonical_nodes_not_ready", true, func(h *DeployableUnitCorrelationHandler, _ *Intent) {
			h.CanonicalQuiescence = staticReducerGraphDrain{uncommittedCanonical: true}
		}},
		{"corpus_fence_incomplete", true, func(h *DeployableUnitCorrelationHandler, _ *Intent) {
			h.ResolvedLoader = &fingerprintedResolvedLoader{resolved: edgeAPIDeploysFrom(), fingerprint: read}
		}},
		{"edge_write_failed", true, func(h *DeployableUnitCorrelationHandler, _ *Intent) {
			h.EdgeWriter = erroringFingerprintEdgeWriter{}
		}},
		{"unclaimed_run", false, func(_ *DeployableUnitCorrelationHandler, intent *Intent) {
			intent.ClaimedAt = nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recorder := &recordingReopenFingerprints{}
			handler := DeployableUnitCorrelationHandler{
				FactLoader:                &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
				ResolvedLoader:            &fingerprintedResolvedLoader{resolved: edgeAPIDeploysFrom(), complete: true, fingerprint: read},
				EdgeWriter:                &recordingDeployableUnitEdgeWriter{},
				ReopenFingerprintRecorder: recorder,
			}
			intent := claimedDeployableUnitIntent(time.Now().UTC())
			tc.mutate(&handler, &intent)

			_, err := handler.Handle(context.Background(), intent)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Handle() error = %v, want error %v", err, tc.wantErr)
			}
			if len(recorder.records) != 0 {
				t.Fatalf("fingerprint records = %+v, want none", recorder.records)
			}
		})
	}
}

// A rejected success write (the claim moved on) fails the run so the stale
// claim never reports success over a fresher claim's input.
func TestDeployableUnitCorrelationFailsWhenFingerprintWriteIsRejected(t *testing.T) {
	t.Parallel()

	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{envelopes: dockerfileCandidateEnvelopes()},
		ResolvedLoader: &fingerprintedResolvedLoader{
			resolved: edgeAPIDeploysFrom(), complete: true,
			fingerprint: reducercontract.ResolvedReadFingerprint{RepoIDs: []string{"repo-edge-api"}, Value: "v"},
		},
		EdgeWriter:                &recordingDeployableUnitEdgeWriter{},
		ReopenFingerprintRecorder: &recordingReopenFingerprints{err: reducercontract.ErrExecutionClaimRejected},
	}

	_, err := handler.Handle(context.Background(), claimedDeployableUnitIntent(time.Now().UTC()))
	if !errors.Is(err, reducercontract.ErrExecutionClaimRejected) {
		t.Fatalf("Handle() error = %v, want ErrExecutionClaimRejected", err)
	}
}
