// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"
	"errors"
	"reflect"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// corpusFenceStubLoader models the production RelationshipStore during a
// foreign scope's retired-or-pending relationship generation: the unfenced
// by-repos read silently omits that scope's rows (docs/internal/evidence/
// 6740-corpus-fence-snapshot.md, "Negative control"), while the fused read
// reports the fence as open.
type corpusFenceStubLoader struct {
	// partial is what the unfenced read returns while the fence is open: the
	// foreign scope's rows are missing.
	partial []relationships.ResolvedRelationship
	// fenced is what the fused read returns when complete is true.
	fenced   []relationships.ResolvedRelationship
	complete bool
	fenceErr error

	unfencedCalls int
	fencedCalls   int
	fencedRepoIDs [][]string
}

func (s *corpusFenceStubLoader) GetResolvedRelationshipsForRepos(
	_ context.Context,
	_ []string,
) ([]relationships.ResolvedRelationship, error) {
	s.unfencedCalls++
	return append([]relationships.ResolvedRelationship(nil), s.partial...), nil
}

func (s *corpusFenceStubLoader) GetResolvedRelationshipsForReposWithCorpusFence(
	_ context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, bool, error) {
	s.fencedCalls++
	s.fencedRepoIDs = append(s.fencedRepoIDs, append([]string(nil), repoIDs...))
	if s.fenceErr != nil {
		return nil, false, s.fenceErr
	}
	if !s.complete {
		return nil, false, nil
	}
	return append([]relationships.ResolvedRelationship(nil), s.fenced...), true, nil
}

// recordingServiceMaterializationWriter records every per-service write so a
// test can assert on exactly what would have been committed.
type recordingServiceMaterializationWriter struct {
	writes []ServiceMaterializationWrite
}

func (w *recordingServiceMaterializationWriter) WriteServiceMaterialization(
	_ context.Context,
	write ServiceMaterializationWrite,
) (ServiceMaterializationWriteResult, error) {
	w.writes = append(w.writes, write)
	return ServiceMaterializationWriteResult{Committed: true}, nil
}

// fenceTestOwnRelationship is the service repository's own deployment
// relationship, produced by its own (active) scope.
var fenceTestOwnRelationship = relationships.ResolvedRelationship{
	SourceRepoID: "repo-checkout", TargetRepoID: "repo-deploy",
	RelationshipType: relationships.RelDeploysFrom, Confidence: 0.9,
}

// fenceTestForeignRelationship is resolved by a foreign scope whose
// relationship generation is retired-or-pending in the deferral cases. It
// still touches repo-checkout, so the by-repos read returns it only while
// that foreign generation is active.
var fenceTestForeignRelationship = relationships.ResolvedRelationship{
	SourceRepoID: "repo-checkout", TargetRepoID: "repo-infra",
	RelationshipType: relationships.RelDiscoversConfigIn, Confidence: 0.8,
}

func fenceTestFactLoader() *stubServiceCatalogCorrelationFactLoader {
	return &stubServiceCatalogCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			serviceTypedCatalogEntityFact("entity", "component:default/checkout", "Checkout"),
			serviceCatalogOwnershipFact("ownership", "component:default/checkout", "team-payments"),
			serviceCatalogRepositoryIDLinkFact("repo-link", "component:default/checkout", "repo-checkout"),
		},
		activeRepos: []facts.Envelope{
			repositoryFact("repo-checkout", "checkout", "https://github.com/acme/checkout.git", false),
		},
	}
}

func fenceTestIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-service-catalog",
		ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		GenerationID: "generation-service-catalog",
		Domain:       reducercontract.DomainServiceCatalogCorrelation,
		SourceSystem: "service_catalog",
	}
}

