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
// generation: the rows persist, and the reported result carries the
// inserted count plus the full resolved repo scope the wake runs over.
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

// stubCorrelationWaker records wake calls and returns a configured result.
// It deliberately does not model the wake predicate: which items reopen is
// the production SQL's contract, proven on live Postgres in the postgres
// package (#8294 review F4). These tests assert only the handler wiring:
// when the wake runs, with which repos, and that its failure fails the pass.
type stubCorrelationWaker struct {
	wakeCalls [][]string
	result    CompletionWakeResult
	wakeErr   error
}

func (w *stubCorrelationWaker) ReopenDeployableUnitCorrelationsForRepos(
	_ context.Context,
	repoIDs []string,
) (CompletionWakeResult, error) {
	if w.wakeErr != nil {
		return CompletionWakeResult{}, w.wakeErr
	}
	w.wakeCalls = append(w.wakeCalls, append([]string(nil), repoIDs...))
	return w.result, nil
}

// Regression test for #8294: the resolution pass that appends rows runs the
// completion wake over the resolved repo scope in that same pass, not at
// the next maintenance pass.
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
	waker := &stubCorrelationWaker{result: CompletionWakeResult{Reopened: 1}}
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
	if got, want := waker.wakeCalls[0], []string{"infra-repo", "app-repo"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("wake repos = %v, want %v", got, want)
	}
}

// A retry whose upsert inserts nothing (the first pass autocommitted the
// rows, then its wake failed or the process died) must still run the wake
// over the resolved repos, or the lost wake never converges (#8294 review
// F3). Only an empty repo scope skips it.
func TestCrossRepoResolutionWakesOnRetryThatInsertedNothing(t *testing.T) {
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
		upsertResult: ResolvedUpsertResult{Inserted: 0, RepoIDs: []string{"infra-repo", "app-repo"}},
	}
	waker := &stubCorrelationWaker{}
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
		t.Fatalf("wake calls = %d, want 1 on a retry that inserted nothing", len(waker.wakeCalls))
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
