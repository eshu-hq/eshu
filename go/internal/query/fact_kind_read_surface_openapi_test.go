// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestFactKindHTTPReadSurfacesAreOpenAPIPaths(t *testing.T) {
	var spec struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	for _, entry := range facts.FactKindRegistry() {
		method, path, ok := strings.Cut(strings.TrimSpace(entry.ReadSurface), " ")
		if !ok {
			continue
		}
		method = strings.ToLower(strings.TrimSpace(method))
		path = strings.TrimSpace(path)
		if method == "" || path == "" || !strings.HasPrefix(path, "/") {
			continue
		}
		methods, ok := spec.Paths[path]
		if !ok {
			t.Fatalf("fact kind %q read surface %q is missing from OpenAPI paths", entry.Kind, entry.ReadSurface)
		}
		if _, ok := methods[method]; !ok {
			t.Fatalf("fact kind %q read surface %q is missing OpenAPI method %q", entry.Kind, entry.ReadSurface, method)
		}
	}
}
