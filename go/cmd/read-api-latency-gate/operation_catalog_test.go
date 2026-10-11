// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"testing"
)

// A declared response fixture, independent of the validator's expected map.
const seededCatalogResponse = `{"verbs":[
{"verb":"CALLS","layer":"code","count":0},
{"verb":"IMPORTS","layer":"code","count":0},
{"verb":"INHERITS","layer":"code","count":0},
{"verb":"REFERENCES","layer":"code","count":0},
{"verb":"OVERRIDES","layer":"code","count":0},
{"verb":"QUERIES_TABLE","layer":"code","count":0},
{"verb":"DEPLOYS_FROM","layer":"deploy","count":0},
{"verb":"INSTANCE_OF","layer":"deploy","count":0},
{"verb":"RECONCILES_FROM","layer":"deploy","count":0},
{"verb":"PROVISIONS_DEPENDENCY_FOR","layer":"infra","count":0},
{"verb":"USES_MODULE","layer":"infra","count":0},
{"verb":"DISCOVERS_CONFIG_IN","layer":"infra","count":0},
{"verb":"MANAGES","layer":"infra","count":0},
{"verb":"ATLANTIS_DEPENDS_ON","layer":"infra","count":0},
{"verb":"USES_WORKFLOW","layer":"infra","count":0},
{"verb":"RUNS_ON","layer":"runtime","count":0},
{"verb":"AWS_lambda_function_uses_image","layer":"runtime","count":0},
{"verb":"DEPENDS_ON","layer":"runtime","count":0},
{"verb":"INVOKES_CLOUD_ACTION","layer":"security","count":0},
{"verb":"READS_CONFIG_FROM","layer":"ops","count":0},
{"verb":"TAINT_FLOWS_TO","layer":"ops","count":0}
],"verb_count":21,"layer_count":6,"total_edges":0}`

func TestPilotCatalogRequiresSeededIdentitiesAndCounts(t *testing.T) {
	if err := validatePilotResponse("relationships_catalog", []byte(seededCatalogResponse)); err != nil {
		t.Fatalf("declared seeded response rejected: %v", err)
	}
	mutants := map[string]func(map[string]any, []any){
		"invented_identity": func(_ map[string]any, verbs []any) {
			verbs[0].(map[string]any)["verb"] = "nonsense"
		},
		"wrong_layer": func(_ map[string]any, verbs []any) {
			verbs[0].(map[string]any)["layer"] = "runtime"
		},
		"duplicate_and_missing": func(_ map[string]any, verbs []any) {
			verbs[0] = verbs[1]
		},
		"missing_self_consistent": func(payload map[string]any, verbs []any) {
			payload["verbs"] = verbs[1:]
			payload["verb_count"] = float64(20)
		},
		"nonzero_self_consistent": func(payload map[string]any, verbs []any) {
			verbs[0].(map[string]any)["count"] = float64(1)
			payload["total_edges"] = float64(1)
		},
	}
	for name, mutate := range mutants {
		t.Run(name, func(t *testing.T) {
			var payload map[string]any
			if err := json.Unmarshal([]byte(seededCatalogResponse), &payload); err != nil {
				t.Fatal(err)
			}
			mutate(payload, payload["verbs"].([]any))
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if err := validatePilotResponse("relationships_catalog", body); err == nil {
				t.Fatal("semantically incorrect catalog passed")
			}
		})
	}
}
