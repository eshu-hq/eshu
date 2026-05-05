package truth

import "testing"

func TestParseLayerAcceptsKnownValues(t *testing.T) {
	t.Parallel()

	tests := map[string]Layer{
		"source_declaration":  LayerSourceDeclaration,
		"applied_declaration": LayerAppliedDeclaration,
		"observed_resource":   LayerObservedResource,
		"canonical_asset":     LayerCanonicalAsset,
	}

	for input, want := range tests {
		got, err := ParseLayer(input)
		if err != nil {
			t.Fatalf("ParseLayer(%q) error = %v, want nil", input, err)
		}
		if got != want {
			t.Fatalf("ParseLayer(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseLayerRejectsUnknownValue(t *testing.T) {
	t.Parallel()

	if _, err := ParseLayer("unknown"); err == nil {
		t.Fatal("ParseLayer() error = nil, want non-nil")
	}
}

func TestContractValidateRequiresCanonicalKindAndSourceLayers(t *testing.T) {
	t.Parallel()

	if err := (Contract{}).Validate(); err == nil {
		t.Fatal("Contract{}.Validate() error = nil, want non-nil")
	}

	contract := Contract{
		CanonicalKind: "cloud_asset",
		SourceLayers: []Layer{
			LayerSourceDeclaration,
			LayerObservedResource,
		},
	}
	if err := contract.Validate(); err != nil {
		t.Fatalf("Contract.Validate() error = %v, want nil", err)
	}
	if !contract.Supports(LayerSourceDeclaration) {
		t.Fatal("Contract.Supports(source_declaration) = false, want true")
	}
	if contract.Supports(LayerCanonicalAsset) {
		t.Fatal("Contract.Supports(canonical_asset) = true, want false")
	}
}

func TestContractValidateRejectsCanonicalSourceLayer(t *testing.T) {
	t.Parallel()

	contract := Contract{
		CanonicalKind: "cloud_asset",
		SourceLayers: []Layer{
			LayerCanonicalAsset,
		},
	}
	if err := contract.Validate(); err == nil {
		t.Fatal("Contract.Validate() error = nil, want non-nil")
	}
}
