// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "strings"

// gcpProjectIDFromFullResourceName derives a GCP project id from a Cloud
// Asset Inventory full resource name by finding a "projects/<id>" segment,
// mirroring go/internal/collector/gcpcloud.ProjectIDFromFullName (a different
// package this loader does not import, to keep the storage layer independent
// of collector internals; the two are intentionally kept as small, easily
// diffed, independently tested duplicates of the same parsing rule -- see
// TestGCPProjectIDFromFullResourceNameMatchesCollectorDerivation). It returns
// "" when no "projects/" segment is present, which is the correct answer for
// an organization- or folder-level asset that genuinely has no project.
//
// This exists because gcp_cloud_resource's project_id is, unlike AWS
// account_id and Azure subscription_id, NOT a required identity field:
// sdk/go/factschema/gcp/v1/resource.go declares ProjectID as
// `*string` `json:"project_id,omitempty"` and documents it as "always emitted
// by the collector but may be empty for a resource whose full resource name
// carries no derivable project segment" -- an organization- or folder-level
// Cloud Asset Inventory asset. cloudInventorySourceFactMapping.accountIDFallback
// uses this function to recover the identifier for the DERIVABLE subset of
// that gap (any project-scoped asset, which is the common case); it does not
// and cannot close the true org/folder case, which has no project to derive
// (#5238).
func gcpProjectIDFromFullResourceName(fullResourceName string) string {
	segments := strings.Split(strings.TrimSpace(fullResourceName), "/")
	for i := 0; i < len(segments)-1; i++ {
		if segments[i] == "projects" {
			return segments[i+1]
		}
	}
	return ""
}
