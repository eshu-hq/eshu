// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func sbomAttestationAttachmentAggregateCountRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/sbom-attestations/attachments/count", query: map[string]string{
		"subject_digest":    str(args, "subject_digest"),
		"document_id":       str(args, "document_id"),
		"document_digest":   str(args, "document_digest"),
		"attachment_status": str(args, "attachment_status"),
		"artifact_kind":     str(args, "artifact_kind"),
		"repository_id":     str(args, "repository_id"),
		"workload_id":       str(args, "workload_id"),
		"service_id":        str(args, "service_id"),
	}}
}

func sbomAttestationAttachmentAggregateInventoryRoute(args map[string]any) *route {
	groupBy := str(args, "group_by")
	if groupBy == "" {
		groupBy = "attachment_status"
	}
	return &route{method: "GET", path: "/api/v0/supply-chain/sbom-attestations/attachments/inventory", query: map[string]string{
		"group_by":          groupBy,
		"subject_digest":    str(args, "subject_digest"),
		"document_id":       str(args, "document_id"),
		"document_digest":   str(args, "document_digest"),
		"attachment_status": str(args, "attachment_status"),
		"artifact_kind":     str(args, "artifact_kind"),
		"repository_id":     str(args, "repository_id"),
		"workload_id":       str(args, "workload_id"),
		"service_id":        str(args, "service_id"),
		"limit":             strconv.Itoa(intOr(args, "limit", 100)),
		"offset":            strconv.Itoa(intOr(args, "offset", 0)),
	}}
}
