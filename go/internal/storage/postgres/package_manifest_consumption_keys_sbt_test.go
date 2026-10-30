// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func sbtSidecarEnvelope(id, cross, artifact string) facts.Envelope {
	metadata := map[string]any{
		"config_kind":       "dependency",
		"package_manager":   "sbt",
		"sbt_cross_version": cross,
	}
	if artifact != "" {
		metadata["sbt_binary_artifact"] = artifact
	}
	return manifestConsumptionTestEnvelope(id, false, map[string]any{
		"entity_type":     "Variable",
		"repo_id":         "repository:scala",
		"entity_name":     "org.typelevel:cats-core",
		"entity_metadata": metadata,
	})
}

func sidecarPackageNames(rows []packageManifestConsumptionKeyRow) []string {
	var names []string
	for _, row := range rows {
		names = append(names, row.Ecosystem+"|"+row.PackageName)
	}
	return names
}

func TestPackageManifestConsumptionKeyRowsJoinSBTArtifact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		envelope facts.Envelope
		want     []packageidentity.ConsumptionKey
	}{
		{
			name:     "binary row keys the suffixed artifact only",
			envelope: sbtSidecarEnvelope("binary", "binary", "org.typelevel:cats-core_2.13"),
			want:     packageidentity.ConsumptionKeys("maven", "org.typelevel:cats-core_2.13"),
		},
		{
			name:     "Scala 3 binary row keys the _3 artifact only",
			envelope: sbtSidecarEnvelope("scala3", "binary", "org.typelevel:cats-core_3"),
			want:     packageidentity.ConsumptionKeys("maven", "org.typelevel:cats-core_3"),
		},
		{
			name:     "percent row keys the exact coordinate",
			envelope: sbtSidecarEnvelope("none", "none", ""),
			want:     packageidentity.ConsumptionKeys("maven", "org.typelevel:cats-core"),
		},
		{name: "binary row without an artifact keys nothing", envelope: sbtSidecarEnvelope("noartifact", "binary", "")},
		{name: "platform row keys nothing", envelope: sbtSidecarEnvelope("js", "scalajs", "org.typelevel:cats-core_sjs1_2.13")},
		{name: "custom cross row keys nothing", envelope: sbtSidecarEnvelope("custom", "custom", "")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rows := packageManifestConsumptionKeyRows([]facts.Envelope{test.envelope})
			var want []string
			for _, key := range test.want {
				want = append(want, string(key.Ecosystem)+"|"+key.PackageName)
			}
			if got := sidecarPackageNames(rows); !reflect.DeepEqual(got, want) {
				t.Fatalf("sidecar keys = %v, want %v", got, want)
			}
		})
	}
}

func TestListActivePackageManifestDependencyFactsReachesSBTAndGradleRows(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	if _, err := NewFactStore(db).ListActivePackageManifestDependencyFacts(
		context.Background(), []string{"maven"}, []string{"org.typelevel:cats-core_2.13"},
	); err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts() error = %v", err)
	}
	call := db.queries[0]
	if got, want := call.args[0], []string{"gradle", "maven", "sbt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("package_manager arg = %v, want %v", got, want)
	}
	for _, want := range []string{
		"fact.payload->'entity_metadata'->>'package_manager' = ANY($1::text[])",
		`(LOWER(TRIM(fact.payload->>'entity_name') COLLATE "C") COLLATE "default") = ANY($2::text[])`,
		"fact.payload->'entity_metadata'->>'package_manager' = 'sbt'",
		"fact.payload->'entity_metadata'->>'sbt_binary_artifact' = ANY($2::text[])",
		`(LOWER(TRIM(fact.payload->>'entity_name') COLLATE "C") COLLATE "default") <> ALL($2::text[])`,
		"UNION ALL",
	} {
		if !strings.Contains(call.query, want) {
			t.Fatalf("query missing %q:\n%s", want, call.query)
		}
	}
}

func TestListActivePackageManifestDependencyFactsMapsGoModuleToParserManager(t *testing.T) {
	t.Parallel()
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	if _, err := NewFactStore(db).ListActivePackageManifestDependencyFacts(
		context.Background(), []string{"gomod"}, []string{"github.com/acme/lib-common"},
	); err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts(gomod): %v", err)
	}
	call := db.queries[0]
	if got, want := call.args[0], []string{"go", "gomod"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("package_manager arg = %v, want %v to include go.mod parser rows", got, want)
	}
	if strings.Contains(call.query, "sbt_binary_artifact") || strings.Contains(call.query, "UNION ALL") {
		t.Fatalf("Go module query carries the sbt branch:\n%s", call.query)
	}
}

