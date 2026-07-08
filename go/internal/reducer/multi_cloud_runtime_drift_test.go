// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/correlation/drift/multicloud"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	gcpOrphanID  = "//compute.googleapis.com/projects/proj/zones/z/instances/orphan"
	azureUnmgdID = "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/unmanaged"
)

type stubMultiCloudRuntimeDriftEvidenceLoader struct {
	rows  []multicloud.Row
	calls int
}

func (s *stubMultiCloudRuntimeDriftEvidenceLoader) LoadMultiCloudRuntimeDriftEvidence(
	context.Context,
	string,
	string,
) ([]multicloud.Row, error) {
	s.calls++
	return append([]multicloud.Row(nil), s.rows...), nil
}

type stubMultiCloudRuntimeDriftFindingWriter struct {
	write MultiCloudRuntimeDriftWrite
	err   error
	calls int
}

func (s *stubMultiCloudRuntimeDriftFindingWriter) WriteMultiCloudRuntimeDriftFindings(
	_ context.Context,
	write MultiCloudRuntimeDriftWrite,
) (MultiCloudRuntimeDriftWriteResult, error) {
	s.calls++
	s.write = write
	if s.err != nil {
		return MultiCloudRuntimeDriftWriteResult{}, s.err
	}
	return MultiCloudRuntimeDriftWriteResult{
		CanonicalWrites: len(write.Candidates),
		EvidenceSummary: "wrote multi cloud runtime drift findings",
	}, nil
}

