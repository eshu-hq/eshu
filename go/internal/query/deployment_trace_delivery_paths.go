// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
)

// This file held the deployment delivery-path builders. They moved to
// internal/query/impacttrace with lane B2 of #6060, which is their primary
// reader; the two helpers below keep forwarding wrappers here for the
// repository-story deployment-evidence reader.

// deploymentEvidenceDeliveryPaths shapes deployment evidence into delivery
// path rows. The implementation moved to impacttrace for #6060; this wrapper
// keeps root callers unchanged.
func deploymentEvidenceDeliveryPaths(deploymentEvidence map[string]any) []map[string]any {
	return impacttrace.DeploymentEvidenceDeliveryPaths(deploymentEvidence)
}

// normalizedDeliveryPathKey keys a delivery path row for dedupe. The
// implementation moved to impacttrace for #6060; this wrapper keeps root
// callers unchanged.
func normalizedDeliveryPathKey(entry map[string]any) string {
	return impacttrace.NormalizedDeliveryPathKey(entry)
}
