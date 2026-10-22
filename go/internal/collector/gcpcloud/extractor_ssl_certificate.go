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

func init() {
	RegisterAssetExtractor(catalog.AssetTypeComputeSSLCertificate, extractSslCertificate)
}

// sslCertificateData is the bounded view of a CAI
// compute.googleapis.com/SslCertificate resource.data blob. The `certificate`
// and `privateKey` fields under `selfManaged` (raw PEM certificate body and
// private key material) are intentionally NOT decoded here: per the GCP
// collector contract Payload Boundaries, no secret or key material is ever
// read. `managed.domains` and `subjectAlternativeNames` are DNS-name-shaped
// values; the typed-depth extractor seam carries no redaction key (see
// AttributeExtraction/ExtractContext), so — mirroring
// extractor_dns_managed_zone.go's treatment of a zone's own dnsName and
// extractor_recaptcha_key.go's treatment of allowed-domain entries — they are
// never persisted raw and are reduced to bounded counts only.
type sslCertificateData struct {
	Type    string `json:"type"`
	Managed *struct {
		Domains []string `json:"domains"`
		Status  string   `json:"status"`
	} `json:"managed"`
	SubjectAlternativeNames []string `json:"subjectAlternativeNames"`
	ExpireTime              string   `json:"expireTime"`
	CreationTimestamp       string   `json:"creationTimestamp"`
}

// extractSslCertificate extracts bounded, redaction-safe typed depth for one
// compute SslCertificate CAI asset. It returns the Terraform/drift/monitoring
// attribute set (managed vs. self-managed type, the managed-certificate
// provisioning status, a bounded managed-domain count, a bounded
// subject-alternative-name count for a self-managed certificate, expiry time,
// and creation time).
//
// The certificate's graph edges are inbound: a Target HTTPS Proxy or Target
// SSL Proxy references this certificate through its own `sslCertificates[]`
// field and resolves the edge from that side (see catalog.AssetTypeComputeSSLCertificate's
// doc comment), the same inbound-edge shape as extractor_iam_role.go's IAM
// Role. This extractor therefore derives no outbound relationships or
// anchors. The raw PEM certificate body, private key, and every domain-name
// value (managed domains and subject alternative names) never reach the
// output — only their bounded counts do.
func extractSslCertificate(ctx ExtractContext) (AttributeExtraction, error) {
	var data sslCertificateData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode ssl certificate data: %w", err)
	}

	attrs := sslCertificateAttributes(data)

	return AttributeExtraction{Attributes: attrs}, nil
}

// sslCertificateAttributes assembles the bounded attribute map. Empty or
// absent fields are omitted rather than written as zero values so a partial
// CAI page does not fabricate a posture, with one deliberate exception: an
// absent `type` is derived to SELF_MANAGED, the value the Compute
// sslCertificates schema defines for an omitted type, so a self-managed
// certificate created without an explicit type still carries usable
// Terraform/monitoring truth instead of silently dropping the field.
// managed_status and domain_count are populated only when the certificate
// carries a `managed` block (a self-managed certificate has none); san_count
// is populated only when subjectAlternativeNames is present (only a
// self-managed certificate reports it, and only after issuance).
func sslCertificateAttributes(data sslCertificateData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Type); v != "" {
		attrs["type"] = v
	} else {
		attrs["type"] = catalog.SslCertificateTypeSelfManaged
	}
	if data.Managed != nil {
		if v := strings.TrimSpace(data.Managed.Status); v != "" {
			attrs["managed_status"] = v
		}
		if n := len(data.Managed.Domains); n > 0 {
			attrs["domain_count"] = n
		}
	}
	if n := len(data.SubjectAlternativeNames); n > 0 {
		attrs["san_count"] = n
	}
	if v, ok := normalizeRFC3339(data.ExpireTime); ok {
		attrs["expire_time"] = v
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}
	return attrs
}
