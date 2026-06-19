package serviceintelhttp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

// fakeSupplyChainSource records the workload id it was asked about and returns
// a canned inventory, so the report-composition seam can be tested without a
// database.
type fakeSupplyChainSource struct {
	inventory   map[string]any
	err         error
	gotWorkload string
	calls       int
}

func (f *fakeSupplyChainSource) SupplyChainInventoryForWorkload(_ context.Context, workloadID string) (map[string]any, error) {
	f.calls++
	f.gotWorkload = workloadID
	return f.inventory, f.err
}

func supplyChainSectionInput(t *testing.T, in serviceintel.ReportInput) (serviceintel.SectionInput, bool) {
	t.Helper()
	for _, section := range in.Sections {
		if section.Kind == serviceintel.SectionSupplyChain {
			return section, true
		}
	}
	return serviceintel.SectionInput{}, false
}

func TestBuildReportInputAppendsSupplyChainWhenSourced(t *testing.T) {
	t.Parallel()

	source := &fakeSupplyChainSource{inventory: map[string]any{"count": 2, "truncated": false}}
	in := buildReportInput(context.Background(), incidentTestDossier(), storyTruth(), nil, source)

	if source.gotWorkload != "workload:checkout" {
		t.Fatalf("source asked about %q, want the dossier workload id", source.gotWorkload)
	}
	section, ok := supplyChainSectionInput(t, in)
	if !ok {
		t.Fatal("a sourced inventory must supply a supply_chain section input")
	}
	if section.NoEvidence {
		t.Fatal("a sourced inventory must not be no-evidence")
	}
	if section.Truth == nil || section.Truth.Capability != supplyChainImpactAggregateCapability {
		t.Fatalf("supply-chain section truth = %#v, want %q capability", section.Truth, supplyChainImpactAggregateCapability)
	}

	report := serviceintel.Compose(in)
	supply := sectionByKind(t, report, serviceintel.SectionSupplyChain)
	if supply.Status == serviceintel.StatusUnsupported {
		t.Fatalf("sourced supply-chain section must not be unsupported, got %q", supply.Status)
	}
}

func TestBuildReportInputEmptySupplyChainLeavesUnsupplied(t *testing.T) {
	t.Parallel()

	source := &fakeSupplyChainSource{inventory: nil}
	in := buildReportInput(context.Background(), incidentTestDossier(), storyTruth(), nil, source)
	if _, ok := supplyChainSectionInput(t, in); ok {
		t.Fatal("empty inventory must leave the section unsupplied (honest: not a false supported empty section)")
	}
}

func TestBuildReportInputSupplyChainErrorLeavesUnsupplied(t *testing.T) {
	t.Parallel()

	source := &fakeSupplyChainSource{err: errors.New("connection reset")}
	in := buildReportInput(context.Background(), incidentTestDossier(), storyTruth(), nil, source)
	if _, ok := supplyChainSectionInput(t, in); ok {
		t.Fatal("a source error must leave the supply-chain section unsupplied, never report-fatal")
	}
	report := serviceintel.Compose(in)
	supply := sectionByKind(t, report, serviceintel.SectionSupplyChain)
	if supply.Status != serviceintel.StatusUnsupported {
		t.Fatalf("supply-chain section after source error = %q, want unsupported", supply.Status)
	}
}

func TestBuildReportInputBlankWorkloadDoesNotQuerySupplyChain(t *testing.T) {
	t.Parallel()

	source := &fakeSupplyChainSource{inventory: map[string]any{"count": 2}}
	dossier := map[string]any{"service_identity": map[string]any{"service_name": "checkout"}}
	in := buildReportInput(context.Background(), dossier, storyTruth(), nil, source)
	if source.calls != 0 {
		t.Fatalf("blank workload id must not query the supply-chain source, calls = %d", source.calls)
	}
	if _, ok := supplyChainSectionInput(t, in); ok {
		t.Fatal("blank workload id must leave the supply-chain section unsupplied")
	}
}

type fakeSupplyChainAggregateStore struct {
	rows          []query.SupplyChainImpactInventoryRow
	err           error
	gotFilter     query.SupplyChainImpactAggregateFilter
	gotDimension  query.SupplyChainImpactInventoryDimension
	gotLimit      int
	gotOffset     int
	inventoryCall int
}

