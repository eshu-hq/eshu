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

// The real-Postgres half of the #5837 value-axis proof.
//
// The reducer-package acceptance test
// (TestAWSCloudRuntimeDriftValueCollapseKeepsADurableFinding) proves the
// candidate set and the retire's keep-set argument through the real handler
// against a fake execer. It cannot prove what the database is left holding,
// which is the thing that actually matters: whether the degraded pass REPLACED
// the drift finding or DELETED it. This runs both passes against real rows and
// counts what survives.
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run AWSCloudRuntimeDriftValueCollapse.*Live -count=1

// awsDriftValueCollapseRow builds the three-layer join for one aws_instance ARN,
// the same shape the evidence loader produces. An empty stateAMI models the
// terraform-state collector's fail-closed redaction: the state row exists and is
// config-backed, it simply carries no comparable value.
func awsDriftValueCollapseRow(arn, scopeID, cloudAMI, stateAMI string) cloudruntime.AddressedRow {
	state := &cloudruntime.ResourceRow{
		ARN:          arn,
		ResourceType: "aws_instance",
		Address:      "aws_instance.web",
		ScopeID:      scopeID,
	}
	if stateAMI != "" {
		state.Attributes = map[string]string{"ami": stateAMI}
	}
	return cloudruntime.AddressedRow{
		ARN:          arn,
		ResourceType: "aws_instance",
		Cloud: &cloudruntime.ResourceRow{
			ARN:          arn,
			ResourceType: "aws_ec2_instance",
			ScopeID:      scopeID,
			Attributes:   map[string]string{"ami": cloudAMI},
		},
		State: state,
		Config: &cloudruntime.ResourceRow{
			ARN:          arn,
			ResourceType: "aws_instance",
			Address:      "aws_instance.web",
			ScopeID:      scopeID,
		},
	}
}

// awsDriftRunValueCollapsePass classifies one row set and writes it through the
// real transactional writer, exactly as the handler does: the insert-admission
// check, the versioned upsert, and the generation-authoritative retire under
// one transaction (#5848).
//
// fencingToken must increase across passes for the same (scope, generation) --
// production issues it from the shared Postgres sequence, and a pass carrying a
// token below one already admitted is correctly rejected as superseded, which
// would make a later pass here look like a classifier bug rather than the
// admission check doing its job.
//
// EvaluatedARNs carries the ARN even on the pass that admits no candidate: the
// retire's blast radius is the evaluated-ARN allowlist, so an empty list makes
// it a no-op and the convergence half of this proof could never fail.
func awsDriftRunValueCollapsePass(
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
		Cause:         "value-axis collapse live proof",
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

// TestAWSCloudRuntimeDriftValueCollapseReplacesRatherThanDeletesLive is the
// #5837 data-loss regression, proven against real rows.
//
// Pass 1 sees a genuine AMI mismatch and writes image_version_drift. Pass 2
// re-reads the SAME (scope, generation) with the declared value redacted, which
// is reachable with nothing upstream asserting a failure. Before the
// value_comparison_inconclusive kind existed, pass 2 produced no candidate, the
// keep-set emptied, and the generation-authoritative retire deleted pass 1's
// still-true finding, leaving the ARN with NOTHING. It must now hold exactly one
// finding, and that finding must be the inconclusive one.
func TestAWSCloudRuntimeDriftValueCollapseReplacesRatherThanDeletesLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftAdmissionLiveDB(t)

	suffix := fmt.Sprintf("5837-value-%d", time.Now().UnixNano())
	scopeID := "scope-" + suffix
	generationID := "gen-" + suffix
	arn := "arn:aws:ec2:us-east-1:123456789012:instance/i-0" + suffix

	seededAt := time.Now().UTC()
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, scopeID, "aws", generationID, seededAt)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, generationID, scopeID, "active", seededAt)

	pass1Writes := awsDriftRunValueCollapsePass(
		t, ctx, sqlDB, scopeID, generationID, arn,
		awsDriftValueCollapseRow(arn, scopeID, "ami-observed", "ami-declared"),
		time.Now().UTC(), 100,
	)
	if pass1Writes != 1 {
		t.Fatalf("pass 1 canonical writes = %d, want 1", pass1Writes)
	}
	if got := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, scopeID, generationID); len(got) != 1 ||
		got[0] != string(cloudruntime.FindingKindImageVersionDrift) {
		t.Fatalf("pass 1 durable finding kinds = %v, want [%s]", got, cloudruntime.FindingKindImageVersionDrift)
	}

	pass2Writes := awsDriftRunValueCollapsePass(
		t, ctx, sqlDB, scopeID, generationID, arn,
		awsDriftValueCollapseRow(arn, scopeID, "ami-observed", ""),
		time.Now().UTC().Add(time.Minute), 200,
	)
	if pass2Writes != 1 {
		t.Fatalf(
			"pass 2 canonical writes = %d, want 1: a degraded comparison that writes nothing empties the "+
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

	// Self-healing: once the declared value is readable again the ARN returns to
	// a real verdict, and the inconclusive row is retired in its turn.
	pass3Writes := awsDriftRunValueCollapsePass(
		t, ctx, sqlDB, scopeID, generationID, arn,
		awsDriftValueCollapseRow(arn, scopeID, "ami-observed", "ami-declared"),
		time.Now().UTC().Add(2*time.Minute), 300,
	)
	if pass3Writes != 1 {
		t.Fatalf("pass 3 canonical writes = %d, want 1", pass3Writes)
	}
	if got := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, scopeID, generationID); len(got) != 1 ||
		got[0] != string(cloudruntime.FindingKindImageVersionDrift) {
		t.Fatalf(
			"durable finding kinds after recovery = %v, want [%s]: the inconclusive row must not outlive the "+
				"condition that produced it",
			got, cloudruntime.FindingKindImageVersionDrift,
		)
	}
}