// TestServiceCatalogHandlerDefersWhileRelationshipCorpusFenceOpen is the #7258
// regression. While a foreign scope's relationship generation is
// retired-or-pending, the unfenced by-repos read omits that scope's rows. The
// handler used to materialize the service generation from that partial set,
// superseding the prior generation and reporting spurious removed deployment
// evidence on changed-since, with nothing ever reopening the intent. It must
// instead defer with a non-counting readiness class and write nothing.
func TestServiceCatalogHandlerDefersWhileRelationshipCorpusFenceOpen(t *testing.T) {
	t.Parallel()

	loader := &corpusFenceStubLoader{
		partial:  []relationships.ResolvedRelationship{fenceTestOwnRelationship},
		complete: false,
	}
	correlations := &recordingServiceCatalogCorrelationWriter{}
	materialization := &recordingServiceMaterializationWriter{}
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:                   fenceTestFactLoader(),
		Writer:                       correlations,
		MaterializationWriter:        materialization,
		DeploymentRelationshipLoader: loader,
	}

	_, err := handler.Handle(context.Background(), fenceTestIntent())
	if err == nil {
		var deploymentRows int
		for _, write := range materialization.writes {
			deploymentRows += len(write.Deployment)
		}
		t.Fatalf("Handle() error = nil, want a corpus-fence deferral; the handler materialized %d service write(s) with %d deployment row(s) from a partial resolved set (foreign scope's rows missing)",
			len(materialization.writes), deploymentRows)
	}
	var classified interface {
		Retryable() bool
		FailureClass() string
	}
	if !errors.As(err, &classified) {
		t.Fatalf("Handle() error = %v (%T), want a retryable classified deferral", err, err)
	}
	if !classified.Retryable() {
		t.Fatal("corpus-fence deferral must be retryable")
	}
	if got, want := classified.FailureClass(), "service_catalog_correlation_resolution_not_ready"; got != want {
		t.Fatalf("FailureClass() = %q, want %q", got, want)
	}
	if correlations.calls != 0 {
		t.Fatalf("WriteServiceCatalogCorrelations calls = %d, want 0: a deferral must write nothing so the retry does not re-write correlation facts", correlations.calls)
	}
	if len(materialization.writes) != 0 {
		t.Fatalf("WriteServiceMaterialization calls = %d, want 0 while the corpus fence is open", len(materialization.writes))
	}
	if loader.unfencedCalls != 0 {
		t.Fatalf("unfenced GetResolvedRelationshipsForRepos calls = %d, want 0", loader.unfencedCalls)
	}
	if loader.fencedCalls != 1 {
		t.Fatalf("fenced read calls = %d, want 1", loader.fencedCalls)
	}
}

// fenceTestFullSet is the complete resolved set touching repo-checkout once
// every scope's relationship generation is active: the own deployment row, the
// foreign scope's deployment row, and one dependency row.
func fenceTestFullSet() []relationships.ResolvedRelationship {
	return []relationships.ResolvedRelationship{
		fenceTestOwnRelationship,
		fenceTestForeignRelationship,
		{SourceRepoID: "repo-checkout", TargetRepoID: "repo-lib", RelationshipType: relationships.RelDependsOn, Confidence: 0.7},
	}
}

func runFenceTestHandler(
	t *testing.T,
	loader *corpusFenceStubLoader,
) (reducercontract.Result, *recordingServiceCatalogCorrelationWriter, *recordingServiceMaterializationWriter, error) {
	t.Helper()
	correlations := &recordingServiceCatalogCorrelationWriter{}
	materialization := &recordingServiceMaterializationWriter{}
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:                   fenceTestFactLoader(),
		Writer:                       correlations,
		MaterializationWriter:        materialization,
		DeploymentRelationshipLoader: loader,
	}
	result, err := handler.Handle(context.Background(), fenceTestIntent())
	return result, correlations, materialization, err
}

func TestServiceCatalogHandlerMaterializesFencedRowsWhenCorpusComplete(t *testing.T) {
	t.Parallel()

	loader := &corpusFenceStubLoader{fenced: fenceTestFullSet(), complete: true}
	_, correlations, materialization, err := runFenceTestHandler(t, loader)
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.fencedCalls != 1 || loader.unfencedCalls != 0 {
		t.Fatalf("fenced/unfenced calls = %d/%d, want 1/0 (one bounded fused load shared by both families)", loader.fencedCalls, loader.unfencedCalls)
	}
	if got := loader.fencedRepoIDs[0]; len(got) != 1 || got[0] != "repo-checkout" {
		t.Fatalf("fenced read repo ids = %v, want [repo-checkout]", got)
	}
	if correlations.calls != 1 {
		t.Fatalf("WriteServiceCatalogCorrelations calls = %d, want 1", correlations.calls)
	}
	if len(materialization.writes) != 1 {
		t.Fatalf("service writes = %d, want 1", len(materialization.writes))
	}
	write := materialization.writes[0]
	wantDeployment := buildServiceDeploymentEvidence([]relationships.ResolvedRelationship{fenceTestOwnRelationship, fenceTestForeignRelationship})
	if len(write.Deployment) != 2 || len(wantDeployment) != 2 {
		t.Fatalf("deployment rows = %d, want 2 (own + foreign scope)", len(write.Deployment))
	}
	for i := range wantDeployment {
		if write.Deployment[i].Identity != wantDeployment[i].Identity {
			t.Fatalf("deployment[%d] identity = %q, want %q", i, write.Deployment[i].Identity, wantDeployment[i].Identity)
		}
	}
	if len(write.Dependencies) != 1 {
		t.Fatalf("dependency rows = %d, want 1", len(write.Dependencies))
	}
}