func (f *fakeSupplyChainAggregateStore) CountSupplyChainImpactFindings(context.Context, query.SupplyChainImpactAggregateFilter) (query.SupplyChainImpactAggregateCount, error) {
	return query.SupplyChainImpactAggregateCount{}, nil
}

func (f *fakeSupplyChainAggregateStore) SupplyChainImpactInventory(
	_ context.Context,
	filter query.SupplyChainImpactAggregateFilter,
	dimension query.SupplyChainImpactInventoryDimension,
	limit int,
	offset int,
) ([]query.SupplyChainImpactInventoryRow, error) {
	f.inventoryCall++
	f.gotFilter = filter
	f.gotDimension = dimension
	f.gotLimit = limit
	f.gotOffset = offset
	return append([]query.SupplyChainImpactInventoryRow(nil), f.rows...), f.err
}

func TestDurableSupplyChainEvidenceSourceLoadsBoundedWorkloadInventory(t *testing.T) {
	t.Parallel()

	store := &fakeSupplyChainAggregateStore{rows: []query.SupplyChainImpactInventoryRow{
		{Dimension: query.SupplyChainImpactInventoryByImpactStatus, Value: "affected_exact", Count: 2},
		{Dimension: query.SupplyChainImpactInventoryByImpactStatus, Value: "possibly_affected", Count: 3},
	}}
	source := NewDurableSupplyChainEvidenceSource(store, nil)

	inventory, err := source.SupplyChainInventoryForWorkload(context.Background(), "workload:checkout")
	if err != nil {
		t.Fatalf("SupplyChainInventoryForWorkload() error = %v, want nil", err)
	}
	if got := store.gotFilter.WorkloadID; got != "workload:checkout" {
		t.Fatalf("filter WorkloadID = %q, want workload:checkout", got)
	}
	if got := store.gotFilter.DetectionProfile; got != query.SupplyChainImpactProfilePrecise {
		t.Fatalf("filter DetectionProfile = %q, want precise", got)
	}
	if got := store.gotDimension; got != query.SupplyChainImpactInventoryByImpactStatus {
		t.Fatalf("dimension = %q, want impact_status", got)
	}
	if got, want := store.gotLimit, query.SupplyChainImpactAggregateMaxLimit+1; got != want {
		t.Fatalf("limit = %d, want %d", got, want)
	}
	if store.gotOffset != 0 {
		t.Fatalf("offset = %d, want 0", store.gotOffset)
	}
	if got := query.IntVal(inventory, "count"); got != 5 {
		t.Fatalf("inventory count = %d, want sum of bucket counts 5", got)
	}
	if got := query.StringVal(inventory, "detection_profile"); got != query.SupplyChainImpactProfilePrecise {
		t.Fatalf("detection_profile = %q, want precise", got)
	}
}

func TestDurableSupplyChainEvidenceSourceEmptyInventoryReturnsNil(t *testing.T) {
	t.Parallel()

	source := NewDurableSupplyChainEvidenceSource(&fakeSupplyChainAggregateStore{}, nil)
	inventory, err := source.SupplyChainInventoryForWorkload(context.Background(), "workload:orphan")
	if err != nil {
		t.Fatalf("error = %v, want nil for empty inventory", err)
	}
	if inventory != nil {
		t.Fatalf("inventory = %#v, want nil for empty inventory", inventory)
	}
}

func TestDurableSupplyChainEvidenceSourceLoadErrorPropagatesAndLogs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	source := NewDurableSupplyChainEvidenceSource(
		&fakeSupplyChainAggregateStore{err: errors.New("connection reset")},
		logger,
	)

	if _, err := source.SupplyChainInventoryForWorkload(context.Background(), "workload:checkout"); err == nil {
		t.Fatal("a supply-chain inventory load failure must propagate")
	}
	if !bytes.Contains(buf.Bytes(), []byte("serviceintel.supply_chain_load_error")) ||
		!bytes.Contains(buf.Bytes(), []byte("workload:checkout")) {
		t.Fatalf("load error must be logged with the workload id, log = %q", buf.String())
	}
}
