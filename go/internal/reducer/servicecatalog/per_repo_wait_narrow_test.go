// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// Per-repo-wait narrowed-path differential tests (#10877), service-catalog leg.
//
// These replay the per_repo_wait_accuracy_baseline_test service-catalog seeds
// through the narrowed read and require exact fingerprint equality with the
// global-path clean runs. The stub implements the package-local restricted
// interfaces (never the reducer root's, per #6061).

// restrictedCorpusFenceStubLoader serves the narrowed fused read: rows, the
// verdict-time blocking scope set, and the verdict, scripted per test.
type restrictedCorpusFenceStubLoader struct {
	fenced      []relationships.ResolvedRelationship
	blocking    []string
	complete    bool
	fingerprint string

	fencedCalls   int
	fencedRepoIDs [][]string
}

func (s *restrictedCorpusFenceStubLoader) GetResolvedRelationshipsForReposWithCorpusFence(
	_ context.Context,
	_ []string,
) ([]relationships.ResolvedRelationship, bool, error) {
	return nil, false, errors.New("narrow stub: the global fused read must not run when the restricted read is wired")
}

func (s *restrictedCorpusFenceStubLoader) GetResolvedRelationshipsForReposWithRestrictedFence(
	_ context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, []string, bool, error) {
	s.fencedCalls++
	s.fencedRepoIDs = append(s.fencedRepoIDs, append([]string(nil), repoIDs...))
	if !s.complete {
		return nil, append([]string(nil), s.blocking...), false, nil
	}
	return append([]relationships.ResolvedRelationship(nil), s.fenced...), append([]string(nil), s.blocking...), true, nil
}

func (s *restrictedCorpusFenceStubLoader) GetFingerprintedResolvedRelationshipsForReposWithRestrictedFence(
	_ context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, []string, bool, serviceCatalogRestrictedRead, error) {
	s.fencedCalls++
	s.fencedRepoIDs = append(s.fencedRepoIDs, append([]string(nil), repoIDs...))
	read := serviceCatalogRestrictedRead{
		RepoIDs:          append([]string(nil), repoIDs...),
		BlockingScopeIDs: append([]string(nil), s.blocking...),
		Value:            s.fingerprint,
	}
	if !s.complete {
		return nil, append([]string(nil), s.blocking...), false, read, nil
	}
	return append([]relationships.ResolvedRelationship(nil), s.fenced...), append([]string(nil), s.blocking...), true, read, nil
}

func runNarrowFenceTestHandler(
	t *testing.T,
	loader *restrictedCorpusFenceStubLoader,
) (fingerprint string, correlations *recordingServiceCatalogCorrelationWriter, materialization *recordingServiceMaterializationWriter, err error) {
	t.Helper()
	correlations = &recordingServiceCatalogCorrelationWriter{}
	materialization = &recordingServiceMaterializationWriter{}
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:                   fenceTestFactLoader(),
		Writer:                       correlations,
		MaterializationWriter:        materialization,
		DeploymentRelationshipLoader: loader,
	}
	_, err = handler.Handle(context.Background(), fenceTestIntent())
	if err != nil {
		return "", correlations, materialization, err
	}
	return perRepoWaitServiceCatalogFingerprint(correlations, materialization), correlations, materialization, nil
}

func TestPerRepoWaitNarrowServiceCatalogShouldMatch(t *testing.T) {
	t.Parallel()

	_, deferredCorrelations, deferredMaterialization, err := runNarrowFenceTestHandler(t,
		&restrictedCorpusFenceStubLoader{blocking: []string{"scope-deploy"}})
	if err == nil {
		t.Fatal("narrow first attempt error = nil, want a corpus-fence deferral")
	}
	var classified interface {
		Retryable() bool
		FailureClass() string
	}
	if !errors.As(err, &classified) {
		t.Fatalf("narrow first attempt error = %v (%T), want a retryable classified deferral", err, err)
	}
	if got, want := classified.FailureClass(), ServiceCatalogCorrelationResolutionNotReadyFailureClass; got != want {
		t.Fatalf("narrow first attempt FailureClass() = %q, want %q", got, want)
	}
	if deferredCorrelations.calls != 0 || len(deferredMaterialization.writes) != 0 {
		t.Fatalf("narrow deferral writes = %d correlation / %d materialization, want 0/0",
			deferredCorrelations.calls, len(deferredMaterialization.writes))
	}

	woken, _, _, err := runNarrowFenceTestHandler(t,
		&restrictedCorpusFenceStubLoader{fenced: fenceTestFullSet(), complete: true, fingerprint: "fp-sc-woken"})
	if err != nil {
		t.Fatalf("narrow retry error = %v, want nil once the foreign scope activates", err)
	}
	_, cleanCorrelations, cleanMaterialization, err := runFenceTestHandler(t,
		&corpusFenceStubLoader{fenced: fenceTestFullSet(), complete: true})
	if err != nil {
		t.Fatalf("global clean run error = %v, want nil", err)
	}
	if want := perRepoWaitServiceCatalogFingerprint(cleanCorrelations, cleanMaterialization); woken != want {
		t.Fatalf("narrow retry fingerprint = %q, global clean = %q", woken, want)
	}
}

func TestPerRepoWaitNarrowServiceCatalogDeferralCarriesWaitKey(t *testing.T) {
	t.Parallel()

	loader := &restrictedCorpusFenceStubLoader{blocking: []string{"scope-deploy", "scope-other"}}
	_, _, _, err := runNarrowFenceTestHandler(t, loader)
	if err == nil {
		t.Fatal("narrow seed error = nil, want a corpus-fence deferral")
	}
	var narrowing interface {
		CorpusWaitRepoIDs() []string
		CorpusWaitBlockingScopeIDs() []string
	}
	if !errors.As(err, &narrowing) {
		t.Fatalf("deferral error = %v (%T), want narrowing-carrying deferral", err, err)
	}
	if got := narrowing.CorpusWaitRepoIDs(); len(got) != 1 || got[0] != "repo-checkout" {
		t.Fatalf("deferral repo ids = %v, want [repo-checkout]", got)
	}
	if got := narrowing.CorpusWaitBlockingScopeIDs(); len(got) != 2 || got[0] != "scope-deploy" || got[1] != "scope-other" {
		t.Fatalf("deferral blocking scopes = %v, want [scope-deploy scope-other]", got)
	}
	if len(loader.fencedRepoIDs) != 1 || len(loader.fencedRepoIDs[0]) != 1 || loader.fencedRepoIDs[0][0] != "repo-checkout" {
		t.Fatalf("narrow fenced read repo ids = %v, want [[repo-checkout]]", loader.fencedRepoIDs)
	}
}
