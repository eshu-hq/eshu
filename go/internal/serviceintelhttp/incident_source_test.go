// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package serviceintelhttp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// fakeIncidentSource records the workload id it was asked about and returns a
// canned result, so the report-composition seam can be tested without a graph.
type fakeIncidentSource struct {
	records     []serviceintel.IncidentRecord
	err         error
	gotWorkload string
	calls       int
}

func (f *fakeIncidentSource) IncidentRecordsForWorkload(_ context.Context, workloadID string) ([]serviceintel.IncidentRecord, error) {
	f.calls++
	f.gotWorkload = workloadID
	return f.records, f.err
}

func incidentTestDossier() map[string]any {
	return map[string]any{
		"service_identity": map[string]any{
			"service_id":   "workload:checkout",
			"service_name": "checkout",
		},
	}
}

func storyTruth() *query.TruthEnvelope {
	return &query.TruthEnvelope{Level: query.TruthLevelDerived, Profile: query.ProfileProduction}
}

func incidentSectionInput(t *testing.T, in serviceintel.ReportInput) (serviceintel.SectionInput, bool) {
	t.Helper()
	for _, section := range in.Sections {
		if section.Kind == serviceintel.SectionIncidentsSupport {
			return section, true
		}
	}
	return serviceintel.SectionInput{}, false
}

func sectionByKind(t *testing.T, report serviceintel.Report, kind serviceintel.SectionKind) serviceintel.ReportSection {
	t.Helper()
	for _, section := range report.Sections {
		if section.Kind == kind {
			return section
		}
	}
	t.Fatalf("section %q not present in report", kind)
	return serviceintel.ReportSection{}
}

func TestBuildReportInputNilSourceLeavesIncidentsUnsupplied(t *testing.T) {
	t.Parallel()

	in := buildReportInput(context.Background(), incidentTestDossier(), storyTruth(), nil)
	if _, ok := incidentSectionInput(t, in); ok {
		t.Fatal("nil incident source must not supply an incidents section input")
	}
	// Compose still surfaces the section as unsupported with its fallback.
	report := serviceintel.Compose(in)
	inc := sectionByKind(t, report, serviceintel.SectionIncidentsSupport)
	if inc.Status != serviceintel.StatusUnsupported {
		t.Fatalf("unsupplied incidents section status = %q, want unsupported", inc.Status)
	}
}

func TestBuildReportInputAppendsIncidentsWhenSourced(t *testing.T) {
	t.Parallel()

	source := &fakeIncidentSource{records: []serviceintel.IncidentRecord{
		{Provider: "pagerduty", ProviderIncidentID: "INC-1", TruthLabel: "exact"},
	}}
	in := buildReportInput(context.Background(), incidentTestDossier(), storyTruth(), source)

	if source.gotWorkload != "workload:checkout" {
		t.Fatalf("source asked about %q, want the dossier workload id", source.gotWorkload)
	}
	section, ok := incidentSectionInput(t, in)
	if !ok {
		t.Fatal("a sourced incident must supply an incidents section input")
	}
	if section.NoEvidence {
		t.Fatal("a sourced incident must not be no-evidence")
	}
	// Codex #2: the incident section truth must be incident-context truth, not the
	// service-story platform truth.
	if section.Truth == nil || section.Truth.Capability != incidentContextCapability {
		t.Fatalf("incident section truth = %#v, want %q capability", section.Truth, incidentContextCapability)
	}

	report := serviceintel.Compose(in)
	inc := sectionByKind(t, report, serviceintel.SectionIncidentsSupport)
	if inc.Status == serviceintel.StatusUnsupported {
		t.Fatalf("sourced incidents section must not be unsupported, got %q", inc.Status)
	}
}

func TestBuildReportInputUnresolvedLeavesUnsupplied(t *testing.T) {
	t.Parallel()

	source := &fakeIncidentSource{records: nil}
	in := buildReportInput(context.Background(), incidentTestDossier(), storyTruth(), source)
	if _, ok := incidentSectionInput(t, in); ok {
		t.Fatal("no incident records must leave the section unsupplied (honest: not a false 'no incidents')")
	}
}

func TestBuildReportInputBlankWorkloadDoesNotQuery(t *testing.T) {
	t.Parallel()

	source := &fakeIncidentSource{records: []serviceintel.IncidentRecord{{Provider: "pagerduty", ProviderIncidentID: "INC-1"}}}
	dossier := map[string]any{"service_identity": map[string]any{"service_name": "checkout"}}
	in := buildReportInput(context.Background(), dossier, storyTruth(), source)
	if source.calls != 0 {
		t.Fatalf("blank workload id must not query the incident source, calls = %d", source.calls)
	}
	if _, ok := incidentSectionInput(t, in); ok {
		t.Fatal("blank workload id must leave the incidents section unsupplied")
	}
}

func TestBuildReportInputSourceErrorLeavesUnsupplied(t *testing.T) {
	t.Parallel()

	source := &fakeIncidentSource{err: errors.New("connection reset")}
	in := buildReportInput(context.Background(), incidentTestDossier(), storyTruth(), source)
	if _, ok := incidentSectionInput(t, in); ok {
		t.Fatal("a source error must leave the incidents section unsupplied, never report-fatal")
	}
	// Compose still surfaces the section as unsupported with its fallback rather
	// than leaking the error as a confident section.
	report := serviceintel.Compose(in)
	inc := sectionByKind(t, report, serviceintel.SectionIncidentsSupport)
	if inc.Status != serviceintel.StatusUnsupported {
		t.Fatalf("incidents section after source error = %q, want unsupported", inc.Status)
	}
}

