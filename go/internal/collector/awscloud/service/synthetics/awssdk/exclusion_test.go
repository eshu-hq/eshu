// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsRunsCodeAndMutation is the metadata-only acceptance
// gate for Synthetics: the SDK adapter must never read canary run artifacts, run
// results, or canary script source code, and must never mutate Synthetics state
// or control canary runs. We reflect over the adapter's read interface and
// confirm no run-read, code-read, mutation, or run-control method is reachable.
// GetCanaryRuns and DescribeCanariesLastRun (run reads), GetCanary (code read),
// and every Create/Update/Delete/Start/Stop method are excluded from the
// interface below, so the adapter cannot reach them. This test fails the build
// if a future edit ever adds one to the adapter surface.
func TestAdapterInterfaceForbidsRunsCodeAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// run artifact / run result reads — never reachable.
		"CanaryRuns", "LastRun", "Runs", "GetGroup", "GetCanary",
		"ListGroupResources",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Import",
		"Tag", "Untag", "Resume", "Restore", "Get",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Synthetics read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden run/code method %q; the Synthetics adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation/control method %q (prefix %q); the Synthetics adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreDescribeReads asserts the adapter interface exposes only
// DescribeCanaries, so the read surface stays the single control-plane list and
// nothing fetches run artifacts, run results, or canary code.
func TestAdapterMethodsAreDescribeReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		if got := ifaceType.Method(i).Name; got != "DescribeCanaries" {
			t.Fatalf("apiClient method %q is not the allowed DescribeCanaries read", got)
		}
	}
}
