// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Asset type constants for the compute TargetHttpsProxy typed-depth
// extractor's edge targets. assetTypeComputeTargetHTTPSProxy itself is
// declared in extractor_forwarding_rule.go (the ForwardingRule extractor
// resolves a `target` reference naming a targetHttpsProxies segment into that
// same asset type) and reused here; this file is the other side of that edge
// — the TargetHttpsProxy resource's own typed depth. assetTypeComputeUrlMap
// is declared in extractor_url_map.go and assetTypeComputeSSLCertificate in
// extractor_ssl_certificate.go (the SslCertificate resource's own typed
// depth); both are reused here as the resolved edge targets rather than
// redeclared.
const (
	assetTypeComputeSSLPolicy = "compute.googleapis.com/SslPolicy"

	// Certificate Manager asset types. A TargetHttpsProxy can serve TLS from
	// Certificate Manager instead of classic Compute SslCertificate resources,
	// either through the `certificateMap` reference or by listing a Certificate
	// Manager certificate self-link inside `sslCertificates`.
	assetTypeCertificateManagerCertificateMap = "certificatemanager.googleapis.com/CertificateMap"
	assetTypeCertificateManagerCertificate    = "certificatemanager.googleapis.com/Certificate"
)

// certificateManagerFullNamePrefix is the CAI full-resource-name prefix for
// Certificate Manager assets. A `certificateMap` or a Certificate Manager
// entry in `sslCertificates` already arrives in this absolute form, or as a
// project/location-qualified relative path this prefix is prepended to.
const certificateManagerFullNamePrefix = "//certificatemanager.googleapis.com/"

// Bounded provider relationship types for TargetHttpsProxy edges. Each is a
// stable string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly.
const (
	relationshipTypeTargetHTTPSProxyUsesURLMap                 = "target_https_proxy_uses_url_map"
	relationshipTypeTargetHTTPSProxyUsesSSLCertificate         = "target_https_proxy_uses_ssl_certificate"
	relationshipTypeTargetHTTPSProxyUsesSSLPolicy              = "target_https_proxy_uses_ssl_policy"
	relationshipTypeTargetHTTPSProxyUsesCertificateMap         = "target_https_proxy_uses_certificate_map"
	relationshipTypeTargetHTTPSProxyUsesCertManagerCertificate = "target_https_proxy_uses_certificate_manager_certificate"
)

// init registers extractTargetHTTPSProxy for the compute
// TargetHttpsProxy CAI asset type. Unlike BackendService or ForwardingRule,
// Cloud Asset Inventory reports only one asset type for a Target HTTPS
// Proxy (no separate regional/global split), so a single registration is
// sufficient.
func init() {
	RegisterAssetExtractor(assetTypeComputeTargetHTTPSProxy, extractTargetHTTPSProxy)
}

// targetHTTPSProxyData is the bounded view of a CAI
// compute.googleapis.com/TargetHttpsProxy resource.data blob. Every field
// here is a control-plane reference or posture value; no certificate key
// material, private key, or response body is ever decoded — sslCertificates,
// certificateMap, and sslPolicy carry only resource self-links, never
// certificate content.
//
// A TargetHttpsProxy sources its serving certificate from exactly one place:
// the Compute API ignores `sslCertificates` when `certificateMap` is set (see
// the API reference cited on issue #4307). The extractor honors that
// precedence so it never emits an edge to a certificate GCP is not serving.
type targetHTTPSProxyData struct {
	URLMap            string   `json:"urlMap"`
	SSLCertificates   []string `json:"sslCertificates"`
	CertificateMap    string   `json:"certificateMap"`
	SSLPolicy         string   `json:"sslPolicy"`
	QuicOverride      string   `json:"quicOverride"`
	CreationTimestamp string   `json:"creationTimestamp"`
}

