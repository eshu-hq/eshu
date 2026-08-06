// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildCanonicalMaterializationPreservesNamedTerraformAttributesMap(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		"attributes": map[string]any{
			"provider_specific_key": "provider-specific-value",
		},
	}
	input := terraformStateFacts()
	for i := range input {
		if input[i].FactKind == facts.TerraformStateResourceFactKind {
			input[i].Payload["attributes"] = want
		}
	}

	result, _ := buildCanonicalMaterialization(
		terraformStateScope(),
		terraformStateGeneration(),
		input,
	)
	if len(result.TerraformStateResources) != 1 {
		t.Fatalf("len(TerraformStateResources) = %d, want 1", len(result.TerraformStateResources))
	}
	if got := result.TerraformStateResources[0].Attributes; !reflect.DeepEqual(got, want) {
		t.Fatalf("Attributes = %#v, want exact named payload value %#v", got, want)
	}
}
