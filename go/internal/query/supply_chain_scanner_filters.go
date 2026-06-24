// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strings"
)

type scannerFilterSet map[string]struct{}

func impactFindingsScannerFilters() scannerFilterSet {
	return scannerFilterSet{
		"advisory_id": {}, "cve_id": {}, "ecosystem": {}, "environment": {},
		"ghsa_id": {}, "impact_status": {}, "osv_id": {}, "package_id": {},
		"image_ref": {}, "priority_bucket": {}, "profile": {}, "repository_id": {},
		"service_id": {}, "severity": {}, "subject_digest": {}, "workload_id": {},
	}
}

func impactExplanationScannerFilters() scannerFilterSet {
	return scannerFilterSet{
		"advisory_id": {}, "cve_id": {}, "finding_id": {}, "package_id": {},
		"image_ref": {}, "repository_id": {}, "service_id": {}, "subject_digest": {},
		"workload_id": {},
	}
}

func securityAlertScannerFilters() scannerFilterSet {
	return scannerFilterSet{
		"cve_id": {}, "ghsa_id": {}, "package_id": {}, "provider": {},
		"provider_state": {}, "reconciliation_status": {}, "repository_id": {},
	}
}

func rejectUnsupportedVulnerabilityScannerFilters(
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
		if QueryParam(r, key) == "" {
			continue
		}
		if _, ok := allowed[key]; ok {
			continue
		}
		WriteError(w, http.StatusBadRequest, fmt.Sprintf(
			"unsupported vulnerability scanner filter %q for this route; call /api/v0/supply-chain/vulnerability-scanner/contract for supported filters",
			key,
		))
		return false
	}
	return true
}

func firstNonEmptyQueryParam(r *http.Request, keys ...string) string {
	for _, key := range keys {
		if value := QueryParam(r, key); value != "" {
			return value
		}
	}
	return ""
}

func parseSupplyChainScannerSeverity(w http.ResponseWriter, r *http.Request) (string, bool) {
	severity := strings.ToLower(strings.TrimSpace(QueryParam(r, "severity")))
	if severity == "" {
		return "", true
	}
	switch severity {
	case "critical", "high", "medium", "low", "none":
		return severity, true
	default:
		WriteError(w, http.StatusBadRequest, "severity must be one of critical, high, medium, low, or none")
		return "", false
	}
}
