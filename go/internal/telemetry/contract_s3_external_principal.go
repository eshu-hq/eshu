// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

const (
	// SpanReducerS3ExternalPrincipalGrantMaterialization wraps S3 external
	// principal grant projection: fact load, source-bucket readiness, bounded
	// principal identity extraction, scoped retract, and ExternalPrincipal plus
	// GRANTS_ACCESS_TO graph writes.
	SpanReducerS3ExternalPrincipalGrantMaterialization = "reducer.s3_external_principal_grant_materialization"
)

func init() {
	for idx, name := range spanNames {
		if name == SpanReducerS3LogsToMaterialization {
			spanNames = slices.Insert(spanNames, idx+1, SpanReducerS3ExternalPrincipalGrantMaterialization)
			return
		}
	}
	spanNames = append(spanNames, SpanReducerS3ExternalPrincipalGrantMaterialization)
}
