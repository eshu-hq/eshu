// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
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

	if err := assertInfraServedFromReadModel(context.Background(), srv.URL, "k", 5*time.Second); err != nil {
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

	err := assertInfraServedFromReadModel(context.Background(), srv.URL, "k", 5*time.Second)

	if err == nil || !strings.Contains(err.Error(), "inventory") || !strings.Contains(err.Error(), "authoritative_graph") {
		t.Fatalf("err = %v, want a failure naming the inventory route and its authoritative_graph basis", err)
	}
}

func TestAssertInfraServedFromReadModelFailsOnANon200(t *testing.T) {
	srv := truthServer(t, map[string]string{}) // every route 404s
	defer srv.Close()

	err := assertInfraServedFromReadModel(context.Background(), srv.URL, "k", 5*time.Second)

	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v, want a failure naming the 404", err)
	}
}
