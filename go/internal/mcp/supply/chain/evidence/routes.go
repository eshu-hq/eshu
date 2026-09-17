// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychainevidencetools

import (
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// Route selects the internal HTTP request for a supply-chain evidence tool
// without executing it. It reports handled only for the five tools this
// package owns: the vulnerability-scanner read contract, the advisory-evidence
// listing, and the SBOM/attestation attachment listing, count, and grouped
// inventory.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "get_vulnerability_scanner_read_contract":
		return vulnerabilityScannerReadContractRequest(args), true
	case "list_advisory_evidence":
		return advisoryEvidenceRequest(args), true
	case "list_sbom_attestation_attachments":
		return sbomAttestationAttachmentsRequest(args), true
	case "count_sbom_attestation_attachments":
		return sbomAttestationAttachmentAggregateCountRequest(args), true
	case "get_sbom_attestation_attachment_inventory":
		return sbomAttestationAttachmentAggregateInventoryRequest(args), true
	default:
		return routecontract.Request{}, false
	}
}

// vulnerabilityScannerReadContractRequest maps
// get_vulnerability_scanner_read_contract to the bounded scanner-contract
// route. route names which documented contract section the caller wants; the
// HTTP handler validates it and the route builder forwards it unchecked.
func vulnerabilityScannerReadContractRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/vulnerability-scanner/contract", Query: map[string]string{
		"route": args.String("route"),
	}}
}

// advisoryEvidenceRequest maps list_advisory_evidence to its bounded,
// keyset-paged route. limit defaults to 50; the HTTP handler enforces its own
// bound and derives the advisory anchor from reducer-owned impact findings.
func advisoryEvidenceRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/advisories/evidence", Query: map[string]string{
		"advisory_id":        args.String("advisory_id"),
		"after_advisory_key": args.String("after_advisory_key"),
		"cve_id":             args.String("cve_id"),
		"limit":              strconv.Itoa(args.IntOr("limit", 50)),
		"package_id":         args.String("package_id"),
		"repository_id":      args.String("repository_id"),
		"service_id":         args.String("service_id"),
		"source":             args.String("source"),
		"workload_id":        args.String("workload_id"),
	}}
}

// sbomAttestationAttachmentsRequest maps list_sbom_attestation_attachments to
// its bounded, keyset-paged route. limit defaults to 50.
func sbomAttestationAttachmentsRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/sbom-attestations/attachments", Query: map[string]string{
		"after_attachment_id": args.String("after_attachment_id"),
		"artifact_kind":       args.String("artifact_kind"),
		"attachment_status":   args.String("attachment_status"),
		"digest":              args.String("digest"),
		"document_digest":     args.String("document_digest"),
		"document_id":         args.String("document_id"),
		"limit":               strconv.Itoa(args.IntOr("limit", 50)),
		"repository_id":       args.String("repository_id"),
		"service_id":          args.String("service_id"),
		"subject_digest":      args.String("subject_digest"),
		"workload_id":         args.String("workload_id"),
	}}
}

// sbomAttestationAttachmentAggregateCountRequest maps
// count_sbom_attestation_attachments to its bounded, scope-filtered count
// route. It carries the same filter keys as the listing, minus paging: a
// count has no page to size and nothing to seek past.
func sbomAttestationAttachmentAggregateCountRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/sbom-attestations/attachments/count", Query: map[string]string{
		"subject_digest":    args.String("subject_digest"),
		"document_id":       args.String("document_id"),
		"document_digest":   args.String("document_digest"),
		"attachment_status": args.String("attachment_status"),
		"artifact_kind":     args.String("artifact_kind"),
		"repository_id":     args.String("repository_id"),
		"workload_id":       args.String("workload_id"),
		"service_id":        args.String("service_id"),
	}}
}

// sbomAttestationAttachmentAggregateInventoryRequest maps
// get_sbom_attestation_attachment_inventory to its bounded, offset-paged
// grouped-inventory route. group_by defaults to attachment_status; limit
// defaults to 100 and offset to 0.
func sbomAttestationAttachmentAggregateInventoryRequest(args routecontract.Arguments) routecontract.Request {
	groupBy := args.String("group_by")
	if groupBy == "" {
		groupBy = "attachment_status"
	}
	return routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/sbom-attestations/attachments/inventory", Query: map[string]string{
		"group_by":          groupBy,
		"subject_digest":    args.String("subject_digest"),
		"document_id":       args.String("document_id"),
		"document_digest":   args.String("document_digest"),
		"attachment_status": args.String("attachment_status"),
		"artifact_kind":     args.String("artifact_kind"),
		"repository_id":     args.String("repository_id"),
		"workload_id":       args.String("workload_id"),
		"service_id":        args.String("service_id"),
		"limit":             strconv.Itoa(args.IntOr("limit", 100)),
		"offset":            strconv.Itoa(args.IntOr("offset", 0)),
	}}
}
