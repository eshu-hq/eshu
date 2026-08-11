// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// The real-Postgres half of the #5861 Lambda-completeness proof.
//
// aws_cloud_runtime_drift_value_completeness_test.go proves
// lambdaImagePackagedWithoutImageURI's effect at the decoder+classifier
// boundary: an Image-packaged Lambda whose observed image_uri is
// unobservable decodes to Compared=0 and Classify returns
// value_comparison_inconclusive rather than converging. It stops there. A
// bug in candidate construction, the versioned upsert, the
// generation-authoritative retire, or the readback query could still hide or
// delete the finding while that unit test, and every other completeness
// test in this package, keeps passing. This decodes the SAME raw payload
// shapes through the SAME production decoders
// (awsRuntimeResourceRowFromPayload / awsRuntimeStateRowFromPayload) and then
// runs the result through the real transactional writer
// aws_cloud_runtime_drift_value_collapse_live_test.go uses for the AMI/EC2
// #5837 proof, so the whole
// decode -> classify -> BuildCandidates -> publish -> retire -> readback
// chain is exercised for the Lambda two-attribute case, not just the decode
// and classify steps.
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run AWSCloudRuntimeDriftLambdaCompleteness.*Live -count=1

// awsDriftLambdaCloudPayload renders the raw aws_resource payload JSON for
// one Lambda ARN, the same shape awsRuntimeResourceRowFromPayload decodes in
// production. An empty imageURI models the AWS client's ListFunctions
// fallback (GetFunction returning a nil output), which carries PackageType
// "Image" but no Code block.
func awsDriftLambdaCloudPayload(arn, imageURI, version string) []byte {
	return []byte(fmt.Sprintf(
		`{"arn":%q,"resource_id":%q,"resource_type":"aws_lambda_function",`+
			`"attributes":{"package_type":"Image","image_uri":%q,"version":%q}}`,
		arn, arn, imageURI, version,
	))
}

// awsDriftLambdaStatePayload renders the raw terraform_state_resource payload
// JSON for one Lambda ARN, the same shape awsRuntimeStateRowFromPayload
// decodes in production.
func awsDriftLambdaStatePayload(arn, imageURI, version string) []byte {
	return []byte(fmt.Sprintf(
		`{"address":"module.app.aws_lambda_function.app","type":"aws_lambda_function",`+
			`"attributes":{"arn":%q,"package_type":"Image","image_uri":%q,"version":%q}}`,
		arn, imageURI, version,
	))
}

// awsDriftLambdaCompletenessRow decodes the cloud and state payloads through
// the real production decoders and assembles the three-layer join
// BuildCandidates needs, exactly as the reducer handler does.
func awsDriftLambdaCompletenessRow(
	t *testing.T,
	arn, scopeID string,
	cloudPayload, statePayload []byte,
) cloudruntime.AddressedRow {
	t.Helper()

	cloud, ok := awsRuntimeResourceRowFromPayload(scopeID, cloudPayload)
	if !ok {
		t.Fatalf("awsRuntimeResourceRowFromPayload(cloud) ok = false, want true")
	}

	state, ok, failureClass := awsRuntimeStateRowFromPayload(
		scopeID, "module.app.aws_lambda_function.app", statePayload,
	)
	if !ok {
		t.Fatalf("awsRuntimeStateRowFromPayload(state) ok = false, failureClass = %q, want true", failureClass)
	}

	return cloudruntime.AddressedRow{
		ARN:          arn,
		ResourceType: "aws_lambda_function",
		Cloud:        cloud,
		State:        state,
		Config: &cloudruntime.ResourceRow{
			ARN:          arn,
			ResourceType: state.ResourceType,
			Address:      state.Address,
			ScopeID:      scopeID,
		},
	}
}

