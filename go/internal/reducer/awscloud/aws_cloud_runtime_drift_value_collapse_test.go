// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// The #5837 value-axis acceptance probe, run through the real handler.
//
// cloudruntime.Classify is degradation-safe on the EXISTENCE axis: a missing
// state row becomes orphaned_cloud_resource and a missing config row becomes
// unmanaged_cloud_resource, and BOTH write a durable finding, so the
// generation-authoritative retire always has a keep-set to protect the ARN
// with. It was NOT degradation-safe on the VALUE axis. With cloud, state, and
// config all present and no comparable attribute left to compare, Classify
// returned "" and BuildCandidates dropped the row, so the keep-set emptied and
// the retire deleted a still-true drift finding.
//
// That is reachable with nothing upstream asserting a failure. The
// terraform-state collector fail-closed-redacts scalar attributes when its
// provider-schema resolver is nil or a schema bundle fails to parse, and both
// paths are silent (terraformstate/schema_resolver.go: LoadPackagedSchemaResolver
// returns (nil, nil); parseSchemaInto skips a corrupt bundle;
// cmd/collector-terraform-state/config.go accepts a nil resolver as non-fatal).
// The condition is sticky per deployment, so replay repeats the deletion rather
// than healing it.
//
// The fix is FindingKindValueComparisonInconclusive: a degraded ARN keeps a
// durable row, so the stale drift claim is REPLACED BY EXPLICIT UNCERTAINTY
// rather than silently deleted, and it self-heals into the corrected kind once
// the upstream problem clears. The retire mechanics are unchanged.

// driftValueCollapseARN is the one ARN both passes classify.
const driftValueCollapseARN = "arn:aws:ec2:us-east-1:123456789012:instance/i-05837valueaxis"

// driftValueCollapseEvidenceAsOf is the deterministic evidence-read watermark
// the probe stamps its writes with. Only the classification is under test here,
// so the exact instant is arbitrary; it just has to be fixed.
var driftValueCollapseEvidenceAsOf = time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)

// driftValueCollapseRetireArgs returns the argument list the
// generation-authoritative retire (awsCloudRuntimeDriftRetireQuery) was issued
// with. Matching on the query text rather than a fixed index keeps the probe
// working if the writer's statement order changes: the admission check and the
// versioned upsert run through the same captured exec slice.
func driftValueCollapseRetireArgs(t *testing.T, execs []fakeAWSCloudRuntimeDriftExecCall) []any {
	t.Helper()

	for _, call := range execs {
		if call.query == awsCloudRuntimeDriftRetireQuery {
			return call.args
		}
	}
	t.Fatalf("no retire statement issued; %d exec calls captured", len(execs))
	return nil
}

// driftValueCollapseRow builds the three-layer join for one aws_instance ARN.
// An empty stateAMI models the redaction: the Terraform-state row exists and is
// config-backed, it simply carries no comparable "ami" value.
func driftValueCollapseRow(scopeID, cloudAMI, stateAMI string) cloudruntime.AddressedRow {
	state := &cloudruntime.ResourceRow{
		ARN:          driftValueCollapseARN,
		ResourceType: "aws_instance",
		Address:      "aws_instance.web",
		ScopeID:      scopeID,
	}
	if stateAMI != "" {
		state.Attributes = map[string]string{"ami": stateAMI}
	}
	return cloudruntime.AddressedRow{
		ARN:          driftValueCollapseARN,
		ResourceType: "aws_instance",
		Cloud: &cloudruntime.ResourceRow{
			ARN:          driftValueCollapseARN,
			ResourceType: "aws_ec2_instance",
			ScopeID:      scopeID,
			Attributes:   map[string]string{"ami": cloudAMI},
		},
		State: state,
		Config: &cloudruntime.ResourceRow{
			ARN:          driftValueCollapseARN,
			ResourceType: "aws_instance",
			Address:      "aws_instance.web",
			ScopeID:      scopeID,
		},
	}
}

// fakeDriftEvidenceLoader serves one fixed row set, so two passes can differ
// only in the evidence they read for the SAME (scope, generation).
type fakeDriftEvidenceLoader struct {
	rows []cloudruntime.AddressedRow
}

func (f fakeDriftEvidenceLoader) LoadAWSCloudRuntimeDriftEvidence(
	_ context.Context,
	_ string,
	_ string,
) ([]cloudruntime.AddressedRow, error) {
	return f.rows, nil
}

// recordingDriftWriter delegates to the real writer (so the real insert and the
// real retire statement both run against the fake execer) while capturing the
// write request, which is what carries the admitted candidate set.
type recordingDriftWriter struct {
	inner  PostgresAWSCloudRuntimeDriftWriter
	writes []AWSCloudRuntimeDriftWrite
}

func (w *recordingDriftWriter) WriteAWSCloudRuntimeDriftFindings(
	ctx context.Context,
	write AWSCloudRuntimeDriftWrite,
) (AWSCloudRuntimeDriftWriteResult, error) {
	w.writes = append(w.writes, write)
	return w.inner.WriteAWSCloudRuntimeDriftFindings(ctx, write)
}

// driftValueCollapsePass is one probe pass: the admitted candidate kinds and
// the exact keep-set the retire was issued with.
type driftValueCollapsePass struct {
	candidateKinds []string
	keepSet        []string
}

