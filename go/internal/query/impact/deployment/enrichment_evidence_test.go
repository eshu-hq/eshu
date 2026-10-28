// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

func terraformEntity(entityType, relativePath string, metadata map[string]any) querycontract.EntityContent {
	return querycontract.EntityContent{
		EntityType:   entityType,
		RelativePath: relativePath,
		Metadata:     metadata,
	}
}

func TestBuildOutgoingTerraformRelationships(t *testing.T) {
	t.Parallel()

	t.Run("ModuleSource", func(t *testing.T) {
		t.Parallel()
		rels, handled, err := BuildOutgoingTerraformRelationships(terraformEntity("TerraformModule", "main.tf", map[string]any{"source": "./modules/app"}))
		if err != nil || !handled {
			t.Fatalf("BuildOutgoingTerraformRelationships() = (_, %v, %v), want (rels, true, nil)", handled, err)
		}
		if len(rels) != 1 || rels[0]["type"] != "USES_MODULE" || rels[0]["reason"] != "terraform_module_source" {
			t.Fatalf("relationships = %v, want one USES_MODULE", rels)
		}
	})
	t.Run("ModuleWithoutSource", func(t *testing.T) {
		t.Parallel()
		rels, handled, err := BuildOutgoingTerraformRelationships(terraformEntity("TerraformModule", "main.tf", map[string]any{}))
		if err != nil || !handled || len(rels) != 0 {
			t.Fatalf("BuildOutgoingTerraformRelationships() = (%v, %v, %v), want (nil, true, nil)", rels, handled, err)
		}
	})
	t.Run("TerragruntConfigFansOut", func(t *testing.T) {
		t.Parallel()
		entity := terraformEntity("TerragruntConfig", "live/app/terragrunt.hcl", map[string]any{
			"terraform_source":             "./module",
			"include_paths":                []any{"../root.hcl", ""},
			"read_config_paths":            []any{"common.hcl"},
			"find_in_parent_folders_paths": []any{"parent.hcl"},
			"local_config_asset_paths":     []any{"local.hcl"},
		})
		rels, handled, err := BuildOutgoingTerraformRelationships(entity)
		if err != nil || !handled {
			t.Fatalf("BuildOutgoingTerraformRelationships() = (_, %v, %v), want (rels, true, nil)", handled, err)
		}
		if len(rels) != 5 {
			t.Fatalf("relationships = %v, want 5 (source + 4 config discoveries, blank skipped)", rels)
		}
	})
	t.Run("TerragruntDependency", func(t *testing.T) {
		t.Parallel()
		rels, handled, err := BuildOutgoingTerraformRelationships(terraformEntity("TerragruntDependency", "live/app/terragrunt.hcl", map[string]any{"config_path": "../db"}))
		if err != nil || !handled || len(rels) != 1 || rels[0]["reason"] != "terragrunt_dependency_config_path" {
			t.Fatalf("BuildOutgoingTerraformRelationships() = (%v, %v, %v)", rels, handled, err)
		}
	})
	t.Run("UnknownTypeUnhandled", func(t *testing.T) {
		t.Parallel()
		rels, handled, err := BuildOutgoingTerraformRelationships(terraformEntity("Deployment", "app.yaml", map[string]any{}))
		if err != nil || handled || len(rels) != 0 {
			t.Fatalf("BuildOutgoingTerraformRelationships() = (%v, %v, %v), want (nil, false, nil)", rels, handled, err)
		}
	})
}

func TestCollectProvisioningChainEvidence(t *testing.T) {
	t.Parallel()

	evidence := collectProvisioningChainEvidence([]querycontract.EntityContent{
		terraformEntity("Function", "main.go", map[string]any{}),
		terraformEntity("TerraformModule", "infra/main.tf", map[string]any{"source": "./modules/db"}),
		terraformEntity("TerragruntConfig", "live/app/terragrunt.hcl", map[string]any{"read_config_paths": []any{"common.hcl"}}),
		terraformEntity("TerragruntDependency", "live/db/terragrunt.hcl", map[string]any{"config_path": "../network"}),
	})
	if _, ok := evidence.samplePaths["infra/main.tf"]; !ok {
		t.Fatalf("sample paths = %v, want the module file recorded", evidence.samplePaths)
	}
	if _, ok := evidence.modules["modules/db"]; !ok {
		t.Fatalf("modules = %v, want the normalized module source recorded", evidence.modules)
	}
	if len(evidence.configPaths) != 2 {
		t.Fatalf("config paths = %v, want common.hcl and ../network", evidence.configPaths)
	}
	if _, ok := evidence.evidenceKinds["terraform_module_source"]; !ok {
		t.Fatalf("evidence kinds = %v, want the module reason recorded", evidence.evidenceKinds)
	}
}

func TestNormalizedIndirectEvidenceHostnames(t *testing.T) {
	t.Parallel()

	if got := normalizedIndirectEvidenceHostnames(nil); got != nil {
		t.Fatalf("nil = %v, want nil", got)
	}
	got := normalizedIndirectEvidenceHostnames([]string{"b.example.test", " a.example.test ", "", "a.example.test"})
	if !slices.Equal(got, []string{"a.example.test", "b.example.test"}) {
		t.Fatalf("hostnames = %v, want trimmed, deduplicated, sorted", got)
	}
}