// awsDriftRunLambdaCompletenessPass classifies one row set and writes it
// through the real transactional writer -- the insert-admission check, the
// versioned upsert, and the generation-authoritative retire under one
// transaction (#5848) -- exactly as
// aws_cloud_runtime_drift_value_collapse_live_test.go's
// awsDriftRunValueCollapsePass does for the AMI/EC2 case.
func awsDriftRunLambdaCompletenessPass(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, generationID, arn string,
	row cloudruntime.AddressedRow,
	evidenceAsOf time.Time,
	fencingToken int64,
) int {
	t.Helper()

	candidates := cloudruntime.BuildCandidates([]cloudruntime.AddressedRow{row}, scopeID)
	for i := range candidates {
		candidates[i].State = "admitted"
	}
	writer := reducer.PostgresAWSCloudRuntimeDriftWriter{
		DB: AWSCloudRuntimeDriftAdmissionBeginner{Beginner: SQLDB{DB: db}},
	}
	result, err := writer.WriteAWSCloudRuntimeDriftFindings(ctx, reducer.AWSCloudRuntimeDriftWrite{
		IntentID:      "intent-" + generationID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		SourceSystem:  "aws",
		Cause:         "lambda completeness live proof",
		EvidenceAsOf:  evidenceAsOf,
		FencingToken:  fencingToken,
		Candidates:    candidates,
		EvaluatedARNs: []string{arn},
	})
	if err != nil {
		t.Fatalf("WriteAWSCloudRuntimeDriftFindings() error = %v, want nil", err)
	}
	return result.CanonicalWrites
}

// TestAWSCloudRuntimeDriftLambdaCompletenessReplacesRatherThanDeletesLive is
// the #5861 data-loss regression, proven against real rows through the real
// decoders.
//
// Pass 1 decodes a genuine image_uri mismatch (both sides readable) and
// writes image_version_drift. Pass 2 re-decodes the SAME (scope, generation)
// with the observed image_uri unobservable -- package_type "Image" but no
// image_uri, the shape the AWS client's ListFunctions fallback produces when
// GetFunction returns a nil output. Before lambdaImagePackagedWithoutImageURI
// existed, lambdaComparableScalarAttrSet still returned {"version": "7"} for
// that decode (image_uri alone was blank, version was not), so the pair
// reported Comparable=2, Compared=1 (version still agreed), Classify returned
// "" (convergence), BuildCandidates dropped the ARN, and the
// generation-authoritative retire deleted pass 1's still-true finding. It
// must now hold exactly one finding, and that finding must be the
// inconclusive one, not zero and not two.
func TestAWSCloudRuntimeDriftLambdaCompletenessReplacesRatherThanDeletesLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftAdmissionLiveDB(t)

	suffix := fmt.Sprintf("5861-lambda-%d", time.Now().UnixNano())
	scopeID := "scope-" + suffix
	generationID := "gen-" + suffix
	arn := "arn:aws:lambda:us-east-1:123456789012:function:app-" + suffix

	seededAt := time.Now().UTC()
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, scopeID, "aws", generationID, seededAt)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, generationID, scopeID, "active", seededAt)

	declaredImageURI := "123456789012.dkr.ecr.us-east-1.amazonaws.com/app:v2"

	pass1Writes := awsDriftRunLambdaCompletenessPass(
		t, ctx, sqlDB, scopeID, generationID, arn,
		awsDriftLambdaCompletenessRow(
			t, arn, scopeID,
			awsDriftLambdaCloudPayload(arn, "123456789012.dkr.ecr.us-east-1.amazonaws.com/app:v1", "7"),
			awsDriftLambdaStatePayload(arn, declaredImageURI, "7"),
		),
		time.Now().UTC(), 100,
	)
	if pass1Writes != 1 {
		t.Fatalf("pass 1 canonical writes = %d, want 1", pass1Writes)
	}
	if got := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, scopeID, generationID); len(got) != 1 ||
		got[0] != string(cloudruntime.FindingKindImageVersionDrift) {
		t.Fatalf("pass 1 durable finding kinds = %v, want [%s]", got, cloudruntime.FindingKindImageVersionDrift)
	}

	// Pass 2: the observed image_uri becomes unobservable. Version alone would
	// still agree (7 == 7), which is exactly the partial-comparison shape that
	// converged before the #5861 completeness rule existed.
	pass2Writes := awsDriftRunLambdaCompletenessPass(
		t, ctx, sqlDB, scopeID, generationID, arn,
		awsDriftLambdaCompletenessRow(
			t, arn, scopeID,
			awsDriftLambdaCloudPayload(arn, "", "7"),
			awsDriftLambdaStatePayload(arn, declaredImageURI, "7"),
		),
		time.Now().UTC().Add(time.Minute), 200,
	)
	if pass2Writes != 1 {
		t.Fatalf(
			"pass 2 canonical writes = %d, want 1: a comparison that reports no candidate empties the "+
				"keep-set and the retire then DELETES the still-true drift finding",
			pass2Writes,
		)
	}

	got := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, scopeID, generationID)
	if len(got) != 1 {
		t.Fatalf(
			"durable finding kinds after pass 2 = %v, want exactly one; zero is the data loss this closes and "+
				"two is the contradictory pair the retire exists to prevent",
			got,
		)
	}
	if got[0] != string(cloudruntime.FindingKindValueComparisonInconclusive) {
		t.Fatalf("durable finding kind after pass 2 = %q, want %q", got[0], cloudruntime.FindingKindValueComparisonInconclusive)
	}

	// Self-healing: once the observed image_uri is readable again and agrees
	// with declared, the ARN converges and the inconclusive row is retired in
	// its turn -- it must not become a permanent tombstone.
	pass3Writes := awsDriftRunLambdaCompletenessPass(
		t, ctx, sqlDB, scopeID, generationID, arn,
		awsDriftLambdaCompletenessRow(
			t, arn, scopeID,
			awsDriftLambdaCloudPayload(arn, declaredImageURI, "7"),
			awsDriftLambdaStatePayload(arn, declaredImageURI, "7"),
		),
		time.Now().UTC().Add(2*time.Minute), 300,
	)
	if pass3Writes != 0 {
		t.Fatalf("pass 3 canonical writes = %d, want 0 (convergence writes nothing)", pass3Writes)
	}
	if got := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, scopeID, generationID); len(got) != 0 {
		t.Fatalf(
			"durable finding kinds after recovery = %v, want none: the inconclusive row must not outlive the "+
				"condition that produced it",
			got,
		)
	}
}

