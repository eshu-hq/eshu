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

// stubProducerScope is one row the quiescence probe returns: a scope registered
// under the queried collector kind, and whether it is quiescent-active.
//
// The pair is the point. A kind with no rows at all (no such collector in this
// deployment) and a kind whose rows are all quiescent=false (collector present,
// still working) are different answers, and the store must not confuse them.
type stubProducerScope struct {
	scopeID   string
	quiescent bool
}

// quiescenceQueryerStub answers the quiescence probe with a caller-supplied set
// of registered scopes per collector kind. It records every call so the tests
// can pin the query text and the bound arguments.
//
// It deliberately does NOT fall back to a default answer for an unexpected
// query: a store that stopped calling producerScopeQuiescenceSQL must fail the
// test loudly rather than pass on a stub's generosity.
type quiescenceQueryerStub struct {
	scopesByKind map[string][]stubProducerScope
	err          error
	calls        []recordedQuiescenceCall
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
		for _, registered := range q.scopesByKind[kind] {
			rows = append(rows, []any{registered.scopeID, registered.quiescent})
		}
	}
	return &fakeRows{rows: rows}, nil
}

// quiescentScopes is shorthand for a kind whose registered scopes have all
// finished publishing.
func quiescentScopes(scopeIDs ...string) []stubProducerScope {
	scopes := make([]stubProducerScope, 0, len(scopeIDs))
	for _, scopeID := range scopeIDs {
		scopes = append(scopes, stubProducerScope{scopeID: scopeID, quiescent: true})
	}
	return scopes
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

			got := crossScopeProducerCollectorKinds(crossScopeProducerDomainsFor(testCase.consumer))
			if !slices.Equal(got, testCase.want) {
				t.Fatalf("collector kinds for %s = %v, want %v", testCase.consumer, got, testCase.want)
			}
		})
	}
}

