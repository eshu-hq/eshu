package mcp

import "strconv"

func supplyChainImpactFindingsRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/impact/findings", query: map[string]string{
		"after_finding_id": str(args, "after_finding_id"),
		"cve_id":           str(args, "cve_id"),
		"impact_status":    str(args, "impact_status"),
		"limit":            strconv.Itoa(intOr(args, "limit", 50)),
		"package_id":       str(args, "package_id"),
		"repository_id":    str(args, "repository_id"),
		"subject_digest":   str(args, "subject_digest"),
	}}
}

func sbomAttestationAttachmentsRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/sbom-attestations/attachments", query: map[string]string{
		"after_attachment_id": str(args, "after_attachment_id"),
		"artifact_kind":       str(args, "artifact_kind"),
		"attachment_status":   str(args, "attachment_status"),
		"document_digest":     str(args, "document_digest"),
		"document_id":         str(args, "document_id"),
		"limit":               strconv.Itoa(intOr(args, "limit", 50)),
		"subject_digest":      str(args, "subject_digest"),
	}}
}
