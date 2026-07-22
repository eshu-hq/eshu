// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	tfstatev1 "github.com/eshu-hq/eshu/sdk/go/factschema/terraformstate/v1"
)

// DecodeTerraformStateSnapshot decodes env.Payload into the latest
// tfstatev1.Snapshot struct for the "terraform_state_snapshot" fact kind,
// dispatching on env.SchemaVersion major per Contract System v1 §3.2. Callers
// (the projector's source-local canonical extractor) receive either the decoded
// struct or a classified *DecodeError; they must never substitute a zero-value
// struct on error. Snapshot has no required field, so a snapshot decode fails
// only on an unsupported major or a payload that cannot unmarshal into the
// struct shape at all — never on a missing identity field.
func DecodeTerraformStateSnapshot(env Envelope) (tfstatev1.Snapshot, error) {
	return decodeLatestMajor[tfstatev1.Snapshot](FactKindTerraformStateSnapshot, env)
}

// EncodeTerraformStateSnapshot marshals a tfstatev1.Snapshot into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeTerraformStateSnapshot for schema-version-1 payloads, used by this
// module's round-trip tests.
func EncodeTerraformStateSnapshot(snapshot tfstatev1.Snapshot) (map[string]any, error) {
	return encodeDirectPayload(snapshot)
}

// DecodeTerraformStateResource decodes env.Payload into the latest
// tfstatev1.Resource struct for the "terraform_state_resource" fact kind. A
// payload missing the required "address" key (or supplying it as null)
// dead-letters as a classified input_invalid error naming the field, rather
// than fabricating an empty-address resource node. See
// DecodeTerraformStateSnapshot for the dispatch and error contract.
func DecodeTerraformStateResource(env Envelope) (tfstatev1.Resource, error) {
	return decodeLatestMajor[tfstatev1.Resource](FactKindTerraformStateResource, env)
}

// EncodeTerraformStateResource marshals a tfstatev1.Resource into the
// map[string]any payload shape an Envelope carries.
func EncodeTerraformStateResource(resource tfstatev1.Resource) (map[string]any, error) {
	return encodeDirectPayload(resource)
}

// DecodeTerraformStateModule decodes env.Payload into the latest
// tfstatev1.Module struct for the "terraform_state_module" fact kind. A payload
// missing the required "module_address" key dead-letters as input_invalid. See
// DecodeTerraformStateSnapshot for the dispatch and error contract.
func DecodeTerraformStateModule(env Envelope) (tfstatev1.Module, error) {
	return decodeLatestMajor[tfstatev1.Module](FactKindTerraformStateModule, env)
}

// EncodeTerraformStateModule marshals a tfstatev1.Module into the
// map[string]any payload shape an Envelope carries.
func EncodeTerraformStateModule(module tfstatev1.Module) (map[string]any, error) {
	return encodeDirectPayload(module)
}

// DecodeTerraformStateOutput decodes env.Payload into the latest
// tfstatev1.Output struct for the "terraform_state_output" fact kind. A payload
// missing the required "name" key dead-letters as input_invalid. See
// DecodeTerraformStateSnapshot for the dispatch and error contract.
func DecodeTerraformStateOutput(env Envelope) (tfstatev1.Output, error) {
	return decodeLatestMajor[tfstatev1.Output](FactKindTerraformStateOutput, env)
}

// EncodeTerraformStateOutput marshals a tfstatev1.Output into the
// map[string]any payload shape an Envelope carries.
func EncodeTerraformStateOutput(output tfstatev1.Output) (map[string]any, error) {
	return encodeDirectPayload(output)
}

// DecodeTerraformStateTagObservation decodes env.Payload into the latest
// tfstatev1.TagObservation struct for the "terraform_state_tag_observation"
// fact kind. A payload missing either required join key ("resource_address" or
// "tag_key_hash") dead-letters as input_invalid, rather than silently breaking
// the tag→resource join. See DecodeTerraformStateSnapshot for the dispatch and
// error contract.
func DecodeTerraformStateTagObservation(env Envelope) (tfstatev1.TagObservation, error) {
	return decodeLatestMajor[tfstatev1.TagObservation](FactKindTerraformStateTagObservation, env)
}

// EncodeTerraformStateTagObservation marshals a tfstatev1.TagObservation into
// the map[string]any payload shape an Envelope carries.
func EncodeTerraformStateTagObservation(observation tfstatev1.TagObservation) (map[string]any, error) {
	return encodeDirectPayload(observation)
}

// DecodeTerraformStateCandidate decodes env.Payload into the latest
// tfstatev1.Candidate struct for the "terraform_state_candidate" fact kind.
// This kind is typed-but-not-yet-consumed (terraformstate/v1/doc.go): the seam
// exists so the contract is ready, but no read path calls it in this wave. A
// payload missing a required discovery field (candidate_source, backend_kind,
// repo_id, relative_path, path_hash) dead-letters as input_invalid.
func DecodeTerraformStateCandidate(env Envelope) (tfstatev1.Candidate, error) {
	return decodeLatestMajor[tfstatev1.Candidate](FactKindTerraformStateCandidate, env)
}

// EncodeTerraformStateCandidate marshals a tfstatev1.Candidate into the
// map[string]any payload shape an Envelope carries.
func EncodeTerraformStateCandidate(candidate tfstatev1.Candidate) (map[string]any, error) {
	return encodeDirectPayload(candidate)
}

// DecodeTerraformStateProviderBinding decodes env.Payload into the latest
// tfstatev1.ProviderBinding struct for the "terraform_state_provider_binding"
// fact kind. Consumed by the projector's provider-binding pre-pass
// (go/internal/projector/tfstate_canonical.go's
// terraformStateProviderBindingsByResource, #5446), which joins the decoded
// ProviderType/ProviderSourceAddress/ProviderAlias onto a
// TerraformStateResource row by ResourceAddress. A payload missing either
// required join key (resource_address, provider_address) dead-letters as
// input_invalid.
func DecodeTerraformStateProviderBinding(env Envelope) (tfstatev1.ProviderBinding, error) {
	return decodeLatestMajor[tfstatev1.ProviderBinding](FactKindTerraformStateProviderBinding, env)
}

// EncodeTerraformStateProviderBinding marshals a tfstatev1.ProviderBinding into
// the map[string]any payload shape an Envelope carries.
func EncodeTerraformStateProviderBinding(binding tfstatev1.ProviderBinding) (map[string]any, error) {
	return encodeDirectPayload(binding)
}

// DecodeTerraformStateWarning decodes env.Payload into the latest
// tfstatev1.Warning struct for the "terraform_state_warning" fact kind.
// Typed-but-not-yet-consumed. A payload missing a required field (warning_kind,
// reason, source) dead-letters as input_invalid.
func DecodeTerraformStateWarning(env Envelope) (tfstatev1.Warning, error) {
	return decodeLatestMajor[tfstatev1.Warning](FactKindTerraformStateWarning, env)
}

// EncodeTerraformStateWarning marshals a tfstatev1.Warning into the
// map[string]any payload shape an Envelope carries.
func EncodeTerraformStateWarning(warning tfstatev1.Warning) (map[string]any, error) {
	return encodeDirectPayload(warning)
}
