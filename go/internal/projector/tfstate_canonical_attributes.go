// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

// terraformStateResourceAttributes unwraps a known factschema decode-plan
// artifact (sdk/go/factschema/decode_map.go's structPlanFor/decodeMapIntoWith):
// any struct field literally named "Attributes" of map kind is treated as the
// generic polymorphic pass-through remainder, not as a normally json-tagged
// field, even though tfstatev1.Resource.Attributes carries an explicit
// `json:"attributes,omitempty"` tag. Because the raw payload also has a
// top-level key literally called "attributes" (holding the real classified
// object), that key never matches a "known" field name and lands in the
// remainder under its own name, so decodeTerraformStateResource's
// resource.Attributes ends up self-nested as {"attributes": {...real
// classified attributes...}} instead of the classified object directly. This
// is a pre-existing decode-library defect out of scope for #5441 to fix (it
// is shared, contract-governed infrastructure with a wide blast radius across
// every polymorphic Resource/Relationship struct — tracked as
// https://github.com/eshu-hq/eshu/issues/5522, which supersedes this
// workaround); this helper only corrects the read on this one new call site
// so TerraformResource attribute promotion
// (go/internal/storage/cypher/terraform_attribute_promotion.go) gets the
// real classified map. It degrades gracefully to the raw value if the
// wrapper is ever fixed upstream (no double-unwrap, no panic) — once #5522
// lands, len(attributes) will never be 1 with a lone "attributes" key for a
// resource carrying more than one classified attribute, and this whole
// function becomes a no-op passthrough that #5522's fix should remove.
//
// Only handles the single-unclaimed-key shape (len(attributes) == 1). If a
// future factschema change adds a second unclaimed field alongside
// "attributes" (widening the remainder to more than one key), the
// len(attributes) != 1 guard below short-circuits and returns the raw,
// still-self-nested remainder unmodified — promotion then silently yields
// zero properties (terraformAttributePathValue finds nothing at the
// remainder's top level) rather than corrupting or fabricating data. See
// TestTerraformStateResourceAttributesMultiKeyRemainderDegradesToRawValue
// for the locked-in current behavior.
func terraformStateResourceAttributes(attributes map[string]any) map[string]any {
	if len(attributes) != 1 {
		return attributes
	}
	wrapped, ok := attributes["attributes"]
	if !ok {
		return attributes
	}
	inner, ok := wrapped.(map[string]any)
	if !ok {
		return attributes
	}
	return inner
}