// runDriftValueCollapsePass drives the real AWSCloudRuntimeDriftHandler over
// one row set and reports what the retire's keep-set ended up being.
func runDriftValueCollapsePass(
	t *testing.T,
	scopeID, generationID string,
	rows []cloudruntime.AddressedRow,
	evidenceAsOf time.Time,
) driftValueCollapsePass {
	t.Helper()

	execer := &fakeAWSCloudRuntimeDriftExecer{}
	writer := &recordingDriftWriter{
		inner: PostgresAWSCloudRuntimeDriftWriter{DB: execer, Now: func() time.Time { return evidenceAsOf }},
	}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     fakeDriftEvidenceLoader{rows: rows},
		Writer:             writer,
		FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1}},
		Now:                func() time.Time { return evidenceAsOf },
	}

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-" + generationID,
		Domain:       reducercontract.DomainAWSCloudRuntimeDrift,
		ScopeID:      scopeID,
		GenerationID: generationID,
		SourceSystem: "aws",
		Cause:        "value-axis acceptance probe",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if len(writer.writes) != 1 {
		t.Fatalf("writes issued = %d, want 1", len(writer.writes))
	}
	write := writer.writes[0]
	if result.CanonicalWrites != len(write.Candidates) {
		t.Fatalf("CanonicalWrites = %d, want %d", result.CanonicalWrites, len(write.Candidates))
	}

	kinds := make([]string, 0, len(write.Candidates))
	for _, candidate := range write.Candidates {
		kinds = append(kinds, awsCloudRuntimeFindingKind(candidate))
	}
	return driftValueCollapsePass{
		candidateKinds: kinds,
		keepSet:        stringArg(t, driftValueCollapseRetireArgs(t, execer.execs)[4], "keep_fact_ids"),
	}
}

// TestAWSCloudRuntimeDriftValueCollapseKeepsADurableFinding is the #5837
// value-axis regression. It must FAIL before FindingKindValueComparisonInconclusive
// exists: pass 2 produces zero candidates, the keep-set empties, and the retire
// deletes pass 1's still-true drift finding outright.
func TestAWSCloudRuntimeDriftValueCollapseKeepsADurableFinding(t *testing.T) {
	t.Parallel()

	const (
		scopeID      = "aws:123456789012:us-east-1:ec2"
		generationID = "gen-5837-value-axis"
	)

	pass1 := runDriftValueCollapsePass(
		t, scopeID, generationID,
		[]cloudruntime.AddressedRow{driftValueCollapseRow(scopeID, "ami-observed", "ami-declared")},
		driftValueCollapseEvidenceAsOf,
	)
	if len(pass1.candidateKinds) != 1 || pass1.candidateKinds[0] != string(cloudruntime.FindingKindImageVersionDrift) {
		t.Fatalf("pass 1 candidate kinds = %v, want [%s]", pass1.candidateKinds, cloudruntime.FindingKindImageVersionDrift)
	}
	if len(pass1.keepSet) != 1 {
		t.Fatalf("pass 1 keep_set = %v, want exactly one fact id", pass1.keepSet)
	}

	// Pass 2 reads the SAME state row with the comparable value redacted. It
	// must still keep a durable row for this ARN.
	pass2 := runDriftValueCollapsePass(
		t, scopeID, generationID,
		[]cloudruntime.AddressedRow{driftValueCollapseRow(scopeID, "ami-observed", "")},
		driftValueCollapseEvidenceAsOf.Add(time.Minute),
	)
	if len(pass2.candidateKinds) != 1 {
		t.Fatalf(
			"pass 2 candidate kinds = %v, want exactly one; an empty set empties the keep-set and the "+
				"generation-authoritative retire then DELETES the still-true drift finding pass 1 wrote",
			pass2.candidateKinds,
		)
	}
	if pass2.candidateKinds[0] != string(cloudruntime.FindingKindValueComparisonInconclusive) {
		t.Fatalf(
			"pass 2 candidate kind = %q, want %q: a degraded comparison must be reported as explicit "+
				"uncertainty, never as convergence",
			pass2.candidateKinds[0], cloudruntime.FindingKindValueComparisonInconclusive,
		)
	}
	if len(pass2.keepSet) != 1 {
		t.Fatalf("pass 2 keep_set = %v, want exactly one fact id", pass2.keepSet)
	}

	// Replaced, not merely retained: the inconclusive finding is a different
	// fact id, so pass 1's drift row is retired by the same statement that
	// protects pass 2's.
	if pass2.keepSet[0] == pass1.keepSet[0] {
		t.Fatalf(
			"pass 2 keep_set = %v, want a NEW fact id: the identity embeds finding_kind, so a corrected "+
				"classification must not reuse the superseded row's id",
			pass2.keepSet,
		)
	}
}

// TestAWSCloudRuntimeDriftConvergedARNStillProducesNoFinding guards the other
// direction. value_comparison_inconclusive must fire only when a comparison was
// impossible, never when one succeeded and agreed — otherwise every healthy
// managed resource in the corpus would grow a finding.
func TestAWSCloudRuntimeDriftConvergedARNStillProducesNoFinding(t *testing.T) {
	t.Parallel()

	const (
		scopeID      = "aws:123456789012:us-east-1:ec2"
		generationID = "gen-5837-converged"
	)

	pass := runDriftValueCollapsePass(
		t, scopeID, generationID,
		[]cloudruntime.AddressedRow{driftValueCollapseRow(scopeID, "ami-same", "ami-same")},
		driftValueCollapseEvidenceAsOf,
	)
	if len(pass.candidateKinds) != 0 {
		t.Fatalf(
			"converged ARN candidate kinds = %v, want none: cloud, state, and config agree and the one "+
				"comparable value matched",
			pass.candidateKinds,
		)
	}
	if len(pass.keepSet) != 0 {
		t.Fatalf("converged ARN keep_set = %v, want empty", pass.keepSet)
	}
}
