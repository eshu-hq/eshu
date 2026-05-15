package reducer

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type stubAWSCloudRuntimeDriftEvidenceLoader struct {
	rows  []cloudruntime.AddressedRow
	calls int
}

func (s *stubAWSCloudRuntimeDriftEvidenceLoader) LoadAWSCloudRuntimeDriftEvidence(
	context.Context,
	string,
	string,
) ([]cloudruntime.AddressedRow, error) {
	s.calls++
	return append([]cloudruntime.AddressedRow(nil), s.rows...), nil
}

type stubAWSCloudRuntimeDriftFindingWriter struct {
	write AWSCloudRuntimeDriftWrite
	err   error
	calls int
}

func (s *stubAWSCloudRuntimeDriftFindingWriter) WriteAWSCloudRuntimeDriftFindings(
	_ context.Context,
	write AWSCloudRuntimeDriftWrite,
) (AWSCloudRuntimeDriftWriteResult, error) {
	s.calls++
	s.write = write
	if s.err != nil {
		return AWSCloudRuntimeDriftWriteResult{}, s.err
	}
	return AWSCloudRuntimeDriftWriteResult{
		CanonicalWrites: len(write.Candidates),
		EvidenceSummary: "wrote aws runtime drift findings",
	}, nil
}

func newAWSCloudRuntimeDriftInstruments(t *testing.T) (*telemetry.Instruments, sdkmetric.Reader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

func TestAWSCloudRuntimeDriftHandlerPublishesAdmittedFindings(t *testing.T) {
	t.Parallel()

	inst, reader := newAWSCloudRuntimeDriftInstruments(t)
	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{
		rows: []cloudruntime.AddressedRow{
			{
				ARN:          "arn:aws:lambda:us-east-1:123456789012:function:orphan",
				ResourceType: "aws_lambda_function",
				Cloud: &cloudruntime.ResourceRow{
					ARN:          "arn:aws:lambda:us-east-1:123456789012:function:orphan",
					ResourceID:   "orphan",
					ResourceType: "aws_lambda_function",
					ScopeID:      "aws:123456789012:us-east-1",
					Tags:         map[string]string{"Environment": "prod"},
				},
			},
			{
				ARN:          "arn:aws:lambda:us-east-1:123456789012:function:unmanaged",
				ResourceType: "aws_lambda_function",
				Cloud: &cloudruntime.ResourceRow{
					ARN:          "arn:aws:lambda:us-east-1:123456789012:function:unmanaged",
					ResourceID:   "unmanaged",
					ResourceType: "aws_lambda_function",
					ScopeID:      "aws:123456789012:us-east-1",
				},
				State: &cloudruntime.ResourceRow{
					ARN:          "arn:aws:lambda:us-east-1:123456789012:function:unmanaged",
					Address:      "aws_lambda_function.unmanaged",
					ResourceType: "aws_lambda_function",
					ScopeID:      "state_snapshot:s3:abc123",
				},
			},
		},
	}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader: loader,
		Writer:         writer,
		Instruments:    inst,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-aws-drift",
		ScopeID:         "aws:123456789012:us-east-1",
		GenerationID:    "generation-aws",
		SourceSystem:    "aws",
		Domain:          DomainAWSCloudRuntimeDrift,
		Cause:           "aws runtime facts observed",
		RelatedScopeIDs: []string{"aws:123456789012:us-east-1"},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("Handle().CanonicalWrites = %d, want %d", got, want)
	}
	for _, want := range []string{
		"evaluated=2",
		"orphaned=1",
		"unmanaged=1",
		"canonical_writes=2",
	} {
		if !strings.Contains(result.EvidenceSummary, want) {
			t.Fatalf("Handle().EvidenceSummary = %q, missing %q", result.EvidenceSummary, want)
		}
	}
	if loader.calls != 1 {
		t.Fatalf("LoadAWSCloudRuntimeDriftEvidence() calls = %d, want 1", loader.calls)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteAWSCloudRuntimeDriftFindings() calls = %d, want 1", writer.calls)
	}
	if got, want := len(writer.write.Candidates), 2; got != want {
		t.Fatalf("writer candidates = %d, want %d", got, want)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := reducerCounterValue(t, rm, "eshu_dp_correlation_orphan_detected_total", map[string]string{
		telemetry.MetricDimensionPack: rules.AWSCloudRuntimeDriftPackName,
		telemetry.MetricDimensionRule: rules.AWSCloudRuntimeDriftRuleAdmitFinding,
	}); got != 1 {
		t.Fatalf("orphan detected counter = %d, want 1", got)
	}
	if got := reducerCounterValue(t, rm, "eshu_dp_correlation_unmanaged_detected_total", map[string]string{
		telemetry.MetricDimensionPack: rules.AWSCloudRuntimeDriftPackName,
		telemetry.MetricDimensionRule: rules.AWSCloudRuntimeDriftRuleAdmitFinding,
	}); got != 1 {
		t.Fatalf("unmanaged detected counter = %d, want 1", got)
	}
}

func TestAWSCloudRuntimeDriftHandlerDoesNotEmitFindingsBeforeDurableWrite(t *testing.T) {
	t.Parallel()

	inst, reader := newAWSCloudRuntimeDriftInstruments(t)
	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{
		rows: []cloudruntime.AddressedRow{{
			ARN: "arn:aws:lambda:us-east-1:123456789012:function:orphan",
			Cloud: &cloudruntime.ResourceRow{
				ARN:          "arn:aws:lambda:us-east-1:123456789012:function:orphan",
				ResourceType: "aws_lambda_function",
				ScopeID:      "aws:123456789012:us-east-1",
			},
		}},
	}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader: loader,
		Writer: &stubAWSCloudRuntimeDriftFindingWriter{
			err: errors.New("database unavailable"),
		},
		Instruments: inst,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-aws-drift",
		ScopeID:         "aws:123456789012:us-east-1",
		GenerationID:    "generation-aws",
		SourceSystem:    "aws",
		Domain:          DomainAWSCloudRuntimeDrift,
		Cause:           "aws runtime facts observed",
		RelatedScopeIDs: []string{"aws:123456789012:us-east-1"},
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want durable write error")
	}

	var rm metricdata.ResourceMetrics
	if collectErr := reader.Collect(context.Background(), &rm); collectErr != nil {
		t.Fatalf("Collect() error = %v", collectErr)
	}
	for _, name := range []string{
		"eshu_dp_correlation_rule_matches_total",
		"eshu_dp_correlation_orphan_detected_total",
		"eshu_dp_correlation_unmanaged_detected_total",
	} {
		if got := counterTotal(rm, name); got != 0 {
			t.Fatalf("%s total = %d, want 0 before durable write succeeds", name, got)
		}
	}
}

