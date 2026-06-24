// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	prometheusQueryRangeEndpoint = "/api/v1/query_range"
	prometheusDefaultTimeout     = 15 * time.Second
	prometheusMaxWindow          = 30 * 24 * time.Hour
	prometheusMinStep            = 10 * time.Second
	prometheusMaxSamples         = 2000
)

var prometheusDurationPattern = regexp.MustCompile(`([0-9]+)(ms|s|m|h|d|w|y)`)

var prometheusMetricExpressions = map[string]string{
	"ingest_rate":  "sum(rate(eshu_dp_facts_committed_total[5m])) * 60",
	"queue_depth":  `sum(eshu_dp_queue_depth{status=~"pending|in_flight|retrying"})`,
	"dead_letters": "sum(eshu_runtime_queue_dead_letter)",
	"graph_nodes":  "sum(eshu_dp_canonical_nodes_written_total)",
	"graph_edges":  "sum(eshu_dp_canonical_edges_written_total)",
	"query_p50":    "histogram_quantile(0.50, sum(rate(eshu_http_request_duration_seconds_bucket[5m])) by (le)) * 1000",
	"query_p95":    "histogram_quantile(0.95, sum(rate(eshu_http_request_duration_seconds_bucket[5m])) by (le)) * 1000",
	"query_p99":    "histogram_quantile(0.99, sum(rate(eshu_http_request_duration_seconds_bucket[5m])) by (le)) * 1000",
}

// PrometheusMetricsTimeSeriesConfig configures a Prometheus-compatible metrics
// source for the console trend API.
type PrometheusMetricsTimeSeriesConfig struct {
	BaseURL    string
	PathPrefix string
	Token      string
	TenantID   string
	Client     *http.Client
	Now        func() time.Time
}

// PrometheusMetricsTimeSeriesSource reads Eshu dashboard metrics through the
// Prometheus-compatible query_range API used by Prometheus and Grafana Mimir.
type PrometheusMetricsTimeSeriesSource struct {
	baseURL    *url.URL
	pathPrefix string
	token      string
	tenantID   string
	client     *http.Client
	now        func() time.Time
}

type prometheusRangeResponse struct {
	Status    string              `json:"status"`
	Data      prometheusRangeData `json:"data"`
	ErrorType string              `json:"errorType"`
}

type prometheusRangeData struct {
	ResultType string                  `json:"resultType"`
	Result     []prometheusRangeResult `json:"result"`
}

type prometheusRangeResult struct {
	Values [][]any `json:"values"`
}

// NewPrometheusMetricsTimeSeriesSource validates config and returns a bounded
// query_range-backed source. Base URLs with embedded credentials are rejected so
// secrets stay in headers.
func NewPrometheusMetricsTimeSeriesSource(
	config PrometheusMetricsTimeSeriesConfig,
) (*PrometheusMetricsTimeSeriesSource, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"))
	if err != nil {
		return nil, fmt.Errorf("parse metrics base_url: %w", err)
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("metrics base_url must include scheme and host")
	}
	if base.User != nil {
		return nil, fmt.Errorf("metrics base_url must not include credentials")
	}
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: prometheusDefaultTimeout}
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &PrometheusMetricsTimeSeriesSource{
		baseURL:    base,
		pathPrefix: strings.Trim(config.PathPrefix, "/"),
		token:      strings.TrimSpace(config.Token),
		tenantID:   strings.TrimSpace(config.TenantID),
		client:     client,
		now:        now,
	}, nil
}

// RangeQuery resolves a logical console metric to closed PromQL and returns one
// ordered point series for the requested window.
func (s *PrometheusMetricsTimeSeriesSource) RangeQuery(
	ctx context.Context,
	query MetricsRangeQuery,
) ([]MetricPoint, error) {
	if s == nil {
		return nil, fmt.Errorf("metrics time-series source is nil")
	}
	expression, ok := prometheusMetricExpressions[query.Metric]
	if !ok {
		return nil, fmt.Errorf("unsupported metrics time-series metric %q", query.Metric)
	}
	window, err := parsePrometheusMetricDuration(query.Window, "window")
	if err != nil {
		return nil, err
	}
	step, err := parsePrometheusMetricDuration(query.Step, "step")
	if err != nil {
		return nil, err
	}
	if err := validatePrometheusMetricRange(window, step); err != nil {
		return nil, err
	}
	end := s.now().UTC()
	start := end.Add(-window)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.queryRangeURL(expression, start, end, query.Step), nil)
	if err != nil {
		return nil, fmt.Errorf("build metrics time-series request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	if s.tenantID != "" {
		req.Header.Set("X-Scope-OrgID", s.tenantID)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query metrics time-series source: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("metrics time-series source returned HTTP %d", resp.StatusCode)
	}
	var decoded prometheusRangeResponse
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err := dec.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode metrics time-series response: %w", err)
	}
	if decoded.Status != "" && decoded.Status != "success" {
		return nil, fmt.Errorf("metrics time-series source returned status %q with error type %q", decoded.Status, decoded.ErrorType)
	}
	return decodePrometheusRangePoints(decoded.Data.Result)
}

