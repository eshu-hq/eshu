// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
)

// decodeAzureCloudResource decodes one azure_cloud_resource envelope into the
// typed azurev1.CloudResource struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// field (arm_resource_id, resource_type, subscription_id, location) or is
// otherwise malformed. It is the single decode site for the
// azure_cloud_resource kind on the reducer side: every handler and join-index
// builder that consumes azure_cloud_resource facts decodes through here, and a
// missing required field is routed through partitionDecodeFailures so it
// dead-letters as a per-fact input_invalid quarantine rather than a silent
// empty-string graph identity or a whole-intent abort.
//
// The sibling azure_tag_observation, azure_identity_observation,
// azure_resource_change, azure_dns_record, azure_image_reference, and
// azure_collection_warning kinds have typed structs and a Decode/Encode seam
// in sdk/go/factschema (decode_azure.go), but no reducer handler in this
// package consumes them yet (their current consumers, e.g.
// go/internal/storage/postgres/cloud_resource_change_evidence.go and
// cloud_identity_policy_evidence.go, decode a raw JSON payload directly and
// are a separate migration boundary). Wiring a decode wrapper for a kind with
// no reducer caller here would be dead code the go-lint unused check rejects;
// add one only alongside the handler that starts consuming it.
func decodeAzureCloudResource(env facts.Envelope) (azurev1.CloudResource, error) {
	resource, err := factschema.DecodeAzureCloudResource(factschemaEnvelope(env))
	if err != nil {
		return azurev1.CloudResource{}, newFactDecodeError(factschema.FactKindAzureCloudResource, err)
	}
	return resource, nil
}

// decodeAzureCloudRelationship decodes one azure_cloud_relationship envelope
// into the typed azurev1.CloudRelationship struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing a
// required field (relationship_type, source_arm_resource_id,
// target_arm_resource_id) or is otherwise malformed. It is the single decode
// site for the azure_cloud_relationship kind on the reducer side.
func decodeAzureCloudRelationship(env facts.Envelope) (azurev1.CloudRelationship, error) {
	relationship, err := factschema.DecodeAzureCloudRelationship(factschemaEnvelope(env))
	if err != nil {
		return azurev1.CloudRelationship{}, newFactDecodeError(factschema.FactKindAzureCloudRelationship, err)
	}
	return relationship, nil
}
