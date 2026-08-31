// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func vulnerabilityScannerReadContractRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/vulnerability-scanner/contract", query: map[string]string{
		"route": str(args, "route"),
	}}
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
