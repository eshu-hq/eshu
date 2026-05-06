package truth

import (
	"fmt"
	"strings"
)

// Layer identifies one bounded truth layer in the canonical reconciliation model.
type Layer string

const (
	// LayerSourceDeclaration records source-authored declarations such as code or IaC.
	LayerSourceDeclaration Layer = "source_declaration"
	// LayerAppliedDeclaration records applied state such as Terraform or CloudFormation state.
	LayerAppliedDeclaration Layer = "applied_declaration"
	// LayerObservedResource records provider-observed runtime state.
	LayerObservedResource Layer = "observed_resource"
	// LayerCanonicalAsset records reducer-owned canonical truth.
	LayerCanonicalAsset Layer = "canonical_asset"
)

// Contract captures the truth-layer contract for one reducer-owned canonical kind.
type Contract struct {
	CanonicalKind string
	SourceLayers  []Layer
}

// ParseLayer converts one raw string into a known truth layer.
func ParseLayer(raw string) (Layer, error) {
	layer := Layer(strings.TrimSpace(raw))
	if err := layer.Validate(); err != nil {
		return "", err
	}
	return layer, nil
}

// Validate checks that the layer is one of the known truth layers.
func (layer Layer) Validate() error {
	switch layer {
	case LayerSourceDeclaration, LayerAppliedDeclaration, LayerObservedResource, LayerCanonicalAsset:
		return nil
	default:
		return fmt.Errorf("unknown truth layer %q", layer)
	}
}

// Validate checks that the truth contract is explicit and usable by reducers.
func (contract Contract) Validate() error {
	if strings.TrimSpace(contract.CanonicalKind) == "" {
		return fmt.Errorf("canonical kind must not be blank")
	}
	if len(contract.SourceLayers) == 0 {
		return fmt.Errorf("source layers must not be empty")
	}

	seen := map[Layer]struct{}{}
	for _, layer := range contract.SourceLayers {
		if err := layer.Validate(); err != nil {
			return err
		}
		if layer == LayerCanonicalAsset {
			return fmt.Errorf("source layers must not include canonical_asset")
		}
		if _, ok := seen[layer]; ok {
			return fmt.Errorf("source layers must not contain duplicates")
		}
		seen[layer] = struct{}{}
	}

	return nil
}

// Supports reports whether the contract accepts the supplied source layer.
func (contract Contract) Supports(layer Layer) bool {
	for _, candidate := range contract.SourceLayers {
		if candidate == layer {
			return true
		}
	}
	return false
}
