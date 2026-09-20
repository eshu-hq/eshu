// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fakeInfraReadModel is the Postgres read model seam for the aggregate store.
type fakeInfraReadModel struct {
	mu           sync.Mutex
	ready        bool
	readyErr     error
	readyCalls   int
	count        []inventory.CountBucket
	dimension    map[string]int64
	countFilters []inventory.Filter
	dimFilters   []inventory.Filter
	dims         []inventory.Dimension
}

func (f *fakeInfraReadModel) Ready(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readyCalls++
	return f.ready, f.readyErr
}

func (f *fakeInfraReadModel) CountBuckets(_ context.Context, filter inventory.Filter) ([]inventory.CountBucket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.countFilters = append(f.countFilters, filter)
	return f.count, nil
}

func (f *fakeInfraReadModel) DimensionBuckets(
	_ context.Context,
	filter inventory.Filter,
	dimension inventory.Dimension,
) (map[string]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dimFilters = append(f.dimFilters, filter)
	f.dims = append(f.dims, dimension)
	return f.dimension, nil
}

func TestInfraReadModelLabelSplitCoversTaxonomyExactly(t *testing.T) {
	t.Parallel()

	graphOnly := map[string]bool{}
	for _, label := range inventory.GraphOnlyLabels {
		graphOnly[label] = true
	}
	table := map[string]bool{}
	for _, label := range inventory.Labels {
		if graphOnly[label] {
			t.Fatalf("label %q is both graph-only and in the read model", label)
		}
		if !querycontract.InfraLabelAllowed(label) {
			t.Fatalf("read-model label %q is not an infra label", label)
		}
		table[label] = true
	}
	for _, label := range querycontract.AllInfraLabels {
		if !graphOnly[label] && !table[label] {
			t.Fatalf("infra label %q is served by neither the read model nor the graph", label)
		}
	}
	want := []string{"CloudResource", "TerraformStateResource"}
	got := append([]string(nil), inventory.GraphOnlyLabels...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("graph-only labels = %v, want %v", got, want)
	}
	// Mixed-writer labels are in the table for their content-derived nodes;
	// the graph keeps only their other writer's nodes.
	wantMixed := map[string]string{
		"TerraformModule": "projector/tfstate",
		"TerraformOutput": "projector/tfstate",
	}
	if !reflect.DeepEqual(infraMixedWriterGraphSource, wantMixed) {
		t.Fatalf("mixed-writer graph sources = %v, want %v", infraMixedWriterGraphSource, wantMixed)
	}
	for label, source := range infraMixedWriterGraphSource {
		if !table[label] {
			t.Fatalf("mixed-writer label %s must also be in the read model", label)
		}
		if source != infraMixedWriterEvidenceSource {
			t.Fatalf("mixed-writer label %s source %q; the graph read binds one $graph_writer_evidence_source %q",
				label, source, infraMixedWriterEvidenceSource)
		}
	}
}