// TestAWSCloudRuntimeDriftLambdaPartialComparabilityKeepsDriftLive is the
// accuracy half of #5861 against real rows.
//
// The test above proves the unreadable comparable cannot DELETE a finding. This
// one proves it does not DOWNGRADE one either. The observed image_uri is
// unobservable and `version` genuinely differs, so one comparison ran and found
// real drift. Under the all-or-nothing rule the unusable image_uri suppressed
// the readable comparison with it, so the durable row said
// value_comparison_inconclusive -- an operator querying image_version_drift saw
// nothing, on an ARN that was demonstrably drifted.
//
// Asserted here rather than only at the decoder because the finding KIND is what
// the versioned upsert keys the row on: a unit test proving Classify returns
// image_version_drift says nothing about which row survives the retire.
func TestAWSCloudRuntimeDriftLambdaPartialComparabilityKeepsDriftLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftAdmissionLiveDB(t)

	suffix := fmt.Sprintf("5861-partial-%d", time.Now().UnixNano())
	scopeID := "scope-" + suffix
	generationID := "gen-" + suffix
	arn := "arn:aws:lambda:us-east-1:123456789012:function:app-" + suffix

	seededAt := time.Now().UTC()
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, scopeID, "aws", generationID, seededAt)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, generationID, scopeID, "active", seededAt)

	declaredImageURI := "123456789012.dkr.ecr.us-east-1.amazonaws.com/app:v2"

	writes := awsDriftRunLambdaCompletenessPass(
		t, ctx, sqlDB, scopeID, generationID, arn,
		awsDriftLambdaCompletenessRow(
			t, arn, scopeID,
			// Observed: image_uri unobservable, version 7.
			awsDriftLambdaCloudPayload(arn, "", "7"),
			// Declared: version 9 -- a real, provable drift.
			awsDriftLambdaStatePayload(arn, declaredImageURI, "9"),
		),
		time.Now().UTC(), 100,
	)
	if writes != 1 {
		t.Fatalf("canonical writes = %d, want 1", writes)
	}

	got := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, scopeID, generationID)
	if len(got) != 1 || got[0] != string(cloudruntime.FindingKindImageVersionDrift) {
		t.Fatalf(
			"durable finding kinds = %v, want [%s]: a drift proven by a comparison that RAN must not be restated "+
				"as uncertainty because a different attribute was unreadable",
			got, cloudruntime.FindingKindImageVersionDrift,
		)
	}
}
