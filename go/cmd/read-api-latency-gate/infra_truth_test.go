// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func truthServer(t *testing.T, basisByPath map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/eshu.envelope+json" {
			t.Errorf("%s: Accept = %q, want the envelope media type (the API only returns truth for it)", r.URL.Path, r.Header.Get("Accept"))
		}
		basis, ok := basisByPath[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/eshu.envelope+json")
		_, _ = w.Write([]byte(`{"data":{},"truth":{"level":"exact","basis":"` + basis + `"}}`))
	}))
}

func TestAssertInfraServedFromReadModelAcceptsHybridAndContentIndex(t *testing.T) {
	srv := truthServer(t, map[string]string{
		"/api/v0/infra/resources/count":     "hybrid",
		"/api/v0/infra/resources/inventory": "content_index",
	})
	defer srv.Close()

	if err := assertInfraServedFromReadModel(context.Background(), srv.URL, "k", 5*time.Second, &bytes.Buffer{}); err != nil {
		t.Fatalf("assertInfraServedFromReadModel: %v", err)
	}
}

// TestAssertInfraServedFromReadModelRejectsTheGraphPath is the point of the
// check: a sweep that reads the graph while the read model is installed
// measures the old path and looks like a measurement of the new one.
func TestAssertInfraServedFromReadModelRejectsTheGraphPath(t *testing.T) {
	srv := truthServer(t, map[string]string{
		"/api/v0/infra/resources/count":     "hybrid",
		"/api/v0/infra/resources/inventory": "authoritative_graph",
	})
	defer srv.Close()

	err := assertInfraServedFromReadModel(context.Background(), srv.URL, "k", 5*time.Second, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "inventory") || !strings.Contains(err.Error(), "authoritative_graph") {
		t.Fatalf("err = %v, want a failure naming the inventory route and its authoritative_graph basis", err)
	}
}

func TestAssertInfraServedFromReadModelFailsOnANon200(t *testing.T) {
	srv := truthServer(t, map[string]string{}) // every route 404s
	defer srv.Close()

	err := assertInfraServedFromReadModel(context.Background(), srv.URL, "k", 5*time.Second, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v, want a failure naming the 404", err)
	}
}

// TestAssertInfraServedFromReadModelRetriesAColdFirstHit pins why the probe
// retries: on a cold NornicDB the count route's graph pass for the graph-only
// labels took 8.3s against the API's 10s deadline, and one run's FIRST infra
// request returned 504 backend_timeout while the next succeeded. The check is
// about which store served the route, not how fast, so a 5xx is retried.
func TestAssertInfraServedFromReadModelRetriesAColdFirstHit(t *testing.T) {
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		if calls[r.URL.Path] <= 2 {
			w.WriteHeader(http.StatusGatewayTimeout)
			_, _ = w.Write([]byte(`{"error":{"code":"backend_timeout"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{},"truth":{"basis":"hybrid"}}`))
	}))
	defer srv.Close()

	if err := assertInfraServedFromReadModel(context.Background(), srv.URL, "k", 5*time.Second, &bytes.Buffer{}); err != nil {
		t.Fatalf("a cold first hit must be retried, got: %v", err)
	}
	if calls["/api/v0/infra/resources/count"] != 3 {
		t.Errorf("count route requested %d times, want 3 (two 504s then success)", calls["/api/v0/infra/resources/count"])
	}
}

func TestAssertInfraServedFromReadModelDoesNotRetryAWrongBasis(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"data":{},"truth":{"basis":"authoritative_graph"}}`))
	}))
	defer srv.Close()

	err := assertInfraServedFromReadModel(context.Background(), srv.URL, "k", 5*time.Second, &bytes.Buffer{})

	if err == nil || calls != 1 {
		t.Fatalf("err = %v after %d calls, want an immediate failure after exactly 1: a wrong basis is the finding, not something to wait out", err, calls)
	}
}

func TestAssertInfraServedFromReadModelGivesUpAfterThreeServerErrors(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer srv.Close()

	err := assertInfraServedFromReadModel(context.Background(), srv.URL, "k", 5*time.Second, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "504") || calls != infraTruthAttempts {
		t.Fatalf("err = %v after %d calls, want a 504 failure after %d attempts", err, calls, infraTruthAttempts)
	}
}

func TestAssertInfraServedFromReadModelLogsEachFailedAttemptsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte(`{"error":{"code":"backend_timeout"}}`))
	}))
	defer srv.Close()

	var log bytes.Buffer
	_ = assertInfraServedFromReadModel(context.Background(), srv.URL, "k", 5*time.Second, &log)

	if got := strings.Count(log.String(), "backend_timeout"); got != infraTruthAttempts {
		t.Errorf("log carries the failure body %d times, want once per attempt (%d):\n%s", got, infraTruthAttempts, log.String())
	}
}
