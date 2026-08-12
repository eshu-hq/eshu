// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// recordedQuiescenceCall is one QueryContext the readiness store made, kept with
// its collector-kind argument so a test can assert WHICH kinds were probed and
// in what order, not merely that some query ran.
type recordedQuiescenceCall struct {
	query string
	kinds []string
}

// quiescenceQueryerStub answers the quiescence probe with a caller-supplied set
// of quiescent scope ids per collector kind. It records every call so the tests
// can pin the query text and the bound arguments.
//
// It deliberately does NOT fall back to a default answer for an unexpected
// query: a store that stopped calling producerScopeQuiescenceSQL must fail the
// test loudly rather than pass on a stub's generosity.
type quiescenceQueryerStub struct {
	quiescentByKind map[string][]string
	err             error
	calls           []recordedQuiescenceCall
}

func (q *quiescenceQueryerStub) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	kinds := collectorKindArgument(args)
	q.calls = append(q.calls, recordedQuiescenceCall{query: query, kinds: kinds})
	if q.err != nil {
		return nil, q.err
	}
	if query != producerScopeQuiescenceSQL {
		return nil, fmt.Errorf("unexpected query, want the quiescence probe: %s", query)
	}
	rows := make([][]any, 0)
	for _, kind := range kinds {
		for _, scopeID := range q.quiescentByKind[kind] {
			rows = append(rows, []any{scopeID})
		}
	}
	return &fakeRows{rows: rows}, nil
}

// collectorKindArgument unwraps the pq.StringArray the probe binds to $1.
func collectorKindArgument(args []any) []string {
	if len(args) == 0 {
		return nil
	}
	kinds, ok := args[0].(pq.StringArray)
	if !ok {
		return nil
	}
	return []string(kinds)
}

// TestCrossScopeProducerCollectorKindsForResolvesEveryCatalogConsumer pins the
// domain-to-collector-kind resolution for the real catalog. The floor blocks on
// the kinds this returns, so a wrong or missing entry silently changes which
// scopes a consumer waits for.
func TestCrossScopeProducerCollectorKindsForResolvesEveryCatalogConsumer(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		consumer reducer.Domain
		want     []string
	}{
		{
			name:     "ci/cd run correlation waits on the OCI registry scopes",
			consumer: reducer.DomainCICDRunCorrelation,
			want:     []string{string(scope.CollectorOCIRegistry)},
		},
		{
			name:     "supply chain impact waits on both producer scope kinds",
			consumer: reducer.DomainSupplyChainImpact,
			want:     []string{string(scope.CollectorCICDRun), string(scope.CollectorOCIRegistry)},
		},
		{
			name:     "a domain outside the catalog waits on nothing",
			consumer: reducer.DomainContainerImageIdentity,
			want:     nil,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := crossScopeProducerCollectorKindsFor(testCase.consumer)
			if !slices.Equal(got, testCase.want) {
				t.Fatalf("crossScopeProducerCollectorKindsFor(%s) = %v, want %v", testCase.consumer, got, testCase.want)
			}
		})
	}
}

// TestCrossScopeProducerCollectorKindsDeduplicatesAndSorts drives the same
// production helper the catalog path uses, with a producer list the real
// catalog does not currently produce: two producers mapping to one kind, plus a
// producer with no mapping at all. Duplicates would probe the same kind twice,
// and an unstable order would make the probe sequence non-deterministic.
func TestCrossScopeProducerCollectorKindsDeduplicatesAndSorts(t *testing.T) {
	t.Parallel()

	got := crossScopeProducerCollectorKinds([]reducer.Domain{
		reducer.DomainCICDRunCorrelation,
		reducer.DomainContainerImageIdentity,
		reducer.DomainContainerImageIdentity,
	})
	want := []string{string(scope.CollectorCICDRun), string(scope.CollectorOCIRegistry)}
	if !slices.Equal(got, want) {
		t.Fatalf("crossScopeProducerCollectorKinds() = %v, want %v", got, want)
	}
}

