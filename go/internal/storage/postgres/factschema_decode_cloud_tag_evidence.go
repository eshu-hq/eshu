// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// decodeAzureTagObservation decodes one azure_tag_observation envelope into
// the typed azurev1.TagObservation struct through the sdk/go/factschema
// seam, returning a self-classifying *postgresFactDecodeError when the
// payload is missing a required field (arm_resource_id,
// normalized_resource_id, resource_type, tag_value_fingerprints) or a
// fingerprint value is not a JSON string.
//
// PostgresCloudTagEvidenceLoader (cloud_tag_evidence.go) is the only
// production reader of azure_tag_observation's payload, so this decode seam
// lives at the storage-loader boundary rather than the reducer — mirroring
// secrets_iam_trust_chain_anchor_decode.go, which decodes other storage-
// loader-only fact kinds the same way (#4686). This wrapper lives in a
// factschema_decode*.go file so the Contract System v1 §6 gate-2
// payload-usage manifest (go/internal/payloadusage) discovers it via the
// LoaderDir (go/internal/storage/postgres) glob and gates its field usage
// against the checked-in azure_tag_observation JSON Schema.
func decodeAzureTagObservation(env factschema.Envelope) (azurev1.TagObservation, error) {
	observation, err := factschema.DecodeAzureTagObservation(env)
	if err != nil {
		return azurev1.TagObservation{}, newPostgresFactDecodeError(factschema.FactKindAzureTagObservation, err)
	}
	return observation, nil
}

// decodeGCPTagObservation decodes one gcp_tag_observation envelope into the
// typed gcpv1.TagObservation struct through the sdk/go/factschema seam,
// returning a self-classifying *postgresFactDecodeError when the payload is
// missing a required field (full_resource_name, asset_type,
// tag_value_fingerprints) or a fingerprint value is not a JSON string. It is
// the single decode site for gcp_tag_observation on the shared
// cloud-tag-evidence storage loader (#4686); see decodeAzureTagObservation's
// doc comment for why this seam lives at the loader boundary.
func decodeGCPTagObservation(env factschema.Envelope) (gcpv1.TagObservation, error) {
	observation, err := factschema.DecodeGCPTagObservation(env)
	if err != nil {
		return gcpv1.TagObservation{}, newPostgresFactDecodeError(factschema.FactKindGCPTagObservation, err)
	}
	return observation, nil
}