func newMultiCloudRuntimeDriftInstruments(t *testing.T) (*telemetry.Instruments, sdkmetric.Reader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

func gcpAndAzureDriftRows() []multicloud.Row {
	return []multicloud.Row{
		{
			Provider:    cloudinventory.ProviderGCP,
			RawIdentity: gcpOrphanID,
			ScopeID:     "gcp:proj:z",
			Cloud:       &cloudruntime.ResourceRow{ARN: gcpOrphanID, ScopeID: "gcp:proj:z"},
		},
		{
			Provider:    cloudinventory.ProviderAzure,
			RawIdentity: azureUnmgdID,
			ScopeID:     "azure:sub:rg",
			Cloud:       &cloudruntime.ResourceRow{ARN: azureUnmgdID, ScopeID: "azure:sub:rg"},
			State:       &cloudruntime.ResourceRow{ARN: azureUnmgdID, Address: "azurerm_storage_account.unmanaged", ScopeID: "state:azure"},
		},
	}
}

func multiCloudDriftIntent() Intent {
	return Intent{
		IntentID:        "intent-multi-drift",
		ScopeID:         "multi:tenant",
		GenerationID:    "generation-multi",
		SourceSystem:    "gcp",
		Domain:          DomainMultiCloudRuntimeDrift,
		Cause:           "provider runtime facts observed",
		RelatedScopeIDs: []string{"gcp:proj:z", "azure:sub:rg"},
	}
}

func TestMultiCloudRuntimeDriftHandlerPublishesGCPAndAzureFindings(t *testing.T) {
	t.Parallel()

	inst, reader := newMultiCloudRuntimeDriftInstruments(t)
	loader := &stubMultiCloudRuntimeDriftEvidenceLoader{rows: gcpAndAzureDriftRows()}
	writer := &stubMultiCloudRuntimeDriftFindingWriter{}
	handler := MultiCloudRuntimeDriftHandler{EvidenceLoader: loader, Writer: writer, Instruments: inst}

	result, err := handler.Handle(context.Background(), multiCloudDriftIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("Handle().CanonicalWrites = %d, want %d", got, want)
	}
	for _, want := range []string{"evaluated=2", "orphaned=1", "unmanaged=1", "canonical_writes=2"} {
		if !strings.Contains(result.EvidenceSummary, want) {
			t.Fatalf("Handle().EvidenceSummary = %q, missing %q", result.EvidenceSummary, want)
		}
	}
	if got, want := len(writer.write.Candidates), 2; got != want {
		t.Fatalf("writer candidates = %d, want %d", got, want)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := reducerCounterValue(t, rm, "eshu_dp_correlation_orphan_detected_total", map[string]string{
		telemetry.MetricDimensionPack: rules.MultiCloudRuntimeDriftPackName,
		telemetry.MetricDimensionRule: rules.MultiCloudRuntimeDriftRuleAdmitFinding,
	}); got != 1 {
		t.Fatalf("orphan detected counter = %d, want 1", got)
	}
	if got := reducerCounterValue(t, rm, "eshu_dp_correlation_unmanaged_detected_total", map[string]string{
		telemetry.MetricDimensionPack: rules.MultiCloudRuntimeDriftPackName,
		telemetry.MetricDimensionRule: rules.MultiCloudRuntimeDriftRuleAdmitFinding,
	}); got != 1 {
		t.Fatalf("unmanaged detected counter = %d, want 1", got)
	}
}

func TestMultiCloudRuntimeDriftHandlerDoesNotEmitBeforeDurableWrite(t *testing.T) {
	t.Parallel()

	inst, reader := newMultiCloudRuntimeDriftInstruments(t)
	loader := &stubMultiCloudRuntimeDriftEvidenceLoader{rows: gcpAndAzureDriftRows()[:1]}
	handler := MultiCloudRuntimeDriftHandler{
		EvidenceLoader: loader,
		Writer:         &stubMultiCloudRuntimeDriftFindingWriter{err: errors.New("database unavailable")},
		Instruments:    inst,
	}

	if _, err := handler.Handle(context.Background(), multiCloudDriftIntent()); err == nil {
		t.Fatal("Handle() error = nil, want durable write error")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
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

func TestMultiCloudRuntimeDriftHandlerRedactsAdmittedFindingLogs(t *testing.T) {
	t.Parallel()

	raw := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.KeyVault/vaults/prod-payments-secret-vault"
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	loader := &stubMultiCloudRuntimeDriftEvidenceLoader{rows: []multicloud.Row{{
		Provider:    cloudinventory.ProviderAzure,
		RawIdentity: raw,
		ScopeID:     "azure:sub:rg",
		Cloud:       &cloudruntime.ResourceRow{ARN: raw, ResourceID: "prod-payments-secret-vault", ScopeID: "azure:sub:rg"},
	}}}
	handler := MultiCloudRuntimeDriftHandler{
		EvidenceLoader: loader,
		Writer:         &stubMultiCloudRuntimeDriftFindingWriter{},
		Logger:         logger,
	}

	if _, err := handler.Handle(context.Background(), multiCloudDriftIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	logText := logs.String()
	for _, leaked := range []string{raw, "prod-payments-secret-vault"} {
		if strings.Contains(logText, leaked) {
			t.Fatalf("admitted finding log leaked %q:\n%s", leaked, logText)
		}
	}
	if !strings.Contains(logText, "drift.provider") || !strings.Contains(logText, cloudinventory.ProviderAzure) {
		t.Fatalf("admitted finding log missing bounded provider label:\n%s", logText)
	}
}

func TestMultiCloudRuntimeDriftHandlerRequiresAdapters(t *testing.T) {
	t.Parallel()

	intent := multiCloudDriftIntent()
	if _, err := (MultiCloudRuntimeDriftHandler{}).Handle(context.Background(), intent); err == nil {
		t.Fatal("Handle() error = nil, want missing evidence loader error")
	}
	handler := MultiCloudRuntimeDriftHandler{EvidenceLoader: &stubMultiCloudRuntimeDriftEvidenceLoader{}}
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("Handle() error = nil, want missing writer error")
	}
}

func TestMultiCloudRuntimeDriftHandlerRejectsWrongDomain(t *testing.T) {
	t.Parallel()

	_, err := MultiCloudRuntimeDriftHandler{}.Handle(context.Background(), Intent{
		IntentID:     "intent-multi-drift",
		ScopeID:      "multi:tenant",
		GenerationID: "generation-multi",
		Domain:       DomainWorkloadIdentity,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want wrong-domain error")
	}
}

func TestPostgresMultiCloudRuntimeDriftWriterRequiresDatabase(t *testing.T) {
	t.Parallel()

	_, err := PostgresMultiCloudRuntimeDriftWriter{}.WriteMultiCloudRuntimeDriftFindings(
		context.Background(),
		MultiCloudRuntimeDriftWrite{IntentID: "intent-multi-drift"},
	)
	if err == nil {
		t.Fatal("WriteMultiCloudRuntimeDriftFindings() error = nil, want missing database error")
	}
}

func buildAdmittedMultiCloudWrite() MultiCloudRuntimeDriftWrite {
	candidates := multicloud.BuildCandidates(gcpAndAzureDriftRows(), "multi:tenant")
	for i := range candidates {
		candidates[i].State = model.CandidateStateAdmitted
	}
	return MultiCloudRuntimeDriftWrite{
		IntentID:     "intent-multi-drift",
		ScopeID:      "multi:tenant",
		GenerationID: "generation-multi",
		SourceSystem: "gcp",
		Cause:        "provider runtime facts observed",
		Candidates:   candidates,
		Summary:      multicloud.Summary{OrphanedResources: 1, UnmanagedResources: 1},
	}
}

func TestPostgresMultiCloudRuntimeDriftWriterPersistsOneFactPerFinding(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 14, 12, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresMultiCloudRuntimeDriftWriter{DB: db, Now: func() time.Time { return now }}

	write := buildAdmittedMultiCloudWrite()
	result, err := writer.WriteMultiCloudRuntimeDriftFindings(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteMultiCloudRuntimeDriftFindings() error = %v, want nil", err)
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
	if got, want := db.execs[0].args[3], multiCloudRuntimeDriftFactKind; got != want {
		t.Fatalf("fact_kind = %v, want %v", got, want)
	}
	if !strings.Contains(db.execs[0].query, "schema_version") {
		t.Fatalf("insert query missing schema_version column for governed reducer fact: %s", db.execs[0].query)
	}
	if got, want := db.execs[0].args[5], "1.0.0"; got != want {
		t.Fatalf("schema_version = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[7], facts.SourceConfidenceInferred; got != want {
		t.Fatalf("source_confidence = %v, want %v", got, want)
	}

	// Payload must carry the canonical uid and provider, and must not invent a
	// config layer for the unmanaged finding.
	var foundUnmanaged bool
	for _, call := range db.execs {
		payloadBytes, ok := call.args[15].([]byte)
		if !ok {
			t.Fatalf("payload arg type = %T, want []byte", call.args[15])
		}
		var payload map[string]any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload["cloud_resource_uid"] == "" {
			t.Fatalf("payload missing cloud_resource_uid: %v", payload)
		}
		if payload["finding_kind"] == string(cloudruntime.FindingKindUnmanagedCloudResource) {
			foundUnmanaged = true
			if got, want := payload["management_status"], cloudruntime.ManagementStatusTerraformStateOnly; got != want {
				t.Fatalf("unmanaged management_status = %#v, want %q", got, want)
			}
			if got, want := payload["matched_terraform_state_address"], "azurerm_storage_account.unmanaged"; got != want {
				t.Fatalf("matched_terraform_state_address = %#v, want %q", got, want)
			}
			if got, want := payload["provider"], cloudinventory.ProviderAzure; got != want {
				t.Fatalf("provider = %#v, want %q", got, want)
			}
		}
	}
	if !foundUnmanaged {
		t.Fatal("expected one unmanaged finding payload")
	}
}

func TestPostgresMultiCloudRuntimeDriftWriterIsIdempotentAcrossReplays(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 14, 12, 0, 0, 0, time.UTC)
	writer := PostgresMultiCloudRuntimeDriftWriter{Now: func() time.Time { return now }}

	first := &fakeWorkloadIdentityExecer{}
	writer.DB = first
	if _, err := writer.WriteMultiCloudRuntimeDriftFindings(context.Background(), buildAdmittedMultiCloudWrite()); err != nil {
		t.Fatalf("first write error = %v", err)
	}
	second := &fakeWorkloadIdentityExecer{}
	writer.DB = second
	if _, err := writer.WriteMultiCloudRuntimeDriftFindings(context.Background(), buildAdmittedMultiCloudWrite()); err != nil {
		t.Fatalf("replay write error = %v", err)
	}
	if len(first.execs) != len(second.execs) {
		t.Fatalf("replay exec count = %d, want %d", len(second.execs), len(first.execs))
	}
	for i := range first.execs {
		if first.execs[i].args[0] != second.execs[i].args[0] {
			t.Fatalf("replay fact id[%d] = %v, want stable %v", i, second.execs[i].args[0], first.execs[i].args[0])
		}
		if first.execs[i].args[4] != second.execs[i].args[4] {
			t.Fatalf("replay stable_fact_key[%d] drifted: %v vs %v", i, first.execs[i].args[4], second.execs[i].args[4])
		}
	}
}

func TestMultiCloudRuntimeDriftHandlerConcurrentInvocationsAreStable(t *testing.T) {
	t.Parallel()

	const workers = 8
	var wg sync.WaitGroup
	keys := make([]string, workers)
	var mu sync.Mutex
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			writer := &stubMultiCloudRuntimeDriftFindingWriter{}
			handler := MultiCloudRuntimeDriftHandler{
				EvidenceLoader: &stubMultiCloudRuntimeDriftEvidenceLoader{rows: gcpAndAzureDriftRows()},
				Writer:         writer,
			}
			if _, err := handler.Handle(context.Background(), multiCloudDriftIntent()); err != nil {
				t.Errorf("concurrent Handle() error = %v", err)
				return
			}
			mu.Lock()
			if len(writer.write.Candidates) > 0 {
				keys[idx] = writer.write.Candidates[0].CorrelationKey
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	for i := 1; i < workers; i++ {
		if keys[i] != keys[0] {
			t.Fatalf("concurrent worker %d key = %q, want stable %q", i, keys[i], keys[0])
		}
	}
}
