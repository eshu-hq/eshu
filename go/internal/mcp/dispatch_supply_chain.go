package mcp

import "strconv"

func containerImageIdentitiesRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/container-images/identities", query: map[string]string{
		"after_identity_id": str(args, "after_identity_id"),
		"digest":            str(args, "digest"),
		"image_ref":         str(args, "image_ref"),
		"limit":             strconv.Itoa(intOr(args, "limit", 50)),
		"outcome":           str(args, "outcome"),
		"repository_id":     str(args, "repository_id"),
	}}
}

func supplyChainImpactFindingsRoute(args map[string]any) *route {
	query := map[string]string{
		"after_finding_id":   str(args, "after_finding_id"),
		"cve_id":             str(args, "cve_id"),
		"impact_status":      str(args, "impact_status"),
		"limit":              strconv.Itoa(intOr(args, "limit", 50)),
		"min_priority_score": strconv.Itoa(intOr(args, "min_priority_score", 0)),
		"package_id":         str(args, "package_id"),
		"priority_bucket":    str(args, "priority_bucket"),
		"profile":            str(args, "profile"),
		"repository_id":      str(args, "repository_id"),
		"sort":               str(args, "sort"),
		"subject_digest":     str(args, "subject_digest"),
		"suppression_state":  str(args, "suppression_state"),
	}
	// include_suppressed is omitted when the caller did not set it so the API
	// applies its documented default (false). Sending an empty value would
	// otherwise be rejected by the parser.
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
		"source":             str(args, "source"),
	}}
}

func supplyChainImpactExplanationRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/supply-chain/impact/explain", query: map[string]string{
		"advisory_id":    str(args, "advisory_id"),
		"cve_id":         str(args, "cve_id"),
		"finding_id":     str(args, "finding_id"),
		"package_id":     str(args, "package_id"),
		"repository_id":  str(args, "repository_id"),
		"subject_digest": str(args, "subject_digest"),
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
		"document_digest":     str(args, "document_digest"),
		"document_id":         str(args, "document_id"),
		"limit":               strconv.Itoa(intOr(args, "limit", 50)),
		"subject_digest":      str(args, "subject_digest"),
	}}
}
