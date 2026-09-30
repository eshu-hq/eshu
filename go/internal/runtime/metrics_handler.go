// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	compositeMetricsContentType = "text/plain; version=0.0.4; charset=utf-8"
	statusMetricsReadTimeout    = 2 * time.Second
)

// NewCompositeMetricsHandler serves OTEL Prometheus output and the hand-rolled
// runtime gauges from the same /metrics endpoint.
func NewCompositeMetricsHandler(statusHandler, prometheusHandler http.Handler) http.Handler {
	if statusHandler == nil {
		return prometheusHandler
	}
	if prometheusHandler == nil {
		return statusHandler
	}

	return compositeMetricsHandler{
		statusHandler:     statusHandler,
		prometheusHandler: prometheusHandler,
	}
}

type compositeMetricsHandler struct {
	statusHandler     http.Handler
	prometheusHandler http.Handler
}

func (h compositeMetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// The hand-rolled status metrics use Prometheus text. Negotiate that same
	// format with promhttp regardless of the caller's Accept and compression.
	prometheusRequest := r.Clone(r.Context())
	prometheusRequest.Method = http.MethodGet
	prometheusRequest.Header.Set("Accept", "text/plain; version=0.0.4")
	prometheusRequest.Header.Del("Accept-Encoding")
	prometheusRecorder := httptest.NewRecorder()
	h.prometheusHandler.ServeHTTP(prometheusRecorder, prometheusRequest)
	if prometheusRecorder.Code != http.StatusOK {
		code := prometheusRecorder.Code
		if code < http.StatusBadRequest || code > 599 {
			code = http.StatusBadGateway
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(code)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(http.StatusText(code) + "\n"))
		}
		return
	}

	statusContext, cancel := context.WithTimeout(r.Context(), statusMetricsReadTimeout)
	defer cancel()
	statusRequest := r.Clone(statusContext)
	statusRequest.Method = http.MethodGet
	statusRecorder := httptest.NewRecorder()
	h.statusHandler.ServeHTTP(statusRecorder, statusRequest)

	w.Header().Set("Content-Type", compositeMetricsContentType)
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}

	_, _ = w.Write(prometheusRecorder.Body.Bytes())
	_, _ = w.Write([]byte("\n"))
	available := "0"
	if statusRecorder.Code == http.StatusOK && statusContext.Err() == nil {
		available = "1"
	}
	_, _ = w.Write([]byte("# HELP " + telemetry.RuntimeStatusSnapshotAvailableMetric + " Whether the runtime status snapshot was available for this scrape.\n"))
	_, _ = w.Write([]byte("# TYPE " + telemetry.RuntimeStatusSnapshotAvailableMetric + " gauge\n"))
	_, _ = w.Write([]byte(telemetry.RuntimeStatusSnapshotAvailableMetric + " " + available + "\n"))
	if available == "1" {
		_, _ = w.Write(statusRecorder.Body.Bytes())
	}
}
