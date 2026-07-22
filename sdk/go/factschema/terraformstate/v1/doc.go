// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "terraform_state" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_terraformstate.go).
//
// Eight fact kinds live here. Six are CONSUMED today by the projector's
// source-local canonical extractor (go/internal/projector/tfstate_canonical.go)
// and decode through the seam on the read path:
//
//   - Snapshot            (terraform_state_snapshot)
//   - Resource            (terraform_state_resource)
//   - Module              (terraform_state_module)
//   - Output              (terraform_state_output)
//   - TagObservation      (terraform_state_tag_observation)
//   - ProviderBinding     (terraform_state_provider_binding, consumed since #5446)
//
// Two are TYPED-BUT-NOT-YET-CONSUMED: their payloads have no read-side
// decode consumer in the current codebase (a candidate fact is discovery
// provenance, and a warning is metadata the projector routes on fact kind
// alone without reading its payload). They are typed here so the contract,
// schema, and fixture pack are ready the moment a consumer is added, matching
// how the gcp family typed gcp_image_reference / gcp_tag_observation ahead of
// their shared consumer (gcp/v1/doc.go). They gain a decode-site conversion, a
// regression test, and a benchmark in the change that first reads them, not in
// this wave:
//
//   - Candidate           (terraform_state_candidate)
//   - Warning             (terraform_state_warning)
//
// Required vs optional. Each struct's required fields are non-pointer with no
// omitempty tag; the decode seam rejects a payload that omits one, or supplies
// an explicit JSON null for one, with a classified ClassificationInputInvalid
// error naming the field, never a zero-value struct. Optional fields are
// pointers, slices, or maps carrying omitempty, so an absent value decodes to
// nil and stays distinct from an observed zero.
//
// The required set for each CONSUMED kind is exactly the identity/join key
// whose ABSENCE produces a broken or empty graph identity in the projector
// today — the accuracy fix Contract System v1 exists to protect:
//
//   - Resource.Address            — the projector drops a resource with an
//     empty address (tfstate_canonical.go terraformStateResourceRow) and folds
//     it into the resource node uid; an absent address must dead-letter, not
//     fabricate an empty-address node.
//   - Module.ModuleAddress        — the module uid and aggregation key; an
//     absent module_address is a broken module identity.
//   - Output.Name                 — the output uid key; an absent name is a
//     broken output identity.
//   - TagObservation.ResourceAddress and TagObservation.TagKeyHash — the two
//     join keys the projector matches a tag to its resource on; either absent
//     breaks the tag→resource join.
//   - Snapshot has NO required field: the projector reads lineage, serial,
//     backend_kind, and locator_hash best-effort and tolerates any of them
//     being empty (it derives a fallback state path from the scope id), so no
//     snapshot field's absence produces a broken identity. Marking one required
//     would flip a today-valid incomplete snapshot into a dead-letter — an
//     accuracy regression the contract forbids — so every snapshot field is
//     optional.
//
// A present-but-empty required value (an empty string) is a VALID decode, not a
// dead-letter, matching the pre-typing projector behavior where an empty
// identity value was simply dropped as non-materializable rather than errored.
// Only an ABSENT key (or explicit null) dead-letters.
//
// The reducer/projector decodes only the latest struct for each kind. Version
// shims for an older schema major live in the parent factschema package's
// decode seam (decodeLatestMajor in decode.go), never in this package or in
// the projector's canonical extractor.
package v1
