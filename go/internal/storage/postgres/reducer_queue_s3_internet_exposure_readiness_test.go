// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerQueueClaimQueryGatesS3InternetExposureOnCloudResourceReadiness(t *testing.T) {
	t.Parallel()

	if !queryHasBoundedReadinessRequirement(
		claimReducerWorkQuery,
		string(reducer.DomainS3InternetExposureMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
	) {
		t.Fatalf("claim query missing S3 internet-exposure readiness requirement:\n%s", claimReducerWorkQuery)
	}
}

func TestReducerConflictBlockageReportsS3InternetExposureReadiness(t *testing.T) {
	t.Parallel()

	if !strings.Contains(reducerConflictBlockageQuery, "s3_internet_exposure_materialization") {
		t.Fatalf("blockage query missing s3 internet-exposure readiness domain:\n%s", reducerConflictBlockageQuery)
	}
}
