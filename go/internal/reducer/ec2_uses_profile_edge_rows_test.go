// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ec2UsesProfileResourceEnvelope builds an aws_resource fact envelope for one
// scanned IAM instance profile, matching the shape the IAM scanner emits:
// resource_type aws_iam_instance_profile, an
// arn:aws:iam::<acct>:instance-profile/<name> ARN, and resource_id equal to that
// ARN. The instance-profile node uid the aws_resource materialization commits is
// keyed by that ARN, so the USES_PROFILE join resolves instance_profile_arn
// against it.
func ec2UsesProfileResourceEnvelope(account, region, name string) facts.Envelope {
	arn := "arn:aws:iam::" + account + ":instance-profile/" + name
	return facts.Envelope{
		FactKind: facts.AWSResourceFactKind,
		Payload: map[string]any{
			"account_id":          account,
			"region":              region,
			"resource_type":       "aws_iam_instance_profile",
			"resource_id":         arn,
			"arn":                 arn,
			"name":                name,
			"correlation_anchors": []string{arn, name},
		},
	}
}

// ec2UsesProfilePostureEnvelope builds an ec2_instance_posture fact envelope for
// one instance with the given instance_profile_arn (blank means the instance has
// no profile, which must produce no edge and no skip-error).
func ec2UsesProfilePostureEnvelope(account, region, instanceID, profileARN string) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.EC2InstancePostureFactKind,
		FactID:   "fact-" + instanceID,
		Payload: map[string]any{
			"account_id":           account,
			"region":               region,
			"resource_type":        "aws_ec2_instance",
			"arn":                  "arn:aws:ec2:" + region + ":" + account + ":instance/" + instanceID,
			"instance_id":          instanceID,
			"state":                "running",
			"instance_profile_arn": profileARN,
		},
	}
}

// ec2InstanceUID recomputes the EC2 instance CloudResource node uid the same way
// PR-A's ec2_instance_node_materialization does, so a test can assert the edge
// source_uid points at the node PR-A committed.
func ec2InstanceUID(account, region, instanceID string) string {
	return cloudResourceUID(account, region, "aws_ec2_instance", instanceID)
}

// ec2InstanceProfileUID recomputes the IAM instance-profile CloudResource node
// uid the same way aws_resource_materialization does (resource_id == ARN for
// instance profiles), so a test can assert the edge target_uid points at the
// scanned profile node.
func ec2InstanceProfileUID(account, region, name string) string {
	arn := "arn:aws:iam::" + account + ":instance-profile/" + name
	return cloudResourceUID(account, region, "aws_iam_instance_profile", arn)
}