// TestCrossScopeProducerCollectorKindsSkipsUnmappedProducers proves an
// unmapped producer domain drops out rather than blocking on a guessed kind.
// Reporting a kind here that the deployment never registers would defer every
// consumer of that producer until the elapsed bound.
func TestCrossScopeProducerCollectorKindsSkipsUnmappedProducers(t *testing.T) {
	t.Parallel()

	got := crossScopeProducerCollectorKinds([]reducer.Domain{reducer.Domain("not_a_producer_with_a_scope")})
	if len(got) != 0 {
		t.Fatalf("crossScopeProducerCollectorKinds() = %v, want no kinds for an unmapped producer", got)
	}
}

// TestCrossScopeProducersReadyUsesTheProvenQuiescenceProbe pins the binding
// between the store and scope_quiescence.go. The probe is the only shape with a
// committed EXPLAIN proof (docs/internal/evidence/5709-quiescence-probe.md);
// a store that grew its own SQL would ship an unproven query on a write-hot
// table.
func TestCrossScopeProducersReadyUsesTheProvenQuiescenceProbe(t *testing.T) {
	t.Parallel()

	stub := &quiescenceQueryerStub{
		quiescentByKind: map[string][]string{
			string(scope.CollectorOCIRegistry): {"oci_registry:ghcr.io/acme"},
		},
	}
	store := CrossScopeProducerReadinessStore{DB: stub}

	ready, err := store.CrossScopeProducersReady(
		context.Background(), reducer.DomainCICDRunCorrelation, "ci_cd_run:acme", "gen-1",
	)
	if err != nil {
		t.Fatalf("CrossScopeProducersReady() error = %v", err)
	}
	if !ready {
		t.Fatal("ready = false, want true when the producer scope is quiescent-active")
	}
	if len(stub.calls) != 1 {
		t.Fatalf("calls = %d, want exactly 1 probe for a single-producer consumer", len(stub.calls))
	}
	if stub.calls[0].query != producerScopeQuiescenceSQL {
		t.Fatalf("query = %q, want producerScopeQuiescenceSQL", stub.calls[0].query)
	}
	if want := []string{string(scope.CollectorOCIRegistry)}; !slices.Equal(stub.calls[0].kinds, want) {
		t.Fatalf("bound collector kinds = %v, want %v", stub.calls[0].kinds, want)
	}
}

// TestCrossScopeProducersReadyDefersWhenNoProducerScopeIsQuiescent is the
// #5709 case the floor exists for: the producer's reducer row succeeded but its
// scope generation has not activated, so the consumer's cross-scope load can
// still see nothing. Reporting ready here writes a durable empty correlation.
func TestCrossScopeProducersReadyDefersWhenNoProducerScopeIsQuiescent(t *testing.T) {
	t.Parallel()

	stub := &quiescenceQueryerStub{quiescentByKind: map[string][]string{}}
	store := CrossScopeProducerReadinessStore{DB: stub}

	ready, err := store.CrossScopeProducersReady(
		context.Background(), reducer.DomainCICDRunCorrelation, "ci_cd_run:acme", "gen-1",
	)
	if err != nil {
		t.Fatalf("CrossScopeProducersReady() error = %v", err)
	}
	if ready {
		t.Fatal("ready = true, want false while no producer scope is quiescent-active")
	}
}