// TestAWSCloudRuntimeDriftConvergedARNIsStillRetiredLive keeps the other
// direction honest against real rows: a genuine convergence must still clear the
// generation, or value_comparison_inconclusive would have turned the retire into
// a no-op and left stale findings live forever.
func TestAWSCloudRuntimeDriftConvergedARNIsStillRetiredLive(t *testing.T) {
	sqlDB, ctx := awsCloudRuntimeDriftAdmissionLiveDB(t)

	suffix := fmt.Sprintf("5837-converge-%d", time.Now().UnixNano())
	scopeID := "scope-" + suffix
	generationID := "gen-" + suffix
	arn := "arn:aws:ec2:us-east-1:123456789012:instance/i-0" + suffix

	seededAt := time.Now().UTC()
	seedAWSCloudRuntimeDriftScope(t, ctx, sqlDB, scopeID, "aws", generationID, seededAt)
	seedAWSCloudRuntimeDriftGeneration(t, ctx, sqlDB, generationID, scopeID, "active", seededAt)

	awsDriftRunValueCollapsePass(
		t, ctx, sqlDB, scopeID, generationID, arn,
		awsDriftValueCollapseRow(arn, scopeID, "ami-observed", "ami-declared"),
		time.Now().UTC(), 100,
	)
	if got := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, scopeID, generationID); len(got) != 1 {
		t.Fatalf("pass 1 durable finding kinds = %v, want exactly one", got)
	}

	// Terraform is applied: the declared and observed AMIs now agree.
	converged := awsDriftRunValueCollapsePass(
		t, ctx, sqlDB, scopeID, generationID, arn,
		awsDriftValueCollapseRow(arn, scopeID, "ami-same", "ami-same"),
		time.Now().UTC().Add(time.Minute), 200,
	)
	if converged != 0 {
		t.Fatalf("converged pass canonical writes = %d, want 0", converged)
	}
	if got := countAWSCloudRuntimeDriftFindingRows(t, ctx, sqlDB, scopeID, generationID); len(got) != 0 {
		t.Fatalf(
			"durable finding kinds after convergence = %v, want none: a resolved ARN must not keep a stale "+
				"finding, and must not be given an inconclusive one either",
			got,
		)
	}
}
