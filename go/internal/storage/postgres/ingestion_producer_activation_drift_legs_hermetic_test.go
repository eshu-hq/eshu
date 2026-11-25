// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/reducer/awscloud"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
)

// Hermetic proof for eshu-hq/eshu#11392 rule (a): the CI corpus path drops
// the state leg of aws_cloud_runtime_drift re-derivation, and the config leg
// never existed. The narrative replicates the corpus ordering: the EC2 drift
// pass converges before its state/config evidence legs activate, so the
// first handler run derives nothing; each leg's later activation must reopen
// the converged drift item through the #7635 producer-activation settle, and
// the second handler run then derives the finding.
//
// (1) State-leg diagnosis (Ack owe vs settle reach). The owe half drives the
// exact call Ack makes and pins the obligation INSERT; the settle half drives
// the real SettleClaimedProducerActivation against staged fakes. Both halves
// hold at the mechanism level, so the corpus drop is NOT in this package's
// mechanism: Ack owes unconditionally, and nothing in the corpus pipeline
// settles (the producer runner stays off unless
// ESHU_PRODUCER_ACTIVATION_CONSUMER_ENABLED is set, which no corpus script
// sets). No owe-wiring is missing on the state half.
//
// (2) Config-leg RED. producerEvidenceCoreSQL has identity-filter and
// terraform_state_resource arms but no config arm, so a config-only
// activation retires inapplicable and never reopens drift. The arm-presence
// test below FAILS before the fix and passes after; the config settle test
// fails before (the settle has no config query to spend the staged keys on)
// and passes after.
//
// Every settle drive here stages the POST-fix query order. SQL text is never
// executed on fakes, so the intersection, staleness, and floor semantics are
// pinned by the derivation test
// (TestProducerListingsDeriveFromShipped, extended with the config listing)
// plus substring pins on the conjuncts, while the drives pin the
// orchestration: probe order, reopen-exactly-listed, terminal outcomes, and
// the per-activation fact probe budget.

const (
	// driftLegARN is the EC2 instance ARN the corpus checks demand the
	// image_version_drift row for (scope aws:123456789012:us-east-1:ec2).
	driftLegARN         = "arn:aws:ec2:us-east-1:123456789012:instance/i-0abcdef1234567890"
	driftLegAWSScope    = "aws:123456789012:us-east-1:ec2"
	driftLegAWSGen      = "aws-gen-1"
	driftLegStateScope  = "state_snapshot:s3:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	driftLegStateGen    = "terraform_state:state_snapshot:s3:9f86:serial:42"
	driftLegConfigScope = "git:iac-demo"
	driftLegConfigGen   = "config-gen-2"
	driftLegDriftItemID = "drift-item-aws-ec2/aws_cloud_runtime_drift"
	driftLegLeaseOwner  = "drift-leg-test"
	driftLegLeaseToken  = int64(7)
)

// driftLegObligation builds the claimed obligation a settle drive settles.
// CreatedAt is the owe moment; the shipped staleness conjunct reopens only
// items that completed before it.
func driftLegObligation(scopeID, generationID string) activation.ProducerObligation {
	return activation.ProducerObligation{
		ScopeID:      scopeID,
		GenerationID: generationID,
		LeaseOwner:   driftLegLeaseOwner,
		LeaseToken:   driftLegLeaseToken,
		LeaseUntil:   time.Now().Add(time.Minute).UTC(),
		CreatedAt:    time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
	}
}

func driftLegStringRows(values []string) [][]any {
	rows := make([][]any, 0, len(values))
	for _, value := range values {
		rows = append(rows, []any{value})
	}
	return rows
}

func driftLegNullStringRows(values []string) [][]any {
	rows := make([][]any, 0, len(values))
	for _, value := range values {
		rows = append(rows, []any{sql.NullString{String: value, Valid: true}})
	}
	return rows
}

