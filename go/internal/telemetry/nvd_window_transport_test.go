// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNVDTransportFoldsOntoClosedSet(t *testing.T) {
	t.Parallel()
	for _, transport := range []string{NVDTransportAPI, NVDTransportFeed, NVDTransportUnknown} {
		if got := NVDTransport(transport); got != transport {
			t.Fatalf("NVDTransport(%q) = %q, want itself", transport, got)
		}
	}
	for _, value := range []string{"", "FEED", "mirror", "cve_list", "feed "} {
		if got := NVDTransport(value); got != NVDTransportUnknown {
			t.Fatalf("NVDTransport(%q) = %q, want %q", value, got, NVDTransportUnknown)
		}
	}
	want := []string{"api", "feed", "unknown"}
	if got := NVDTransports(); !slices.Equal(got, want) {
		t.Fatalf("NVDTransports() = %v, want %v", got, want)
	}
}

func TestNVDFeedGuardClassFoldsOntoClosedSet(t *testing.T) {
	t.Parallel()
	for _, class := range []string{NVDFeedGuardYearStale, NVDFeedGuardGroupOversize, NVDFeedGuardProbeFailed} {
		if got := NVDFeedGuardClass(class); got != class {
			t.Fatalf("NVDFeedGuardClass(%q) = %q, want itself", class, got)
		}
	}
	if got := NVDFeedGuardClass("nvd_feed_surprise"); got != NVDFeedGuardOther {
		t.Fatalf("NVDFeedGuardClass(unknown) = %q, want %q", got, NVDFeedGuardOther)
	}
	if got := NVDFeedGuardClass(""); got != NVDFeedGuardOther {
		t.Fatalf("NVDFeedGuardClass(blank) = %q, want %q", got, NVDFeedGuardOther)
	}
}

// TestNVDCursorLagTrackerLabelsTheAdvancingTransport pins the transport
// label rule (eshu-hq/eshu#10716): a position keeps the transport of the
// record that last moved its cursor, so a stale record from the other
// transport cannot relabel a series, and a flip from feed to API moves the
// lag onto the api series with no leftover feed series.
func TestNVDCursorLagTrackerLabelsTheAdvancingTransport(t *testing.T) {
	t.Parallel()
	micros := func(value string) int64 { return nvdCursorTestMicros(t, value) }
	tracker := NewNVDCursorLagTracker()
	tracker.RecordCursorPosition("nvd", NVDTransportFeed, "instance-a",
		micros("2026-01-03T00:00:00Z"), micros("2026-01-05T00:00:00Z"))
	got := tracker.LagSeconds()
	feed := NVDLagSeries{Source: "nvd", Transport: NVDTransportFeed}
	if len(got) != 1 || got[feed] != 172800 {
		t.Fatalf("after feed record lag = %v, want only {nvd feed} = 172800", got)
	}

	// A stale API record (cursor behind the stored one) must not relabel.
	tracker.RecordCursorPosition("nvd", NVDTransportAPI, "instance-a",
		micros("2026-01-02T00:00:00Z"), micros("2026-01-02T00:00:00Z"))
	got = tracker.LagSeconds()
	if len(got) != 1 || got[feed] != 172800 {
		t.Fatalf("after stale api record lag = %v, want the feed series unchanged", got)
	}

	// An advancing API record takes the series: head and cursor keep their
	// max, the transport follows the record that moved the cursor.
	tracker.RecordCursorPosition("nvd", NVDTransportAPI, "instance-a",
		micros("2026-01-04T00:00:00Z"), micros("2026-01-04T12:00:00Z"))
	got = tracker.LagSeconds()
	api := NVDLagSeries{Source: "nvd", Transport: NVDTransportAPI}
	if len(got) != 1 || got[api] != 86400 {
		t.Fatalf("after api advance lag = %v, want only {nvd api} = 86400 (head keeps its max, cursor moved)", got)
	}
}

func TestNVDCursorLagTrackerFoldsTheTransport(t *testing.T) {
	t.Parallel()
	tracker := NewNVDCursorLagTracker()
	tracker.RecordCursorPosition("nvd", "mirror", "instance-a", 1_000_000, 3_000_000)
	got := tracker.LagSeconds()
	if _, ok := got[NVDLagSeries{Source: "nvd", Transport: NVDTransportUnknown}]; !ok || len(got) != 1 {
		t.Fatalf("lag series = %v, want the unrecognized transport folded to unknown", got)
	}
}