// extractTargetHTTPSProxy extracts bounded, redaction-safe typed depth for
// one compute TargetHttpsProxy CAI asset (the GCP HTTPS load-balancer target
// proxy resource). It returns the Terraform/drift/monitoring attribute set
// (QUIC negotiation override and creation time), cross-source correlation
// anchors for the resolvable URL map, serving certificate(s), and SSL policy,
// and the typed edges to those resources.
//
// TLS certificate precedence: when `certificateMap` is set, the Compute API
// ignores `sslCertificates`, so this extractor emits only the CertificateMap
// edge/anchor and suppresses the classic sslCertificates edges — otherwise it
// would surface a stale relationship to a certificate GCP is not serving. When
// no certificateMap is set, each `sslCertificates` entry is routed by domain:
// a Compute self-link resolves to a compute SslCertificate, while a
// Certificate Manager self-link resolves to a certificatemanager Certificate.
//
// The reverse edge from a ForwardingRule to this proxy is already emitted by
// extractForwardingRule (relationshipTypeForwardingRuleTargetsTargetProxy,
// see extractor_forwarding_rule.go): CAI's TargetHttpsProxy resource.data
// carries no back-reference to the forwarding rule that targets it — the
// reference is one-directional (ForwardingRule.target names the proxy, never
// the reverse) — so this extractor emits no forwarding-rule edge of its own;
// the issue's "forwarding rule" edge requirement is satisfied once this
// asset type resolves as a real typed node for the existing ForwardingRule
// edge to land on.
func extractTargetHTTPSProxy(ctx ExtractContext) (AttributeExtraction, error) {
	var data targetHTTPSProxyData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode target https proxy data: %w", err)
	}

	attrs := targetHTTPSProxyAttributes(data)

	var anchors []string
	var rels []RelationshipObservation

	if urlMapName := computeResourceFullNameFromSelfLink(data.URLMap, "urlMaps", ctx.ProjectID); urlMapName != "" {
		anchors = append(anchors, urlMapName)
		rels = append(rels, targetHTTPSProxyEdge(ctx, relationshipTypeTargetHTTPSProxyUsesURLMap, urlMapName, assetTypeComputeUrlMap))
	}

	// Serving-certificate precedence: certificateMap wins over sslCertificates.
	if mapName := certificateManagerFullName(data.CertificateMap, "certificateMaps"); mapName != "" {
		anchors = append(anchors, mapName)
		rels = append(rels, targetHTTPSProxyEdge(ctx, relationshipTypeTargetHTTPSProxyUsesCertificateMap, mapName, assetTypeCertificateManagerCertificateMap))
	} else {
		for _, cert := range data.SSLCertificates {
			certName, certType := resolveTargetHTTPSProxyCertificate(cert, ctx.ProjectID)
			if certName == "" {
				continue
			}
			anchors = append(anchors, certName)
			rels = append(rels, targetHTTPSProxyEdge(ctx, targetHTTPSProxyCertificateRelationship(certType), certName, certType))
		}
	}

	if policyName := computeResourceFullNameFromSelfLink(data.SSLPolicy, "sslPolicies", ctx.ProjectID); policyName != "" {
		anchors = append(anchors, policyName)
		rels = append(rels, targetHTTPSProxyEdge(ctx, relationshipTypeTargetHTTPSProxyUsesSSLPolicy, policyName, assetTypeComputeSSLPolicy))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      dedupeTargetHTTPSProxyRelationships(rels),
	}, nil
}

// targetHTTPSProxyAttributes assembles the bounded attribute map. Empty or
// absent fields are omitted rather than written as zero values so a partial
// CAI page does not fabricate a posture. urlMap, sslCertificates,
// certificateMap, and sslPolicy are not duplicated into the attribute map
// since they already surface as typed edges and correlation anchors.
func targetHTTPSProxyAttributes(data targetHTTPSProxyData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.QuicOverride); v != "" {
		attrs["quic_override"] = v
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}
	return attrs
}

// resolveTargetHTTPSProxyCertificate routes a single `sslCertificates` entry
// to its CAI full resource name and asset type. A Certificate Manager
// self-link (`//certificatemanager.googleapis.com/.../certificates/...` or the
// project/location-qualified relative form) resolves to a certificatemanager
// Certificate; any other value is treated as a classic Compute SslCertificate
// self-link. An empty name is returned for an unresolvable reference so the
// caller drops it.
func resolveTargetHTTPSProxyCertificate(ref, sourceProjectID string) (fullName, assetType string) {
	if cmName := certificateManagerFullName(ref, "certificates"); cmName != "" {
		return cmName, assetTypeCertificateManagerCertificate
	}
	if computeName := computeResourceFullNameFromSelfLink(ref, "sslCertificates", sourceProjectID); computeName != "" {
		return computeName, assetTypeComputeSSLCertificate
	}
	return "", ""
}

// targetHTTPSProxyCertificateRelationship maps a resolved certificate asset
// type to its edge relationship type.
func targetHTTPSProxyCertificateRelationship(certType string) string {
	if certType == assetTypeCertificateManagerCertificate {
		return relationshipTypeTargetHTTPSProxyUsesCertManagerCertificate
	}
	return relationshipTypeTargetHTTPSProxyUsesSSLCertificate
}

// certificateManagerFullName resolves a Certificate Manager reference into its
// CAI full resource name only when the path names the given resource segment
// (`certificateMaps` or `certificates`). An already-absolute
// `//certificatemanager.googleapis.com/...` reference is returned unchanged
// when it carries that prefix; a project/location-qualified relative path is
// prefixed with the Certificate Manager domain. A wrong-domain, blank, or
// wrong-segment reference yields "" so a compute self-link is never
// misclassified as a Certificate Manager asset.
func certificateManagerFullName(ref, segment string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	var full string
	switch {
	case strings.HasPrefix(trimmed, certificateManagerFullNamePrefix):
		full = trimmed
	case strings.HasPrefix(trimmed, "//"):
		// Absolute reference to a different service — not Certificate Manager.
		return ""
	case strings.HasPrefix(strings.TrimPrefix(trimmed, "/"), "projects/"):
		full = certificateManagerFullNamePrefix + strings.TrimPrefix(trimmed, "/")
	default:
		return ""
	}
	if !strings.Contains(full, "/"+segment+"/") {
		return ""
	}
	return full
}

// dedupeTargetHTTPSProxyRelationships drops duplicate relationship
// observations keyed on (RelationshipType, TargetFullResourceName,
// TargetAssetType), mirroring the URL Map extractor's relationship dedupe.
// A malformed or hand-edited sslCertificates list could repeat the same
// self-link; without this dedup each repetition would emit a separate
// gcp_cloud_relationship fact for the same logical edge.
func dedupeTargetHTTPSProxyRelationships(rels []RelationshipObservation) []RelationshipObservation {
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

// targetHTTPSProxyEdge builds a supported typed relationship observation
// rooted at the target HTTPS proxy.
func targetHTTPSProxyEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