func TestInfraAggregateCountUsesReadModelAndOneGraphPassWhenBackfilled(t *testing.T) {
	t.Parallel()

	readModel := &fakeInfraReadModel{ready: true, count: []inventory.CountBucket{
		{Label: "TerraformResource", Provider: "aws", Environment: "prod", Count: 5},
		{Label: "TerraformResource", Provider: "aws", Environment: "unknown", Count: 1},
		{Label: "K8sResource", Provider: "unknown", Environment: "prod", Count: 2},
		{Label: "CloudResource", Provider: "aws", Environment: "unknown", Count: 3},
	}}
	graph := &stubInfraGraphQuery{responses: map[string][]map[string]any{
		"MATCH (n:TerraformModule)": {
			{"label": "TerraformModule", "provider_bucket": "unknown", "environment_bucket": "unknown", "bucket_count": int64(4)},
			// An empty mixed-writer branch still yields one zero-count row.
			{"label": nil, "provider_bucket": nil, "environment_bucket": nil, "bucket_count": int64(0)},
		},
	}}
	store := NewGraphInfraResourceAggregateStore(graph).WithReadModel(readModel)

	got, err := store.CountInfraResources(context.Background(), InfraResourceAggregateFilter{})
	if err != nil {
		t.Fatalf("CountInfraResources() error = %v", err)
	}
	want := InfraResourceAggregateCount{
		TotalResources: 15,
		ByProvider:     map[string]int{"aws": 9, "unknown": 6},
		ByEnvironment:  map[string]int{"prod": 7, "unknown": 8},
		ByLabel:        map[string]int{"TerraformResource": 6, "K8sResource": 2, "CloudResource": 3, "TerraformModule": 4},
		Source:         InfraResourceAggregateSourceHybrid,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("count:\n got %+v\nwant %+v", got, want)
	}

	if len(graph.calls) != 1 {
		t.Fatalf("graph calls = %d, want one pass over the mixed-writer labels", len(graph.calls))
	}
	cypher := graph.calls[0].Cypher
	for _, label := range inventory.GraphOnlyLabels {
		if strings.Contains(cypher, "MATCH (n:"+label+")") {
			t.Fatalf("graph pass must not read fact-served label %s whole:\n%s", label, cypher)
		}
	}
	for label := range infraMixedWriterGraphSource {
		if !strings.Contains(cypher, "MATCH (n:"+label+") WHERE n.evidence_source = $graph_writer_evidence_source RETURN '"+label+"' AS label") {
			t.Fatalf("graph pass must read mixed-writer label %s only through the indexed evidence_source seek:\n%s", label, cypher)
		}
	}
	if graph.calls[0].Params["graph_writer_evidence_source"] != "projector/tfstate" {
		t.Fatalf("graph params = %v, want graph_writer_evidence_source bound", graph.calls[0].Params)
	}
	mixed := map[string]bool{}
	for label := range infraMixedWriterGraphSource {
		mixed[label] = true
	}
	for _, label := range inventory.Labels {
		if !mixed[label] && strings.Contains(cypher, "MATCH (n:"+label+")") {
			t.Fatalf("graph pass reads read-model label %s:\n%s", label, cypher)
		}
	}
	if len(readModel.countFilters) != 1 {
		t.Fatalf("read model count calls = %d, want 1", len(readModel.countFilters))
	}
	filter := readModel.countFilters[0]
	if !filter.AllCategories || !reflect.DeepEqual(filter.Labels, allInfraLabels) {
		t.Fatalf("read model filter = %+v, want all categories over every taxonomy label", filter)
	}
}

func TestInfraAggregateCountStaysOnGraphUntilBackfillMarker(t *testing.T) {
	t.Parallel()

	readModel := &fakeInfraReadModel{ready: false}
	graph := &stubInfraGraphQuery{}
	store := NewGraphInfraResourceAggregateStore(graph).WithReadModel(readModel)

	got, err := store.CountInfraResources(context.Background(), InfraResourceAggregateFilter{})
	if err != nil {
		t.Fatalf("CountInfraResources() error = %v", err)
	}
	if got.Source != InfraResourceAggregateSourceGraph {
		t.Fatalf("source = %q, want graph before the backfill marker", got.Source)
	}
	if len(readModel.countFilters) != 0 {
		t.Fatal("read model counted before the backfill marker existed")
	}
	if len(graph.calls) == 0 {
		t.Fatal("graph not read before the backfill marker existed")
	}
}

func TestInfraAggregateScopedReadsStayOnGraph(t *testing.T) {
	t.Parallel()

	readModel := &fakeInfraReadModel{ready: true}
	store := NewGraphInfraResourceAggregateStore(&stubInfraGraphQuery{}).WithReadModel(readModel)

	got, err := store.CountInfraResources(context.Background(), InfraResourceAggregateFilter{
		AllowedRepositoryIDs: []string{"repo-1"},
	})
	if err != nil {
		t.Fatalf("CountInfraResources() error = %v", err)
	}
	if got.Source != InfraResourceAggregateSourceGraph {
		t.Fatalf("scoped source = %q, want graph", got.Source)
	}
	if readModel.readyCalls != 0 || len(readModel.countFilters) != 0 {
		t.Fatal("scoped read consulted the read model; edge-authorized labels need the graph")
	}
}

