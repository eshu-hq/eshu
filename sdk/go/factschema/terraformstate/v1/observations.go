// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Module is the schema-version-1 typed payload for the
// "terraform_state_module" fact kind (Contract System v1 §3.1).
//
// One fact is emitted per (module, resource) observation; the projector
// aggregates them by module address into one canonical TerraformStateModule
// node (go/internal/projector/tfstate_canonical.go terraformStateModuleRow and
// aggregateTerraformStateModuleRows), summing ResourceCount. The projector
// DROPS a module observation whose module_address is empty, so ModuleAddress is
// the sole REQUIRED identity field: an absent module_address dead-letters as
// input_invalid rather than fabricating an empty-identity module node.
// ResourceCount is OPTIONAL descriptive data (the count contributed by this one
// observation); the projector tolerates an absent count as 0.
type Module struct {
	// ModuleAddress is the module's fully-qualified address (for example
	// "module.vpc" or "module.vpc.module.subnet"). Required — the module node's
	// identity and aggregation key.
	ModuleAddress string `json:"module_address"`

	// ResourceCount is the number of resource instances this observation
	// contributes to the module (1 per emitted fact today). Optional pointer so
	// nil (unreported) stays distinct from an observed 0; the projector sums it
	// across observations.
	ResourceCount *int64 `json:"resource_count,omitempty"`
}

// Output is the schema-version-1 typed payload for the
// "terraform_state_output" fact kind (Contract System v1 §3.1).
//
// One fact is emitted per Terraform output observed in state. The projector
// materializes a canonical TerraformStateOutput node keyed by a uid folded from
// the output Name (go/internal/projector/tfstate_canonical.go
// terraformStateOutputRow), which DROPS an output whose name is empty. Name is
// therefore the sole REQUIRED identity field. Sensitive, ValueShape, and the
// redacted Value are OPTIONAL: the projector derives a value shape and a
// sensitivity flag best-effort and tolerates any of them being absent.
type Output struct {
	// Name is the Terraform output name. Required — the output node's identity;
	// an absent name dead-letters rather than fabricating an empty-identity node.
	Name string `json:"name"`

	// Sensitive reports whether the output was declared sensitive. Optional
	// pointer so nil (unreported) stays distinct from an observed false; the
	// projector treats nil as false.
	Sensitive *bool `json:"sensitive,omitempty"`

	// ValueShape classifies the output value's shape ("scalar",
	// "redacted_scalar", "composite"). Optional: the projector derives a
	// fallback shape from the presence of the raw "value" key plus Sensitive when
	// it is absent.
	ValueShape *string `json:"value_shape,omitempty"`

	// The raw "value" payload key (the redacted output value or its redaction
	// envelope) is intentionally NOT modeled as a named field: it is polymorphic
	// (a scalar or a redaction object) and no consumer reads its content. The
	// projector reads only whether the key is PRESENT to derive a fallback
	// ValueShape, which it does against the raw envelope payload, not the decoded
	// struct. Leaving it unmodeled keeps this struct convention-compliant (a bare
	// optional `any` field is banned by TestPayloadStructShapeConvention) while
	// the open schema (additionalProperties: true) still permits the key and the
	// decode seam preserves it in the envelope for the presence check.
}

// TagObservation is the schema-version-1 typed payload for the
// "terraform_state_tag_observation" fact kind (Contract System v1 §3.1).
//
// One fact is emitted per resource tag key observed in state attributes. The
// projector joins each tag to its resource by the pair (ResourceAddress,
// TagKeyHash) (go/internal/projector/tfstate_canonical.go
// terraformStateTagHashesByResource), which SKIPS an observation missing either
// key. Both are therefore REQUIRED join keys: either absent breaks the
// tag→resource join, so an absent resource_address or tag_key_hash dead-letters
// as input_invalid rather than silently dropping the tag with no operator
// signal. TagSource is OPTIONAL provenance describing where in the attributes
// the tag was found.
type TagObservation struct {
	// ResourceAddress is the address of the resource the tag was observed on.
	// Required — the first half of the tag→resource join key.
	ResourceAddress string `json:"resource_address"`

	// TagKeyHash is the keyed hash of the tag key (the raw key is never
	// persisted). Required — the second half of the tag→resource join key and
	// the tag's own identity.
	TagKeyHash string `json:"tag_key_hash"`

	// TagSource is the attributes path the tag was found under (for example
	// "tags" or "tags_all"). Optional provenance the join does not read.
	TagSource *string `json:"tag_source,omitempty"`

	// The raw "tag_key" and "tag_value" payload keys (each a fail-closed
	// classification envelope the emitter adds only when schema coverage
	// classified the tag) are intentionally NOT modeled as named fields: no
	// consumer reads them, and a bare optional `any` field is banned by
	// TestPayloadStructShapeConvention. The open schema still permits the keys and
	// the decode seam preserves them in the envelope, so a future consumer can
	// model them when it needs them.
}