func nvdTestCollect(t *testing.T, reader sdkmetric.Reader) map[string]metricdata.Metrics {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	byName := make(map[string]metricdata.Metrics)
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			byName[m.Name] = m
		}
	}
	return byName
}

func nvdTestInstruments(t *testing.T) (*Instruments, sdkmetric.Reader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst, err := NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

func nvdTestAttr(t *testing.T, set attribute.Set, key string) string {
	t.Helper()
	value, ok := set.Value(attribute.Key(key))
	if !ok {
		t.Fatalf("attribute %q missing from %v", key, set.ToSlice())
	}
	return value.AsString()
}

func TestInstrumentsReportNVDCursorLagWithTransportLabel(t *testing.T) {
	t.Parallel()
	inst, reader := nvdTestInstruments(t)
	inst.NVDCursorLags.RecordCursorPosition("nvd", NVDTransportFeed, "instance-a", 1_000_000, 172_801_000_000)
	m, ok := nvdTestCollect(t, reader)["eshu_dp_vulnerability_nvd_cursor_lag_seconds"]
	if !ok {
		t.Fatal("eshu_dp_vulnerability_nvd_cursor_lag_seconds was not collected")
	}
	gauge, ok := m.Data.(metricdata.Gauge[float64])
	if !ok || len(gauge.DataPoints) != 1 {
		t.Fatalf("lag data = %#v, want one Gauge[float64] point", m.Data)
	}
	point := gauge.DataPoints[0]
	if point.Value != 172800 {
		t.Fatalf("lag = %v, want 172800 seconds", point.Value)
	}
	if got := nvdTestAttr(t, point.Attributes, MetricDimensionSource); got != "nvd" {
		t.Fatalf("source = %q, want nvd", got)
	}
	if got := nvdTestAttr(t, point.Attributes, MetricDimensionTransport); got != NVDTransportFeed {
		t.Fatalf("transport = %q, want %q", got, NVDTransportFeed)
	}
	if got := point.Attributes.Len(); got != 2 {
		t.Fatalf("lag labels = %v, want exactly source and transport", point.Attributes.ToSlice())
	}
}

func TestInstrumentsRegisterNVDWindowClaimInstruments(t *testing.T) {
	t.Parallel()
	inst, reader := nvdTestInstruments(t)
	ctx := context.Background()
	if inst.NVDWindowClaims == nil || inst.NVDWindowClaimDuration == nil || inst.NVDWindowCursorAdvances == nil {
		t.Fatal("NVD window claim instruments (claims, duration, cursor advances) are not registered")
	}
	labels := metric.WithAttributes(
		attribute.String(MetricDimensionTransport, NVDTransportFeed),
		attribute.String(MetricDimensionOutcome, NVDWindowClaimCommitted),
	)
	inst.NVDWindowClaims.Add(ctx, 1, labels)
	inst.NVDWindowClaimDuration.Record(ctx, 12.5, labels)
	inst.NVDWindowCursorAdvances.Add(ctx, 1, metric.WithAttributes(
		attribute.String(MetricDimensionTransport, NVDTransportAPI),
		attribute.String(MetricDimensionOutcome, NVDWindowAdvanceHeld),
	))
	collected := nvdTestCollect(t, reader)

	claims, ok := collected["eshu_dp_vulnerability_nvd_window_claims_total"]
	if !ok {
		t.Fatal("eshu_dp_vulnerability_nvd_window_claims_total was not collected")
	}
	sum, ok := claims.Data.(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 1 {
		t.Fatalf("claims data = %#v, want one Sum[int64] point of 1", claims.Data)
	}
	point := sum.DataPoints[0]
	if nvdTestAttr(t, point.Attributes, MetricDimensionTransport) != "feed" || nvdTestAttr(t, point.Attributes, MetricDimensionOutcome) != "committed" {
		t.Fatalf("claims labels = %v, want transport=feed outcome=committed", point.Attributes.ToSlice())
	}

	duration, ok := collected["eshu_dp_vulnerability_nvd_window_claim_duration_seconds"]
	if !ok {
		t.Fatal("eshu_dp_vulnerability_nvd_window_claim_duration_seconds was not collected")
	}
	if duration.Unit != "s" {
		t.Fatalf("duration unit = %q, want s", duration.Unit)
	}
	histogram, ok := duration.Data.(metricdata.Histogram[float64])
	if !ok || len(histogram.DataPoints) != 1 {
		t.Fatalf("duration data = %#v, want one Histogram[float64] point", duration.Data)
	}
	hp := histogram.DataPoints[0]
	if hp.Count != 1 || hp.Sum != 12.5 {
		t.Fatalf("duration point = count %d sum %v, want 1 and 12.5", hp.Count, hp.Sum)
	}
	wantBounds := []float64{1, 2.5, 5, 10, 30, 60, 120, 300, 600, 900}
	if !slices.Equal(hp.Bounds, wantBounds) {
		t.Fatalf("duration bounds = %v, want the explicit claim-window set %v", hp.Bounds, wantBounds)
	}

	advances, ok := collected["eshu_dp_vulnerability_nvd_window_cursor_advance_total"]
	if !ok {
		t.Fatal("eshu_dp_vulnerability_nvd_window_cursor_advance_total was not collected")
	}
	advanceSum, ok := advances.Data.(metricdata.Sum[int64])
	if !ok || len(advanceSum.DataPoints) != 1 {
		t.Fatalf("advance data = %#v, want one Sum[int64] point", advances.Data)
	}
	if nvdTestAttr(t, advanceSum.DataPoints[0].Attributes, MetricDimensionOutcome) != "held" {
		t.Fatalf("advance labels = %v, want outcome=held", advanceSum.DataPoints[0].Attributes.ToSlice())
	}
}

func TestNVDFeedAgeTrackerReportsAgeAtScrapeTime(t *testing.T) {
	t.Parallel()
	var nilTracker *NVDFeedAgeTracker
	nilTracker.RecordFeedTime("nvd", 1)
	if got := nilTracker.AgeSecondsBySource(); len(got) != 0 {
		t.Fatalf("nil AgeSecondsBySource() = %v, want empty", got)
	}
	feedTime := time.Date(2026, 11, 15, 6, 0, 0, 0, time.UTC)
	now := feedTime.Add(3 * time.Hour)
	tracker := NewNVDFeedAgeTracker(func() time.Time { return now })
	tracker.RecordFeedTime("", feedTime.UnixMicro())
	tracker.RecordFeedTime("nvd", 0)
	if got := tracker.AgeSecondsBySource(); len(got) != 0 {
		t.Fatalf("blank source or zero time recorded %v, want nothing", got)
	}
	tracker.RecordFeedTime("nvd", feedTime.UnixMicro())
	if got := tracker.AgeSecondsBySource()["nvd"]; got != 10800 {
		t.Fatalf("age = %v, want 10800 (3 h)", got)
	}
	// The age keeps growing with no new feed read: a feed NVD stopped
	// regenerating must read older at every scrape, not freeze.
	now = feedTime.Add(5 * time.Hour)
	if got := tracker.AgeSecondsBySource()["nvd"]; got != 18000 {
		t.Fatalf("age after 2 more hours = %v, want 18000", got)
	}
	// An older feed time (a replayed or out-of-order claim) cannot rewind.
	tracker.RecordFeedTime("nvd", feedTime.Add(-24*time.Hour).UnixMicro())
	if got := tracker.AgeSecondsBySource()["nvd"]; got != 18000 {
		t.Fatalf("age after an older record = %v, want 18000 (max keeps)", got)
	}
	// A feed time ahead of the local clock reads zero, not negative.
	tracker.RecordFeedTime("nvd", now.Add(time.Hour).UnixMicro())
	if got := tracker.AgeSecondsBySource()["nvd"]; got != 0 {
		t.Fatalf("age with a future feed time = %v, want floored 0", got)
	}
}

// TestNVDFeedAgeTrackerClampsAFutureFeedTime pins the clamp: a feed time ahead
// of the clock is recorded as now, so it ages from the moment it was read.
// Stored verbatim, a bogus future time would win the newest-time slot and the
// age would read zero until the clock caught up with it, which hides a feed
// that stopped regenerating behind a value that looks perfectly fresh.
func TestNVDFeedAgeTrackerClampsAFutureFeedTime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	tracker := NewNVDFeedAgeTracker(func() time.Time { return now })
	tracker.RecordFeedTime("nvd", now.Add(1000*24*time.Hour).UnixMicro())
	if got := tracker.AgeSecondsBySource()["nvd"]; got != 0 {
		t.Fatalf("age right after a future-dated record = %v, want 0", got)
	}
	now = now.Add(2 * time.Hour)
	if got := tracker.AgeSecondsBySource()["nvd"]; got != 7200 {
		t.Fatalf("age 2 h after a future-dated record = %v, want 7200: the time must age from when it was recorded, not stay pinned at zero", got)
	}
	// A real feed time newer than the clamped one still wins the slot.
	tracker.RecordFeedTime("nvd", now.Add(-time.Hour).UnixMicro())
	if got := tracker.AgeSecondsBySource()["nvd"]; got != 3600 {
		t.Fatalf("age after a real, newer feed time = %v, want 3600", got)
	}
	// A time exactly at the clock is not ahead of it and records unchanged.
	tracker.RecordFeedTime("nvd", now.UnixMicro())
	if got := tracker.AgeSecondsBySource()["nvd"]; got != 0 {
		t.Fatalf("age for a feed time equal to now = %v, want 0", got)
	}
}

func TestInstrumentsReportNVDFeedAgeGauge(t *testing.T) {
	t.Parallel()
	inst, reader := nvdTestInstruments(t)
	feedTime := time.Date(2026, 11, 15, 6, 0, 0, 0, time.UTC)
	if inst.NVDFeedAgeSeconds == nil {
		t.Fatal("NVDFeedAgeSeconds gauge is not registered")
	}
	inst.NVDFeedAges = NewNVDFeedAgeTracker(func() time.Time { return feedTime.Add(90 * time.Minute) })
	inst.NVDFeedAges.RecordFeedTime("nvd", feedTime.UnixMicro())
	m, ok := nvdTestCollect(t, reader)["eshu_dp_vulnerability_nvd_feed_age_seconds"]
	if !ok {
		t.Fatal("eshu_dp_vulnerability_nvd_feed_age_seconds was not collected")
	}
	gauge, ok := m.Data.(metricdata.Gauge[float64])
	if !ok || len(gauge.DataPoints) != 1 {
		t.Fatalf("feed age data = %#v, want one Gauge[float64] point", m.Data)
	}
	point := gauge.DataPoints[0]
	if point.Value != 5400 {
		t.Fatalf("feed age = %v, want 5400 seconds", point.Value)
	}
	if got := nvdTestAttr(t, point.Attributes, MetricDimensionSource); got != "nvd" || point.Attributes.Len() != 1 {
		t.Fatalf("feed age labels = %v, want source=nvd only", point.Attributes.ToSlice())
	}
}

func TestInstrumentsRegisterNVDFeedCounters(t *testing.T) {
	t.Parallel()
	inst, reader := nvdTestInstruments(t)
	ctx := context.Background()
	if inst.NVDFeedGuardTrips == nil || inst.NVDFeedDeadlineShort == nil || inst.NVDFeedSHA256Mismatch == nil || inst.NVDFeedMetaUnavailable == nil {
		t.Fatal("NVD feed counters (guard trips, deadline short, sha256 mismatch, meta unavailable) are not registered")
	}
	inst.NVDFeedGuardTrips.Add(ctx, 1, metric.WithAttributes(
		attribute.String(MetricDimensionFailureClass, NVDFeedGuardYearStale)))
	inst.NVDFeedDeadlineShort.Add(ctx, 1)
	inst.NVDFeedSHA256Mismatch.Add(ctx, 1)
	inst.NVDFeedMetaUnavailable.Add(ctx, 1)
	collected := nvdTestCollect(t, reader)
	for _, name := range []string{
		"eshu_dp_vulnerability_nvd_feed_guard_trips_total",
		"eshu_dp_vulnerability_nvd_feed_deadline_short_total",
		"eshu_dp_vulnerability_nvd_feed_sha256_mismatch_total",
		"eshu_dp_vulnerability_nvd_feed_meta_unavailable_total",
	} {
		m, ok := collected[name]
		if !ok {
			t.Fatalf("%s was not collected", name)
		}
		sum, ok := m.Data.(metricdata.Sum[int64])
		if !ok || !sum.IsMonotonic || len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 1 {
			t.Fatalf("%s data = %#v, want one monotonic Sum[int64] point of 1", name, m.Data)
		}
	}
	guard := collected["eshu_dp_vulnerability_nvd_feed_guard_trips_total"].Data.(metricdata.Sum[int64]).DataPoints[0]
	if got := nvdTestAttr(t, guard.Attributes, MetricDimensionFailureClass); got != NVDFeedGuardYearStale {
		t.Fatalf("guard failure_class = %q, want %q", got, NVDFeedGuardYearStale)
	}
}