func TestListActivePackageManifestDependencyFactsKeepsNonMavenQueryUnchanged(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}}}
	if _, err := NewFactStore(db).ListActivePackageManifestDependencyFacts(
		context.Background(), []string{"npm"}, []string{"left-pad"},
	); err != nil {
		t.Fatalf("ListActivePackageManifestDependencyFacts() error = %v", err)
	}
	call := db.queries[0]
	if got, want := call.args[0], []string{"npm"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("package_manager arg = %v, want %v", got, want)
	}
	if strings.Contains(call.query, "sbt_binary_artifact") || strings.Contains(call.query, "UNION ALL") {
		t.Fatalf("non-Maven query carries the sbt branch:\n%s", call.query)
	}
}

type insertRecorder struct {
	queries  []string
	affected int64
}

func (r *insertRecorder) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	r.queries = append(r.queries, query)
	return rowsAffectedResult{rowsAffected: r.affected}, nil
}

func (r *insertRecorder) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("insertRecorder.QueryContext must not be called")
}

// TestInsertPackageManifestConsumptionKeysIfAbsentNeverDeletesOrOverwrites pins
// the repair writer's contract: one insert per batch, DO NOTHING on conflict,
// no delete, and the row count it reports is the database's count.
func TestInsertPackageManifestConsumptionKeysIfAbsentNeverDeletesOrOverwrites(t *testing.T) {
	t.Parallel()

	recorder := &insertRecorder{affected: 2}
	inserted, err := insertPackageManifestConsumptionKeysIfAbsent(context.Background(), recorder, []facts.Envelope{
		sbtSidecarEnvelope("binary", "binary", "org.typelevel:cats-core_2.13"),
	})
	if err != nil {
		t.Fatalf("insertPackageManifestConsumptionKeysIfAbsent() error = %v", err)
	}
	if inserted != 2 || len(recorder.queries) != 1 {
		t.Fatalf("inserted = %d over %d statements, want 2 over 1", inserted, len(recorder.queries))
	}
	query := recorder.queries[0]
	if !strings.Contains(query, "ON CONFLICT (fact_id, ecosystem, package_name) DO NOTHING") {
		t.Fatalf("query lacks DO NOTHING:\n%s", query)
	}
	for _, forbidden := range []string{"DO UPDATE", "DELETE"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("query contains %q:\n%s", forbidden, query)
		}
	}
	empty := &insertRecorder{}
	if n, err := insertPackageManifestConsumptionKeysIfAbsent(context.Background(), empty, []facts.Envelope{
		sbtSidecarEnvelope("noartifact", "binary", ""),
	}); err != nil || n != 0 || len(empty.queries) != 0 {
		t.Fatalf("a row with no keys inserted %d over %d statements, err %v, want none", n, len(empty.queries), err)
	}
}

// TestSBTRepairPageQueryIsTheShippedPageQueryPlusOnePredicate keeps the repair
// on migration 181's index: the same text, narrowed by the sbt predicate before
// ORDER BY, with the same three parameters and limit.
func TestSBTRepairPageQueryIsTheShippedPageQueryPlusOnePredicate(t *testing.T) {
	t.Parallel()

	shipped := listPackageManifestConsumptionKeyScopeFactsSQL
	derived := listPackageManifestConsumptionKeySBTScopeFactsSQL
	cut := strings.Index(shipped, "ORDER BY fact_id")
	if cut < 0 || !strings.HasPrefix(derived, shipped[:cut]) || !strings.HasSuffix(derived, shipped[cut:]) {
		t.Fatalf("derived query is not the shipped query with one inserted clause:\n%s", derived)
	}
	if inserted := derived[cut : len(derived)-len(shipped[cut:])]; !strings.Contains(inserted, "'package_manager' = 'sbt'") {
		t.Fatalf("inserted clause = %q, want the sbt predicate", inserted)
	}
	for _, want := range []string{"scope_id = $1", "fact_id > $2", "LIMIT $3"} {
		if !strings.Contains(derived, want) {
			t.Fatalf("derived query lacks %q", want)
		}
	}
}
