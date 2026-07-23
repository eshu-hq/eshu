// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"regexp"
	"strings"
	"testing"
)

var queryplanCypherParameterPattern = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)

func TestQueryplanProfileParamsCoverFluxDeploymentBindingQueries(t *testing.T) {
	profileParams := queryplanProfileParams()
	production := captureFluxDeploymentBindingQueryplanRuns(t)
	tests := []struct {
		entryID string
		run     int
		params  map[string]string
	}{
		{
			entryID: "QP-IMPACT-FLUX-BINDINGS-FIRST-HOP",
			run:     0,
			params: map[string]string{
				"repo_id":         "string",
				"source_limit":    "int",
				"source_repo_ids": "[]string",
			},
		},
		{
			entryID: "QP-IMPACT-FLUX-BINDINGS-TARGET-EXPANSION",
			run:     1,
			params: map[string]string{
				"artifact_ids":    "[]string",
				"repo_id":         "string",
				"source_limit":    "int",
				"source_repo_ids": "[]string",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.entryID, func(t *testing.T) {
			for _, match := range queryplanCypherParameterPattern.FindAllStringSubmatch(production.cypher[tt.run], -1) {
				if _, ok := tt.params[match[1]]; !ok {
					t.Fatalf("captured production Cypher binds $%s without a profile parameter expectation", match[1])
				}
			}
			for name, wantType := range tt.params {
				productionValue, ok := production.params[tt.run][name]
				if !ok {
					t.Fatalf("captured production params missing %s", name)
				}
				profileValue, ok := profileParams[name]
				if !ok {
					t.Fatalf("profile params missing %s", name)
				}
				if !queryplanProfileParamMatchesType(productionValue, wantType) {
					t.Fatalf("captured production %s = %#v, want %s", name, productionValue, wantType)
				}
				if !queryplanProfileParamMatchesType(profileValue, wantType) {
					t.Fatalf("profile %s = %#v, want %s", name, profileValue, wantType)
				}
			}
		})
	}
}

func queryplanProfileParamMatchesType(value any, wantType string) bool {
	switch wantType {
	case "string":
		value, ok := value.(string)
		return ok && strings.TrimSpace(value) != ""
	case "int":
		value, ok := value.(int)
		return ok && value == 51
	case "[]string":
		value, ok := value.([]string)
		return ok && len(value) == 1 && strings.TrimSpace(value[0]) != ""
	default:
		return false
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
