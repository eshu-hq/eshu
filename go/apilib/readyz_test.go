// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apilib

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// serveReadiness issues one GET /readyz against a mux with the given probes.
func serveReadiness(t *testing.T, service string, postgres, graph ReadinessProbe) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	MountReadiness(mux, service, postgres, graph)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// decodeReadiness decodes a /readyz body into a field map.
func decodeReadiness(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("readyz body is not JSON: %v", err)
	}
	return body
}

func okProbe() ReadinessProbe {
	return func(context.Context) error { return nil }
}

func TestMountReadinessBothOK(t *testing.T) {
	rec := serveReadiness(t, "eshu-core-api", okProbe(), okProbe())
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz both-ok: want 200, got %d", rec.Code)
	}
	body := decodeReadiness(t, rec)
	if body["service"] != "eshu-core-api" || body["probe"] != "readyz" || body["status"] != "ok" {
		t.Fatalf("readyz both-ok: want core shape {service,probe:readyz,status:ok}, got %v", body)
	}
	if _, ok := body["error"]; ok {
		t.Fatalf("readyz both-ok: want no error key, got %v", body)
	}
	if retry := rec.Header().Get("Retry-After"); retry != "" {
		t.Fatalf("readyz both-ok: want no Retry-After, got %q", retry)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("readyz both-ok: want core-exact Content-Type application/json, got %q", ct)
	}
}

func TestMountReadinessOneFailureIsPermanent503(t *testing.T) {
	rec := serveReadiness(t, "eshu-core-api", okProbe(), func(context.Context) error {
		return errors.New("graph: boom")
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz one-failure: want 503, got %d", rec.Code)
	}
	body := decodeReadiness(t, rec)
	if body["status"] != "error" {
		t.Fatalf("readyz one-failure: want status error, got %v", body)
	}
	if body["error"] == nil || body["error"] == "" {
		t.Fatalf("readyz one-failure: want error text, got %v", body)
	}
	if retry := rec.Header().Get("Retry-After"); retry != "" {
		t.Fatalf("readyz one-failure: permanent 503 must not carry Retry-After, got %q", retry)
	}
}

func TestMountReadinessNilProbeIsNotConfigured(t *testing.T) {
	rec := serveReadiness(t, "eshu-core-api", nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz nil probes: want 503, got %d", rec.Code)
	}
	body := decodeReadiness(t, rec)
	if body["status"] != "error" {
		t.Fatalf("readyz nil probes: want status error, got %v", body)
	}
}

func TestMountReadinessProbesRunConcurrentlyWithTimeout(t *testing.T) {
	release := make(chan struct{})
	ran := make(chan string, 2)
	block := func(name string) ReadinessProbe {
		return func(ctx context.Context) error {
			ran <- name
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
				return nil
			}
		}
	}
	start := time.Now()
	rec := serveReadiness(t, "eshu-core-api", block("postgres"), block("graph"))
	elapsed := time.Since(start)
	close(release)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz blocking probes: want 503 after probe timeout, got %d", rec.Code)
	}
	if elapsed > 15*time.Second {
		t.Fatalf("readyz blocking probes: want concurrent 2s timeouts, took %v", elapsed)
	}
	got := map[string]bool{}
	for range 2 {
		select {
		case name := <-ran:
			got[name] = true
		case <-time.After(5 * time.Second):
			t.Fatal("readyz blocking probes: both probes must run")
		}
	}
	if !got["postgres"] || !got["graph"] {
		t.Fatalf("readyz blocking probes: want both probes run, got %v", got)
	}
}
