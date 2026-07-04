// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeCertificateManagerDNSAuthorization and
// assetTypeCertificateManagerCertificateIssuanceConfig are the CAI asset types
// for the two Certificate Manager resources this Certificate extractor
// resolves outbound edges toward. Neither resource has its own typed-depth
// extractor yet, so these constants are declared here — the first extractor
// to reference them — for a future DNSAuthorization or
// CertificateIssuanceConfig extractor to reuse, mirroring how
// extractor_forwarding_rule.go declares its proxy-kind asset type constants
// for reuse by their own eventual typed-depth extractors.
// assetTypeCertificateManagerCertificate and certificateManagerFullName are
// declared in extractor_target_https_proxy.go (that extractor resolves a
// TargetHttpsProxy's serving-certificate reference into this same asset type)
// and reused here for the Certificate resource's own typed depth.
const (
	assetTypeCertificateManagerDNSAuthorization          = "certificatemanager.googleapis.com/DnsAuthorization"
	assetTypeCertificateManagerCertificateIssuanceConfig = "certificatemanager.googleapis.com/CertificateIssuanceConfig"
)

// Certificate Manager Certificate `type` classification values. Certificate
// Manager's Certificate resource reports exactly one of `managed`,
// `selfManaged`, or `managedIdentity` populated (never a `type` field the way
// Compute's classic SslCertificate does), so the extractor derives this
// classification from which block is present rather than reading a field.
const (
	certManagerCertificateTypeManaged         = "MANAGED"
	certManagerCertificateTypeSelfManaged     = "SELF_MANAGED"
	certManagerCertificateTypeManagedIdentity = "MANAGED_IDENTITY"
)

// Bounded provider relationship types for Certificate Manager Certificate
// edges. Each is a stable string carried on a gcp_cloud_relationship fact;
// the reducer materializes an edge only when both endpoints resolve exactly.
const (
	relationshipTypeCertManagerCertificateUsesDNSAuthorization = "certificate_manager_certificate_uses_dns_authorization"
	relationshipTypeCertManagerCertificateUsesIssuanceConfig   = "certificate_manager_certificate_uses_issuance_config"
)

func init() {
	RegisterAssetExtractor(assetTypeCertificateManagerCertificate, extractCertificateManagerCertificate)
}

// certManagerCertificateData is the bounded view of a CAI
// certificatemanager.googleapis.com/Certificate resource.data blob. Every
// certificate/key-material-shaped field is intentionally NOT decoded here:
// per the GCP collector contract Payload Boundaries, no certificate body or
// private key is ever read — `pemCertificate` (the top-level provisioned
// chain) and `selfManaged.pemCertificate`/`selfManaged.pemPrivateKey` (the
// uploaded chain and key) are omitted from this struct entirely so a stray
// value in the blob cannot be surfaced. `managed.domains`, `sanDnsnames`, and
// `managedIdentity.identity` (a SPIFFE ID naming a workload/namespace) are
// DNS-name- or identity-shaped values; the typed-depth extractor seam carries
// no redaction key (see AttributeExtraction/ExtractContext), so — mirroring
// extractor_ssl_certificate.go's treatment of managed.domains and
// subjectAlternativeNames — they are never persisted raw and are reduced to
// bounded counts or presence only. `managed.provisioningIssue` and
// `managed.authorizationAttemptInfo` carry free-text failure detail and
// domain names respectively and are never decoded.
type certManagerCertificateData struct {
	Scope   string `json:"scope"`
	Managed *struct {
		Domains           []string `json:"domains"`
		DNSAuthorizations []string `json:"dnsAuthorizations"`
		IssuanceConfig    string   `json:"issuanceConfig"`
		State             string   `json:"state"`
	} `json:"managed"`
	ManagedIdentity *struct {
		State string `json:"state"`
	} `json:"managedIdentity"`
	SANDNSNames []string          `json:"sanDnsnames"`
	Labels      map[string]string `json:"labels"`
	ExpireTime  string            `json:"expireTime"`
	CreateTime  string            `json:"createTime"`
	UpdateTime  string            `json:"updateTime"`
}