// stageDriftLegSettleTx stages one full settle in the exact query order
// SettleClaimedProducerActivation issues: scope lock, obligation lock,
// evidence probe, OCI keys, drift ARNs, config kinds, the OCI listings,
// the drift listing, the config listing, the lease recheck. Empty slices
// stage empty results; the completion is an exec needing no staging.
func stageDriftLegSettleTx(
	generationID string,
	carries bool,
	driftARNs, configKinds, ociListA, ociListB, driftList, configList []string,
) *fakeTx {
	return &fakeTx{queryResponses: []queueFakeRows{
		{rows: [][]any{{sql.NullString{String: generationID, Valid: true}}}},
		{rows: [][]any{{driftLegLeaseToken, driftLegLeaseOwner, "leased", true}}},
		{rows: [][]any{{carries}}},
		{rows: [][]any{}},
		{rows: driftLegNullStringRows(driftARNs)},
		{rows: driftLegNullStringRows(configKinds)},
		{rows: driftLegStringRows(ociListA)},
		{rows: driftLegStringRows(ociListB)},
		{rows: driftLegStringRows(driftList)},
		{rows: driftLegStringRows(configList)},
		{rows: [][]any{{true}}},
	}}
}

// driveDriftLegSettle runs the real settle over a fake-backed store and
// returns the reported counts, outcome, and the transaction for exec
// inspection.
func driveDriftLegSettle(
	t *testing.T,
	work activation.ProducerObligation,
	tx *fakeTx,
) (map[string]int, activation.ProducerOutcome) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fakeDB := &fakeTransactionalDB{tx: tx}
	store := NewIngestionStore(fakeDB)
	counts, outcome, err := store.SettleClaimedProducerActivation(ctx, work)
	if err != nil {
		t.Fatalf("settle %s/%s: %v", work.ScopeID, work.GenerationID, err)
	}
	return counts, outcome
}

