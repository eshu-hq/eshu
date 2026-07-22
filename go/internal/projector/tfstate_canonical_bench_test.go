// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// tfstateBenchResourceEnvelopes builds n synthetic terraform_state_resource
// envelopes and, when withProviderBinding is true, one matching
// terraform_state_provider_binding envelope per resource (#5446). This lets
// BenchmarkExtractTerraformStateRowsProviderBindingOverhead isolate exactly
// the pre-pass's added cost: same resource set, only the binding facts
// differ.
func tfstateBenchResourceEnvelopes(n int, withProviderBinding bool) []facts.Envelope {
	observedAt := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	envelopes := make([]facts.Envelope, 0, 2*n)
	for i := 0; i < n; i++ {
		address := fmt.Sprintf("aws_instance.web_%d", i)
		envelopes = append(envelopes, facts.Envelope{
			FactID:           fmt.Sprintf("tf-resource-%d", i),
			ScopeID:          "tf-scope-bench",
			GenerationID:     "tf-generation-bench",
			FactKind:         facts.TerraformStateResourceFactKind,
			SchemaVersion:    facts.TerraformStateResourceSchemaVersion,
			CollectorKind:    string(scope.CollectorTerraformState),
			SourceConfidence: facts.SourceConfidenceObserved,
			ObservedAt:       observedAt,
			Payload: map[string]any{
				"address": address,
				"type":    "aws_instance",
				"mode":    "managed",
			},
		})
		if !withProviderBinding {
			continue
		}
		envelopes = append(envelopes, facts.Envelope{
			FactID:           fmt.Sprintf("tf-provider-binding-%d", i),
			ScopeID:          "tf-scope-bench",
			GenerationID:     "tf-generation-bench",
			FactKind:         facts.TerraformStateProviderBindingFactKind,
			SchemaVersion:    facts.TerraformStateProviderBindingSchemaVersion,
			CollectorKind:    string(scope.CollectorTerraformState),
			SourceConfidence: facts.SourceConfidenceObserved,
			ObservedAt:       observedAt,
			Payload: map[string]any{
				"resource_address":        address,
				"provider_address":        "provider[\"registry.terraform.io/hashicorp/aws\"]",
				"provider_source_address": "registry.terraform.io/hashicorp/aws",
				"provider_type":           "aws",
			},
		})
	}
	return envelopes
}

// BenchmarkExtractTerraformStateRowsProviderBindingOverhead is the #5446
// Prove-The-Theory-First proof for the new provider-binding pre-pass: it
// measures extractTerraformStateRows on the SAME 5,000-resource synthetic
// batch with and without a matching terraform_state_provider_binding fact
// per resource, isolating exactly the pre-pass's added decode+join cost
// (terraformStateProviderBindingsByResource is a single O(n) pass over the
// envelope slice, mirroring the pre-existing terraformStateTagHashesByResource
// pre-pass this batch already ran before #5446).
func BenchmarkExtractTerraformStateRowsProviderBindingOverhead(b *testing.B) {
	const resourceCount = 5_000

	b.Run("without_provider_binding_facts", func(b *testing.B) {
		envelopes := tfstateBenchResourceEnvelopes(resourceCount, false)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mat := &CanonicalMaterialization{ScopeID: "tf-scope-bench"}
			_ = extractTerraformStateRows(mat, envelopes)
		}
	})

	b.Run("with_provider_binding_facts", func(b *testing.B) {
		envelopes := tfstateBenchResourceEnvelopes(resourceCount, true)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mat := &CanonicalMaterialization{ScopeID: "tf-scope-bench"}
			_ = extractTerraformStateRows(mat, envelopes)
		}
	})
}
