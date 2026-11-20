// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"
	"errors"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
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
) ([]relationships.ResolvedRelationship, []string, bool, reducercontract.ResolvedReadFingerprint, error) {
	s.fencedCalls++
	s.fencedRepoIDs = append(s.fencedRepoIDs, append([]string(nil), repoIDs...))
	read := reducercontract.ResolvedReadFingerprint{
		RepoIDs: append([]string(nil), repoIDs...),
		Value:   s.fingerprint,
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

// nonFingerprintedRestrictedStubLoader offers only the non-fingerprinted
// restricted read, so the handler must take the silent fallback branch:
// rows attach, the read reports unfingerprinted, and nothing records.
type nonFingerprintedRestrictedStubLoader struct {
	fenced   []relationships.ResolvedRelationship
	blocking []string
	complete bool
	calls    int
}

func (s *nonFingerprintedRestrictedStubLoader) GetResolvedRelationshipsForReposWithCorpusFence(
	_ context.Context,
	_ []string,
) ([]relationships.ResolvedRelationship, bool, error) {
	return nil, false, errors.New("non-fingerprinted narrow stub: the global fused read must not run when the restricted read is wired")
}

func (s *nonFingerprintedRestrictedStubLoader) GetResolvedRelationshipsForReposWithRestrictedFence(
	_ context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, []string, bool, error) {
	s.calls++
	if !s.complete {
		return nil, append([]string(nil), s.blocking...), false, nil
	}
	return append([]relationships.ResolvedRelationship(nil), s.fenced...),
		append([]string(nil), s.blocking...), true, nil
}

// recordingServiceCatalogReopenFingerprintRecorder captures the fingerprint
// a claimed run records, so tests prove the write instead of its absence.
type recordingServiceCatalogReopenFingerprintRecorder struct {
	workItemIDs  []string
	claimedAts   []time.Time
	fingerprints []reducercontract.ResolvedReadFingerprint
	err          error
}

func (r *recordingServiceCatalogReopenFingerprintRecorder) RecordServiceCatalogCorrelationReopenFingerprint(
	_ context.Context,
	workItemID string,
	claimedAt time.Time,
	fingerprint reducercontract.ResolvedReadFingerprint,
) error {
	if r.err != nil {
		return r.err
	}
	r.workItemIDs = append(r.workItemIDs, workItemID)
	r.claimedAts = append(r.claimedAts, claimedAt)
	r.fingerprints = append(r.fingerprints, fingerprint)
	return nil
}

// TestPerRepoWaitNarrowServiceCatalogNonFingerprintedFallbackAttachesOnly
// proves the silent fallback branch: a loader with only the
// non-fingerprinted restricted read still attaches deployment evidence, but
// the read reports unfingerprinted so the reopen gate records nothing.
func TestPerRepoWaitNarrowServiceCatalogNonFingerprintedFallbackAttachesOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	loader := &nonFingerprintedRestrictedStubLoader{
		fenced:   fenceTestFullSet(),
		blocking: []string{"scope-deploy"},
		complete: true,
	}
	handler := ServiceCatalogCorrelationHandler{DeploymentRelationshipLoader: loader}
	writes := []ServiceMaterializationWrite{{ServiceID: "component:default/checkout"}}
	decisions := []ServiceCatalogCorrelationDecision{
		{ServiceID: "component:default/checkout", RepositoryID: "repo-checkout"},
	}

	fingerprint, fingerprinted, err := handler.attachServiceRelationshipEvidence(
		ctx, "scope-test", "generation-1", writes, decisions)
	if err != nil {
		t.Fatalf("attach error = %v, want nil on the complete fallback read", err)
	}
	if fingerprinted {
		t.Fatal("fingerprinted = true, want false: the fallback read carries no input fingerprint")
	}
	if len(fingerprint.RepoIDs) != 0 || fingerprint.Value != "" {
		t.Fatalf("fingerprint = %+v, want zero on the fallback read", fingerprint)
	}
	if len(writes[0].Deployment) == 0 {
		t.Fatal("deployment evidence is empty, want the fallback read's rows attached")
	}
	if loader.calls != 1 {
		t.Fatalf("restricted read calls = %d, want 1", loader.calls)
	}
}

// TestPerRepoWaitNarrowServiceCatalogClaimedRunRecordsFingerprint proves
// the record gate end to end at the handler seam: a claimed run with a
// fingerprinted narrowed read records exactly its fingerprint under its
// claim; an unclaimed or unfingerprinted run records nothing; a recorder
// failure fails the run instead of succeeding silently.
func TestPerRepoWaitNarrowServiceCatalogClaimedRunRecordsFingerprint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	claimedAt := time.Now().UTC()
	claimed := fenceTestIntent()
	claimed.IntentID = "intent-service-catalog-claimed"
	claimed.ClaimedAt = &claimedAt
	fingerprint := reducercontract.ResolvedReadFingerprint{
		RepoIDs: []string{"repo-checkout"},
		Value:   "fp-narrow-claimed",
	}

	recorder := &recordingServiceCatalogReopenFingerprintRecorder{}
	handler := ServiceCatalogCorrelationHandler{ReopenFingerprintRecorder: recorder}
	if err := handler.recordServiceCatalogReopenFingerprint(ctx, claimed, fingerprint, true); err != nil {
		t.Fatalf("record error = %v, want nil for the claimed fingerprinted run", err)
	}
	if len(recorder.fingerprints) != 1 {
		t.Fatalf("recorded fingerprints = %d, want 1", len(recorder.fingerprints))
	}
	if recorder.workItemIDs[0] != claimed.IntentID || !recorder.claimedAts[0].Equal(claimedAt) {
		t.Fatalf("recorded claim = %v @ %v, want %v @ %v",
			recorder.workItemIDs[0], recorder.claimedAts[0], claimed.IntentID, claimedAt)
	}
	if got := recorder.fingerprints[0]; len(got.RepoIDs) != 1 || got.RepoIDs[0] != "repo-checkout" || got.Value != "fp-narrow-claimed" {
		t.Fatalf("recorded fingerprint = %+v, want %+v", recorder.fingerprints[0], fingerprint)
	}

	silent := &recordingServiceCatalogReopenFingerprintRecorder{}
	silentHandler := ServiceCatalogCorrelationHandler{ReopenFingerprintRecorder: silent}
	unclaimed := fenceTestIntent()
	if err := silentHandler.recordServiceCatalogReopenFingerprint(ctx, unclaimed, fingerprint, true); err != nil {
		t.Fatalf("unclaimed record error = %v, want nil", err)
	}
	if err := silentHandler.recordServiceCatalogReopenFingerprint(ctx, claimed, fingerprint, false); err != nil {
		t.Fatalf("unfingerprinted record error = %v, want nil", err)
	}
	nilHandler := ServiceCatalogCorrelationHandler{}
	if err := nilHandler.recordServiceCatalogReopenFingerprint(ctx, claimed, fingerprint, true); err != nil {
		t.Fatalf("nil-recorder record error = %v, want nil", err)
	}
	if len(silent.fingerprints) != 0 {
		t.Fatalf("recorded fingerprints = %d, want 0: unclaimed, unfingerprinted, and nil-recorder runs record nothing",
			len(silent.fingerprints))
	}

	failing := &recordingServiceCatalogReopenFingerprintRecorder{err: errors.New("fingerprint write rejected")}
	failingHandler := ServiceCatalogCorrelationHandler{ReopenFingerprintRecorder: failing}
	if err := failingHandler.recordServiceCatalogReopenFingerprint(ctx, claimed, fingerprint, true); err == nil {
		t.Fatal("record error = nil, want the recorder failure to fail the run")
	}
}
