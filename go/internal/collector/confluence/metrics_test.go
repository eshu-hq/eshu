package confluence

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSourceRecordsBoundedConfluenceMetrics(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	source := Source{
		Client: &fakeClient{
			treePageIDs: []string{"root", "visible", "hidden"},
			pagesByID: map[string]Page{
				"root":    confluencePage("root", "Root", 1, `<p>root</p>`),
				"visible": confluencePage("visible", "Visible", 2, `<p><a href="https://example.com/service">service</a></p>`),
			},
			forbiddenPageIDs: map[string]struct{}{"hidden": {}},
		},
		Config: SourceConfig{
			BaseURL:    "https://example.atlassian.net/wiki",
			RootPageID: "root",
			Now:        fixedNow,
		},
		Instruments: instruments,
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	_ = drainFacts(t, collected.Facts)

	rm := collectConfluenceMetrics(t, reader)
	if got := confluenceCounterValue(t, rm, "eshu_dp_confluence_documents_observed_total", map[string]string{
		telemetry.MetricDimensionResult: "success",
	}); got != 2 {
		t.Fatalf("documents counter = %d, want 2", got)
	}
	if got := confluenceCounterValue(t, rm, "eshu_dp_confluence_sections_emitted_total", map[string]string{
		telemetry.MetricDimensionResult: "success",
	}); got != 2 {
		t.Fatalf("sections counter = %d, want 2", got)
	}
	if got := confluenceCounterValue(t, rm, "eshu_dp_confluence_links_emitted_total", map[string]string{
		telemetry.MetricDimensionResult: "success",
	}); got != 1 {
		t.Fatalf("links counter = %d, want 1", got)
	}
	if got := confluenceCounterValue(t, rm, "eshu_dp_confluence_permission_denied_pages_total", map[string]string{
		telemetry.MetricDimensionOperation: "fetch_page",
	}); got != 1 {
		t.Fatalf("permission-denied counter = %d, want 1", got)
	}

	assertConfluenceMetricLabelKeys(t, rm)
}

func TestSourceRecordsSyncFailureClass(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	source := Source{
		Client: errorClient{err: errors.New("confluence unavailable")},
		Config: SourceConfig{
			BaseURL: "https://example.atlassian.net/wiki",
			SpaceID: "100",
			Now:     fixedNow,
		},
		Instruments: instruments,
	}

	_, _, err = source.Next(context.Background())
	if err == nil {
		t.Fatal("Next() error = nil, want source read failure")
	}

	rm := collectConfluenceMetrics(t, reader)
	if got := confluenceCounterValue(t, rm, "eshu_dp_confluence_sync_failures_total", map[string]string{
		telemetry.MetricDimensionFailureClass: "source_read",
	}); got != 1 {
		t.Fatalf("sync failure counter = %d, want 1", got)
	}
	assertConfluenceMetricLabelKeys(t, rm)
}

func TestHTTPClientRecordsBoundedRequestMetrics(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/pages/hidden" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(pageListResponse{
			Results: []Page{confluencePage("123", "Payment", 1, "<p>body</p>")},
		})
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{
		BaseURL:     server.URL,
		BearerToken: "token",
		Client:      server.Client(),
		Instruments: instruments,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v", err)
	}
	if _, err := client.ListSpacePages(context.Background(), "100", 25); err != nil {
		t.Fatalf("ListSpacePages() error = %v, want nil", err)
	}
	if _, err := client.GetPage(context.Background(), "hidden"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("GetPage() error = %v, want ErrPermissionDenied", err)
	}

	rm := collectConfluenceMetrics(t, reader)
	if got := confluenceCounterValue(t, rm, "eshu_dp_confluence_http_requests_total", map[string]string{
		telemetry.MetricDimensionOperation:   "list_pages",
		telemetry.MetricDimensionResult:      "success",
		telemetry.MetricDimensionStatusClass: "2xx",
	}); got != 1 {
		t.Fatalf("list pages request counter = %d, want 1", got)
	}
	if got := confluenceCounterValue(t, rm, "eshu_dp_confluence_http_requests_total", map[string]string{
		telemetry.MetricDimensionOperation:   "fetch_page",
		telemetry.MetricDimensionResult:      "permission_denied",
		telemetry.MetricDimensionStatusClass: "4xx",
	}); got != 1 {
		t.Fatalf("permission request counter = %d, want 1", got)
	}
	if got := confluenceHistogramCount(t, rm, "eshu_dp_confluence_fetch_duration_seconds", map[string]string{
		telemetry.MetricDimensionOperation: "list_pages",
		telemetry.MetricDimensionResult:    "success",
	}); got != 1 {
		t.Fatalf("list pages duration count = %d, want 1", got)
	}

	assertConfluenceMetricLabelKeys(t, rm)
}

func collectConfluenceMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return rm
}

func confluenceCounterValue(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	wantAttrs map[string]string,
) int64 {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricName, metricRecord.Data)
			}
			for _, dp := range sum.DataPoints {
				if confluenceAttrsMatch(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Value
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
	return 0
}

func confluenceHistogramCount(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	wantAttrs map[string]string,
) uint64 {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", metricName, metricRecord.Data)
			}
			for _, dp := range histogram.DataPoints {
				if confluenceAttrsMatch(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Count
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
	return 0
}

func confluenceAttrsMatch(actual []attribute.KeyValue, want map[string]string) bool {
	if len(actual) != len(want) {
		return false
	}
	for _, attr := range actual {
		if want[string(attr.Key)] != attr.Value.AsString() {
			return false
		}
	}
	return true
}

func assertConfluenceMetricLabelKeys(t *testing.T, rm metricdata.ResourceMetrics) {
	t.Helper()
	allowed := map[string]struct{}{
		telemetry.MetricDimensionOperation:    {},
		telemetry.MetricDimensionResult:       {},
		telemetry.MetricDimensionStatusClass:  {},
		telemetry.MetricDimensionFailureClass: {},
	}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if !isConfluenceMetric(metricRecord.Name) {
				continue
			}
			switch data := metricRecord.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					assertConfluenceAttrsAllowed(t, metricRecord.Name, dp.Attributes.ToSlice(), allowed)
				}
			case metricdata.Histogram[float64]:
				for _, dp := range data.DataPoints {
					assertConfluenceAttrsAllowed(t, metricRecord.Name, dp.Attributes.ToSlice(), allowed)
				}
			}
		}
	}
}

func isConfluenceMetric(name string) bool {
	switch name {
	case "eshu_dp_confluence_http_requests_total",
		"eshu_dp_confluence_fetch_duration_seconds",
		"eshu_dp_confluence_permission_denied_pages_total",
		"eshu_dp_confluence_documents_observed_total",
		"eshu_dp_confluence_sections_emitted_total",
		"eshu_dp_confluence_links_emitted_total",
		"eshu_dp_confluence_sync_failures_total":
		return true
	default:
		return false
	}
}

func assertConfluenceAttrsAllowed(
	t *testing.T,
	metricName string,
	attrs []attribute.KeyValue,
	allowed map[string]struct{},
) {
	t.Helper()
	for _, attr := range attrs {
		if _, ok := allowed[string(attr.Key)]; !ok {
			t.Fatalf("metric %q label key %q is not allowed", metricName, attr.Key)
		}
	}
}

type errorClient struct {
	err error
}

func (c errorClient) GetSpace(context.Context, string) (Space, error) {
	return Space{}, c.err
}

func (c errorClient) ListSpacePages(context.Context, string, int) ([]Page, error) {
	return nil, c.err
}

func (c errorClient) ListPageTree(context.Context, string, int) ([]string, error) {
	return nil, c.err
}

func (c errorClient) GetPage(context.Context, string) (Page, error) {
	return Page{}, c.err
}
