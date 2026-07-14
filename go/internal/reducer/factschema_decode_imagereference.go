// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// This file holds the reducer-side decode wrappers for the cross-provider
// image_reference family (aws_image_reference, azure_image_reference,
// gcp_image_reference), consumed only by the shared
// container-image-identity domain's evidence extractor
// (container_image_identity_evidence.go). The per-cloud typed-decode waves
// (aws #4568, gcp/azure Wave 4a) deliberately left these three kinds
// undecoded: they are read by ONE shared cross-provider consumer, and typing
// one provider while the consumer still read every provider raw would have
// left the untouched providers' typed structs as hollow contracts (a decode
// seam nothing on the read path called). This family migrates together
// (#4685), so all three wrappers land in one file.
//
// This wrapper lives in its own factschema_decode_imagereference.go file
// following the per-family convention factschema_decode_azure.go established:
// the Contract System v1 §6 gate-2 payload-usage manifest globs the reducer
// dir's factschema_decode*.go files for decode seams
// (go/internal/payloadusage), so a per-family file is discovered and gated
// the same as the main file.

// decodeAWSImageReference decodes one aws_image_reference envelope into the
// typed awsv1.ImageReference struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// field (account_id, region, repository_name, image_digest, manifest_digest)
// or is otherwise malformed. It is the single decode site for the
// aws_image_reference kind on the reducer side: addAWSImageReference
// (container_image_identity_evidence.go) decodes through here, and a missing
// required field is routed through partitionDecodeFailures so it dead-letters
// as a per-fact input_invalid quarantine rather than building a registry
// reference string from an empty account or region segment.
func decodeAWSImageReference(env facts.Envelope) (awsv1.ImageReference, error) {
	reference, err := factschema.DecodeAWSImageReference(factschemaEnvelope(env))
	if err != nil {
		return awsv1.ImageReference{}, newFactDecodeError(factschema.FactKindAWSImageReference, err)
	}
	return reference, nil
}

// decodeAzureImageReference decodes one azure_image_reference envelope into
// the typed azurev1.ImageReference struct through the contracts seam,
// returning a self-classifying *factDecodeError when the payload is missing a
// required field (owning_arm_resource_id, owning_normalized_id,
// owning_resource_type, tag_digest_confidence) or is otherwise malformed. It
// is the single decode site for the azure_image_reference kind on the
// reducer side.
func decodeAzureImageReference(env facts.Envelope) (azurev1.ImageReference, error) {
	reference, err := factschema.DecodeAzureImageReference(factschemaEnvelope(env))
	if err != nil {
		return azurev1.ImageReference{}, newFactDecodeError(factschema.FactKindAzureImageReference, err)
	}
	return reference, nil
}

// decodeGCPImageReference decodes one gcp_image_reference envelope into the
// typed gcpv1.ImageReference struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// field (owning_full_resource_name, tag_digest_confidence) or is otherwise
// malformed. It is the single decode site for the gcp_image_reference kind on
// the reducer side.
func decodeGCPImageReference(env facts.Envelope) (gcpv1.ImageReference, error) {
	reference, err := factschema.DecodeGCPImageReference(factschemaEnvelope(env))
	if err != nil {
		return gcpv1.ImageReference{}, newFactDecodeError(factschema.FactKindGCPImageReference, err)
	}
	return reference, nil
}
