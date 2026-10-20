// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsRecordsAndMutation is the metadata-only acceptance
// gate the issue calls out for Timestream: the SDK adapter must never read
// time-series records or measures and must never write records or mutate
// Timestream state. We reflect over the adapter's read interface and confirm no
// record-read, query, write, or mutation method is reachable. WriteRecords
// lives on the timestream-write client but is excluded from the interface
// below; Query lives only in the separate timestream-query module this package
// never imports, so it cannot appear at all. This test fails the build if a
// future edit ever adds one of these to the adapter surface.
func TestAdapterInterfaceForbidsRecordsAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// data-plane record / query reads — never reachable.
		"WriteRecords", "Query", "Select", "Records", "Measure",
		// batch ingestion is a write surface.
		"BatchLoad",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Timestream read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden record/mutation method %q; the Timestream adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Timestream adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListReads asserts every method on the adapter interface
// is a List read so the read surface stays explicit and auditable. The scanner
// reads database and table metadata and resource tags only; nothing describes
// or fetches record payloads.
func TestAdapterMethodsAreListReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") {
			t.Fatalf("apiClient method %q is not a List read", name)
		}
	}
}
