// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// runNornicDBBootstrapOverStoreWith bootstraps against a NornicDB store that
// holds every expected schema object plus the retired constraint names in
// retired, applying the real schema through the executor run hands to the
// apply step (the missing-object filter on NornicDB). It returns whether the
// schema was applied and the statements that reached the backend.
func runNornicDBBootstrapOverStoreWith(t *testing.T, retired ...string) (applied bool, cyphers []string) {
	t.Helper()

	expectedNames, err := expectedGraphSchemaObjectNames(graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("expectedGraphSchemaObjectNames() error = %v, want nil", err)
	}
	names := make(map[string]struct{}, len(expectedNames)+len(retired))
	for name := range expectedNames {
		names[name] = struct{}{}
	}
	for _, name := range retired {
		names[name] = struct{}{}
	}

	backend := &fakeNeo4jExecutor{}
	db := &fakeBootstrapDB{queryRows: []fakeBootstrapRows{{rows: nil}}}
	err = run(
		context.Background(),
		func(string) string { return "" },
		testLogger(t),
		func(context.Context, func(string) string) (bootstrapDB, error) { return db, nil },
		func(context.Context, bootstrapExecutor) error { return nil },
		func(context.Context, func(string) string) (neo4jDeps, error) {
			return neo4jDeps{executor: backend, inspector: &fakeGraphSchemaInspector{names: names}, close: func() error { return nil }}, nil
		},
		func(ctx context.Context, executor graph.CypherExecutor, logger *slog.Logger, b graph.SchemaBackend) error {
			applied = true
			return graph.EnsureSchemaWithBackendStrict(ctx, executor, logger, b)
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	return applied, backend.cyphers
}

// TestRunDoesNotAdoptNornicDBSchemaThatStillHasRetiredConstraints guards #7097:
// the NornicDB schema drops the single-property uniqueness constraints narrower
// than the canonical uid. A store that has every current object but still
// carries one is not the current schema; adopting it would record the marker
// without running the DROP, and a moved HelmValues, KustomizeOverlay or
// TerragruntConfig block would keep retrying on the UNIQUE violation.
func TestRunDoesNotAdoptNornicDBSchemaThatStillHasRetiredConstraints(t *testing.T) {
	t.Parallel()

	expectedNames, err := expectedGraphSchemaObjectNames(graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("expectedGraphSchemaObjectNames() error = %v, want nil", err)
	}
	for _, retired := range []string{"helm_values_unique", "kustomize_unique", "tg_config_unique"} {
		if _, ok := expectedNames[retired]; ok {
			t.Fatalf("expected NornicDB schema objects include retired %s", retired)
		}
	}

	applied, _ := runNornicDBBootstrapOverStoreWith(t, "helm_values_unique")
	if !applied {
		t.Fatal("run() adopted a NornicDB schema that still has retired constraint helm_values_unique; want the schema applied so the DROP runs")
	}
}

// TestNornicDBRetiredConstraintDropsReachTheBackendOnPartialAdoption proves the
// missing-object filter, which skips every CREATE the store already has, does
// not swallow the DROP statements: they name an object the schema must not
// have, so skipping them as "present" would leave the constraint in place.
func TestNornicDBRetiredConstraintDropsReachTheBackendOnPartialAdoption(t *testing.T) {
	t.Parallel()

	retired := []string{"helm_values_unique", "kustomize_unique", "tg_config_unique"}
	_, cyphers := runNornicDBBootstrapOverStoreWith(t, retired...)

	for _, name := range retired {
		drop := "DROP CONSTRAINT " + name + " IF EXISTS"
		if !slices.Contains(cyphers, drop) {
			t.Errorf("backend did not receive %q; statements = %d", drop, len(cyphers))
		}
	}
	for _, cypher := range cyphers {
		if _, isDrop := graphSchemaDropObjectName(cypher); isDrop {
			continue
		}
		t.Errorf("backend received %q although the store already held every current object; the filter should skip it", cypher)
	}
}
