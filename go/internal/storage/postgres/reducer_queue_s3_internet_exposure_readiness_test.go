package postgres

import (
	"strings"
	"testing"
)

func TestReducerQueueClaimQueryGatesS3InternetExposureOnCloudResourceReadiness(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"s3_internet_exposure_materialization",
		"aws_nodes.keyspace = 'cloud_resource_uid'",
		"aws_nodes.phase = 'canonical_nodes_committed'",
	} {
		if !strings.Contains(claimReducerWorkQuery, want) {
			t.Fatalf("claim query missing S3 internet-exposure readiness token %q:\n%s", want, claimReducerWorkQuery)
		}
	}
}

func TestReducerConflictBlockageReportsS3InternetExposureReadiness(t *testing.T) {
	t.Parallel()

	if !strings.Contains(reducerConflictBlockageQuery, "s3_internet_exposure_materialization") {
		t.Fatalf("blockage query missing s3 internet-exposure readiness domain:\n%s", reducerConflictBlockageQuery)
	}
}