// TestCrossScopeProducersReadyProbesEveryProducerKindAndStopsAtTheFirstMiss
// covers the multi-producer consumer. Each declared producer kind must be
// satisfied, and the first miss must short-circuit rather than run the rest.
func TestCrossScopeProducersReadyProbesEveryProducerKindAndStopsAtTheFirstMiss(t *testing.T) {
	t.Parallel()

	t.Run("every kind quiescent reports ready", func(t *testing.T) {
		t.Parallel()

		stub := &quiescenceQueryerStub{
			quiescentByKind: map[string][]string{
				string(scope.CollectorCICDRun):     {"ci_cd_run:acme"},
				string(scope.CollectorOCIRegistry): {"oci_registry:ghcr.io/acme"},
			},
		}
		store := CrossScopeProducerReadinessStore{DB: stub}

		ready, err := store.CrossScopeProducersReady(
			context.Background(), reducer.DomainSupplyChainImpact, "vuln:acme", "gen-1",
		)
		if err != nil {
			t.Fatalf("CrossScopeProducersReady() error = %v", err)
		}
		if !ready {
			t.Fatal("ready = false, want true when both producer kinds are quiescent-active")
		}
		if len(stub.calls) != 2 {
			t.Fatalf("calls = %d, want 2, one per declared producer collector kind", len(stub.calls))
		}
		for index, want := range [][]string{
			{string(scope.CollectorCICDRun)},
			{string(scope.CollectorOCIRegistry)},
		} {
			if !slices.Equal(stub.calls[index].kinds, want) {
				t.Fatalf("call %d bound kinds = %v, want %v", index, stub.calls[index].kinds, want)
			}
		}
	})

	t.Run("the first non-quiescent kind short-circuits", func(t *testing.T) {
		t.Parallel()

		stub := &quiescenceQueryerStub{
			quiescentByKind: map[string][]string{
				string(scope.CollectorOCIRegistry): {"oci_registry:ghcr.io/acme"},
			},
		}
		store := CrossScopeProducerReadinessStore{DB: stub}

		ready, err := store.CrossScopeProducersReady(
			context.Background(), reducer.DomainSupplyChainImpact, "vuln:acme", "gen-1",
		)
		if err != nil {
			t.Fatalf("CrossScopeProducersReady() error = %v", err)
		}
		if ready {
			t.Fatal("ready = true, want false when the ci_cd_run producer kind has no quiescent scope")
		}
		if len(stub.calls) != 1 {
			t.Fatalf("calls = %d, want 1: the first miss must short-circuit the remaining probes", len(stub.calls))
		}
	})
}

// TestCrossScopeProducersReadyLeavesUnregisteredConsumersAlone proves a domain
// outside the catalog neither blocks nor touches the database. A nil DB here is
// the assertion: any query attempt would panic.
func TestCrossScopeProducersReadyLeavesUnregisteredConsumersAlone(t *testing.T) {
	t.Parallel()

	store := CrossScopeProducerReadinessStore{DB: nil}

	ready, err := store.CrossScopeProducersReady(
		context.Background(), reducer.DomainContainerImageIdentity, "oci_registry:ghcr.io/acme", "gen-1",
	)
	if err != nil {
		t.Fatalf("CrossScopeProducersReady() error = %v", err)
	}
	if !ready {
		t.Fatal("ready = false, want true for a domain the cross-scope catalog does not register")
	}
}

// TestCrossScopeProducersReadyRequiresADatabaseForRegisteredConsumers proves a
// registered consumer with no wired database fails loud. Reporting ready would
// silently disable the floor for every consumer in that deployment.
func TestCrossScopeProducersReadyRequiresADatabaseForRegisteredConsumers(t *testing.T) {
	t.Parallel()

	store := CrossScopeProducerReadinessStore{DB: nil}

	ready, err := store.CrossScopeProducersReady(
		context.Background(), reducer.DomainCICDRunCorrelation, "ci_cd_run:acme", "gen-1",
	)
	if err == nil {
		t.Fatal("expected an error when a registered consumer has no database")
	}
	if ready {
		t.Fatal("ready = true alongside an error, want false")
	}
}

// TestCrossScopeProducersReadySurfacesQueryErrors proves a failed probe is
// returned as an error rather than as "not ready". The caller classifies a
// readiness miss into a non-counting retry class that never dead-letters, so a
// broken query reported as a miss would retry forever without ever surfacing.
func TestCrossScopeProducersReadySurfacesQueryErrors(t *testing.T) {
	t.Parallel()

	stub := &quiescenceQueryerStub{err: fmt.Errorf("connection reset")}
	store := CrossScopeProducerReadinessStore{DB: stub}

	ready, err := store.CrossScopeProducersReady(
		context.Background(), reducer.DomainCICDRunCorrelation, "ci_cd_run:acme", "gen-1",
	)
	if err == nil {
		t.Fatal("expected the probe error to surface")
	}
	if ready {
		t.Fatal("ready = true alongside an error, want false")
	}
}