func TestInfraAggregateReadModelMarkerErrorFailsTheRead(t *testing.T) {
	t.Parallel()

	readModel := &fakeInfraReadModel{readyErr: errors.New("pg down")}
	graph := &stubInfraGraphQuery{}
	store := NewGraphInfraResourceAggregateStore(graph).WithReadModel(readModel)

	if _, err := store.CountInfraResources(context.Background(), InfraResourceAggregateFilter{}); err == nil {
		t.Fatal("CountInfraResources() error = nil, want the marker check failure surfaced")
	}
	if len(graph.calls) != 0 {
		t.Fatal("marker check failure fell back to the graph silently")
	}
}

func TestInfraAggregateCategoryRoutesToOneSide(t *testing.T) {
	t.Parallel()

	k8sModel := &fakeInfraReadModel{ready: true, count: []inventory.CountBucket{
		{Label: "K8sResource", Provider: "unknown", Environment: "prod", Count: 2},
	}}
	k8sGraph := &stubInfraGraphQuery{}
	k8sCount, err := NewGraphInfraResourceAggregateStore(k8sGraph).WithReadModel(k8sModel).
		CountInfraResources(context.Background(), InfraResourceAggregateFilter{Category: "k8s"})
	if err != nil {
		t.Fatalf("k8s CountInfraResources() error = %v", err)
	}
	if k8sCount.Source != InfraResourceAggregateSourceReadModel {
		t.Fatalf("k8s source = %q, want read_model (table only)", k8sCount.Source)
	}
	if len(k8sGraph.calls) != 0 {
		t.Fatalf("k8s category read the graph %d times, want 0 (every k8s label is in the read model)", len(k8sGraph.calls))
	}
	if got := k8sModel.countFilters[0]; got.AllCategories || !reflect.DeepEqual(got.Labels, []string{"K8sResource", "KustomizeOverlay"}) {
		t.Fatalf("k8s read model filter = %+v", got)
	}

	cloudModel := &fakeInfraReadModel{ready: true, count: []inventory.CountBucket{
		{Label: "CloudResource", Provider: "aws", Environment: "unknown", Count: 4},
	}}
	cloudGraph := &stubInfraGraphQuery{}
	cloudCount, err := NewGraphInfraResourceAggregateStore(cloudGraph).WithReadModel(cloudModel).
		CountInfraResources(context.Background(), InfraResourceAggregateFilter{Category: "cloud"})
	if err != nil {
		t.Fatalf("cloud CountInfraResources() error = %v", err)
	}
	if cloudCount.Source != InfraResourceAggregateSourceReadModel {
		t.Fatalf("cloud source = %q, want read_model (CloudResource served from facts)", cloudCount.Source)
	}
	if len(cloudModel.countFilters) != 1 {
		t.Fatalf("cloud category read model calls = %d, want 1", len(cloudModel.countFilters))
	}
	if got := cloudModel.countFilters[0]; got.AllCategories || !reflect.DeepEqual(got.Labels, []string{"CloudResource"}) {
		t.Fatalf("cloud read model filter = %+v, want cloud category over CloudResource", got)
	}
	if len(cloudGraph.calls) != 0 {
		t.Fatalf("cloud category read the graph %d times, want 0 (no whole-label scan)", len(cloudGraph.calls))
	}
}

