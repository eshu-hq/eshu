// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsBodiesAndMutation is the metadata-only acceptance
// gate for the Image Builder adapter: it must never read component build-document
// bodies, Dockerfile bodies, image build artifacts, scan findings, or workflow
// definitions, and must never mutate Image Builder state or control a build run.
// We reflect over the adapter's read interface and confirm no body-read, run, or
// mutation method is reachable. This test fails the build if a future edit ever
// adds one of these to the adapter surface.
func TestAdapterInterfaceForbidsBodiesAndMutation(t *testing.T) {
	// forbiddenSubstrings catch artifact, build-document, and policy reads. They
	// are intentionally not "GetImage"/"Images", which would also match the
	// legitimate GetImageRecipe read; the per-built-image reads are blocked by the
	// exact-name list below instead.
	forbiddenSubstrings := []string{
		// component / build-document / artifact reads — never reachable.
		"Component", "BuildVersion", "Workflow", "ScanFinding", "ScanFindings",
		"Package", "MarketplaceResource", "Lifecycle",
		// resource-policy reads expose access policy documents.
		"Policy",
	}
	// forbiddenExact blocks the per-built-image reads that expose build outputs
	// and artifacts without tripping the recipe/config reads the scanner needs.
	forbiddenExact := map[string]struct{}{
		"GetImage":   {},
		"ListImages": {},
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Start", "Stop", "Cancel",
		"Import", "Run", "Send", "Add", "Remove", "Enable", "Disable",
		"Register", "Deregister", "Associate", "Disassociate", "Tag", "Untag",
		"Execute",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Image Builder read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if _, banned := forbiddenExact[name]; banned {
			t.Fatalf("apiClient exposes per-built-image read %q; the Image Builder adapter is metadata-only", name)
		}
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden body/run/mutation method %q; the Image Builder adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Image Builder adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListOrGetReads asserts every method on the adapter
// interface is a List enumeration or a Get control-plane read so the read
// surface stays explicit and auditable. The scanner reads pipeline, recipe,
// container recipe, infrastructure configuration, and distribution configuration
// metadata only; nothing fetches a build artifact, component body, or scan
// finding.
func TestAdapterMethodsAreListOrGetReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Get") {
			t.Fatalf("apiClient method %q is not a List or Get read", name)
		}
	}
}
