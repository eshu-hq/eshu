// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud //nolint:dirgate // #6696 wave-1: flat extractor moves to extractor/<group> in a later slice

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
)

// Family constants for this extractor now live in catalog/gcp_compute_core.go (#6696 HK slice); uses below name catalog.X.

// init registers extractTargetHTTPSProxy for the compute
// TargetHttpsProxy CAI asset type. Unlike BackendService or ForwardingRule,
// Cloud Asset Inventory reports only one asset type for a Target HTTPS
// Proxy (no separate regional/global split), so a single registration is
// sufficient.
func init() {
	RegisterAssetExtractor(catalog.AssetTypeComputeTargetHTTPSProxy, extractTargetHTTPSProxy)
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
// extractForwardingRule (catalog.RelationshipTypeForwardingRuleTargetsTargetProxy,
// see catalog/gcp_compute_lb.go): CAI's TargetHttpsProxy resource.data
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
		rels = append(rels, targetHTTPSProxyEdge(ctx, catalog.RelationshipTypeTargetHTTPSProxyUsesURLMap, urlMapName, catalog.AssetTypeComputeUrlMap))
	}

	// Serving-certificate precedence: certificateMap wins over sslCertificates.
	if mapName := certificateManagerFullName(data.CertificateMap, "certificateMaps"); mapName != "" {
		anchors = append(anchors, mapName)
		rels = append(rels, targetHTTPSProxyEdge(ctx, catalog.RelationshipTypeTargetHTTPSProxyUsesCertificateMap, mapName, catalog.AssetTypeCertificateManagerCertificateMap))
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
		rels = append(rels, targetHTTPSProxyEdge(ctx, catalog.RelationshipTypeTargetHTTPSProxyUsesSSLPolicy, policyName, catalog.AssetTypeComputeSSLPolicy))
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
		return cmName, catalog.AssetTypeCertificateManagerCertificate
	}
	if computeName := computeResourceFullNameFromSelfLink(ref, "sslCertificates", sourceProjectID); computeName != "" {
		return computeName, catalog.AssetTypeComputeSSLCertificate
	}
	return "", ""
}

// targetHTTPSProxyCertificateRelationship maps a resolved certificate asset
// type to its edge relationship type.
func targetHTTPSProxyCertificateRelationship(certType string) string {
	if certType == catalog.AssetTypeCertificateManagerCertificate {
		return catalog.RelationshipTypeTargetHTTPSProxyUsesCertManagerCertificate
	}
	return catalog.RelationshipTypeTargetHTTPSProxyUsesSSLCertificate
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
	case strings.HasPrefix(trimmed, catalog.CertificateManagerFullNamePrefix):
		full = trimmed
	case strings.HasPrefix(trimmed, "//"):
		// Absolute reference to a different service — not Certificate Manager.
		return ""
	case strings.HasPrefix(strings.TrimPrefix(trimmed, "/"), "projects/"):
		full = catalog.CertificateManagerFullNamePrefix + strings.TrimPrefix(trimmed, "/")
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