// --- DurableIncidentEvidenceSource ---

type fakeCatalogResolver struct {
	id  string
	err error
}

func (f fakeCatalogResolver) ResolveCatalogServiceID(context.Context, string) (string, error) {
	return f.id, f.err
}

type fakeEvidenceLoader struct {
	byService map[string][]reducer.ServiceIncidentRecord
	err       error
	gotIDs    []string
	gotLimit  int
}

func (f *fakeEvidenceLoader) GetIncidentEvidenceForServicesBounded(_ context.Context, serviceIDs []string, rowLimit int) (map[string][]reducer.ServiceIncidentRecord, error) {
	f.gotIDs = serviceIDs
	f.gotLimit = rowLimit
	return f.byService, f.err
}

func TestDurableIncidentEvidenceSourceResolvesLoadsAndMaps(t *testing.T) {
	t.Parallel()

	loader := &fakeEvidenceLoader{byService: map[string][]reducer.ServiceIncidentRecord{
		"component:default/checkout": {
			{Provider: "pagerduty", ProviderIncidentID: "INC-1", TruthLabel: "exact"},
			{Provider: "pagerduty", ProviderIncidentID: "INC-2", TruthLabel: "exact"},
		},
	}}
	source := NewDurableIncidentEvidenceSource(
		fakeCatalogResolver{id: "component:default/checkout"}, loader, nil,
	)

	records, err := source.IncidentRecordsForWorkload(context.Background(), "workload:checkout")
	if err != nil {
		t.Fatalf("IncidentRecordsForWorkload() error = %v, want nil", err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2 mapped incident records", len(records))
	}
	if records[0].Provider != "pagerduty" || records[0].ProviderIncidentID != "INC-1" || records[0].TruthLabel != "exact" {
		t.Fatalf("mapped record[0] = %#v, want pagerduty/INC-1/exact", records[0])
	}
	if len(loader.gotIDs) != 1 || loader.gotIDs[0] != "component:default/checkout" {
		t.Fatalf("loader queried %v, want the resolved catalog service id", loader.gotIDs)
	}
	if loader.gotLimit != reportIncidentEvidenceRowLimit {
		t.Fatalf("loader row limit = %d, want the bounded report limit %d", loader.gotLimit, reportIncidentEvidenceRowLimit)
	}
}

func TestDurableIncidentEvidenceSourceUnresolvedReturnsNoRecords(t *testing.T) {
	t.Parallel()

	loader := &fakeEvidenceLoader{}
	source := NewDurableIncidentEvidenceSource(fakeCatalogResolver{id: ""}, loader, nil)

	records, err := source.IncidentRecordsForWorkload(context.Background(), "workload:orphan")
	if err != nil {
		t.Fatalf("error = %v, want nil for unresolved workload", err)
	}
	if records != nil {
		t.Fatalf("records = %v, want nil for unresolved workload", records)
	}
	if loader.gotIDs != nil {
		t.Fatal("loader must not be called when the workload does not resolve")
	}
}

func TestDurableIncidentEvidenceSourceAmbiguousIsNoRecords(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	loader := &fakeEvidenceLoader{}
	source := NewDurableIncidentEvidenceSource(
		fakeCatalogResolver{err: postgres.ErrAmbiguousCatalogService}, loader, logger,
	)

	records, err := source.IncidentRecordsForWorkload(context.Background(), "workload:checkout")
	if err != nil {
		t.Fatalf("ambiguous catalog ownership must not error, got %v", err)
	}
	if records != nil {
		t.Fatal("ambiguous catalog ownership must not attribute incidents to any service")
	}
	if loader.gotIDs != nil {
		t.Fatal("loader must not be called on ambiguous resolution")
	}
	if !bytes.Contains(buf.Bytes(), []byte("serviceintel.incident_ambiguous_catalog_service")) {
		t.Fatalf("ambiguous resolution must be logged for the operator, log = %q", buf.String())
	}
}

func TestDurableIncidentEvidenceSourceResolverErrorPropagatesAndLogs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	source := NewDurableIncidentEvidenceSource(
		fakeCatalogResolver{err: errors.New("connection reset")}, &fakeEvidenceLoader{}, logger,
	)
	if _, err := source.IncidentRecordsForWorkload(context.Background(), "workload:checkout"); err == nil {
		t.Fatal("a resolver infra error must propagate")
	}
	if !bytes.Contains(buf.Bytes(), []byte("serviceintel.incident_load_error")) ||
		!bytes.Contains(buf.Bytes(), []byte("workload:checkout")) {
		t.Fatalf("resolver infra error must be logged with the workload id, log = %q", buf.String())
	}
}

func TestDurableIncidentEvidenceSourceLoaderErrorPropagatesAndLogs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	loader := &fakeEvidenceLoader{err: errors.New("query failed")}
	source := NewDurableIncidentEvidenceSource(
		fakeCatalogResolver{id: "component:default/checkout"}, loader, logger,
	)
	if _, err := source.IncidentRecordsForWorkload(context.Background(), "workload:checkout"); err == nil {
		t.Fatal("a loader infra error must propagate")
	}
	// The load failure log carries both the workload id and the resolved catalog
	// service id — the operator's two anchors at 3 AM.
	if !bytes.Contains(buf.Bytes(), []byte("serviceintel.incident_load_error")) ||
		!bytes.Contains(buf.Bytes(), []byte("workload:checkout")) ||
		!bytes.Contains(buf.Bytes(), []byte("component:default/checkout")) {
		t.Fatalf("loader infra error must be logged with workload and catalog ids, log = %q", buf.String())
	}
}
