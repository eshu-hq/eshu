// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsContentAndMutation is the metadata-only acceptance
// gate for DataZone: the SDK adapter must never read catalog asset or glossary
// content, subscription, time-series, or lineage data, and must never mutate
// DataZone state. We reflect over the adapter's read interface and confirm no
// content-read or mutation method is reachable. The describe reads the adapter
// does use (GetDomain, GetDataSource) are explicitly allowed; every other Get is
// banned so a future edit cannot reach GetAsset/GetGlossary/GetListing and leak
// governed content. This test fails the build if a forbidden method ever appears
// on the adapter surface.
func TestAdapterInterfaceForbidsContentAndMutation(t *testing.T) {
	allowedGet := map[string]struct{}{
		"GetDomain":     {},
		"GetDataSource": {},
	}
	forbiddenSubstrings := []string{
		// governed content / data-plane reads — never reachable.
		"Asset", "Glossary", "Listing", "Subscription", "TimeSeries",
		"Lineage", "Notebook", "DataProduct", "MetadataGeneration", "Credentials",
		"LoginUrl", "Rule", "Grant",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister", "Accept",
		"Reject", "Associate", "Disassociate", "Send", "Cancel", "Revoke",
		"Post", "Search", "Import", "Tag", "Untag", "Remove",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the DataZone read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden content/mutation method %q; the DataZone adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the DataZone adapter is metadata-only", name, prefix)
			}
		}
		if strings.HasPrefix(name, "Get") {
			if _, ok := allowedGet[name]; !ok {
				t.Fatalf("apiClient exposes Get method %q outside the allowed describe set; only GetDomain/GetDataSource are permitted", name)
			}
		}
	}
}

// TestAdapterMethodsAreReadOnly asserts every method on the adapter interface is
// a List read or one of the two allowed describe (Get) reads, so the read
// surface stays explicit and auditable.
func TestAdapterMethodsAreReadOnly(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if strings.HasPrefix(name, "List") || name == "GetDomain" || name == "GetDataSource" {
			continue
		}
		t.Fatalf("apiClient method %q is not a List or allowed describe read", name)
	}
}
