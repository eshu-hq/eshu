// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

const (
	// SpanReducerS3ExternalPrincipalGrantMaterialization wraps S3 external
	// principal grant projection: fact load, source-bucket readiness, bounded
	// principal identity extraction, scoped retract, and ExternalPrincipal plus
	// GRANTS_ACCESS_TO graph writes.
	SpanReducerS3ExternalPrincipalGrantMaterialization = "reducer.s3_external_principal_grant_materialization"
)
