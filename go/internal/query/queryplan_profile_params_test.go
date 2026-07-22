// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestQueryplanProfileParamsCoverFluxDeploymentBindingQueries(t *testing.T) {
	production := legacyQueryplanProductionCypher(t)
	params := queryplanProfileParams()
	for entryID, names := range map[string][]string{
		"QP-IMPACT-FLUX-BINDINGS-FIRST-HOP":        {"source_repo_ids", "source_limit"},
		"QP-IMPACT-FLUX-BINDINGS-TARGET-EXPANSION": {"artifact_ids", "source_repo_ids"},
	} {
		cypher := production[entryID]
		for _, name := range names {
			if !strings.Contains(cypher, "$"+name) {
				t.Fatalf("%s does not bind $%s", entryID, name)
			}
			if _, ok := params[name]; !ok {
				t.Fatalf("profile params missing %s for %s", name, entryID)
			}
		}
	}
	if sourceRepoIDs, ok := params["source_repo_ids"].([]string); !ok || len(sourceRepoIDs) != 1 {
		t.Fatalf("source_repo_ids = %#v, want one repository id", params["source_repo_ids"])
	}
	if sourceLimit, ok := params["source_limit"].(int); !ok || sourceLimit != 51 {
		t.Fatalf("source_limit = %#v, want int 51", params["source_limit"])
	}
	if artifactIDs, ok := params["artifact_ids"].([]string); !ok || len(artifactIDs) != 1 {
		t.Fatalf("artifact_ids = %#v, want one artifact id", params["artifact_ids"])
	}
}

func queryplanProfileParams() map[string]any {
	return map[string]any{
		"account_id":             "proof-account",
		"after_id":               "proof-id",
		"after_dependency_id":    "proof-dependency",
		"after_edge":             "proof-edge",
		"after_name":             "proof-name",
		"after_order_index":      -1,
		"after_pattern":          "",
		"after_ref":              "",
		"after_resource_type":    "proof-type",
		"after_version_id":       "proof-version",
		"allowed_repository_ids": []string{"proof-repository"},
		"allowed_scope_ids":      []string{"proof-scope"},
		"artifact_ids":           []string{"proof-artifact"},
		"cycle_language":         "python",
		"edge_scan_limit":        callGraphMetricsEdgeScanLimit + 1,
		"ecosystem":              "proof-ecosystem",
		"entity_id":              "proof-entity",
		"environment":            "",
		"from":                   "proof-repository",
		"from_id":                "proof-repository",
		"instance_ids":           []string{"proof-instance"},
		"ids":                    []string{"proof-id"},
		"language":               "go",
		"limit":                  10,
		"name":                   "proof",
		"offset":                 0,
		"package":                "proof-package",
		"package_id":             "proof-package",
		"platform_edge_limit":    workloadPlatformEdgeLimit + 1,
		"provider":               "proof-provider",
		"q":                      "proof",
		"query":                  "proof",
		"region":                 "proof-region",
		"repo_id":                "proof-repository",
		"resource_id":            "proof-resource",
		"resource_arn":           "arn:proof",
		"resource_type":          "proof-type",
		"resource_type_query":    "proof-type",
		"selector":               "proof",
		"semantic_filter":        "proof",
		"service_id":             "proof-service",
		"scan_limit":             importDependencyInternalScanLimit + 1,
		"source_file":            "src/proof.py",
		"source_limit":           51,
		"source_module":          "proof.source",
		"source_paths":           []string{"/proof/src/proof.py"},
		"source_repo_ids":        []string{"proof-repository"},
		"source_tool":            "proof-tool",
		"target_file":            "src/target.py",
		"target_id":              "proof-target",
		"target_module":          "proof.target",
		"target_paths":           []string{"/proof/src/target.py"},
		"type":                   "Function",
		"uids":                   []string{"proof-cloud-resource"},
		"version_id":             "proof-version",
		"workload_id":            "proof-workload",
	}
}
