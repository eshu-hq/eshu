// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerQueueClaimQueryGatesS3ExternalPrincipalGrantOnCloudResourceReadiness(t *testing.T) {
	t.Parallel()

	if !queryHasBoundedReadinessRequirement(
		claimReducerWorkQuery,
		string(reducer.DomainS3ExternalPrincipalGrantMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
	) {
		t.Fatalf("claimReducerWorkQuery missing s3 external-principal readiness requirement")
	}
	if !queryHasBoundedReadinessRequirement(
		claimReducerWorkBatchQuery,
		string(reducer.DomainS3ExternalPrincipalGrantMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
	) {
		t.Fatalf("claimReducerWorkBatchQuery missing s3 external-principal readiness requirement")
	}
	if !strings.Contains(reducerConflictBlockageQuery, "s3_external_principal_grant_materialization") {
		t.Fatalf("reducerConflictBlockageQuery missing s3 external-principal readiness blockage domain")
	}
}