func TestBackfillConsumerRepositoryDisplayNames(t *testing.T) {
	t.Parallel()

	t.Run("NilGraphIsNoOp", func(t *testing.T) {
		t.Parallel()
		entry := map[string]any{"repository": "repo-a"}
		if err := backfillConsumerRepositoryDisplayNames(t.Context(), nil, map[string]map[string]any{"repo-a": entry}); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if entry["repository"] != "repo-a" {
			t.Fatalf("entry = %v, want untouched without a graph", entry)
		}
	})
	t.Run("FillsDisplayNames", func(t *testing.T) {
		t.Parallel()
		reader := graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"repo_id": "repo-a", "repo_name": "org/api"}}, nil
		}}
		consumers := map[string]map[string]any{
			"repo-a": {"repository": "repo-a", "repo_name": ""},
			"":       {"repository": "x"},
		}
		if err := backfillConsumerRepositoryDisplayNames(t.Context(), reader, consumers); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if consumers["repo-a"]["repo_name"] != "org/api" || consumers["repo-a"]["repository"] != "org/api" {
			t.Fatalf("entry = %v, want the looked-up display name backfilled", consumers["repo-a"])
		}
	})
	t.Run("ReaderErrorPropagates", func(t *testing.T) {
		t.Parallel()
		reader := graph.FakeGraphReader{RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, errors.New("backend down")
		}}
		consumers := map[string]map[string]any{"repo-a": {"repository": ""}}
		if err := backfillConsumerRepositoryDisplayNames(t.Context(), reader, consumers); err == nil {
			t.Fatal("error = nil, want the reader failure propagated")
		}
	})
}

func TestAccumulatorHelpers(t *testing.T) {
	t.Parallel()

	t.Run("MergeStringSet", func(t *testing.T) {
		t.Parallel()
		dst := map[string]struct{}{"a": {}}
		mergeStringSet(dst, map[string]struct{}{"b": {}, "a": {}})
		if len(dst) != 2 {
			t.Fatalf("merged = %v, want {a b}", dst)
		}
	})
	t.Run("MergeTraceEvidenceByRepo", func(t *testing.T) {
		t.Parallel()
		dst := map[string]traceEvidenceAccumulator{"r1": {samplePaths: map[string]struct{}{"a": {}}}}
		mergeTraceEvidenceByRepo(dst, map[string]traceEvidenceAccumulator{
			"r1": {samplePaths: map[string]struct{}{"b": {}}},
			"r2": {samplePaths: map[string]struct{}{"c": {}}},
		})
		if len(dst["r1"].samplePaths) != 2 || len(dst["r2"].samplePaths) != 1 {
			t.Fatalf("merged = %v, want union per repo", dst)
		}
	})
	t.Run("SortedAccumulatorValues", func(t *testing.T) {
		t.Parallel()
		if got := sortedAccumulatorValues(nil); got != nil {
			t.Fatalf("nil = %v, want nil", got)
		}
		got := sortedAccumulatorValues(map[string]struct{}{"b": {}, " a ": {}, "": {}})
		if !slices.Equal(got, []string{"a", "b"}) {
			t.Fatalf("values = %v, want trimmed and sorted", got)
		}
	})
	t.Run("AppendConsumerEvidence", func(t *testing.T) {
		t.Parallel()
		entry := map[string]any{"consumer_kinds": []string{"graph_provisioning_consumer"}}
		appendConsumerEvidence(entry, traceEvidenceAccumulator{
			evidenceKinds: map[string]struct{}{"repository_reference": {}, "hostname_reference": {}},
			matchedValues: map[string]struct{}{"svc": {}},
			samplePaths:   map[string]struct{}{"main.go": {}},
		})
		kinds, _ := entry["consumer_kinds"].([]string)
		if !slices.Equal(kinds, []string{"graph_provisioning_consumer", "hostname_reference_consumer", "service_reference_consumer"}) {
			t.Fatalf("consumer kinds = %v, want all three sorted", kinds)
		}
	})
	t.Run("ConsumerRepositorySortScore", func(t *testing.T) {
		t.Parallel()
		plain := consumerRepositorySortScore(map[string]any{})
		graph := consumerRepositorySortScore(map[string]any{"consumer_kinds": []string{"graph_provisioning_consumer"}})
		if graph <= plain {
			t.Fatalf("scores = %d/%d, want the graph consumer to outrank a bare entry", graph, plain)
		}
	})
	t.Run("ExactCaseServiceNameSearch", func(t *testing.T) {
		t.Parallel()
		if !exactCaseServiceNameSearch("payments") {
			t.Fatal("lowercase name must search exact-case")
		}
		if exactCaseServiceNameSearch("Payments") || exactCaseServiceNameSearch("") {
			t.Fatal("mixed-case and empty names must not search exact-case")
		}
	})
}