func TestServiceCatalogHandlerSkipsFenceWithoutRepository(t *testing.T) {
	t.Parallel()

	loader := &corpusFenceStubLoader{complete: false}
	correlations := &recordingServiceCatalogCorrelationWriter{}
	materialization := &recordingServiceMaterializationWriter{}
	handler := ServiceCatalogCorrelationHandler{
		FactLoader: &stubServiceCatalogCorrelationFactLoader{
			scopeFacts: []facts.Envelope{
				serviceTypedCatalogEntityFact("entity", "component:default/checkout", "Checkout"),
				serviceCatalogOwnershipFact("ownership", "component:default/checkout", "team-payments"),
			},
		},
		Writer:                       correlations,
		MaterializationWriter:        materialization,
		DeploymentRelationshipLoader: loader,
	}
	if _, err := handler.Handle(context.Background(), fenceTestIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil: no repository means no read and no fence", err)
	}
	if loader.fencedCalls != 0 || loader.unfencedCalls != 0 {
		t.Fatalf("relationship reads = %d fenced / %d unfenced, want none without a repository", loader.fencedCalls, loader.unfencedCalls)
	}
	if correlations.calls != 1 {
		t.Fatalf("WriteServiceCatalogCorrelations calls = %d, want 1", correlations.calls)
	}
}

func TestServiceCatalogHandlerSkipsFenceWithoutMaterializationWriter(t *testing.T) {
	t.Parallel()

	loader := &corpusFenceStubLoader{complete: false}
	correlations := &recordingServiceCatalogCorrelationWriter{}
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:                   fenceTestFactLoader(),
		Writer:                       correlations,
		DeploymentRelationshipLoader: loader,
	}
	if _, err := handler.Handle(context.Background(), fenceTestIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil: without a materialization writer nothing reads relationships", err)
	}
	if loader.fencedCalls != 0 || loader.unfencedCalls != 0 {
		t.Fatalf("relationship reads = %d fenced / %d unfenced, want none", loader.fencedCalls, loader.unfencedCalls)
	}
	if correlations.calls != 1 {
		t.Fatalf("WriteServiceCatalogCorrelations calls = %d, want 1", correlations.calls)
	}
}

func TestServiceCatalogHandlerFencedReadErrorIsNotADeferral(t *testing.T) {
	t.Parallel()

	readErr := errors.New("connection reset")
	loader := &corpusFenceStubLoader{fenceErr: readErr}
	_, correlations, materialization, err := runFenceTestHandler(t, loader)
	if !errors.Is(err, readErr) {
		t.Fatalf("Handle() error = %v, want wrapping %v", err, readErr)
	}
	var classified interface{ FailureClass() string }
	if errors.As(err, &classified) {
		t.Fatalf("read error classified as %q; a read failure must not masquerade as a non-counting readiness deferral", classified.FailureClass())
	}
	if correlations.calls != 0 || len(materialization.writes) != 0 {
		t.Fatalf("writes after read error = %d correlation / %d materialization, want 0/0", correlations.calls, len(materialization.writes))
	}
}

// TestServiceCatalogHandlerRetryAfterDeferralMatchesCleanRun proves the
// deferral is idempotent: the retry that runs once the fence closes produces
// exactly the writes a clean first run produces.
func TestServiceCatalogHandlerRetryAfterDeferralMatchesCleanRun(t *testing.T) {
	t.Parallel()

	correlations := &recordingServiceCatalogCorrelationWriter{}
	materialization := &recordingServiceMaterializationWriter{}
	loader := &corpusFenceStubLoader{fenced: fenceTestFullSet(), complete: false}
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:                   fenceTestFactLoader(),
		Writer:                       correlations,
		MaterializationWriter:        materialization,
		DeploymentRelationshipLoader: loader,
	}
	if _, err := handler.Handle(context.Background(), fenceTestIntent()); err == nil {
		t.Fatal("first attempt with the fence open: error = nil, want deferral")
	}
	loader.complete = true
	retryResult, err := handler.Handle(context.Background(), fenceTestIntent())
	if err != nil {
		t.Fatalf("retry Handle() error = %v, want nil", err)
	}

	cleanResult, cleanCorrelations, cleanMaterialization, err := runFenceTestHandler(
		t, &corpusFenceStubLoader{fenced: fenceTestFullSet(), complete: true},
	)
	if err != nil {
		t.Fatalf("clean Handle() error = %v", err)
	}
	if correlations.calls != 1 || cleanCorrelations.calls != 1 {
		t.Fatalf("correlation write calls retry/clean = %d/%d, want 1/1", correlations.calls, cleanCorrelations.calls)
	}
	if !reflect.DeepEqual(correlations.write, cleanCorrelations.write) {
		t.Fatalf("retry correlation write differs from clean run:\nretry: %#v\nclean: %#v", correlations.write, cleanCorrelations.write)
	}
	if !reflect.DeepEqual(materialization.writes, cleanMaterialization.writes) {
		t.Fatalf("retry materialization writes differ from clean run:\nretry: %#v\nclean: %#v", materialization.writes, cleanMaterialization.writes)
	}
	if !reflect.DeepEqual(retryResult, cleanResult) {
		t.Fatalf("retry result differs from clean run:\nretry: %#v\nclean: %#v", retryResult, cleanResult)
	}
}