func TestAWSCloudRuntimeDriftHandlerRequiresAdapters(t *testing.T) {
	t.Parallel()

	intent := Intent{
		IntentID:     "intent-aws-drift",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "generation-aws",
		SourceSystem: "aws",
		Domain:       DomainAWSCloudRuntimeDrift,
	}
	if _, err := (AWSCloudRuntimeDriftHandler{}).Handle(context.Background(), intent); err == nil {
		t.Fatal("Handle() error = nil, want missing evidence loader error")
	}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader: &stubAWSCloudRuntimeDriftEvidenceLoader{},
	}
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("Handle() error = nil, want missing writer error")
	}
}

func TestAWSCloudRuntimeDriftHandlerRejectsWrongDomain(t *testing.T) {
	t.Parallel()

	_, err := AWSCloudRuntimeDriftHandler{}.Handle(context.Background(), Intent{
		IntentID:     "intent-aws-drift",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "generation-aws",
		SourceSystem: "aws",
		Domain:       DomainWorkloadIdentity,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want wrong-domain error")
	}
}

func TestPostgresAWSCloudRuntimeDriftWriterRequiresDatabase(t *testing.T) {
	t.Parallel()

	_, err := PostgresAWSCloudRuntimeDriftWriter{}.WriteAWSCloudRuntimeDriftFindings(
		context.Background(),
		AWSCloudRuntimeDriftWrite{IntentID: "intent-aws-drift"},
	)
	if err == nil {
		t.Fatal("WriteAWSCloudRuntimeDriftFindings() error = nil, want missing database error")
	}
}

func TestPostgresAWSCloudRuntimeDriftWriterPersistsOneFactPerFinding(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 14, 12, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresAWSCloudRuntimeDriftWriter{
		DB:  db,
		Now: func() time.Time { return now },
	}
	candidates := []model.Candidate{
		{
			ID:             "aws_cloud_runtime_drift:arn:aws:lambda:us-east-1:123456789012:function:orphan:orphaned_cloud_resource",
			Kind:           rules.AWSCloudRuntimeDriftPackName,
			CorrelationKey: "arn:aws:lambda:us-east-1:123456789012:function:orphan",
			Confidence:     1,
			State:          model.CandidateStateAdmitted,
			Evidence: []model.EvidenceAtom{
				{
					ID:           "candidate/arn",
					SourceSystem: "reducer/aws_cloud_runtime_drift",
					EvidenceType: cloudruntime.EvidenceTypeCloudResourceARN,
					ScopeID:      "aws:123456789012:us-east-1",
					Key:          "arn",
					Value:        "arn:aws:lambda:us-east-1:123456789012:function:orphan",
					Confidence:   1,
				},
				{
					ID:           "candidate/finding_kind",
					SourceSystem: "reducer/aws_cloud_runtime_drift",
					EvidenceType: cloudruntime.EvidenceTypeFindingKind,
					ScopeID:      "aws:123456789012:us-east-1",
					Key:          "finding_kind",
					Value:        string(cloudruntime.FindingKindOrphanedCloudResource),
					Confidence:   1,
				},
			},
		},
		{
			ID:             "aws_cloud_runtime_drift:arn:aws:lambda:us-east-1:123456789012:function:unmanaged:unmanaged_cloud_resource",
			Kind:           rules.AWSCloudRuntimeDriftPackName,
			CorrelationKey: "arn:aws:lambda:us-east-1:123456789012:function:unmanaged",
			Confidence:     1,
			State:          model.CandidateStateAdmitted,
			Evidence: []model.EvidenceAtom{
				{
					ID:           "candidate/finding_kind",
					SourceSystem: "reducer/aws_cloud_runtime_drift",
					EvidenceType: cloudruntime.EvidenceTypeFindingKind,
					ScopeID:      "aws:123456789012:us-east-1",
					Key:          "finding_kind",
					Value:        string(cloudruntime.FindingKindUnmanagedCloudResource),
					Confidence:   1,
				},
				{
					ID:           "candidate/state",
					SourceSystem: "reducer/aws_cloud_runtime_drift",
					EvidenceType: cloudruntime.EvidenceTypeStateResource,
					ScopeID:      "state_snapshot:s3:hash",
					Key:          "resource_address",
					Value:        "aws_lambda_function.unmanaged",
					Confidence:   1,
				},
			},
		},
	}

	result, err := writer.WriteAWSCloudRuntimeDriftFindings(context.Background(), AWSCloudRuntimeDriftWrite{
		IntentID:     "intent-aws-drift",
		ScopeID:      "aws:123456789012:us-east-1",
		GenerationID: "generation-aws",
		SourceSystem: "aws",
		Cause:        "aws runtime facts observed",
		Candidates:   candidates,
		Summary: cloudruntime.Summary{
			OrphanedResources:  1,
			UnmanagedResources: 1,
		},
	})
	if err != nil {
		t.Fatalf("WriteAWSCloudRuntimeDriftFindings() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("ExecContext calls = %d, want %d", got, want)
	}
	if db.execs[0].args[0] == db.execs[1].args[0] {
		t.Fatalf("fact ids must differ for multiple findings: %v", db.execs[0].args[0])
	}
	if got, want := db.execs[0].args[3], awsCloudRuntimeDriftFactKind; got != want {
		t.Fatalf("fact_kind = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[6], facts.SourceConfidenceInferred; got != want {
		t.Fatalf("source_confidence = %v, want %v", got, want)
	}

	payloadBytes, ok := db.execs[0].args[14].([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", db.execs[0].args[14])
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got, want := payload["finding_kind"], string(cloudruntime.FindingKindOrphanedCloudResource); got != want {
		t.Fatalf("payload finding_kind = %#v, want %q", got, want)
	}
	if got, want := payload["canonical_id"], result.CanonicalIDs[0]; got != want {
		t.Fatalf("payload canonical_id = %#v, want %q", got, want)
	}

	payloadBytes, ok = db.execs[1].args[14].([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", db.execs[1].args[14])
	}
	payload = map[string]any{}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("unmarshal unmanaged payload: %v", err)
	}
	if got, want := payload["management_status"], cloudruntime.ManagementStatusTerraformStateOnly; got != want {
		t.Fatalf("unmanaged management_status = %#v, want %q", got, want)
	}
	if got, want := payload["matched_terraform_state_address"], "aws_lambda_function.unmanaged"; got != want {
		t.Fatalf("matched_terraform_state_address = %#v, want %q", got, want)
	}
	if got := stringSliceFromAny(payload["missing_evidence"]); !slices.Equal(got, []string{"terraform_config_resource"}) {
		t.Fatalf("missing_evidence = %#v, want terraform_config_resource", got)
	}
}

func stringSliceFromAny(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if ok {
			out = append(out, text)
		}
	}
	return out
}
