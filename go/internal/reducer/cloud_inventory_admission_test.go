// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type stubCloudInventoryEvidenceLoader struct {
	records []CloudInventoryRecord
	err     error
	calls   int
}

func (s *stubCloudInventoryEvidenceLoader) LoadCloudInventoryEvidence(
	context.Context,
	string,
	string,
) ([]CloudInventoryRecord, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return append([]CloudInventoryRecord(nil), s.records...), nil
}

type stubCloudInventoryAdmissionWriter struct {
	writes []CloudInventoryAdmissionWrite
	err    error
}

func (s *stubCloudInventoryAdmissionWriter) WriteCloudInventoryAdmission(
	_ context.Context,
	write CloudInventoryAdmissionWrite,
) (CloudInventoryAdmissionWriteResult, error) {
	if s.err != nil {
		return CloudInventoryAdmissionWriteResult{}, s.err
	}
	s.writes = append(s.writes, write)
	return CloudInventoryAdmissionWriteResult{
		CanonicalWrites: len(write.Resources),
		EvidenceSummary: "stub admission write",
	}, nil
}

func newCloudInventoryInstruments(t *testing.T) (*telemetry.Instruments, sdkmetric.Reader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

func cloudInventoryIntent() Intent {
	return Intent{
		IntentID:        "intent-cloud-inventory",
		ScopeID:         "gcp:org:eshu:project:prod",
		GenerationID:    "generation-1",
		SourceSystem:    "gcp",
		Domain:          DomainCloudInventoryAdmission,
		Cause:           "cloud inventory facts observed",
		RelatedScopeIDs: []string{"gcp:org:eshu:project:prod"},
	}
}

func TestCloudInventoryAdmissionAdmitsGCPAndAzureIdentities(t *testing.T) {
	t.Parallel()

	inst, reader := newCloudInventoryInstruments(t)
	loader := &stubCloudInventoryEvidenceLoader{records: []CloudInventoryRecord{
		{
			Provider:     cloudinventory.ProviderGCP,
			FactKind:     "gcp_cloud_resource",
			RawIdentity:  "//compute.googleapis.com/projects/eshu-prod/zones/us-central1-a/instances/api-1",
			ResourceType: "compute.googleapis.com/Instance",
			SourceLayer:  SourceLayerObserved,
		},
		{
			Provider:     cloudinventory.ProviderAzure,
			FactKind:     "azure_cloud_resource",
			RawIdentity:  "/subscriptions/0000/resourceGroups/rg-prod/providers/Microsoft.Compute/virtualMachines/api-1",
			ResourceType: "Microsoft.Compute/virtualMachines",
			SourceLayer:  SourceLayerObserved,
		},
	}}
	writer := &stubCloudInventoryAdmissionWriter{}
	handler := CloudInventoryAdmissionHandler{
		EvidenceLoader: loader,
		Writer:         writer,
		Instruments:    inst,
	}

	result, err := handler.Handle(context.Background(), cloudInventoryIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if got, want := result.CanonicalWrites, 2; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if len(writer.writes) != 1 {
		t.Fatalf("writer writes = %d, want 1", len(writer.writes))
	}
	if got, want := len(writer.writes[0].Resources), 2; got != want {
		t.Fatalf("admitted resources = %d, want %d", got, want)
	}
	for _, res := range writer.writes[0].Resources {
		if res.CloudResourceUID == "" {
			t.Fatalf("admitted resource %q has empty uid", res.RawIdentity)
		}
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := counterTotal(rm, "eshu_dp_cloud_inventory_admissions_total"); got != 2 {
		t.Fatalf("admissions counter total = %d, want 2", got)
	}
}

func TestCloudInventoryAdmissionCountsAmbiguousUnsupportedUnresolved(t *testing.T) {
	t.Parallel()

	inst, reader := newCloudInventoryInstruments(t)
	loader := &stubCloudInventoryEvidenceLoader{records: []CloudInventoryRecord{
		{
			Provider:    cloudinventory.ProviderAzure,
			FactKind:    "azure_cloud_resource",
			RawIdentity: "resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/api", // no /subscriptions/ prefix
			SourceLayer: SourceLayerObserved,
		},
		{
			Provider:    "oraclecloud",
			FactKind:    "oci_cloud_resource",
			RawIdentity: "ocid1.instance.oc1..abc",
			SourceLayer: SourceLayerObserved,
		},
		{
			Provider:    cloudinventory.ProviderGCP,
			FactKind:    "gcp_cloud_resource",
			RawIdentity: "   ", // blank
			SourceLayer: SourceLayerObserved,
		},
	}}
	writer := &stubCloudInventoryAdmissionWriter{}
	handler := CloudInventoryAdmissionHandler{EvidenceLoader: loader, Writer: writer, Instruments: inst}

	result, err := handler.Handle(context.Background(), cloudInventoryIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got, want := result.CanonicalWrites, 0; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d (nothing fabricated)", got, want)
	}
	if len(writer.writes) != 1 {
		t.Fatalf("writer writes = %d, want 1 (summary write even with zero admits)", len(writer.writes))
	}
	summary := writer.writes[0].Summary
	if summary.Ambiguous != 1 || summary.Unsupported != 1 || summary.Unresolved != 1 {
		t.Fatalf("summary = %+v, want ambiguous=1 unsupported=1 unresolved=1", summary)
	}
	if len(writer.writes[0].Resources) != 0 {
		t.Fatalf("admitted resources = %d, want 0", len(writer.writes[0].Resources))
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := counterTotal(rm, "eshu_dp_cloud_inventory_admissions_total"); got != 3 {
		t.Fatalf("admissions counter total = %d, want 3", got)
	}
}

func TestCloudInventoryAdmissionObservedDoesNotOverwriteDeclared(t *testing.T) {
	t.Parallel()

	const raw = "//compute.googleapis.com/projects/eshu-prod/zones/us-central1-a/instances/api-1"
	loader := &stubCloudInventoryEvidenceLoader{records: []CloudInventoryRecord{
		{
			Provider:     cloudinventory.ProviderGCP,
			FactKind:     "gcp_cloud_resource",
			RawIdentity:  raw,
			ResourceType: "compute.googleapis.com/Instance",
			SourceLayer:  SourceLayerObserved,
		},
		{
			Provider:     cloudinventory.ProviderGCP,
			FactKind:     "terraform_managed_resource",
			RawIdentity:  raw,
			ResourceType: "compute.googleapis.com/Instance",
			SourceLayer:  SourceLayerDeclared,
		},
	}}
	writer := &stubCloudInventoryAdmissionWriter{}
	handler := CloudInventoryAdmissionHandler{EvidenceLoader: loader, Writer: writer}

	if _, err := handler.Handle(context.Background(), cloudInventoryIntent()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(writer.writes) != 1 || len(writer.writes[0].Resources) != 1 {
		t.Fatalf("want exactly one admitted canonical row for the shared uid, got %d writes", len(writer.writes))
	}
	res := writer.writes[0].Resources[0]
	if res.ManagementOrigin != ManagementOriginDeclared {
		t.Fatalf("ManagementOrigin = %q, want %q (declared must win over observed)", res.ManagementOrigin, ManagementOriginDeclared)
	}
	if !res.HasObservedEvidence {
		t.Fatalf("admitted row must still record the observed evidence layer")
	}
	if !res.HasDeclaredEvidence {
		t.Fatalf("admitted row must record the declared evidence layer")
	}
}

func TestCloudInventoryAdmissionEmptyInput(t *testing.T) {
	t.Parallel()

	loader := &stubCloudInventoryEvidenceLoader{}
	writer := &stubCloudInventoryAdmissionWriter{}
	handler := CloudInventoryAdmissionHandler{EvidenceLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), cloudInventoryIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 on empty input", result.CanonicalWrites)
	}
	if len(writer.writes) != 1 || len(writer.writes[0].Resources) != 0 {
		t.Fatalf("empty input should still publish a zero-resource summary write")
	}
}

func TestCloudInventoryAdmissionStaleGenerationSuperseded(t *testing.T) {
	t.Parallel()

	loader := &stubCloudInventoryEvidenceLoader{records: []CloudInventoryRecord{{
		Provider:    cloudinventory.ProviderGCP,
		FactKind:    "gcp_cloud_resource",
		RawIdentity: "//compute.googleapis.com/projects/p/zones/z/instances/i",
		SourceLayer: SourceLayerObserved,
	}}}
	identityLoader := &stubCloudIdentityPolicyEvidenceLoader{records: []CloudIdentityPolicyEvidenceRecord{{
		Provider:             cloudinventory.ProviderGCP,
		RawIdentity:          "//compute.googleapis.com/projects/p/zones/z/instances/i",
		EvidenceKey:          "identity-stable-stale",
		IdentityType:         "system_assigned",
		PrincipalFingerprint: "principal-marker",
	}}}
	writer := &stubCloudInventoryAdmissionWriter{}
	handler := CloudInventoryAdmissionHandler{
		EvidenceLoader:               loader,
		Writer:                       writer,
		IdentityPolicyEvidenceLoader: identityLoader,
		GenerationCheck: func(context.Context, string, string) (bool, error) {
			return false, nil // superseded
		},
	}

	result, err := handler.Handle(context.Background(), cloudInventoryIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != ResultStatusSuperseded {
		t.Fatalf("Status = %q, want %q for a stale generation", result.Status, ResultStatusSuperseded)
	}
	if len(writer.writes) != 0 {
		t.Fatalf("stale generation must not write canonical rows, got %d writes", len(writer.writes))
	}
	if loader.calls != 0 {
		t.Fatalf("stale generation must short-circuit before loading evidence, calls = %d", loader.calls)
	}
	if identityLoader.calls != 0 {
		t.Fatalf("stale generation must short-circuit before loading identity evidence, calls = %d", identityLoader.calls)
	}
}

func TestCloudInventoryAdmissionDoesNotWriteOnLoadError(t *testing.T) {
	t.Parallel()

	loader := &stubCloudInventoryEvidenceLoader{err: errors.New("postgres unavailable")}
	writer := &stubCloudInventoryAdmissionWriter{}
	handler := CloudInventoryAdmissionHandler{EvidenceLoader: loader, Writer: writer}

	if _, err := handler.Handle(context.Background(), cloudInventoryIntent()); err == nil {
		t.Fatal("Handle() error = nil, want load error")
	}
	if len(writer.writes) != 0 {
		t.Fatalf("load failure must not publish, got %d writes", len(writer.writes))
	}
}

func TestCloudInventoryAdmissionRejectsWrongDomain(t *testing.T) {
	t.Parallel()

	handler := CloudInventoryAdmissionHandler{
		EvidenceLoader: &stubCloudInventoryEvidenceLoader{},
		Writer:         &stubCloudInventoryAdmissionWriter{},
	}
	intent := cloudInventoryIntent()
	intent.Domain = DomainWorkloadIdentity
	if _, err := handler.Handle(context.Background(), intent); err == nil {
		t.Fatal("Handle() error = nil, want wrong-domain error")
	}
}

func TestCloudInventoryAdmissionRequiresAdapters(t *testing.T) {
	t.Parallel()

	intent := cloudInventoryIntent()
	if _, err := (CloudInventoryAdmissionHandler{}).Handle(context.Background(), intent); err == nil {
		t.Fatal("Handle() error = nil, want missing evidence loader error")
	}
	if _, err := (CloudInventoryAdmissionHandler{EvidenceLoader: &stubCloudInventoryEvidenceLoader{}}).Handle(context.Background(), intent); err == nil {
		t.Fatal("Handle() error = nil, want missing writer error")
	}
}
