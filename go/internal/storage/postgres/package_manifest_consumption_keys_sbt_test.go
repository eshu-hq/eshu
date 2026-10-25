// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
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
		"fact.payload->>'entity_name' = ANY($2::text[])",
		"fact.payload->'entity_metadata'->>'package_manager' = 'sbt'",
		"fact.payload->'entity_metadata'->>'sbt_binary_artifact' = ANY($2::text[])",
		"fact.payload->>'entity_name' <> ALL($2::text[])",
		"UNION ALL",
	} {
		if !strings.Contains(call.query, want) {
			t.Fatalf("query missing %q:\n%s", want, call.query)
		}
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
