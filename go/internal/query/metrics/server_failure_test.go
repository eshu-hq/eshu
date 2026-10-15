// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package metrics

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

const metricsTimeSeriesPath = "/api/v0/metrics/timeseries?metric=ingest_rate"

// TestMetricsTimeSeriesAnswersFixedServerFailures is the #7674 regression for
// GET /api/v0/metrics/timeseries. It answered 500 with the time-series
// source's error text and answered a client cancel as a server fault. The
// route opens no handler span, so the suite has no HandlerTracer and the
// harness reads the failure off the request span; nothing global is swapped.
func TestMetricsTimeSeriesAnswersFixedServerFailures(t *testing.T) {
	t.Parallel()
	failure.Run(t, failure.Suite{Routes: []failure.Route{{
		Name: "metrics timeseries", Path: metricsTimeSeriesPath,
		Message: metricsQueryFailedMessage,
		Handler: func(err error) failure.Mounter { return &Handler{Source: fakeMetricsSource{err: err}} },
	}}})
}

// TestMetricsTimeSeriesInvalidRangeStays400 pins the 400 audit verdict: a
// range rejection carries only window and step validation text, so it keeps
// its message.
func TestMetricsTimeSeriesInvalidRangeStays400(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("%w: window must be at most 7d", errInvalidMetricsRange)
	rec, _ := failure.Serve(failure.Suite{}, failure.Route{Path: metricsTimeSeriesPath},
		&Handler{Source: fakeMetricsSource{err: err}}, false)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if got, want := failure.Message(rec.Body.Bytes()), "invalid metrics range: "+err.Error(); got != want {
		t.Fatalf("body message = %q, want %q", got, want)
	}
}
