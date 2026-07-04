// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// coverageEdgeRowKey indexes extracted COVERS rows by their logical edge
// identity so assertions do not depend on slice order.
func coverageEdgeRowKey(row map[string]any) string {
	return anyToString(row["observability_uid"]) + "|" +
		anyToString(row["coverage_signal"]) + "|" +
		anyToString(row["target_uid"])
}

func indexCoverageEdgeRows(rows []map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		out[coverageEdgeRowKey(row)] = row
	}
	return out
}

// ec2AlarmCoverageFixture builds the canonical positive fixture: a scanned EC2
// instance plus a CloudWatch alarm whose InstanceId dimension resolves to it by
// bare id. This is the only edge-eligible (exact) coverage shape.
func ec2AlarmCoverageFixture() []facts.Envelope {
	instance := awsResourceFact("fact-ec2", "aws_ec2_instance", "i-0123456789abcdef0",
		"arn:aws:ec2:us-east-1:111122223333:instance/i-0123456789abcdef0", "web-1", false)
	alarm := awsResourceFact("fact-alarm", "aws_cloudwatch_alarm",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high", "cpu-high", false)
	rel := alarmObservesMetricFact("fact-rel",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high", "metric-1",
		[]map[string]any{{"name": "InstanceId", "value": "i-0123456789abcdef0"}})
	return []facts.Envelope{instance, alarm, rel}
}

func TestExtractObservabilityCoverageEdgeRowsEmitsExactEdge(t *testing.T) {
	t.Parallel()

	rows, tally, _, err := ExtractObservabilityCoverageEdgeRows(ec2AlarmCoverageFixture())
	if err != nil {
		t.Fatalf("ExtractObservabilityCoverageEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("edge rows = %d, want 1 exact COVERS edge", len(rows))
	}
	instanceUID := cloudResourceUID(testCoverageAccount, testCoverageRegion, "aws_ec2_instance", "i-0123456789abcdef0")
	alarmUID := cloudResourceUID(testCoverageAccount, testCoverageRegion, "aws_cloudwatch_alarm",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high")
	row := rows[0]
	if got := anyToString(row["observability_uid"]); got != alarmUID {
		t.Fatalf("observability_uid = %q, want alarm uid %q", got, alarmUID)
	}
	if got := anyToString(row["target_uid"]); got != instanceUID {
		t.Fatalf("target_uid = %q, want instance uid %q", got, instanceUID)
	}
	if got := anyToString(row["coverage_signal"]); got != coverageSignalAlarm {
		t.Fatalf("coverage_signal = %q, want %q", got, coverageSignalAlarm)
	}
	if got := anyToString(row["resolution_mode"]); got != coverageResolutionBareID {
		t.Fatalf("resolution_mode = %q, want %q", got, coverageResolutionBareID)
	}
	if tally.materialized[coverageSignalAlarm] != 1 {
		t.Fatalf("materialized[alarm] = %d, want 1", tally.materialized[coverageSignalAlarm])
	}
}

func TestExtractObservabilityCoverageEdgeRowsGapEmitsNoEdge(t *testing.T) {
	t.Parallel()

	// An RDS instance with no alarm relationship is a gap (unresolved/provenance)
	// and must never fabricate a COVERS edge. We add a covered EC2 peer so the
	// gap finding is even produced, then assert the gap target gets no edge.
	rds := awsResourceFact("fact-rds", "aws_db_instance", "db-prod",
		"arn:aws:rds:us-east-1:111122223333:db:db-prod", "db-prod", false)
	envelopes := append(ec2AlarmCoverageFixture(), rds)

	rows, _, _, err := ExtractObservabilityCoverageEdgeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractObservabilityCoverageEdgeRows() error = %v, want nil", err)
	}
	rdsUID := cloudResourceUID(testCoverageAccount, testCoverageRegion, "aws_db_instance", "db-prod")
	for _, row := range rows {
		if anyToString(row["target_uid"]) == rdsUID {
			t.Fatalf("gap target %q must not produce a COVERS edge: %v", rdsUID, row)
		}
	}
}

func TestExtractObservabilityCoverageEdgeRowsAmbiguousEmitsNoEdge(t *testing.T) {
	t.Parallel()

	// Two distinct scanned resources share a non-unique dimension value, so the
	// alarm dimension matches both. Ambiguous coverage must never pick one and
	// must never write an edge.
	one := awsResourceFact("fact-one", "aws_ec2_instance", "shared-id",
		"arn:aws:ec2:us-east-1:111122223333:instance/one", "one", false)
	two := awsResourceFact("fact-two", "aws_lambda_function", "shared-id",
		"arn:aws:lambda:us-east-1:111122223333:function:two", "two", false)
	alarm := awsResourceFact("fact-alarm", "aws_cloudwatch_alarm",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:amb",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:amb", "amb", false)
	rel := alarmObservesMetricFact("fact-rel",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:amb", "metric-1",
		[]map[string]any{{"name": "Dim", "value": "shared-id"}})

	rows, _, _, err := ExtractObservabilityCoverageEdgeRows([]facts.Envelope{one, two, alarm, rel})
	if err != nil {
		t.Fatalf("ExtractObservabilityCoverageEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("ambiguous coverage produced %d edge row(s), want 0", len(rows))
	}
}

func TestExtractObservabilityCoverageEdgeRowsStaleEmitsNoEdge(t *testing.T) {
	t.Parallel()

	// The watched instance was deleted (tombstoned) but the alarm lingers: a real
	// drift signal classified stale, never a covered edge.
	instance := awsResourceFact("fact-ec2", "aws_ec2_instance", "i-stale",
		"arn:aws:ec2:us-east-1:111122223333:instance/i-stale", "stale", true)
	alarm := awsResourceFact("fact-alarm", "aws_cloudwatch_alarm",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:stale",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:stale", "stale", false)
	rel := alarmObservesMetricFact("fact-rel",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:stale", "metric-1",
		[]map[string]any{{"name": "InstanceId", "value": "i-stale"}})

	rows, _, _, err := ExtractObservabilityCoverageEdgeRows([]facts.Envelope{instance, alarm, rel})
	if err != nil {
		t.Fatalf("ExtractObservabilityCoverageEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("stale coverage produced %d edge row(s), want 0", len(rows))
	}
}

func TestExtractObservabilityCoverageEdgeRowsRejectedEmitsNoEdge(t *testing.T) {
	t.Parallel()

	// A metric-name-only alarm (no resolvable resource dimension) is rejected and
	// must never produce a covered edge.
	alarm := awsResourceFact("fact-alarm", "aws_cloudwatch_alarm",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:billing",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:billing", "billing", false)
	rel := alarmObservesMetricFact("fact-rel",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:billing", "metric-1",
		nil)

	rows, _, _, err := ExtractObservabilityCoverageEdgeRows([]facts.Envelope{alarm, rel})
	if err != nil {
		t.Fatalf("ExtractObservabilityCoverageEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rejected coverage produced %d edge row(s), want 0", len(rows))
	}
}

func TestExtractObservabilityCoverageEdgeRowsXRayDerivedEmitsNoEdge(t *testing.T) {
	t.Parallel()

	// X-Ray service coverage is derived/provenance-only keyed on a service name,
	// not a CloudResource uid, so it has no target node to MATCH and must never
	// produce a COVERS edge.
	rule := awsResourceFact("fact-rule", "aws_xray_sampling_rule", "rule-1",
		"arn:aws:xray:us-east-1:111122223333:sampling-rule/rule-1", "rule-1", false)
	rel := xraySamplingMatchesServiceFact("fact-rel",
		"arn:aws:xray:us-east-1:111122223333:sampling-rule/rule-1", "checkout")

	rows, _, _, err := ExtractObservabilityCoverageEdgeRows([]facts.Envelope{rule, rel})
	if err != nil {
		t.Fatalf("ExtractObservabilityCoverageEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("x-ray derived coverage produced %d edge row(s), want 0 (no target uid)", len(rows))
	}
}

func TestExtractObservabilityCoverageEdgeRowsEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	rows, tally, _, err := ExtractObservabilityCoverageEdgeRows(nil)
	if err != nil {
		t.Fatalf("ExtractObservabilityCoverageEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("empty input produced %d edge row(s), want 0", len(rows))
	}
	if len(tally.materialized) != 0 {
		t.Fatalf("empty input materialized tally = %v, want empty", tally.materialized)
	}
}

func TestExtractObservabilityCoverageEdgeRowsDeduplicatesAndSorts(t *testing.T) {
	t.Parallel()

	// Two alarms covering the same instance with the same signal collapse to one
	// logical edge (same obs uid, signal, target uid would only collapse if the
	// obs uid matched; distinct alarms are distinct edges). Here we feed the same
	// fixture twice to prove duplicate facts converge on one row.
	envelopes := append(ec2AlarmCoverageFixture(), ec2AlarmCoverageFixture()...)
	rows, _, _, err := ExtractObservabilityCoverageEdgeRows(envelopes)
	if err != nil {
		t.Fatalf("ExtractObservabilityCoverageEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("duplicate facts produced %d edge row(s), want 1 deduplicated edge", len(rows))
	}

	indexed := indexCoverageEdgeRows(rows)
	if len(indexed) != len(rows) {
		t.Fatalf("rows are not unique by (obs,signal,target): %d rows, %d keys", len(rows), len(indexed))
	}
}
