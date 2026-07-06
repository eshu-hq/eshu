// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestServiceCatalogCorrelationHandleIsIdempotentUnderReplay proves the typed
// decode conversion (Contract System v1 Wave 4f S3, issue #4755) did not
// introduce nondeterminism into the correlation decision set: calling Handle
// twice with the identical input envelopes (a queue redelivery / retry, the
// same intent replayed after a crash before Ack) produces byte-identical
// decisions both times, including a malformed fact's quarantine outcome. A
// reducer whose decode path allocated map-iteration-order-dependent state (a
// real risk when the previous read walked payload maps directly) would be
// vulnerable to this regressing; decoding through the typed contracts seam
// must not change that.
func TestServiceCatalogCorrelationHandleIsIdempotentUnderReplay(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		serviceCatalogEntityFact("entity-checkout", "component:default/checkout", "Checkout"),
		serviceCatalogOwnershipFact("owner-checkout", "component:default/checkout", "group:default/payments"),
		serviceCatalogRepositoryLinkFact("repo-link-checkout", "component:default/checkout", "https://github.com/acme/checkout.git"),
		serviceCatalogEntityFact("entity-ambiguous", "component:default/shared", "Shared"),
		serviceCatalogRepositoryLinkFact("repo-link-ambiguous", "component:default/shared", "https://github.com/acme/shared.git"),
		// A malformed fact must quarantine identically on every replay, never
		// flapping between quarantined and admitted.
		{
			FactID:   "malformed-entity",
			FactKind: facts.ServiceCatalogEntityFactKind,
			Payload:  map[string]any{"provider": "backstage"},
		},
	}
	activeRepos := []facts.Envelope{
		repositoryFact("repo-checkout", "checkout", "https://github.com/acme/checkout.git", false),
		repositoryFact("repo-shared-1", "shared-a", "https://github.com/acme/shared.git", false),
		repositoryFact("repo-shared-2", "shared-b", "git@github.com:acme/shared.git", false),
	}

	runOnce := func() ([]ServiceCatalogCorrelationDecision, []quarantinedFact) {
		loader := &stubServiceCatalogCorrelationFactLoader{
			scopeFacts:  append([]facts.Envelope(nil), envelopes...),
			activeRepos: append([]facts.Envelope(nil), activeRepos...),
		}
		writer := &recordingServiceCatalogCorrelationWriter{}
		handler := ServiceCatalogCorrelationHandler{FactLoader: loader, Writer: writer}
		if _, err := handler.Handle(context.Background(), Intent{
			IntentID:     "intent-replay",
			ScopeID:      "service-catalog-manifest://repo-checkout/catalog-info.yaml",
			GenerationID: "generation-replay",
			Domain:       DomainServiceCatalogCorrelation,
			SourceSystem: "service_catalog",
			Cause:        "replay proof",
		}); err != nil {
			t.Fatalf("Handle() error = %v, want nil", err)
		}
		index, quarantined, fatal := buildServiceCatalogCorrelationIndexWithQuarantine(
			append(append([]facts.Envelope(nil), envelopes...), activeRepos...),
		)
		if fatal != nil {
			t.Fatalf("fatal = %v, want nil", fatal)
		}
		return serviceCatalogDecisionsFromIndex(index), quarantined
	}

	firstDecisions, firstQuarantined := runOnce()
	secondDecisions, secondQuarantined := runOnce()

	if !reflect.DeepEqual(firstDecisions, secondDecisions) {
		t.Fatalf("replay produced different decisions:\nfirst:  %#v\nsecond: %#v", firstDecisions, secondDecisions)
	}
	if !reflect.DeepEqual(firstQuarantined, secondQuarantined) {
		t.Fatalf("replay produced different quarantine sets:\nfirst:  %#v\nsecond: %#v", firstQuarantined, secondQuarantined)
	}
	if len(firstQuarantined) != 1 || firstQuarantined[0].factID != "malformed-entity" {
		t.Fatalf("quarantined = %#v, want exactly the malformed-entity fact quarantined on every replay", firstQuarantined)
	}
}