func TestInfraAggregateInventoryMergesReadModelAndGraphBuckets(t *testing.T) {
	t.Parallel()

	readModel := &fakeInfraReadModel{ready: true, dimension: map[string]int64{"aws": 10, "unknown": 2}}
	graph := &stubInfraGraphQuery{responses: map[string][]map[string]any{
		"MATCH (n:TerraformModule)": {
			{"bucket": "aws", "bucket_count": int64(3)},
			{"bucket": "google", "bucket_count": int64(2)},
			{"bucket": nil, "bucket_count": int64(0)},
		},
	}}
	store := NewGraphInfraResourceAggregateStore(graph).WithReadModel(readModel)

	rows, source, err := store.InfraResourceInventory(context.Background(), InfraResourceAggregateFilter{Provider: "aws"},
		InfraResourceInventoryByProvider, 3, 0)
	if err != nil {
		t.Fatalf("InfraResourceInventory() error = %v", err)
	}
	if source != InfraResourceAggregateSourceHybrid {
		t.Fatalf("source = %q, want hybrid", source)
	}
	want := []InfraResourceInventoryRow{
		{Dimension: InfraResourceInventoryByProvider, Value: "aws", Count: 13},
		// Ties order by bucket name, as on the graph-only path.
		{Dimension: InfraResourceInventoryByProvider, Value: "google", Count: 2},
		{Dimension: InfraResourceInventoryByProvider, Value: "unknown", Count: 2},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows:\n got %+v\nwant %+v", rows, want)
	}
	if got := readModel.dimFilters[0]; got.Provider != "aws" || !got.AllCategories {
		t.Fatalf("read model filter = %+v, want the provider filter forwarded", got)
	}
	if readModel.dims[0] != inventory.DimensionProvider {
		t.Fatalf("read model dimension = %q, want provider", readModel.dims[0])
	}
	if len(graph.calls) != 1 || graph.calls[0].Params["provider"] != "aws" {
		t.Fatalf("graph calls = %+v, want one mixed-writer pass with the provider filter", graph.calls)
	}
	for _, label := range inventory.GraphOnlyLabels {
		if strings.Contains(graph.calls[0].Cypher, "MATCH (n:"+label+")") {
			t.Fatalf("graph pass must not read fact-served label %s whole:\n%s", label, graph.calls[0].Cypher)
		}
	}
}

func TestInfraAggregateReadsCounterRecordsRouteAndSource(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("infra-inventory-reads-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	ready := NewGraphInfraResourceAggregateStore(&stubInfraGraphQuery{}).
		WithReadModel(&fakeInfraReadModel{ready: true}).WithInstruments(instruments)
	notReady := NewGraphInfraResourceAggregateStore(&stubInfraGraphQuery{}).
		WithReadModel(&fakeInfraReadModel{}).WithInstruments(instruments)
	ctx := context.Background()
	if _, err := ready.CountInfraResources(ctx, InfraResourceAggregateFilter{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ready.InfraResourceInventory(ctx, InfraResourceAggregateFilter{}, InfraResourceInventoryByLabel, 10, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := notReady.CountInfraResources(ctx, InfraResourceAggregateFilter{}); err != nil {
		t.Fatal(err)
	}

	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatalf("collect: %v", err)
	}
	got := map[string]int64{}
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_infra_inventory_reads_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric type = %T, want int64 sum", m.Data)
			}
			for _, point := range sum.DataPoints {
				route, _ := point.Attributes.Value("route")
				source, _ := point.Attributes.Value("source")
				got[route.AsString()+"/"+source.AsString()] += point.Value
			}
		}
	}
	want := map[string]int64{"count/read_model": 1, "inventory/read_model": 1, "count/graph": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("eshu_dp_infra_inventory_reads_total = %v, want %v", got, want)
	}
}

