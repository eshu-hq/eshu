// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type scannerFilterSet map[string]struct{}

// FindingsScannerFilters is the set of query-param filter names the findings
// route accepts, used to reject an unsupported filter.
func FindingsScannerFilters() scannerFilterSet {
	return scannerFilterSet{
		"advisory_id": {}, "cve_id": {}, "ecosystem": {}, "environment": {},
		"ghsa_id": {}, "impact_status": {}, "osv_id": {}, "package_id": {},
		"image_ref": {}, "priority_bucket": {}, "profile": {}, "repository_id": {},
		"service_id": {}, "severity": {}, "subject_digest": {}, "workload_id": {},
	}
}

// ExplanationScannerFilters is the set of query-param filter names the
// explain route accepts, used to reject an unsupported filter.
func ExplanationScannerFilters() scannerFilterSet {
	return scannerFilterSet{
		"advisory_id": {}, "cve_id": {}, "finding_id": {}, "package_id": {},
		"image_ref": {}, "repository_id": {}, "service_id": {}, "subject_digest": {},
		"workload_id": {},
	}
}

// SecurityAlertScannerFilters is the set of query-param filter names the
// security-alert route accepts, used to reject an unsupported filter.
func SecurityAlertScannerFilters() scannerFilterSet {
	return scannerFilterSet{
		"cve_id": {}, "ghsa_id": {}, "package_id": {}, "provider": {},
		"provider_state": {}, "reconciliation_status": {}, "repository_id": {},
	}
}

// RejectUnsupportedVulnerabilityScannerFilters checks every recognized
// vulnerability-scanner query parameter present on r against allowed. If any
// present parameter is not in allowed, it writes a 400 naming the
// unsupported filter and returns false; otherwise it returns true and writes
// nothing.
func RejectUnsupportedVulnerabilityScannerFilters(
	w http.ResponseWriter,
	r *http.Request,
	allowed scannerFilterSet,
) bool {
	for _, key := range []string{
		"advisory_id", "cve_id", "ecosystem", "environment", "ghsa_id",
		"image_ref", "impact_status", "language", "osv_id", "package_id", "provider_state",
		"provider", "readiness", "reconciliation_status", "repository_id",
		"service_id", "severity", "status", "subject_digest", "workload_id",
	} {
		if querycontract.QueryParam(r, key) == "" {
			continue
		}
		if _, ok := allowed[key]; ok {
			continue
		}
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf(
			"unsupported vulnerability scanner filter %q for this route; call /api/v0/supply-chain/vulnerability-scanner/contract for supported filters",
			key,
		))
		return false
	}
	return true
}

// FirstNonEmptyQueryParam returns the value of the first key in keys that has
// a non-empty query parameter on r, or "" when none do.
func FirstNonEmptyQueryParam(r *http.Request, keys ...string) string {
	for _, key := range keys {
		if value := querycontract.QueryParam(r, key); value != "" {
			return value
		}
	}
	return ""
}

// ParseScannerSeverity parses the optional severity query parameter from r,
// lower-cased and trimmed. It returns ("", true) when the caller omits the
// parameter, (severity, true) for one of the closed severity values, and
// writes a 400 response and returns (_, false) for any other value.
func ParseScannerSeverity(w http.ResponseWriter, r *http.Request) (string, bool) {
	severity := strings.ToLower(strings.TrimSpace(querycontract.QueryParam(r, "severity")))
	if severity == "" {
		return "", true
	}
	switch severity {
	case "critical", "high", "medium", "low", "none":
		return severity, true
	default:
		querycontract.WriteError(w, http.StatusBadRequest, "severity must be one of critical, high, medium, low, or none")
		return "", false
	}
}
