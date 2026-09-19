// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"
)

// iacEntityTypes are the three entity_type values
// go/internal/query/iac/inventory_postgres.go's currentInventoryCTE filters
// on. A fact whose entity_type is anything else is invisible to that query
// and does not exercise its cost.
var iacEntityTypes = []string{"TerraformResource", "TerraformModule", "TerraformDataSource"}

// SeedIaCFact is one planned fact_records row of fact_kind "content_entity",
// shaped so currentInventoryCTE (go/internal/query/iac/inventory_postgres.go)
// selects it: active generation, is_tombstone=false, entity_type one of
// iacEntityTypes.
type SeedIaCFact struct {
	FactID       string
	ScopeID      string
	GenerationID string
	EntityID     string
	EntityName   string
	EntityType   string
	RelativePath string
	Provider     string
	ResourceType string
	// LargePayload marks a fact whose jsonb payload should be large enough
	// (~15KB) to be TOASTed out-of-line, matching the #6793 root-cause
	// payload-size mix; false means a small (~1KB) payload.
	LargePayload bool
}

// BuildIaCFacts deterministically builds count IaC content_entity facts
// anchored on scopeID's generationID, cycling through iacEntityTypes and
// marking roughly one in ten LargePayload — the #6793 root cause is a jsonb
// detoast cost that depends on a realistic mix of small and TOASTed payload
// sizes, not a uniform one.
func BuildIaCFacts(scopeID, generationID string, count int) []SeedIaCFact {
	facts := make([]SeedIaCFact, 0, count)
	for i := 0; i < count; i++ {
		entityType := iacEntityTypes[i%len(iacEntityTypes)]
		facts = append(facts, SeedIaCFact{
			FactID:       fmt.Sprintf("%s-iac-fact-%d", generationID, i),
			ScopeID:      scopeID,
			GenerationID: generationID,
			EntityID:     fmt.Sprintf("iac-entity-%d", i),
			EntityName:   fmt.Sprintf("aws_instance.seed_%d", i),
			EntityType:   entityType,
			RelativePath: "infra/main.tf",
			Provider:     "aws",
			ResourceType: "aws_instance",
			LargePayload: i%10 == 0,
		})
	}
	return facts
}

// iacFactPayload renders f's fact_records.payload JSONB. Small payloads
// target ~1KB, large ones ~15KB, via a padding field — realistic Terraform
// resource facts carry a variably-sized "raw config" style blob, and the
// #6793 root cause is specifically the jsonb detoast cost of scanning that
// blob at scale.
func iacFactPayload(f SeedIaCFact) map[string]any {
	paddingSize := 900
	if f.LargePayload {
		paddingSize = 15000
	}
	return map[string]any{
		"entity_id":     f.EntityID,
		"entity_name":   f.EntityName,
		"entity_type":   f.EntityType,
		"relative_path": f.RelativePath,
		"repo_id":       f.ScopeID,
		"entity_metadata": map[string]any{
			"resource_type": f.ResourceType,
			"provider":      f.Provider,
			// Padding models the raw HCL/attribute blob a real Terraform
			// resource fact carries; its size is what drives the jsonb
			// detoast cost, not its content.
			"raw_config": strings.Repeat("x", paddingSize),
		},
	}
}
