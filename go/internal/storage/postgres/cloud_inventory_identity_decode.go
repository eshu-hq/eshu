// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
)

// This file is deliberately NOT named factschema_decode*.go, even though it
// decodes through the sdk/go/factschema seam: that naming is the glob
// go/internal/payloadusage's gate-2 payload-usage manifest uses to discover
// NEW decode<Kind> seams (ParseDecodeSeamsGlob, LoaderDir =
// go/internal/storage/postgres). aws_resource and azure_cloud_resource
// already have a canonical reducer-side seam (decodeAWSResource,
// decodeAzureCloudResource in go/internal/reducer/schemadecode/factschema_decode*.go) that
// already gates account_id/subscription_id as required, declared fields; a
// second same-named-pattern seam here for the SAME fact kind would give the
// manifest TWO KindManifest entries sharing one FactKind constant, which
// go/internal/payloadusage's single-entry-per-FactKind lookups (e.g.
// TestLoadCoversWiredAzureKinds's byKind map) are not designed to
// disambiguate, and would add no real coverage since the field is already
// required and already tracked. This mirrors the established precedent in
// secrets_iam_trust_chain_anchor_decode.go, which decodes several fact kinds
// (e.g. facts.AWSIAMPrincipalFactKind, already seamed by
// go/internal/reducer/schemadecode/factschema_decode.go's decodeAWSIAMPrincipal) the same
// way, through factschema.Decode* directly, in a file intentionally outside
// the factschema_decode*.go glob.

// decodeAWSResourceForCloudInventory decodes one aws_resource envelope into
// the typed awsv1.Resource struct through the sdk/go/factschema seam,
// returning a self-classifying *postgresFactDecodeError when a required
// identity field (account_id, resource_id, region, resource_type) is
// missing, JSON null, or the wrong JSON type.
//
// PostgresCloudInventoryEvidenceLoader (cloud_inventory_evidence.go) is the
// caller: it routes account_id resolution through this seam instead of a
// tolerant raw map[string]any + coerceJSONString lookup (#5881 review
// follow-up to #5238), because account_id is a REQUIRED identity field on
// every aws_resource fact -- the collector emitter (awscloud.
// NewResourceEnvelope) validates it non-empty, and the #5238 rollout-gap
// signal (cloud_inventory_rollout_signal.go) depends on that being
// structurally true: coerceJSONString would otherwise silently accept a
// missing account_id as "" or a non-string JSON value (a number, bool,
// object) as its stringified form, masking a malformed fact as valid data
// instead of rejecting it.
func decodeAWSResourceForCloudInventory(env facts.Envelope) (awsv1.Resource, error) {
	resource, err := factschema.DecodeAWSResource(postgresFactschemaEnvelope(env))
	if err != nil {
		return awsv1.Resource{}, newPostgresFactDecodeError(factschema.FactKindAWSResource, err)
	}
	return resource, nil
}

// decodeAzureCloudResourceForCloudInventory decodes one azure_cloud_resource
// envelope into the typed azurev1.CloudResource struct through the
// sdk/go/factschema seam, returning a self-classifying
// *postgresFactDecodeError when a required identity field (arm_resource_id,
// resource_type, subscription_id, location) is missing, JSON null, or the
// wrong JSON type. See decodeAWSResourceForCloudInventory's doc comment for
// why this loader routes AWS and Azure identity resolution through the typed
// contract instead of a tolerant raw lookup; subscription_id is Azure's
// REQUIRED counterpart to AWS account_id, validated non-empty by the
// collector emitter (azurecloud.NewResourceEnvelope).
func decodeAzureCloudResourceForCloudInventory(env facts.Envelope) (azurev1.CloudResource, error) {
	resource, err := factschema.DecodeAzureCloudResource(postgresFactschemaEnvelope(env))
	if err != nil {
		return azurev1.CloudResource{}, newPostgresFactDecodeError(factschema.FactKindAzureCloudResource, err)
	}
	return resource, nil
}

// cloudInventoryResolveAccountID resolves one provider fact's raw account/
// project/subscription identity for cloudInventoryRecordFromRow
// (cloud_inventory_evidence.go). For AWS and Azure -- both REQUIRED fields in
// their typed factschema contract -- it decodes the full envelope through
// decodeAWSResourceForCloudInventory / decodeAzureCloudResourceForCloudInventory
// and rejects the row (ok=false) when that decode fails, rather than falling
// back to a raw lookup. GCP's project_id is genuinely OPTIONAL in its typed
// contract (an org/folder-level Cloud Asset Inventory asset has none), so it
// keeps the pre-existing raw map[string]any + coerceJSONString lookup;
// mapping.accountIDFallback (GCP only) still applies afterward for a blank
// value.
//
// The typed decode dispatches at schema major "1"
// (postgresDefaultSchemaMajorVersion) rather than reading
// fact_records.schema_version, which this loader's SQL does not select: both
// collectors emit only major 1 today (facts.AWSResourceSchemaVersion,
// facts.AzureCloudResourceSchemaVersion = "1.0.0"), and this loader already
// hardcodes v1 payload key names throughout (account_id, resource_id,
// region, resource_type, ...), so this adds no new versioning assumption. A
// future schema major bump for either fact kind needs this loader's SQL,
// this dispatch, and every other v1 key-name assumption here updated
// together.
func cloudInventoryResolveAccountID(
	factKind string,
	mapping cloudInventorySourceFactMapping,
	decoded map[string]any,
) (string, bool) {
	switch factKind {
	case facts.AWSResourceFactKind:
		resource, err := decodeAWSResourceForCloudInventory(facts.Envelope{
			FactKind:      factKind,
			SchemaVersion: postgresDefaultSchemaMajorVersion,
			Payload:       decoded,
		})
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(resource.AccountID), true
	case facts.AzureCloudResourceFactKind:
		resource, err := decodeAzureCloudResourceForCloudInventory(facts.Envelope{
			FactKind:      factKind,
			SchemaVersion: postgresDefaultSchemaMajorVersion,
			Payload:       decoded,
		})
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(resource.SubscriptionID), true
	default:
		return strings.TrimSpace(coerceJSONString(decoded[mapping.accountIDKey])), true
	}
}
