// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// TestProducerTerminalParkDomainsFromError turns the floor deferral into
// the parked producer set: the error's own domains win, the catalog
// derivation covers a bare miss, and nothing parks when neither yields a
// domain.
func TestProducerTerminalParkDomainsFromError(t *testing.T) {
	t.Parallel()

	consumer := reducer.DomainCICDRunCorrelation
	direct := crossscope.NewProducerNotReadyError(consumer, "s", "g",
		[]reducercontract.Domain{reducer.DomainContainerImageIdentity})
	wrapped := errors.Join(errors.New("claim fence"), direct)
	other := errors.New("boom")

	for _, tt := range []struct {
		name     string
		cause    error
		consumer reducercontract.Domain
		want     []reducercontract.Domain
	}{
		{"direct error", direct, consumer, []reducercontract.Domain{reducer.DomainContainerImageIdentity}},
		{"wrapped error", wrapped, consumer, []reducercontract.Domain{reducer.DomainContainerImageIdentity}},
		{"catalog fallback", other, consumer, []reducercontract.Domain{reducer.DomainContainerImageIdentity}},
		{"nothing to park", other, reducercontract.Domain("no_such_consumer"), nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := producerTerminalParkDomains(tt.cause, tt.consumer)
			if len(got) != len(tt.want) {
				t.Fatalf("park domains = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("park domains = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// TestProducerTerminalDomainKeyIsDomainOnly pins the #11000 key shape: the
// producer domain alone, no scope or generation, so a commit in the
// producer's own scope matches.
func TestProducerTerminalDomainKeyIsDomainOnly(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(waitingKey{ProducerDomain: string(reducer.DomainContainerImageIdentity)})
	if err != nil {
		t.Fatalf("marshal domain key: %v", err)
	}
	if string(raw) != `{"producer_domain":"container_image_identity"}` {
		t.Fatalf("domain key = %s, want the producer domain alone", raw)
	}
}

// TestCompletionEdgeProducerSetCoversCatalogWakeUniverse pins the reducer
// Ack wake universe: every CompletionEdges producer wakes, and consumers
// that never commit a terminal row of their own never wake.
func TestCompletionEdgeProducerSetCoversCatalogWakeUniverse(t *testing.T) {
	t.Parallel()

	set := completionEdgeProducerSet()
	for _, producer := range []reducercontract.Domain{
		reducer.DomainContainerImageIdentity,
		reducer.DomainCICDRunCorrelation,
		reducer.DomainWorkloadMaterialization,
	} {
		if !set[producer] {
			t.Errorf("producer set misses catalog producer %q", producer)
		}
	}
	if set[reducer.DomainSupplyChainImpact] {
		t.Errorf("producer set contains the supply_chain_impact consumer, which commits no terminal row of its own")
	}
}

// TestWaitingDeferralMapCrossScopeProducerRowIsInTx pins the #11000 row:
// the cross-scope producer class wakes in-transaction from both providing
// saves, and the class row stays un-narrowed (one row per class; the park
// fans out per producer domain).
func TestWaitingDeferralMapCrossScopeProducerRowIsInTx(t *testing.T) {
	t.Parallel()

	entry, ok := WaitingDeferralByClass()[crossscope.ProducerNotReadyFailureClass]
	if !ok {
		t.Fatalf("no deferral map row for %q", crossscope.ProducerNotReadyFailureClass)
	}
	if entry.WaitsForKind != WaitingKindProducerTerminal {
		t.Errorf("waits-for kind = %q, want producer_domain_terminal (closed enum unchanged)", entry.WaitsForKind)
	}
	if entry.WakeSite != WaitingWakeSiteInTx {
		t.Errorf("wake site = %q, want in_tx", entry.WakeSite)
	}
	if entry.ProducerDomain != "" {
		t.Errorf("producer domain = %q, want empty (park fans out per domain, the class row does not narrow)", entry.ProducerDomain)
	}
	if !strings.Contains(entry.ProvidingSave, "projector") {
		t.Errorf("providing save %q does not name the projector-Ack generation activation", entry.ProvidingSave)
	}
	if !strings.Contains(entry.ProvidingSave, "reducer") {
		t.Errorf("providing save %q does not name the producer reducer Ack", entry.ProvidingSave)
	}
}
