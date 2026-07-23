// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func vulnerabilityScannerReadContractRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/vulnerability-scanner/contract", query: map[string]string{
		"route": str(args, "route"),
	}}
}

func containerImageIdentitiesRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/container-images/identities", query: map[string]string{
		"after_identity_id":    str(args, "after_identity_id"),
		"digest":               str(args, "digest"),
		"image_ref":            str(args, "image_ref"),
		"limit":                strconv.Itoa(intOr(args, "limit", 50)),
		"outcome":              str(args, "outcome"),
		"repository_id":        str(args, "repository_id"),
		"source_repository_id": str(args, "source_repository_id"),
	}}
}

func containerImageTagHistoryRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/images/tag-history", query: map[string]string{
		"repository_id": str(args, "repository_id"),
		"tag":           str(args, "tag"),
		"limit":         strconv.Itoa(intOr(args, "limit", 50)),
		"offset":        strconv.Itoa(intOr(args, "offset", 0)),
	}}
}

func supplyChainImpactFindingsRoute(args map[string]any) *route {
	query := map[string]string{
		"advisory_id":        str(args, "advisory_id"),
		"after_finding_id":   str(args, "after_finding_id"),
		"cve_id":             str(args, "cve_id"),
		"ecosystem":          str(args, "ecosystem"),
		"environment":        str(args, "environment"),
		"ghsa_id":            str(args, "ghsa_id"),
		"image_ref":          str(args, "image_ref"),
		"impact_status":      str(args, "impact_status"),
		"limit":              strconv.Itoa(intOr(args, "limit", 50)),
		"min_priority_score": strconv.Itoa(intOr(args, "min_priority_score", 0)),
		"osv_id":             str(args, "osv_id"),
		"package_id":         str(args, "package_id"),
		"priority_bucket":    str(args, "priority_bucket"),
		"profile":            str(args, "profile"),
		"repository_id":      str(args, "repository_id"),
		"service_id":         str(args, "service_id"),
		"severity":           str(args, "severity"),
		"sort":               str(args, "sort"),
		"subject_digest":     str(args, "subject_digest"),
		"suppression_state":  str(args, "suppression_state"),
		"workload_id":        str(args, "workload_id"),
	}
	// include_suppressed is omitted when the caller did not set it so the
	// query string stays free of the empty key; the API parser accepts a
	// missing value as the documented default (false) and only rejects
	// non-true/false strings.
	if encoded := boolStr(args, "include_suppressed"); encoded != "" {
		query["include_suppressed"] = encoded
	}
	return &route{method: "GET", path: "/api/v0/supply-chain/impact/findings", query: query}
}

func advisoryEvidenceRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/advisories/evidence", query: map[string]string{
		"advisory_id":        str(args, "advisory_id"),
		"after_advisory_key": str(args, "after_advisory_key"),
		"cve_id":             str(args, "cve_id"),
		"limit":              strconv.Itoa(intOr(args, "limit", 50)),
		"package_id":         str(args, "package_id"),
		"repository_id":      str(args, "repository_id"),
		"service_id":         str(args, "service_id"),
		"source":             str(args, "source"),
		"workload_id":        str(args, "workload_id"),
	}}
}

func supplyChainImpactExplanationRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/impact/explain", query: map[string]string{
		"advisory_id":    str(args, "advisory_id"),
		"cve_id":         str(args, "cve_id"),
		"finding_id":     str(args, "finding_id"),
		"image_ref":      str(args, "image_ref"),
		"package_id":     str(args, "package_id"),
		"repository_id":  str(args, "repository_id"),
		"service_id":     str(args, "service_id"),
		"subject_digest": str(args, "subject_digest"),
		"workload_id":    str(args, "workload_id"),
	}}
}

func securityAlertReconciliationsRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/security-alerts/reconciliations", query: map[string]string{
		"after_reconciliation_id": str(args, "after_reconciliation_id"),
		"cve_id":                  str(args, "cve_id"),
		"ghsa_id":                 str(args, "ghsa_id"),
		"limit":                   strconv.Itoa(intOr(args, "limit", 50)),
		"package_id":              str(args, "package_id"),
		"provider":                str(args, "provider"),
		"provider_state":          str(args, "provider_state"),
		"reconciliation_status":   str(args, "reconciliation_status"),
		"repository_id":           str(args, "repository_id"),
	}}
}

func sbomAttestationAttachmentsRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/sbom-attestations/attachments", query: map[string]string{
		"after_attachment_id": str(args, "after_attachment_id"),
		"artifact_kind":       str(args, "artifact_kind"),
		"attachment_status":   str(args, "attachment_status"),
		"digest":              str(args, "digest"),
		"document_digest":     str(args, "document_digest"),
		"document_id":         str(args, "document_id"),
		"limit":               strconv.Itoa(intOr(args, "limit", 50)),
		"repository_id":       str(args, "repository_id"),
		"service_id":          str(args, "service_id"),
		"subject_digest":      str(args, "subject_digest"),
		"workload_id":         str(args, "workload_id"),
	}}
}
