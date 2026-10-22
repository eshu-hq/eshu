// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud //nolint:dirgate // #6696 wave-1: flat extractor moves to extractor/<group> in a later slice

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
)

// Family constants for this extractor now live in catalog/gcp_security.go (#6696 HK slice); uses below name catalog.X.

func init() {
	RegisterAssetExtractor(catalog.WorkloadIdentityPoolProviderAssetType, extractWorkloadIdentityPoolProvider)
}

// workloadIdentityPoolProviderData is the bounded view of a CAI
// iam.googleapis.com/WorkloadIdentityPoolProvider resource.data blob. Only
// redaction-safe control-plane trust metadata is decoded: the external trust
// target (AWS account id, OIDC issuer URI) and posture. Key/credential material
// is deliberately never a field — the OIDC inline JWKS (oidc.jwksJson) and the
// SAML IdP metadata XML (saml.idpMetadataXml) are never decoded so they cannot be
// surfaced. attributeMapping values and the attributeCondition CEL expression can
// reference asserted claim names/values, so only the mapping key count and a
// condition-presence flag are kept, never the expressions themselves.
type workloadIdentityPoolProviderData struct {
	State              string            `json:"state"`
	Disabled           *bool             `json:"disabled"`
	AttributeMapping   map[string]string `json:"attributeMapping"`
	AttributeCondition string            `json:"attributeCondition"`
	AWS                *struct {
		AccountID string `json:"accountId"`
	} `json:"aws"`
	OIDC *struct {
		IssuerURI        string   `json:"issuerUri"`
		AllowedAudiences []string `json:"allowedAudiences"`
	} `json:"oidc"`
	SAML *struct {
		// DisplayName only; idpMetadataXml is intentionally omitted so the
		// certificate/metadata blob can never be surfaced.
		DisplayName string `json:"displayName"`
	} `json:"saml"`
	// X509 presence only; the trustStore (trustAnchors / intermediateCas PEM
	// certificate material) is intentionally omitted so no certificate blob can
	// be surfaced.
	X509 *struct{} `json:"x509"`
}

// extractWorkloadIdentityPoolProvider extracts bounded, redaction-safe typed depth
// for one CAI IAM Workload Identity Pool Provider asset. It surfaces the external
// trust type (aws/oidc/saml) and its bounded trust anchor — the AWS account id or
// OIDC issuer URI (cross-cloud/OIDC correlation join keys) — the attribute-mapping
// key count, an attribute-condition presence flag, and the lifecycle/disabled
// posture; and emits the typed provider -> pool edge to the parent
// WorkloadIdentityPool with the trust anchor as the correlation anchor. OIDC JWKS
// key material and SAML IdP metadata are never read; attribute-mapping values and
// the attribute-condition expression are never persisted.
func extractWorkloadIdentityPoolProvider(ctx ExtractContext) (AttributeExtraction, error) {
	var data workloadIdentityPoolProviderData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode workload identity pool provider data: %w", err)
	}

	attrs := map[string]any{}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if data.Disabled != nil {
		attrs["disabled"] = *data.Disabled
	}

	var anchors []string
	switch {
	case data.AWS != nil:
		attrs["provider_type"] = "aws"
		if id := strings.TrimSpace(data.AWS.AccountID); id != "" {
			attrs["aws_account_id"] = id
			anchors = append(anchors, id)
		}
	case data.OIDC != nil:
		attrs["provider_type"] = "oidc"
		if uri := strings.TrimSpace(data.OIDC.IssuerURI); uri != "" {
			attrs["oidc_issuer_uri"] = uri
			anchors = append(anchors, uri)
		}
		if n := len(data.OIDC.AllowedAudiences); n > 0 {
			attrs["oidc_allowed_audience_count"] = n
		}
	case data.SAML != nil:
		attrs["provider_type"] = "saml"
	case data.X509 != nil:
		attrs["provider_type"] = "x509"
	}

	// Report the effective attribute-mapping key count. IAM applies a default
	// two-key mapping to an AWS provider that declares none, so a bare AWS
	// provider still has a real mapping posture rather than an empty one.
	mappingCount := len(data.AttributeMapping)
	if mappingCount == 0 && data.AWS != nil {
		mappingCount = catalog.AwsDefaultAttributeMappingKeyCount
	}
	if mappingCount > 0 {
		attrs["attribute_mapping_key_count"] = mappingCount
	}
	if strings.TrimSpace(data.AttributeCondition) != "" {
		attrs["has_attribute_condition"] = true
	}

	var rels []RelationshipObservation
	if pool := parentWorkloadIdentityPoolFullName(ctx.FullResourceName); pool != "" {
		rels = append(rels, RelationshipObservation{
			SourceFullResourceName: ctx.FullResourceName,
			SourceAssetType:        ctx.AssetType,
			RelationshipType:       catalog.RelationshipTypeWIFProviderOfPool,
			TargetFullResourceName: pool,
			TargetAssetType:        catalog.WorkloadIdentityPoolAssetType,
			SupportState:           RelationshipSupportSupported,
		})
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: anchors,
		Relationships:      rels,
	}, nil
}

// parentWorkloadIdentityPoolFullName derives the parent WorkloadIdentityPool full
// resource name from a provider full resource name by trimming the
// `/providers/<id>` suffix. It returns "" when the input carries no provider
// suffix, or when the marker has no id after it, so the caller emits no
// provider -> pool edge from a name that does not identify a provider.
func parentWorkloadIdentityPoolFullName(providerFullName string) string {
	trimmed := strings.TrimSpace(providerFullName)
	index := strings.LastIndex(trimmed, catalog.WifProvidersMarker)
	if index < 0 || index+len(catalog.WifProvidersMarker) >= len(trimmed) {
		return ""
	}
	return trimmed[:index]
}
