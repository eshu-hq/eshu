// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAdapterInterfaceForbidsMutation is the metadata-only acceptance gate for
// Network Manager: the SDK adapter must never mutate Network Manager state. We
// reflect over the adapter's read interface and confirm no create, update,
// delete, register/deregister, associate/disassociate, tag, put, start, or
// route-analysis method is reachable. This test fails the build if a future edit
// ever adds one to the adapter surface.
func TestAdapterInterfaceForbidsMutation(t *testing.T) {
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Start", "Stop", "Add",
		"Disable", "Enable", "Register", "Deregister", "Associate",
		"Disassociate", "Send", "Tag", "Untag", "Accept", "Reject",
		"Execute", "Restore", "Modify",
	}
	forbiddenSubstrings := []string{"RouteAnalysis"}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatal("apiClient has no methods; expected the Network Manager read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Network Manager adapter is metadata-only", name, prefix)
			}
		}
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden method %q; the Network Manager adapter is metadata-only", name)
			}
		}
	}
}

// TestAdapterMethodsAreControlPlaneReads asserts every adapter method is a
// Describe, Get, or List read so the read surface stays explicit and auditable.
func TestAdapterMethodsAreControlPlaneReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "Describe") &&
			!strings.HasPrefix(name, "Get") &&
			!strings.HasPrefix(name, "List") {
			t.Fatalf("apiClient method %q is not a Describe/Get/List read", name)
		}
	}
}

// TestGlobalServiceRegionIsPartitionAware proves the adapter pins the Network
// Manager control-plane region per partition: commercial us-west-2, GovCloud
// us-gov-west-1, China cn-north-1. A wrong region would make every call fail
// against the wrong partition endpoint.
func TestGlobalServiceRegionIsPartitionAware(t *testing.T) {
	cases := []struct {
		partition string
		want      string
	}{
		{awscloud.PartitionAWS, "us-west-2"},
		{awscloud.PartitionGovCloud, "us-gov-west-1"},
		{awscloud.PartitionChina, "cn-north-1"},
		{"unknown-partition", "us-west-2"},
	}
	for _, tc := range cases {
		if got := globalServiceRegion(tc.partition); got != tc.want {
			t.Fatalf("globalServiceRegion(%q) = %q, want %q", tc.partition, got, tc.want)
		}
	}
}
