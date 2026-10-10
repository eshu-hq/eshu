// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package metrics

import (
	"fmt"
	"net/http"
	"testing"
)

// TestMetricsTimeSeriesAnswersFixedServerFailures is the #7674 regression for
// GET /api/v0/metrics/timeseries. It answered 500 with the time-series
// source's error text and answered a client cancel as a server fault.
func TestMetricsTimeSeriesAnswersFixedServerFailures(t *testing.T) {
	t.Parallel()
	runServerFailureRoutes(t, []serverFailureRoute{{
		name: "metrics timeseries", path: "/api/v0/metrics/timeseries?metric=ingest_rate",
		message: metricsQueryFailedMessage,
		handler: func(err error) serverFailureMounter { return &Handler{Source: fakeMetricsSource{err: err}} },
	}})
}

// TestMetricsTimeSeriesInvalidRangeStays400 pins the 400 audit verdict: a
// range rejection carries only window and step validation text, so it keeps
// its message.
func TestMetricsTimeSeriesInvalidRangeStays400(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("%w: window must be at most 7d", errInvalidMetricsRange)
	rec, _ := serveServerFailure(t, serverFailureRoute{path: "/api/v0/metrics/timeseries?metric=ingest_rate"},
		&Handler{Source: fakeMetricsSource{err: err}}, false)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	if got, want := serverFailureMessage(rec.Body.Bytes()), "invalid metrics range: "+err.Error(); got != want {
		t.Fatalf("body message = %q, want %q", got, want)
	}
}
