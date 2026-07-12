// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape_test

// Collector fault-injection coverage (C-14, #4367, epic #4172).
//
// The replay-coverage gate requires a `fault` depth scenario for every
// implemented collector boundary. These tests satisfy that requirement
// honestly: each one drives a REAL collector's poll with the inputtape timeout
// fault injected at its HTTP boundary, then asserts the collector surfaces the
// fault as a classified timeout rather than swallowing it, mis-classifying it,
// or hanging.
//
// The fault value injected is inputtape.ErrFaultTimeout — the exact error the
// Replayer serves for a FaultKindTimeout interaction (a *timeoutError that both
// wraps context.DeadlineExceeded and reports Timeout() bool == true). Injecting
// it through a RoundTripper reproduces, at the collector boundary, precisely
// what a recorded timeout-fault tape would deliver, without depending on a
// collector's request shape or volatile query params (which defeat tape
// request-key matching). The Replayer's own fault-injection mechanics are
// proven separately by fault_test.go and fault_timeout_test.go; these tests
// assert the COLLECTOR's reaction to a boundary fault, which is the C-14 grain
// (fault -> every implemented collector boundary).
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// concurrency-deadlock-rigor.

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/grafana"
	"github.com/eshu-hq/eshu/go/internal/collector/loki"
	"github.com/eshu-hq/eshu/go/internal/collector/prometheusmimir"
	"github.com/eshu-hq/eshu/go/internal/collector/tempo"
	"github.com/eshu-hq/eshu/go/internal/replay/inputtape"
)

// timeoutFaultClient returns an *http.Client whose transport injects the
// inputtape timeout fault on every request, driving the real collector wired to
// it straight into a boundary timeout.
func timeoutFaultClient() *http.Client {
	return &http.Client{Transport: faultTransport{err: inputtape.ErrFaultTimeout}}
}

// faultTransport is an http.RoundTripper that fails every request with a fixed
// fault error, reproducing an injected inputtape fault at a collector's HTTP
// boundary without a recorded tape.
type faultTransport struct{ err error }

// RoundTrip injects the configured fault instead of performing the request. The
// net/http stack wraps the returned error in *url.Error, whose Timeout() and
// Unwrap() delegate to the fault error — the shape a live transport fault takes.
func (f faultTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, f.err
}

// assertSurfacesInjectedTimeout asserts that a real collector's poll (collect)
// surfaces an injected boundary timeout as a classified timeout on both paths
// real SDKs use to gate retries: the context.DeadlineExceeded sentinel and the
// net.Error Timeout() interface. A collector that returns nil (swallows the
// fault) or an unclassified error fails the assertion.
func assertSurfacesInjectedTimeout(t *testing.T, collect func() error) {
	t.Helper()

	err := collect()
	if err == nil {
		t.Fatalf("collector swallowed the injected timeout; want a surfaced error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("collector error not classified as context.DeadlineExceeded: %v", err)
	}
	var timeout interface{ Timeout() bool }
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("collector error not reachable as a net timeout: %v", err)
	}
}

// TestGrafanaCollectorSurfacesInjectedTimeout drives the real Grafana REST
// client into a boundary timeout and asserts it surfaces a classified timeout.
func TestGrafanaCollectorSurfacesInjectedTimeout(t *testing.T) {
	t.Parallel()

	assertSurfacesInjectedTimeout(t, func() error {
		client, err := grafana.NewHTTPClient(grafana.HTTPClientConfig{
			BaseURL: "https://grafana.invalid",
			Token:   "grafana-token",
			Client:  timeoutFaultClient(),
		})
		if err != nil {
			return err
		}
		_, err = client.CollectObservedMetadata(context.Background(), grafana.TargetConfig{
			Provider:      grafana.ProviderGrafana,
			ScopeID:       "grafana:instance:prod",
			InstanceID:    "grafana-prod",
			BaseURL:       "https://grafana.invalid",
			Token:         "grafana-token",
			ResourceLimit: 50,
		})
		return err
	})
}

// TestLokiCollectorSurfacesInjectedTimeout drives the real Loki metadata client
// into a boundary timeout.
func TestLokiCollectorSurfacesInjectedTimeout(t *testing.T) {
	t.Parallel()

	assertSurfacesInjectedTimeout(t, func() error {
		client, err := loki.NewHTTPClient(loki.HTTPClientConfig{
			BaseURL: "https://loki.invalid",
			Client:  timeoutFaultClient(),
		})
		if err != nil {
			return err
		}
		_, err = client.CollectObservedMetadata(context.Background(), loki.TargetConfig{
			ScopeID:       "loki:tenant:prod",
			InstanceID:    "loki-prod",
			BaseURL:       "https://loki.invalid",
			Token:         "loki-token",
			TenantID:      "tenant-prod",
			ResourceLimit: 50,
		})
		return err
	})
}

// TestPrometheusMimirCollectorSurfacesInjectedTimeout drives the real
// Prometheus/Mimir metadata client into a boundary timeout.
func TestPrometheusMimirCollectorSurfacesInjectedTimeout(t *testing.T) {
	t.Parallel()

	assertSurfacesInjectedTimeout(t, func() error {
		client, err := prometheusmimir.NewHTTPClient(prometheusmimir.HTTPClientConfig{
			BaseURL: "https://prometheus.invalid",
			Client:  timeoutFaultClient(),
		})
		if err != nil {
			return err
		}
		_, err = client.CollectObservedMetadata(context.Background(), prometheusmimir.TargetConfig{
			ScopeID:       "prom:cluster:prod",
			InstanceID:    "prom-prod",
			Provider:      prometheusmimir.ProviderPrometheus,
			BaseURL:       "https://prometheus.invalid",
			Token:         "prom-token",
			ResourceLimit: 50,
		})
		return err
	})
}

// TestTempoCollectorSurfacesInjectedTimeout drives the real Tempo metadata
// client into a boundary timeout.
func TestTempoCollectorSurfacesInjectedTimeout(t *testing.T) {
	t.Parallel()

	assertSurfacesInjectedTimeout(t, func() error {
		client, err := tempo.NewHTTPClient(tempo.HTTPClientConfig{
			BaseURL: "https://tempo.invalid",
			Client:  timeoutFaultClient(),
		})
		if err != nil {
			return err
		}
		_, err = client.CollectObservedMetadata(context.Background(), tempo.TargetConfig{
			ScopeID:       "tempo:cluster:prod",
			InstanceID:    "tempo-prod",
			BaseURL:       "https://tempo.invalid",
			ResourceLimit: 50,
		})
		return err
	})
}
