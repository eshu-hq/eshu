// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	tfstatev1 "github.com/eshu-hq/eshu/sdk/go/factschema/terraformstate/v1"
)

// This file holds the projector-side decode wrappers for the terraform_state
// fact family. Each wraps the contracts-module Decode* seam and, on a
// classified *factschema.DecodeError (a missing/null required identity
// field), returns a *projectorDecodeError so partitionProjectorDecodeFailures
// can quarantine the fact per-fact rather than the extractor computing a
// graph identity from an empty-string segment. The CONSUMED terraform_state
// kinds get a wrapper here: snapshot, resource, module, output,
// tag_observation, and (#5446) provider_binding, joined onto a resource row's
// Provider/ProviderSourceAddress/ProviderAlias fields by
// terraformStateProviderBindingsByResource in tfstate_canonical.go. The two
// remaining typed-but-not-yet-consumed kinds (candidate, warning) have no
// projector read site and therefore no wrapper — decode_terraformstate.go's
// Decode* seam exists for them already, but wiring a projector wrapper with
// no caller would be dead code.

// decodeTerraformStateSnapshot decodes one terraform_state_snapshot envelope
// into the typed struct through the contracts seam. Snapshot has no required
// field, so this only fails on an unsupported schema major or a payload that
// cannot unmarshal into the struct shape at all.
func decodeTerraformStateSnapshot(env facts.Envelope) (tfstatev1.Snapshot, error) {
	snapshot, err := factschema.DecodeTerraformStateSnapshot(factschemaEnvelope(env))
	if err != nil {
		return tfstatev1.Snapshot{}, newProjectorDecodeError(factschema.FactKindTerraformStateSnapshot, err)
	}
	return snapshot, nil
}

// decodeTerraformStateResource decodes one terraform_state_resource envelope
// into the typed struct. A missing required field (address) yields a
// self-classifying *projectorDecodeError.
func decodeTerraformStateResource(env facts.Envelope) (tfstatev1.Resource, error) {
	resource, err := factschema.DecodeTerraformStateResource(factschemaEnvelope(env))
	if err != nil {
		return tfstatev1.Resource{}, newProjectorDecodeError(factschema.FactKindTerraformStateResource, err)
	}
	return resource, nil
}

// decodeTerraformStateModule decodes one terraform_state_module envelope into
// the typed struct. A missing required field (module_address) yields a
// self-classifying *projectorDecodeError.
func decodeTerraformStateModule(env facts.Envelope) (tfstatev1.Module, error) {
	module, err := factschema.DecodeTerraformStateModule(factschemaEnvelope(env))
	if err != nil {
		return tfstatev1.Module{}, newProjectorDecodeError(factschema.FactKindTerraformStateModule, err)
	}
	return module, nil
}

// decodeTerraformStateOutput decodes one terraform_state_output envelope into
// the typed struct. A missing required field (name) yields a self-classifying
// *projectorDecodeError.
func decodeTerraformStateOutput(env facts.Envelope) (tfstatev1.Output, error) {
	output, err := factschema.DecodeTerraformStateOutput(factschemaEnvelope(env))
	if err != nil {
		return tfstatev1.Output{}, newProjectorDecodeError(factschema.FactKindTerraformStateOutput, err)
	}
	return output, nil
}

// decodeTerraformStateTagObservation decodes one
// terraform_state_tag_observation envelope into the typed struct. A missing
// required join key (resource_address, tag_key_hash) yields a
// self-classifying *projectorDecodeError.
func decodeTerraformStateTagObservation(env facts.Envelope) (tfstatev1.TagObservation, error) {
	observation, err := factschema.DecodeTerraformStateTagObservation(factschemaEnvelope(env))
	if err != nil {
		return tfstatev1.TagObservation{}, newProjectorDecodeError(factschema.FactKindTerraformStateTagObservation, err)
	}
	return observation, nil
}

// decodeTerraformStateProviderBinding decodes one
// terraform_state_provider_binding envelope into the typed struct (#5446). A
// missing required join key (resource_address, provider_address) yields a
// self-classifying *projectorDecodeError.
func decodeTerraformStateProviderBinding(env facts.Envelope) (tfstatev1.ProviderBinding, error) {
	binding, err := factschema.DecodeTerraformStateProviderBinding(factschemaEnvelope(env))
	if err != nil {
		return tfstatev1.ProviderBinding{}, newProjectorDecodeError(factschema.FactKindTerraformStateProviderBinding, err)
	}
	return binding, nil
}

// tfstateDerefString returns the value a *string points at, or "" when it is
// nil. The typed terraform_state structs carry optional common fields as
// *string so an absent key stays distinct from an observed empty value; the
// row builders substitute "" for an unobserved field, matching the pre-typing
// payloadString("") behavior.
func tfstateDerefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// tfstateDerefInt64 returns the value a *int64 points at, or 0 when it is
// nil.
func tfstateDerefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// tfstateDerefBool returns the value a *bool points at, or false when it is
// nil.
func tfstateDerefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}
