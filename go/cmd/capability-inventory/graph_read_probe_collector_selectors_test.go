// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func serveCollectors(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v0/status/collectors" {
			http.NotFound(w, request)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

// TestDiscoverCollectorInstanceSelectorsPairsKindWithItsInstance pins that the
// two selectors come from one collector row. A kind with no instance (an
// unregistered collector) listed first must not lend its kind to a later
// row's instance, which the failed-scope list would answer as a mismatch.
func TestDiscoverCollectorInstanceSelectorsPairsKindWithItsInstance(t *testing.T) {
	server := serveCollectors(t, `{"collectors":[
		{"collector_kind":"unregistered-kind"},
		{"collector_kind":"aws","instance_id":"aws-prod"},
		{"collector_kind":"git","instance_id":"git-main"}]}`)
	defer server.Close()
	selectors := map[string]string{}
	discoverCollectorInstanceSelectors(server.Client(), server.URL, "user", selectors)
	if selectors["collector_kind"] != "aws" || selectors["instance_id"] != "aws-prod" {
		t.Fatalf("selectors = %#v, want collector_kind aws paired with instance_id aws-prod", selectors)
	}
}

// TestDiscoverCollectorInstanceSelectorsLeavesClassesUndiscoveredWithoutAnInstance
// pins the explicit missing-selector failure when no collector carries an
// instance, such as a scoped caller, instead of inventing a value.
func TestDiscoverCollectorInstanceSelectorsLeavesClassesUndiscoveredWithoutAnInstance(t *testing.T) {
	server := serveCollectors(t, `{"collectors":[{"collector_kind":"git"}]}`)
	defer server.Close()
	selectors := map[string]string{}
	discoverCollectorInstanceSelectors(server.Client(), server.URL, "user", selectors)
	if _, ok := selectors["collector_kind"]; ok {
		t.Fatalf("selectors = %#v, want collector_kind left undiscovered", selectors)
	}
	if _, ok := selectors["instance_id"]; ok {
		t.Fatalf("selectors = %#v, want instance_id left undiscovered", selectors)
	}
}