// readinessFor is the per-producer answer for one producer domain, plus whether
// the store said anything about it at all. A store that silently drops a
// declared producer is the failure this separates from an explicit false: the
// reducer floor reads a missing entry as unready, so the two look alike from the
// outside and only this distinction catches the drop.
func readinessFor(
	t *testing.T,
	readiness reducer.CrossScopeProducerReadinessByDomain,
	producer reducer.Domain,
) bool {
	t.Helper()

	ready, answered := readiness[producer]
	if !answered {
		t.Fatalf("readiness = %v, want an entry for the declared producer %s", readiness, producer)
	}
	return ready
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
		scopesByKind: map[string][]stubProducerScope{
			string(scope.CollectorOCIRegistry): quiescentScopes("oci_registry:ghcr.io/acme"),
		},
	}
	store := CrossScopeProducerReadinessStore{DB: stub}

	readiness, err := store.CrossScopeProducersReady(
		context.Background(), reducer.DomainCICDRunCorrelation, "ci_cd_run:acme", "gen-1",
	)
	if err != nil {
		t.Fatalf("CrossScopeProducersReady() error = %v", err)
	}
	if !readinessFor(t, readiness, reducer.DomainContainerImageIdentity) {
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

// TestCrossScopeProducersReadyIsReadyWhenNoScopeOfTheProducerKindExists is the
// counterpart to the deferral test below, and the two only stay distinct
// because the probe reports registered scopes as well as quiescent ones.
//
// Not every deployment runs every collector. The OCI registry collector needs
// registry credentials, so a deployment indexing repositories whose CI publishes
// image digests may well have no oci_registry scope at all. Waiting for one is
// waiting for something that will never arrive: the intent defers to the full
// 30-minute bound, re-claiming about every 30 seconds because this failure class
// freezes attempt_count and so never backs off, which is roughly 60 no-op claims
// per row per repair cycle against the write-hot fact_work_items table.
//
// Two stubs, one difference. No rows for the kind means ready. One row for the
// kind that is not quiescent means defer. A store that only counts the quiescent
// set answers both the same way.
func TestCrossScopeProducersReadyIsReadyWhenNoScopeOfTheProducerKindExists(t *testing.T) {
	t.Parallel()

	stub := &quiescenceQueryerStub{scopesByKind: map[string][]stubProducerScope{}}
	store := CrossScopeProducerReadinessStore{DB: stub}

	readiness, err := store.CrossScopeProducersReady(
		context.Background(), reducer.DomainCICDRunCorrelation, "ci_cd_run:acme", "gen-1",
	)
	if err != nil {
		t.Fatalf("CrossScopeProducersReady() error = %v", err)
	}
	if !readinessFor(t, readiness, reducer.DomainContainerImageIdentity) {
		t.Fatal("ready = false, want true: no oci_registry scope is registered, so there is no activation to wait for")
	}
	if len(stub.calls) != 1 {
		t.Fatalf("calls = %d, want exactly 1: absence must be answered by the same probe, not a second query", len(stub.calls))
	}
}

// TestCrossScopeProducersReadySkipsOnlyTheAbsentKind proves absence is decided
// per collector kind, not for the consumer as a whole. A two-producer consumer
// whose first kind is absent must still wait for the second kind's scope to
// finish, rather than reading one absence as blanket permission to commit.
//
// The two producers get OPPOSITE answers here, which is the shape the aggregate
// bool could not carry: ci_cd_run_correlation is ready because no ci_cd_run
// scope exists to wait for, and container_image_identity is not because its
// registered oci_registry scope has not finished.
func TestCrossScopeProducersReadySkipsOnlyTheAbsentKind(t *testing.T) {
	t.Parallel()

	stub := &quiescenceQueryerStub{
		scopesByKind: map[string][]stubProducerScope{
			string(scope.CollectorOCIRegistry): {{scopeID: "oci_registry:ghcr.io/acme", quiescent: false}},
		},
	}
	store := CrossScopeProducerReadinessStore{DB: stub}

	readiness, err := store.CrossScopeProducersReady(
		context.Background(), reducer.DomainSupplyChainImpact, "vuln:acme", "gen-1",
	)
	if err != nil {
		t.Fatalf("CrossScopeProducersReady() error = %v", err)
	}
	if readinessFor(t, readiness, reducer.DomainContainerImageIdentity) {
		t.Fatal("container_image_identity ready = true, want false: its registered oci_registry scope has not finished")
	}
	if !readinessFor(t, readiness, reducer.DomainCICDRunCorrelation) {
		t.Fatal("ci_cd_run_correlation ready = false, want true: no ci_cd_run scope is registered to wait for")
	}
	if len(stub.calls) != 2 {
		t.Fatalf("calls = %d, want 2: an absent kind is skipped, and the remaining kinds are still probed", len(stub.calls))
	}
}

// TestCrossScopeProducersReadyDefersWhenNoProducerScopeIsQuiescent is the
// #5709 case the floor exists for: the producer's reducer row succeeded but its
// scope generation has not activated, so the consumer's cross-scope load can
// still see nothing. Reporting ready here writes a durable empty correlation.
//
// The scope is REGISTERED and not quiescent, which is what separates this from
// the absent-kind case above.
func TestCrossScopeProducersReadyDefersWhenNoProducerScopeIsQuiescent(t *testing.T) {
	t.Parallel()

	stub := &quiescenceQueryerStub{
		scopesByKind: map[string][]stubProducerScope{
			string(scope.CollectorOCIRegistry): {{scopeID: "oci_registry:ghcr.io/acme", quiescent: false}},
		},
	}
	store := CrossScopeProducerReadinessStore{DB: stub}

	readiness, err := store.CrossScopeProducersReady(
		context.Background(), reducer.DomainCICDRunCorrelation, "ci_cd_run:acme", "gen-1",
	)
	if err != nil {
		t.Fatalf("CrossScopeProducersReady() error = %v", err)
	}
	if readinessFor(t, readiness, reducer.DomainContainerImageIdentity) {
		t.Fatal("ready = true, want false while no producer scope is quiescent-active")
	}
}

// TestCrossScopeProducersReadyAnswersEveryProducerSeparately covers the
// multi-producer consumer. Every declared producer gets its own answer, and a
// producer that is not ready no longer stops the store from answering the rest.
//
// The short-circuit this replaced was the aggregate bool's only justification
// for skipping probes, and it is exactly what made the readiness signal
// unusable per producer: a consumer that stops probing at the first miss cannot
// say whether its OTHER producer was ready, so the floor had one bool to pair
// with one evidence count and could not tell the two producers apart.
func TestCrossScopeProducersReadyAnswersEveryProducerSeparately(t *testing.T) {
	t.Parallel()

	t.Run("every kind quiescent reports every producer ready", func(t *testing.T) {
		t.Parallel()

		stub := &quiescenceQueryerStub{
			scopesByKind: map[string][]stubProducerScope{
				string(scope.CollectorCICDRun):     quiescentScopes("ci_cd_run:acme"),
				string(scope.CollectorOCIRegistry): quiescentScopes("oci_registry:ghcr.io/acme"),
			},
		}
		store := CrossScopeProducerReadinessStore{DB: stub}

		readiness, err := store.CrossScopeProducersReady(
			context.Background(), reducer.DomainSupplyChainImpact, "vuln:acme", "gen-1",
		)
		if err != nil {
			t.Fatalf("CrossScopeProducersReady() error = %v", err)
		}
		for _, producer := range []reducer.Domain{
			reducer.DomainContainerImageIdentity,
			reducer.DomainCICDRunCorrelation,
		} {
			if !readinessFor(t, readiness, producer) {
				t.Fatalf("%s ready = false, want true when its producer kind is quiescent-active", producer)
			}
		}
		if len(stub.calls) != 2 {
			t.Fatalf("calls = %d, want 2, one per distinct declared producer collector kind", len(stub.calls))
		}
	})

	t.Run("one non-quiescent kind does not decide the other producer", func(t *testing.T) {
		t.Parallel()

		stub := &quiescenceQueryerStub{
			scopesByKind: map[string][]stubProducerScope{
				string(scope.CollectorCICDRun):     {{scopeID: "ci_cd_run:acme", quiescent: false}},
				string(scope.CollectorOCIRegistry): quiescentScopes("oci_registry:ghcr.io/acme"),
			},
		}
		store := CrossScopeProducerReadinessStore{DB: stub}

		readiness, err := store.CrossScopeProducersReady(
			context.Background(), reducer.DomainSupplyChainImpact, "vuln:acme", "gen-1",
		)
		if err != nil {
			t.Fatalf("CrossScopeProducersReady() error = %v", err)
		}
		if readinessFor(t, readiness, reducer.DomainCICDRunCorrelation) {
			t.Fatal("ci_cd_run_correlation ready = true, want false: its registered scope has not finished")
		}
		if !readinessFor(t, readiness, reducer.DomainContainerImageIdentity) {
			t.Fatal("container_image_identity ready = false, want true: its own oci_registry scope is quiescent-active")
		}
		if len(stub.calls) != 2 {
			t.Fatalf("calls = %d, want 2: every declared kind is probed so every producer gets an answer", len(stub.calls))
		}
	})
}

// TestCrossScopeProducersReadyLeavesUnregisteredConsumersAlone proves a domain
// outside the catalog neither blocks nor touches the database. A nil DB here is
// the assertion: any query attempt would panic.
func TestCrossScopeProducersReadyLeavesUnregisteredConsumersAlone(t *testing.T) {
	t.Parallel()

	store := CrossScopeProducerReadinessStore{DB: nil}

	readiness, err := store.CrossScopeProducersReady(
		context.Background(), reducer.DomainContainerImageIdentity, "oci_registry:ghcr.io/acme", "gen-1",
	)
	if err != nil {
		t.Fatalf("CrossScopeProducersReady() error = %v", err)
	}
	if len(readiness) != 0 {
		t.Fatalf("readiness = %v, want no producers for a domain the cross-scope catalog does not register", readiness)
	}
}

// TestCrossScopeProducersReadyRequiresADatabaseForRegisteredConsumers proves a
// registered consumer with no wired database fails loud. Reporting ready would
// silently disable the floor for every consumer in that deployment.
func TestCrossScopeProducersReadyRequiresADatabaseForRegisteredConsumers(t *testing.T) {
	t.Parallel()

	store := CrossScopeProducerReadinessStore{DB: nil}

	readiness, err := store.CrossScopeProducersReady(
		context.Background(), reducer.DomainCICDRunCorrelation, "ci_cd_run:acme", "gen-1",
	)
	if err == nil {
		t.Fatal("expected an error when a registered consumer has no database")
	}
	if readiness != nil {
		t.Fatalf("readiness = %v alongside an error, want nil", readiness)
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

	readiness, err := store.CrossScopeProducersReady(
		context.Background(), reducer.DomainCICDRunCorrelation, "ci_cd_run:acme", "gen-1",
	)
	if err == nil {
		t.Fatal("expected the probe error to surface")
	}
	if readiness != nil {
		t.Fatalf("readiness = %v alongside an error, want nil", readiness)
	}
}
