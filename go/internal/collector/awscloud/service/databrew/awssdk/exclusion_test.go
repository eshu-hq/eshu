// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsDescribeAndMutation is the metadata-only acceptance
// gate for DataBrew: the SDK adapter must never fetch recipe step expressions,
// custom SQL query strings, or sample data, and must never mutate DataBrew
// state. DataBrew exposes recipe steps and dataset path options through the
// Describe* operations and through ListRecipeVersions detail; we reflect over
// the adapter's read interface and confirm only the four metadata List reads are
// reachable, so no Describe, Create, Update, Delete, Start, Stop, Publish, or
// Send operation can leak into the adapter. This test fails the build if a
// future edit ever adds one of these.
func TestAdapterInterfaceForbidsDescribeAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// detail reads that would expose recipe step expressions or sample data.
		"Describe", "RecipeVersions",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore", "Publish", "Modify",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the DataBrew read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden detail/mutation method %q; the DataBrew adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the DataBrew adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListReads asserts every method on the adapter interface
// is a List read so the read surface stays explicit and auditable. The scanner
// lists datasets, recipes, jobs, and projects only; nothing describes a recipe,
// fetches steps, or reads sample data.
func TestAdapterMethodsAreListReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") {
			t.Fatalf("apiClient method %q is not a List read", name)
		}
	}
}