func TestInfraAggregateHandlersReportTruthBasisOfServingStore(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		path      string
		store     *stubInfraResourceAggregateStore
		wantBasis string
		wantLevel string
	}{
		{
			"count from read model and graph", "/api/v0/infra/resources/count",
			&stubInfraResourceAggregateStore{count: InfraResourceAggregateCount{Source: InfraResourceAggregateSourceHybrid}},
			"hybrid", "derived",
		},
		{
			"count from read model only", "/api/v0/infra/resources/count",
			&stubInfraResourceAggregateStore{count: InfraResourceAggregateCount{Source: InfraResourceAggregateSourceReadModel}},
			"content_index", "derived",
		},
		{
			"count from graph", "/api/v0/infra/resources/count",
			&stubInfraResourceAggregateStore{count: InfraResourceAggregateCount{Source: InfraResourceAggregateSourceGraph}},
			"authoritative_graph", "exact",
		},
		{
			"inventory from read model and graph", "/api/v0/infra/resources/inventory",
			&stubInfraResourceAggregateStore{invSource: InfraResourceAggregateSourceHybrid},
			"hybrid", "derived",
		},
		{
			"inventory from graph", "/api/v0/infra/resources/inventory",
			&stubInfraResourceAggregateStore{invSource: InfraResourceAggregateSourceGraph},
			"authoritative_graph", "exact",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := &InfraHandler{Aggregates: tc.store, Profile: ProfileProduction}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
			}
			var envelope struct {
				Truth struct {
					Basis string `json:"basis"`
					Level string `json:"level"`
				} `json:"truth"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode: %v; body = %s", err, w.Body.String())
			}
			if envelope.Truth.Basis != tc.wantBasis || envelope.Truth.Level != tc.wantLevel {
				t.Fatalf("truth = %+v, want basis %s level %s", envelope.Truth, tc.wantBasis, tc.wantLevel)
			}
		})
	}
}

// failingInfraReadModel fails every table read.
type failingInfraReadModel struct{ fakeInfraReadModel }

func (f *failingInfraReadModel) CountBuckets(context.Context, inventory.Filter) ([]inventory.CountBucket, error) {
	return nil, errors.New("table unavailable")
}

// blockingGraphQuery blocks until its context is canceled.
type blockingGraphQuery struct{}

func (blockingGraphQuery) Run(ctx context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingGraphQuery) RunSingle(ctx context.Context, _ string, _ map[string]any) (map[string]any, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// cancelTimingGraphQuery records how long each graph call ran. It blocks
// like blockingGraphQuery; the first (concurrent-leg) call ends when the
// table failure cancels it, while a P2-2 fallback call runs to the deadline.
type cancelTimingGraphQuery struct {
	mu   sync.Mutex
	durs []time.Duration
}

func (q *cancelTimingGraphQuery) Run(ctx context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	start := time.Now()
	<-ctx.Done()
	q.mu.Lock()
	q.durs = append(q.durs, time.Since(start))
	q.mu.Unlock()
	return nil, ctx.Err()
}

func (q *cancelTimingGraphQuery) RunSingle(ctx context.Context, _ string, _ map[string]any) (map[string]any, error) {
	if _, err := q.Run(ctx, "", nil); err != nil {
		return nil, err
	}
	return nil, nil
}

// TestInfraAggregateReadModelFailureCancelsTheGraphLegThenFallsBack: a table
// failure must still cancel the concurrent graph pass promptly (P2-2 keeps
// the cancellation instead of waiting out a multi-second label scan), then
// the fallback re-reads the graph sequentially. Against a hung graph the
// route fails with the caller's deadline: the canceled concurrent leg ends
// fast even though the fallback consumes the budget.
func TestInfraAggregateReadModelFailureCancelsTheGraphLegThenFallsBack(t *testing.T) {
	t.Parallel()

	readModel := &failingInfraReadModel{fakeInfraReadModel{ready: true}}
	graph := &cancelTimingGraphQuery{}
	store := NewGraphInfraResourceAggregateStore(graph).WithReadModel(readModel)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := store.CountInfraResources(ctx, InfraResourceAggregateFilter{})
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Fatalf("CountInfraResources() error = %v, want the fallback deadline without a cancellation", err)
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	if len(graph.durs) != 2 {
		t.Fatalf("graph calls = %d, want 2 (canceled concurrent leg plus one fallback)", len(graph.durs))
	}
	if graph.durs[0] > 2*time.Second {
		t.Fatalf("concurrent graph leg took %s; the table failure did not cancel it promptly", graph.durs[0])
	}
}