func (s *PrometheusMetricsTimeSeriesSource) queryRangeURL(expression string, start, end time.Time, step string) string {
	reqURL := *s.baseURL
	reqURL.Path = path.Join(s.baseURL.Path, s.pathPrefix, prometheusQueryRangeEndpoint)
	query := reqURL.Query()
	query.Set("query", expression)
	query.Set("start", start.Format(time.RFC3339))
	query.Set("end", end.Format(time.RFC3339))
	query.Set("step", strings.TrimSpace(step))
	reqURL.RawQuery = query.Encode()
	return reqURL.String()
}

func decodePrometheusRangePoints(results []prometheusRangeResult) ([]MetricPoint, error) {
	valuesByTimestamp := map[int64]float64{}
	for _, result := range results {
		for _, raw := range result.Values {
			if len(raw) != 2 {
				return nil, fmt.Errorf("metrics time-series sample must contain timestamp and value")
			}
			timestamp, err := prometheusSampleTimestamp(raw[0])
			if err != nil {
				return nil, err
			}
			value, err := prometheusSampleValue(raw[1])
			if err != nil {
				return nil, err
			}
			valuesByTimestamp[timestamp.UnixNano()] += value
		}
	}
	keys := make([]int64, 0, len(valuesByTimestamp))
	for key := range valuesByTimestamp {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	points := make([]MetricPoint, 0, len(keys))
	for _, key := range keys {
		points = append(points, MetricPoint{
			T: time.Unix(0, key).UTC().Format(time.RFC3339),
			V: valuesByTimestamp[key],
		})
	}
	return points, nil
}

func prometheusSampleTimestamp(raw any) (time.Time, error) {
	value, err := prometheusFloat(raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("decode metrics sample timestamp: %w", err)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return time.Time{}, fmt.Errorf("metrics sample timestamp must be finite")
	}
	seconds, fraction := math.Modf(value)
	return time.Unix(int64(seconds), int64(fraction*1e9)).UTC(), nil
}

func prometheusSampleValue(raw any) (float64, error) {
	value, err := prometheusFloat(raw)
	if err != nil {
		return 0, fmt.Errorf("decode metrics sample value: %w", err)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("metrics sample value must be finite")
	}
	return value, nil
}

func prometheusFloat(raw any) (float64, error) {
	switch value := raw.(type) {
	case json.Number:
		return value.Float64()
	case float64:
		return value, nil
	case string:
		return strconv.ParseFloat(value, 64)
	default:
		return 0, fmt.Errorf("unsupported JSON value %T", raw)
	}
}

func parsePrometheusMetricDuration(raw string, field string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, fmt.Errorf("%w: metrics time-series %s is required", errInvalidMetricsRange, field)
	}
	if value, err := time.ParseDuration(trimmed); err == nil {
		if value <= 0 {
			return 0, fmt.Errorf("%w: metrics time-series %s must be positive", errInvalidMetricsRange, field)
		}
		return value, nil
	}
	value, ok := parsePrometheusWholeDuration(trimmed)
	if !ok || value <= 0 {
		return 0, fmt.Errorf("%w: metrics time-series %s must be a positive Prometheus duration", errInvalidMetricsRange, field)
	}
	return value, nil
}

func validatePrometheusMetricRange(window time.Duration, step time.Duration) error {
	if window > prometheusMaxWindow {
		return fmt.Errorf("%w: window must be at most %s", errInvalidMetricsRange, prometheusMaxWindow)
	}
	if step < prometheusMinStep {
		return fmt.Errorf("%w: step must be at least %s", errInvalidMetricsRange, prometheusMinStep)
	}
	samples := int(window/step) + 1
	if window%step != 0 {
		samples++
	}
	if samples > prometheusMaxSamples {
		return fmt.Errorf("%w: range would request %d samples; maximum is %d", errInvalidMetricsRange, samples, prometheusMaxSamples)
	}
	return nil
}

func parsePrometheusWholeDuration(raw string) (time.Duration, bool) {
	matches := prometheusDurationPattern.FindAllStringSubmatchIndex(raw, -1)
	if len(matches) == 0 {
		return 0, false
	}
	var cursor int
	var total time.Duration
	for _, match := range matches {
		if match[0] != cursor {
			return 0, false
		}
		amount, err := strconv.ParseInt(raw[match[2]:match[3]], 10, 64)
		if err != nil {
			return 0, false
		}
		unit := raw[match[4]:match[5]]
		total += time.Duration(amount) * prometheusDurationUnit(unit)
		cursor = match[1]
	}
	return total, cursor == len(raw)
}

func prometheusDurationUnit(unit string) time.Duration {
	switch unit {
	case "ms":
		return time.Millisecond
	case "s":
		return time.Second
	case "m":
		return time.Minute
	case "h":
		return time.Hour
	case "d":
		return 24 * time.Hour
	case "w":
		return 7 * 24 * time.Hour
	case "y":
		return 365 * 24 * time.Hour
	default:
		return 0
	}
}
