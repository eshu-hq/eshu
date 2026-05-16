package mcp

import "strconv"

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