// reopenedDriftLegIDs returns the work item ids the settle passed to the
// reopen UPDATE, in issue order.
func reopenedDriftLegIDs(tx *fakeTx) []string {
	var ids []string
	for _, exec := range tx.execs {
		if exec.query != reopenSucceededReducerWorkQuery {
			continue
		}
		if len(exec.args) != 2 {
			continue
		}
		id, ok := exec.args[1].(string)
		if !ok {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// TestProducerStateActivationOwesObligationHermetic is the owe half of the
// #11392 rule (a)(1) diagnosis: the exact call Ack makes for a
// state_snapshot activation owes one producer-activation obligation inside
// the activation transaction. The corpus path goes through Ack, so it owes;
// no owe-wiring is missing on the state half.
func TestProducerStateActivationOwesObligationHermetic(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	tx := &fakeTx{}
	if err := oweProducerActivationObligation(ctx, tx, driftLegStateScope, driftLegStateGen); err != nil {
		t.Fatalf("owe producer activation obligation: %v", err)
	}
	if len(tx.execs) != 1 {
		t.Fatalf("owe issued %d execs, want 1", len(tx.execs))
	}
	exec := tx.execs[0]
	if !strings.Contains(exec.query, "INSERT INTO producer_activation_obligations") {
		t.Fatalf("owe query = %q, want the producer obligation insert", exec.query)
	}
	wantArgs := []any{driftLegStateScope, driftLegStateGen, projectorWorkItemID(driftLegStateScope, driftLegStateGen)}
	if len(exec.args) != len(wantArgs) {
		t.Fatalf("owe args = %v, want %v", exec.args, wantArgs)
	}
	for i := range wantArgs {
		if exec.args[i] != wantArgs[i] {
			t.Fatalf("owe args = %v, want %v", exec.args, wantArgs)
		}
	}
}

// TestProducerEvidenceCoreCoversConfigBackendFacts is the RED test for
// #11392 rule (a)(2): the producer-evidence predicate must match a config
// generation carrying terraform_backends parser facts, or the settle retires
// every config-only activation as inapplicable and drift never reopens on
// the config leg. It FAILS before the fix (no config arm) and passes after.
func TestProducerEvidenceCoreCoversConfigBackendFacts(t *testing.T) {
	t.Parallel()
	for _, fragment := range []string{
		"terraform_backends",
		"fact_kind = 'file'",
		"source_system = 'git'",
	} {
		if !strings.Contains(producerEvidenceCoreSQL, fragment) {
			t.Errorf("producerEvidenceCoreSQL misses config-arm fragment %q; "+
				"a config-only activation carries no producer evidence and retires inapplicable", fragment)
		}
	}
}

// TestProducerSettleReachesDriftOnStateActivationHermetic is the reach half
// of the diagnosis: a state_snapshot activation carrying ARN A reopens the
// converged drift item on ARN A and completes. The drift ARNs arrive from
// the owed generation's terraform_state_resource facts; the intersection
// with the drift item's own-generation ARNs is the shipped
// producerDriftLinkageConjunct (pinned textually by
// TestProducerListingsDeriveFromShipped), so the drive stages the listing
// outcome and pins reopen-exactly-listed plus the terminal completed state.
func TestProducerSettleReachesDriftOnStateActivationHermetic(t *testing.T) {
	t.Parallel()
	work := driftLegObligation(driftLegStateScope, driftLegStateGen)
	tx := stageDriftLegSettleTx(driftLegStateGen, true,
		[]string{driftLegARN}, nil, nil, nil, []string{driftLegDriftItemID}, nil)
	counts, outcome := driveDriftLegSettle(t, work, tx)
	if outcome != activation.ProducerCompleted {
		t.Fatalf("settle outcome = %q, want completed", outcome)
	}
	if len(counts) != 1 || counts[string(reducercontract.DomainAWSCloudRuntimeDrift)] != 1 {
		t.Fatalf("settle reopened %v, want exactly one aws_cloud_runtime_drift item", counts)
	}
	if ids := reopenedDriftLegIDs(tx); len(ids) != 1 || ids[0] != driftLegDriftItemID {
		t.Fatalf("reopened ids = %v, want exactly [%s]", ids, driftLegDriftItemID)
	}
	if !tx.committed {
		t.Fatal("settle did not commit the reopen transaction")
	}
}

// TestProducerSettleReopensDriftOnConfigActivationHermetic is the RED settle
// test for #11392 rule (a)(2): a config commit moving the backend anchor
// (owed kinds [s3]) reopens the converged drift item whose generation ARNs
// intersect the owned s3 states' ARNs, through the config two-hop linkage
// (owed config commit, owned state scopes, intersecting drift ARNs). It
// FAILS before the fix: the settle has no config-keys query and no config
// listing, so the staged config rows land in the wrong positions and the
// settle cannot complete the config path. It passes after.
func TestProducerSettleReopensDriftOnConfigActivationHermetic(t *testing.T) {
	t.Parallel()
	work := driftLegObligation(driftLegConfigScope, driftLegConfigGen)
	tx := stageDriftLegSettleTx(driftLegConfigGen, true,
		nil, []string{"s3"}, nil, nil, nil, []string{driftLegDriftItemID})
	counts, outcome := driveDriftLegSettle(t, work, tx)
	if outcome != activation.ProducerCompleted {
		t.Fatalf("settle outcome = %q, want completed", outcome)
	}
	if len(counts) != 1 || counts[string(reducercontract.DomainAWSCloudRuntimeDrift)] != 1 {
		t.Fatalf("settle reopened %v, want exactly one aws_cloud_runtime_drift item", counts)
	}
	if ids := reopenedDriftLegIDs(tx); len(ids) != 1 || ids[0] != driftLegDriftItemID {
		t.Fatalf("reopened ids = %v, want exactly [%s]", ids, driftLegDriftItemID)
	}
	if !tx.committed {
		t.Fatal("settle did not commit the reopen transaction")
	}
}

// TestProducerConfigActivationWithoutIntersectionReopensNothingHermetic is
// the linked-reopen-only negative: a config activation for backend kind gcs
// must not reopen a drift item whose ARNs live in s3-owned states. The
// config listing is staged empty (no ARN intersection, the SQL's own
// verdict), so the settle completes with zero reopens and issues no reopen
// for the disjoint item: zero queue growth beyond linked items.
func TestProducerConfigActivationWithoutIntersectionReopensNothingHermetic(t *testing.T) {
	t.Parallel()
	work := driftLegObligation(driftLegConfigScope, driftLegConfigGen)
	tx := stageDriftLegSettleTx(driftLegConfigGen, true,
		nil, []string{"gcs"}, nil, nil, nil, nil)
	counts, outcome := driveDriftLegSettle(t, work, tx)
	if outcome != activation.ProducerCompleted {
		t.Fatalf("settle outcome = %q, want completed", outcome)
	}
	if len(counts) != 0 {
		t.Fatalf("settle reopened %v, want nothing (no ARN intersection)", counts)
	}
	if ids := reopenedDriftLegIDs(tx); len(ids) != 0 {
		t.Fatalf("reopened ids = %v, want none", ids)
	}
}

// TestProducerSettleReopensNothingSecondTimeHermetic is the exactly-once
// negative: once the drift item has replayed it is no longer succeeded, so
// a second settle lists nothing and reopens zero. The second drive stages
// empty listings (the item left the succeeded set); the obligation stays
// terminal and the queue does not grow.
func TestProducerSettleReopensNothingSecondTimeHermetic(t *testing.T) {
	t.Parallel()
	work := driftLegObligation(driftLegConfigScope, driftLegConfigGen)
	first := stageDriftLegSettleTx(driftLegConfigGen, true,
		nil, []string{"s3"}, nil, nil, nil, []string{driftLegDriftItemID})
	if counts, outcome := driveDriftLegSettle(t, work, first); outcome != activation.ProducerCompleted || counts[string(reducercontract.DomainAWSCloudRuntimeDrift)] != 1 {
		t.Fatalf("first settle = (%v, %q), want ({aws_cloud_runtime_drift:1}, completed)", counts, outcome)
	}
	second := stageDriftLegSettleTx(driftLegConfigGen, true,
		nil, []string{"s3"}, nil, nil, nil, nil)
	counts, outcome := driveDriftLegSettle(t, work, second)
	if outcome != activation.ProducerCompleted {
		t.Fatalf("second settle outcome = %q, want completed", outcome)
	}
	if len(counts) != 0 {
		t.Fatalf("second settle reopened %v, want nothing (exactly-once)", counts)
	}
	if ids := reopenedDriftLegIDs(second); len(ids) != 0 {
		t.Fatalf("second settle reopened ids = %v, want none", ids)
	}
}

// TestProducerBackendlessConfigActivationRetiresInapplicableHermetic guards
// the over-owe direction on the new arm: a config generation with no
// terraform_backends rows carries no producer evidence, so the settle
// retires it inapplicable with zero reopens, exactly like any other
// non-producer generation before and after the fix.
func TestProducerBackendlessConfigActivationRetiresInapplicableHermetic(t *testing.T) {
	t.Parallel()
	work := driftLegObligation(driftLegConfigScope, driftLegConfigGen)
	tx := stageDriftLegSettleTx(driftLegConfigGen, false,
		nil, nil, nil, nil, nil, nil)
	counts, outcome := driveDriftLegSettle(t, work, tx)
	if outcome != activation.ProducerInapplicable {
		t.Fatalf("settle outcome = %q, want inapplicable", outcome)
	}
	if len(counts) != 0 {
		t.Fatalf("settle reopened %v, want nothing", counts)
	}
	if ids := reopenedDriftLegIDs(tx); len(ids) != 0 {
		t.Fatalf("reopened ids = %v, want none", ids)
	}
	retired := false
	for _, exec := range tx.execs {
		if strings.Contains(exec.query, "producer_activation_obligations") &&
			strings.Contains(exec.query, "'inapplicable'") {
			retired = true
		}
	}
	if !retired {
		t.Fatal("settle issued no inapplicable retirement for the backend-less config generation")
	}
}

// TestProducerSettleFactProbeBudgetHermetic pins the per-activation probe
// budget the #11392 thresholds require: one settle issues exactly eight
// fact_records-touching statements (four probes, evidence plus one key read
// per leg, and four listings, one per consumer family), each anchored on its
// scope and generation, never a corpus scan. Adding a leg must update this
// budget deliberately, not silently.
func TestProducerSettleFactProbeBudgetHermetic(t *testing.T) {
	t.Parallel()
	work := driftLegObligation(driftLegConfigScope, driftLegConfigGen)
	tx := stageDriftLegSettleTx(driftLegConfigGen, true,
		nil, []string{"s3"}, nil, nil, nil, []string{driftLegDriftItemID})
	if _, outcome := driveDriftLegSettle(t, work, tx); outcome != activation.ProducerCompleted {
		t.Fatalf("settle outcome = %q, want completed", outcome)
	}
	probes := 0
	for _, call := range tx.queries {
		if !strings.Contains(call.query, "fact_records") {
			continue
		}
		probes++
		if !strings.Contains(call.query, "scope_id") {
			t.Errorf("fact probe missing its scope anchor:\n%s", call.query)
		}
	}
	const wantProbes = 8
	if probes != wantProbes {
		t.Fatalf("settle issued %d fact_records statements, want %d (4 probes + 4 listings)", probes, wantProbes)
	}
}

// driftLegEvidenceLoader is a scripted AWSCloudRuntimeDriftEvidenceLoader:
// the corpus narrative needs a first run that converges empty (legs not yet
// active) and a second run that sees the joined evidence (legs active).
type driftLegEvidenceLoader struct {
	rows []cloudruntime.AddressedRow
}

func (f *driftLegEvidenceLoader) LoadAWSCloudRuntimeDriftEvidence(
	_ context.Context,
	_, _ string,
) ([]cloudruntime.AddressedRow, error) {
	return f.rows, nil
}

// driftLegFindingWriter captures the publication request; CanonicalWrites
// follows the admitted count like the production writer's contract.
type driftLegFindingWriter struct {
	writes []awscloud.AWSCloudRuntimeDriftWrite
}

func (f *driftLegFindingWriter) WriteAWSCloudRuntimeDriftFindings(
	_ context.Context,
	write awscloud.AWSCloudRuntimeDriftWrite,
) (awscloud.AWSCloudRuntimeDriftWriteResult, error) {
	f.writes = append(f.writes, write)
	return awscloud.AWSCloudRuntimeDriftWriteResult{CanonicalWrites: len(write.Candidates)}, nil
}

type driftLegTokenIssuer struct{}

func (driftLegTokenIssuer) NextAWSCloudRuntimeDriftFencingToken(_ context.Context) (int64, error) {
	return 1, nil
}

// TestAWSCloudRuntimeDriftConvergesEmptyThenDerivesFindingHermetic closes
// the #11392 narrative on the handler: the first run (state/config legs not
// yet active, nothing classifiable) converges empty with zero writes, and
// the rerun after the legs activate (the reopen the settle tests above
// produce) derives the finding from the joined evidence. The handler
// already supports delta-narrowed reopens; both runs here are full so the
// test pins derivation, not narrowing.
func TestAWSCloudRuntimeDriftConvergesEmptyThenDerivesFindingHermetic(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	loader := &driftLegEvidenceLoader{}
	writer := &driftLegFindingWriter{}
	handler := awscloud.AWSCloudRuntimeDriftHandler{
		EvidenceLoader:     loader,
		Writer:             writer,
		FencingTokenIssuer: driftLegTokenIssuer{},
	}
	intent := reducercontract.Intent{
		IntentID:     "drift-leg-intent",
		ScopeID:      driftLegAWSScope,
		GenerationID: driftLegAWSGen,
		SourceSystem: "aws",
		Domain:       reducercontract.DomainAWSCloudRuntimeDrift,
		Cause:        "test",
	}

	first, err := handler.Handle(ctx, intent)
	if err != nil {
		t.Fatalf("first Handle() error = %v, want nil", err)
	}
	if first.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("first Handle().Status = %q, want succeeded", first.Status)
	}
	if first.CanonicalWrites != 0 {
		t.Fatalf("first Handle().CanonicalWrites = %d, want 0 (converges empty before the legs activate)", first.CanonicalWrites)
	}
	if len(writer.writes) != 1 {
		t.Fatalf("writer calls = %d, want 1 (the empty run still records its write)", len(writer.writes))
	}

	loader.rows = []cloudruntime.AddressedRow{{
		ARN:          driftLegARN,
		ResourceType: "aws_instance",
		Cloud: &cloudruntime.ResourceRow{
			ARN:          driftLegARN,
			ResourceID:   "i-0abcdef1234567890",
			ResourceType: "aws_instance",
			ScopeID:      driftLegAWSScope,
		},
		State: &cloudruntime.ResourceRow{
			ARN:          driftLegARN,
			Address:      "aws_instance.web",
			ResourceType: "aws_instance",
			ScopeID:      driftLegStateScope,
		},
	}}
	second, err := handler.Handle(ctx, intent)
	if err != nil {
		t.Fatalf("second Handle() error = %v, want nil", err)
	}
	if second.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("second Handle().Status = %q, want succeeded", second.Status)
	}
	if second.CanonicalWrites != 1 {
		t.Fatalf("second Handle().CanonicalWrites = %d, want 1 (the joined run derives the finding)", second.CanonicalWrites)
	}
	if got := len(writer.writes); got != 2 {
		t.Fatalf("writer calls = %d, want 2", got)
	}
	if got := len(writer.writes[1].Candidates); got != 1 {
		t.Fatalf("second write candidates = %d, want 1", got)
	}
	if !strings.Contains(second.EvidenceSummary, "unmanaged=1") {
		t.Fatalf("second EvidenceSummary = %q, want unmanaged=1 (state joined, config not yet joined)", second.EvidenceSummary)
	}
}
