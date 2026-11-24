// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// supportsNotReadyStubError stands in for the supports not-ready refusal the
// postgres loader returns (supplychaincore.SupplyChainImpactSupportsNotReadyError):
// retryable, an expected wait, the non-counting class. The servicecatalog family
// does not import the supply-chain core, so the handler proof uses a stand-in with
// the same contract.
type supportsNotReadyStubError struct{}

func (supportsNotReadyStubError) Error() string      { return "supports not ready" }
func (supportsNotReadyStubError) Retryable() bool    { return true }
func (supportsNotReadyStubError) ExpectedWait() bool { return true }
func (supportsNotReadyStubError) FailureClass() string {
	return "supply_chain_impact_supports_not_ready"
}

type refusingVulnerabilityLoader struct{ calls int }

func (l *refusingVulnerabilityLoader) GetSupplyChainAdvisoriesForRepos(
	context.Context,
	[]string,
) (map[string][]ServiceVulnerabilityRecord, error) {
	l.calls++
	return nil, supportsNotReadyStubError{}
}

// TestServiceCatalogHandlerDefersBeforeAnyWriteWhenVulnerabilitySupportsNotReady
// pins the pre-write fence (#11199, arbiter-b2c decision 3): a not-ready
// supports table must defer the intent with nothing written. The loader used to
// run after WriteServiceCatalogCorrelations, so a refusal there rewrote the
// correlation facts on every retry cycle. The refusal reaches the queue as the
// retryable, non-counting class, and neither the correlation writer nor the
// materialization writer is called.
func TestServiceCatalogHandlerDefersBeforeAnyWriteWhenVulnerabilitySupportsNotReady(t *testing.T) {
	t.Parallel()

	loader := &stubServiceCatalogCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			serviceTypedCatalogEntityFact("entity", "component:default/checkout", "Checkout"),
			serviceCatalogOwnershipFact("ownership", "component:default/checkout", "team-payments"),
			serviceCatalogRepositoryIDLinkFact("repo-link", "component:default/checkout", "repo-checkout"),
		},
		activeRepos: []facts.Envelope{
			repositoryFact("repo-checkout", "checkout", "https://github.com/acme/checkout.git", false),
		},
	}
	refusing := &refusingVulnerabilityLoader{}
	writer := &recordingServiceCatalogCorrelationWriter{}
	materialization := newFakeServiceMaterializationStore()
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:                  loader,
		Writer:                      writer,
		MaterializationWriter:       PostgresServiceMaterializationWriter{DB: materialization, Now: time.Now},
		VulnerabilityEvidenceLoader: refusing,
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-service-catalog",
		ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
		GenerationID: "generation-service-catalog",
		Domain:       reducercontract.DomainServiceCatalogCorrelation,
		SourceSystem: "service_catalog",
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want the supports not-ready refusal")
	}
	if !reducercontract.IsRetryable(err) || !reducercontract.IsExpectedWait(err) {
		t.Fatalf("Handle() error = %v, want a retryable expected wait so the queue defers it non-counting", err)
	}
	if refusing.calls != 1 {
		t.Fatalf("vulnerability loader calls = %d, want 1", refusing.calls)
	}
	if writer.calls != 0 {
		t.Fatalf("correlation writer calls = %d, want 0: a not-ready refusal must defer before any write", writer.calls)
	}
	if len(materialization.generations) != 0 || len(materialization.snapshots) != 0 {
		t.Fatalf("materialization store wrote %d generations, %d snapshots; want nothing", len(materialization.generations), len(materialization.snapshots))
	}
}