func TestExtractEC2UsesProfileEdgeRowsResolvesScannedProfile(t *testing.T) {
	t.Parallel()

	const acct = "111122223333"
	const region = "us-east-1"
	const profileRegion = "aws-global"
	resources := []facts.Envelope{
		ec2UsesProfileResourceEnvelope(acct, profileRegion, "app"),
	}
	postures := []facts.Envelope{
		ec2UsesProfilePostureEnvelope(acct, region, "i-aaa",
			"arn:aws:iam::"+acct+":instance-profile/app"),
	}

	rows, tally, err := ExtractEC2UsesProfileEdgeRows(resources, postures)
	if err != nil {
		t.Fatalf("ExtractEC2UsesProfileEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	wantSource := ec2InstanceUID(acct, region, "i-aaa")
	wantTarget := ec2InstanceProfileUID(acct, profileRegion, "app")
	if got := anyToString(rows[0]["source_uid"]); got != wantSource {
		t.Fatalf("source_uid = %q, want %q", got, wantSource)
	}
	if got := anyToString(rows[0]["target_uid"]); got != wantTarget {
		t.Fatalf("target_uid = %q, want %q", got, wantTarget)
	}
	if got := anyToString(rows[0]["relationship_type"]); got != "USES_PROFILE" {
		t.Fatalf("relationship_type = %q, want USES_PROFILE", got)
	}
	if got := anyToString(rows[0]["resolution_mode"]); got != ec2UsesProfileModeARN {
		t.Fatalf("resolution_mode = %q, want %q", got, ec2UsesProfileModeARN)
	}
	if tally.totalSkipped() != 0 {
		t.Fatalf("totalSkipped = %d, want 0", tally.totalSkipped())
	}
	if tally.resolved[ec2UsesProfileModeARN] != 1 {
		t.Fatalf("resolved[arn] = %d, want 1", tally.resolved[ec2UsesProfileModeARN])
	}
}

func TestExtractEC2UsesProfileEdgeRowsBlankProfileNoEdge(t *testing.T) {
	t.Parallel()

	const acct = "111122223333"
	const region = "us-east-1"
	resources := []facts.Envelope{
		ec2UsesProfileResourceEnvelope(acct, "aws-global", "app"),
	}
	postures := []facts.Envelope{
		// No instance profile: a bare instance, not a lost edge.
		ec2UsesProfilePostureEnvelope(acct, region, "i-noprofile", ""),
	}

	rows, tally, err := ExtractEC2UsesProfileEdgeRows(resources, postures)
	if err != nil {
		t.Fatalf("ExtractEC2UsesProfileEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 for blank instance_profile_arn", len(rows))
	}
	if tally.totalSkipped() != 0 {
		t.Fatalf("totalSkipped = %d, want 0 (blank profile is not a skip-error)", tally.totalSkipped())
	}
}

func TestExtractEC2UsesProfileEdgeRowsUnscannedProfileSkipped(t *testing.T) {
	t.Parallel()

	const acct = "111122223333"
	const region = "us-east-1"
	// The profile referenced by the instance is NOT in the scanned resource set
	// (cross-account / out-of-scope). No dangling node, no fabrication.
	postures := []facts.Envelope{
		ec2UsesProfilePostureEnvelope(acct, region, "i-aaa",
			"arn:aws:iam::999988887777:instance-profile/external"),
	}

	rows, tally, err := ExtractEC2UsesProfileEdgeRows(nil, postures)
	if err != nil {
		t.Fatalf("ExtractEC2UsesProfileEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 for unscanned target", len(rows))
	}
	if tally.skipped[ec2UsesProfileSkipTargetUnresolved] != 1 {
		t.Fatalf("skipped[target_unresolved] = %d, want 1", tally.skipped[ec2UsesProfileSkipTargetUnresolved])
	}
	if tally.totalSkipped() != 1 {
		t.Fatalf("totalSkipped = %d, want 1", tally.totalSkipped())
	}
}

func TestExtractEC2UsesProfileEdgeRowsTwoInstancesSameProfile(t *testing.T) {
	t.Parallel()

	const acct = "111122223333"
	const region = "us-east-1"
	const profileRegion = "aws-global"
	profileARN := "arn:aws:iam::" + acct + ":instance-profile/app"
	resources := []facts.Envelope{
		ec2UsesProfileResourceEnvelope(acct, profileRegion, "app"),
	}
	postures := []facts.Envelope{
		ec2UsesProfilePostureEnvelope(acct, region, "i-aaa", profileARN),
		ec2UsesProfilePostureEnvelope(acct, region, "i-bbb", profileARN),
	}

	rows, _, err := ExtractEC2UsesProfileEdgeRows(resources, postures)
	if err != nil {
		t.Fatalf("ExtractEC2UsesProfileEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 distinct edges (no merge/cartesian)", len(rows))
	}
	sources := map[string]struct{}{}
	for _, row := range rows {
		sources[anyToString(row["source_uid"])] = struct{}{}
		if got := anyToString(row["target_uid"]); got != ec2InstanceProfileUID(acct, profileRegion, "app") {
			t.Fatalf("target_uid = %q, want the shared profile uid", got)
		}
	}
	if len(sources) != 2 {
		t.Fatalf("distinct source uids = %d, want 2 (one per instance)", len(sources))
	}
}

func TestExtractEC2UsesProfileEdgeRowsDuplicateInputOneEdge(t *testing.T) {
	t.Parallel()

	const acct = "111122223333"
	const region = "us-east-1"
	const profileRegion = "aws-global"
	profileARN := "arn:aws:iam::" + acct + ":instance-profile/app"
	resources := []facts.Envelope{
		ec2UsesProfileResourceEnvelope(acct, profileRegion, "app"),
		ec2UsesProfileResourceEnvelope(acct, profileRegion, "app"), // duplicate scan
	}
	postures := []facts.Envelope{
		ec2UsesProfilePostureEnvelope(acct, region, "i-aaa", profileARN),
		ec2UsesProfilePostureEnvelope(acct, region, "i-aaa", profileARN), // duplicate fact
	}

	rows, _, err := ExtractEC2UsesProfileEdgeRows(resources, postures)
	if err != nil {
		t.Fatalf("ExtractEC2UsesProfileEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (idempotent dedup by (source,target))", len(rows))
	}
}

func TestExtractEC2UsesProfileEdgeRowsEmptyInputNoPanic(t *testing.T) {
	t.Parallel()

	rows, tally, err := ExtractEC2UsesProfileEdgeRows(nil, nil)
	if err != nil {
		t.Fatalf("ExtractEC2UsesProfileEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 for nil input", len(rows))
	}
	if tally.totalSkipped() != 0 {
		t.Fatalf("totalSkipped = %d, want 0 for nil input", tally.totalSkipped())
	}
}

func TestExtractEC2UsesProfileEdgeRowsDeterministicOrder(t *testing.T) {
	t.Parallel()

	const acct = "111122223333"
	const region = "us-east-1"
	const profileRegion = "aws-global"
	profileARN := "arn:aws:iam::" + acct + ":instance-profile/app"
	resources := []facts.Envelope{ec2UsesProfileResourceEnvelope(acct, profileRegion, "app")}

	forward := []facts.Envelope{
		ec2UsesProfilePostureEnvelope(acct, region, "i-bbb", profileARN),
		ec2UsesProfilePostureEnvelope(acct, region, "i-aaa", profileARN),
	}
	reverse := []facts.Envelope{
		ec2UsesProfilePostureEnvelope(acct, region, "i-aaa", profileARN),
		ec2UsesProfilePostureEnvelope(acct, region, "i-bbb", profileARN),
	}

	rowsForward, _, err := ExtractEC2UsesProfileEdgeRows(resources, forward)
	if err != nil {
		t.Fatalf("ExtractEC2UsesProfileEdgeRows() error = %v, want nil", err)
	}
	rowsReverse, _, err := ExtractEC2UsesProfileEdgeRows(resources, reverse)
	if err != nil {
		t.Fatalf("ExtractEC2UsesProfileEdgeRows() error = %v, want nil", err)
	}
	if len(rowsForward) != len(rowsReverse) {
		t.Fatalf("row count differs by ordering: %d vs %d", len(rowsForward), len(rowsReverse))
	}
	for i := range rowsForward {
		if anyToString(rowsForward[i]["source_uid"]) != anyToString(rowsReverse[i]["source_uid"]) {
			t.Fatalf("row %d source_uid not stable across input ordering", i)
		}
	}
}

func TestExtractEC2UsesProfileEdgeRowsTombstoneSkipped(t *testing.T) {
	t.Parallel()

	const acct = "111122223333"
	const region = "us-east-1"
	const profileRegion = "aws-global"
	profileARN := "arn:aws:iam::" + acct + ":instance-profile/app"
	resources := []facts.Envelope{ec2UsesProfileResourceEnvelope(acct, profileRegion, "app")}

	tomb := ec2UsesProfilePostureEnvelope(acct, region, "i-terminated", profileARN)
	tomb.IsTombstone = true
	postures := []facts.Envelope{tomb}

	rows, tally, err := ExtractEC2UsesProfileEdgeRows(resources, postures)
	if err != nil {
		t.Fatalf("ExtractEC2UsesProfileEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0 for a tombstoned instance", len(rows))
	}
	if tally.totalSkipped() != 0 {
		t.Fatalf("totalSkipped = %d, want 0 (a terminated instance is not a lost edge)", tally.totalSkipped())
	}
}
