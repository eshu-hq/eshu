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
// This wrapper lives in a per-family factschema_decode_azure.go file. The
// Contract System v1 §6 gate-2 payload-usage manifest globs the reducer dir's
// factschema_decode*.go files for decode seams (go/internal/payloadusage), so a
// per-family file is discovered and gated the same as the main file; keeping
// each cloud family's decode wrappers in its own file keeps the diff of a new
// family self-contained.
//
// Only the two WIRED azure kinds (azure_cloud_resource,
// azure_cloud_relationship) are decoded through a typed seam this wave. The
// family's other consumed kinds (azure_tag_observation,
// azure_identity_observation, azure_resource_change, azure_image_reference)
// are intentionally NOT decoded through a typed seam: their read-side consumers
// are a shared cross-provider surface or an Azure-specific storage loader not
// converted in this wave, so a decode wrapper for them would be dead code with
// no caller (and a hollow, never-validated contract). They migrate with the
// surface that reads them.
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
