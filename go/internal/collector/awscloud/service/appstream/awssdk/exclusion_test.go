// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsSessionsAndMutation is the metadata-only acceptance
// gate for AppStream: the SDK adapter must never mutate AppStream state, never
// mint a streaming-URL credential, and never read streaming-session, user, or
// credential content. We reflect over the adapter's read interface and confirm
// no mutation, credential, or session/user method is reachable. CreateStreamingURL
// (a session credential), the session/user describe APIs (DescribeSessions,
// DescribeUsers, DescribeUserStackAssociations), and every Create/Delete/Update/
// Start/Stop mutation are excluded from the interface; this test fails the build
// if a future edit ever adds one to the adapter surface.
func TestAdapterInterfaceForbidsSessionsAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// session / user / credential reads — never reachable.
		"Session", "User", "StreamingURL", "Streaming", "AppLicenseUsage",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Start", "Stop", "Expire",
		"Associate", "Disassociate", "Enable", "Disable", "Batch",
		"Tag", "Untag", "Copy", "Import", "Send",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the AppStream read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden session/credential method %q; the AppStream adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the AppStream adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreDescribeOrListReads asserts every method on the adapter
// interface is a Describe or List read so the read surface stays explicit and
// auditable. The scanner reads fleet, stack, image builder, image, association,
// and tag metadata only; nothing fetches session or credential payloads.
func TestAdapterMethodsAreDescribeOrListReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "Describe") && !strings.HasPrefix(name, "List") {
			t.Fatalf("apiClient method %q is not a Describe or List read", name)
		}
	}
}