// extractCertificateManagerCertificate extracts bounded, redaction-safe typed
// depth for one certificatemanager.googleapis.com/Certificate CAI asset. It
// returns the Terraform/drift/monitoring attribute set (certificate
// classification, scope, managed-provisioning state, a bounded managed-domain
// count, a bounded DNS-authorization count, a bounded subject-alternative-name
// count, a bounded label count, expiry/create/update time); the resolved
// dnsAuthorizations and issuanceConfig references as correlation anchors; and
// the typed certificate_manager_certificate_uses_dns_authorization /
// certificate_manager_certificate_uses_issuance_config edges. The certificate
// body, private key, every domain-name value, and the managed-identity SPIFFE
// ID never reach the output — only bounded counts and resolvable resource
// references do.
//
// The certificate's other graph value — a Target HTTPS Proxy or a Certificate
// Manager CertificateMap referencing this certificate — is an inbound edge
// already emitted from the referencing side (see
// extractor_target_https_proxy.go's certificateMap/sslCertificates
// resolution), the same inbound-edge shape as the classic SslCertificate
// extractor, so this extractor derives no edge for `usedBy`.
func extractCertificateManagerCertificate(ctx ExtractContext) (AttributeExtraction, error) {
	var data certManagerCertificateData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode certificate manager certificate data: %w", err)
	}

	attrs := certManagerCertificateAttributes(data)

	var anchors []string
	var rels []RelationshipObservation

	if data.Managed != nil {
		for _, ref := range data.Managed.DNSAuthorizations {
			name := certificateManagerFullName(ref, "dnsAuthorizations")
			if name == "" {
				continue
			}
			anchors = append(anchors, name)
			rels = append(rels, certManagerCertificateEdge(ctx, relationshipTypeCertManagerCertificateUsesDNSAuthorization, name, assetTypeCertificateManagerDNSAuthorization))
		}
		if name := certificateManagerFullName(data.Managed.IssuanceConfig, "certificateIssuanceConfigs"); name != "" {
			anchors = append(anchors, name)
			rels = append(rels, certManagerCertificateEdge(ctx, relationshipTypeCertManagerCertificateUsesIssuanceConfig, name, assetTypeCertificateManagerCertificateIssuanceConfig))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      dedupeCertManagerCertificateRelationships(rels),
	}, nil
}

// certManagerCertificateAttributes assembles the bounded attribute map. Empty
// or absent fields are omitted rather than written as zero values so a
// partial CAI page does not fabricate a posture, with one deliberate
// exception: `type` is always populated by deriving it from which of
// `managed`/`selfManaged`/`managedIdentity` is present, defaulting to
// SELF_MANAGED when none is set — the same default the Certificate Manager
// API applies to an uploaded certificate with no management block — so a
// certificate never carries an unclassified posture. managed_state,
// managed_domain_count, and dns_authorization_count are populated only when
// the certificate carries a `managed` block; managed_state also covers the
// managedIdentity case, since both share the same state enum and neither
// self-managed certificate reports a lifecycle state at all.
func certManagerCertificateAttributes(data certManagerCertificateData) map[string]any {
	attrs := map[string]any{}

	switch {
	case data.Managed != nil:
		attrs["type"] = certManagerCertificateTypeManaged
	case data.ManagedIdentity != nil:
		attrs["type"] = certManagerCertificateTypeManagedIdentity
	default:
		attrs["type"] = certManagerCertificateTypeSelfManaged
	}

	if v := strings.TrimSpace(data.Scope); v != "" {
		attrs["scope"] = v
	}

	if data.Managed != nil {
		if v := strings.TrimSpace(data.Managed.State); v != "" {
			attrs["managed_state"] = v
		}
		if n := len(data.Managed.Domains); n > 0 {
			attrs["managed_domain_count"] = n
		}
		if n := len(data.Managed.DNSAuthorizations); n > 0 {
			attrs["dns_authorization_count"] = n
		}
	} else if data.ManagedIdentity != nil {
		if v := strings.TrimSpace(data.ManagedIdentity.State); v != "" {
			attrs["managed_state"] = v
		}
	}

	if n := len(data.SANDNSNames); n > 0 {
		attrs["san_count"] = n
	}
	if n := len(data.Labels); n > 0 {
		attrs["label_count"] = n
	}
	if v, ok := normalizeRFC3339(data.ExpireTime); ok {
		attrs["expire_time"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["create_time"] = v
	}
	if v, ok := normalizeRFC3339(data.UpdateTime); ok {
		attrs["update_time"] = v
	}
	return attrs
}

// dedupeCertManagerCertificateRelationships drops duplicate relationship
// observations keyed on (RelationshipType, TargetFullResourceName,
// TargetAssetType), mirroring the Target HTTPS Proxy extractor's relationship
// dedupe. A malformed or hand-edited dnsAuthorizations list could repeat the
// same reference; without this dedup each repetition would emit a separate
// gcp_cloud_relationship fact for the same logical edge.
func dedupeCertManagerCertificateRelationships(rels []RelationshipObservation) []RelationshipObservation {
	if len(rels) == 0 {
		return rels
	}
	seen := make(map[string]struct{}, len(rels))
	deduped := make([]RelationshipObservation, 0, len(rels))
	for _, rel := range rels {
		key := rel.RelationshipType + "|" + rel.TargetFullResourceName + "|" + rel.TargetAssetType
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, rel)
	}
	return deduped
}

// certManagerCertificateEdge builds a supported typed relationship
// observation rooted at the certificate.
func certManagerCertificateEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
