// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// stubCompletionPersister simulates an append under an already-active
// generation: the rows persist, and the reported inserted set names exactly
// the rows the ON CONFLICT DO NOTHING insert actually wrote.
type stubCompletionPersister struct {
	fakeResolutionPersister
	upsertResult ResolvedUpsertResult
	upsertErr    error
}

func (f *stubCompletionPersister) UpsertResolved(
	_ context.Context,
	_ string,
	resolved []relationships.ResolvedRelationship,
) (ResolvedUpsertResult, error) {
	if f.upsertErr != nil {
		return ResolvedUpsertResult{}, f.upsertErr
	}
	f.resolved = append(f.resolved, resolved...)
	return f.upsertResult, nil
}

// stubCorrelationWaker models the succeeded deployable_unit_correlation items
// by their recorded fingerprint repo_ids. A wake flips an item to pending
// exactly when its stored set overlaps the inserted repos, mirroring the
// single-UPDATE predicate, so the test seeds a succeeded dependent and
// asserts it reopens in the same Resolve call.
type stubCorrelationWaker struct {
	storedRepoIDs map[string][]string
	pending       map[string]bool
	wakeCalls     [][]string
	wakeErr       error
}

func (w *stubCorrelationWaker) ReopenDeployableUnitCorrelationsForRepos(
	_ context.Context,
	repoIDs []string,
) (int64, error) {
	if w.wakeErr != nil {
		return 0, w.wakeErr
	}
	w.wakeCalls = append(w.wakeCalls, append([]string(nil), repoIDs...))
	inserted := make(map[string]struct{}, len(repoIDs))
	for _, id := range repoIDs {
		inserted[id] = struct{}{}
	}
	var reopened int64
	for itemID, stored := range w.storedRepoIDs {
		if w.pending[itemID] {
			continue
		}
		for _, id := range stored {
			if _, ok := inserted[id]; ok {
				w.pending[itemID] = true
				reopened++
				break
			}
		}
	}
	return reopened, nil
}

// Seeded RED test for #8294: a deployable_unit_correlation item that
// succeeded before its dependency's generation appended rows must reopen in
// the same resolution pass that appended them, not at the next maintenance
// pass.
func TestCrossRepoResolutionWakesDeployableUnitCorrelationsOnInsertedRows(t *testing.T) {
	t.Parallel()

	evidenceLoader := &fakeEvidenceFactLoader{
		facts: []relationships.EvidenceFact{
			{
				EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
				RelationshipType: relationships.RelProvisionsDependencyFor,
				SourceRepoID:     "infra-repo",
				TargetRepoID:     "app-repo",
				Confidence:       0.99,
			},
		},
	}
	persister := &stubCompletionPersister{
		upsertResult: ResolvedUpsertResult{
			Inserted: 1,
			RepoIDs:  []string{"infra-repo", "app-repo"},
		},
	}
	waker := &stubCorrelationWaker{
		storedRepoIDs: map[string][]string{
			"duc-item-1": {"app-repo", "other-repo"},
			"duc-item-2": {"unrelated-repo"},
		},
		pending: map[string]bool{},
	}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader:  evidenceLoader,
		IntentWriter:    &recordingRepoDependencyIntentWriter{},
		Persister:       persister,
		CorrelationWake: waker,
	}

	if _, err := handler.Resolve(context.Background(), "scope-1", "gen-1"); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(waker.wakeCalls) != 1 {
		t.Fatalf("wake calls = %d, want 1 in the same pass", len(waker.wakeCalls))
	}
	if !waker.pending["duc-item-1"] {
		t.Fatal("overlapping succeeded item was not reopened in the same pass")
	}
	if waker.pending["duc-item-2"] {
		t.Fatal("non-overlapping item must stay succeeded")
	}
}

func TestCrossRepoResolutionSkipsWakeWhenNothingInserted(t *testing.T) {
	t.Parallel()

	evidenceLoader := &fakeEvidenceFactLoader{
		facts: []relationships.EvidenceFact{
			{
				EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
				RelationshipType: relationships.RelProvisionsDependencyFor,
				SourceRepoID:     "infra-repo",
				TargetRepoID:     "app-repo",
				Confidence:       0.99,
			},
		},
	}
	persister := &stubCompletionPersister{
		upsertResult: ResolvedUpsertResult{Inserted: 0},
	}
	waker := &stubCorrelationWaker{
		storedRepoIDs: map[string][]string{"duc-item-1": {"app-repo"}},
		pending:       map[string]bool{},
	}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader:  evidenceLoader,
		IntentWriter:    &recordingRepoDependencyIntentWriter{},
		Persister:       persister,
		CorrelationWake: waker,
	}

	if _, err := handler.Resolve(context.Background(), "scope-1", "gen-1"); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(waker.wakeCalls) != 0 {
		t.Fatalf("wake calls = %d, want 0 when nothing was inserted", len(waker.wakeCalls))
	}
	if waker.pending["duc-item-1"] {
		t.Fatal("item must stay succeeded when no row was appended")
	}
}

func TestCrossRepoResolutionWakeErrorFailsResolution(t *testing.T) {
	t.Parallel()

	evidenceLoader := &fakeEvidenceFactLoader{
		facts: []relationships.EvidenceFact{
			{
				EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
				RelationshipType: relationships.RelProvisionsDependencyFor,
				SourceRepoID:     "infra-repo",
				TargetRepoID:     "app-repo",
				Confidence:       0.99,
			},
		},
	}
	persister := &stubCompletionPersister{
		upsertResult: ResolvedUpsertResult{
			Inserted: 1,
			RepoIDs:  []string{"infra-repo", "app-repo"},
		},
	}
	waker := &stubCorrelationWaker{wakeErr: fmt.Errorf("wake unavailable")}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader:  evidenceLoader,
		IntentWriter:    &recordingRepoDependencyIntentWriter{},
		Persister:       persister,
		CorrelationWake: waker,
	}

	if _, err := handler.Resolve(context.Background(), "scope-1", "gen-1"); err == nil {
		t.Fatal("Resolve() error = nil, want the wake failure to fail the pass so it retries")
	}
}

func TestCrossRepoResolutionWithoutWakerSucceeds(t *testing.T) {
	t.Parallel()

	evidenceLoader := &fakeEvidenceFactLoader{
		facts: []relationships.EvidenceFact{
			{
				EvidenceKind:     relationships.EvidenceKindTerraformAppRepo,
				RelationshipType: relationships.RelProvisionsDependencyFor,
				SourceRepoID:     "infra-repo",
				TargetRepoID:     "app-repo",
				Confidence:       0.99,
			},
		},
	}
	handler := CrossRepoRelationshipHandler{
		EvidenceLoader: evidenceLoader,
		IntentWriter:   &recordingRepoDependencyIntentWriter{},
		Persister: &stubCompletionPersister{
			upsertResult: ResolvedUpsertResult{Inserted: 1, RepoIDs: []string{"app-repo"}},
		},
	}

	if _, err := handler.Resolve(context.Background(), "scope-1", "gen-1"); err != nil {
		t.Fatalf("Resolve() without a waker error = %v, want nil", err)
	}
}
