// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsEndpointsAndMessaging is the metadata-only
// acceptance gate for Pinpoint: the SDK adapter must never read endpoint
// records, addresses, or message content, and must never send a message or
// mutate Pinpoint state. We reflect over the adapter's read interface and
// confirm no endpoint-read, message-send, export, or mutation method is
// reachable. This test fails the build if a future edit ever adds one of these
// to the adapter surface.
func TestAdapterInterfaceForbidsEndpointsAndMessaging(t *testing.T) {
	forbiddenSubstrings := []string{
		// endpoint / user record reads — never reachable.
		"Endpoint", "Endpoints", "User",
		// message / template content and export reads.
		"Message", "Messages", "Template", "Export", "Import",
		// recipients / phone / email address surfaces.
		"PhoneNumber", "Email",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Send", "Start", "Stop",
		"Phone", "Verify", "Tag", "Untag", "Remove",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Pinpoint read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			// GetEmailChannel is the one allowed method whose name contains a
			// banned substring ("Email"); it reads channel settings only, never an
			// address. Allow it explicitly and ban every other match.
			if name == "GetEmailChannel" {
				continue
			}
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden endpoint/message method %q; the Pinpoint adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation/send method %q (prefix %q); the Pinpoint adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreGetReads asserts every method on the adapter interface is
// a Get read so the read surface stays explicit and auditable. The scanner reads
// application, segment, and channel-settings metadata only; nothing sends a
// message or reads endpoint records.
func TestAdapterMethodsAreGetReads(t *testing.T) {
	allowed := map[string]struct{}{
		"GetApps":         {},
		"GetSegments":     {},
		"GetChannels":     {},
		"GetEmailChannel": {},
	}
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "Get") {
			t.Fatalf("apiClient method %q is not a Get read", name)
		}
		if _, ok := allowed[name]; !ok {
			t.Fatalf("apiClient method %q is not in the allowed metadata-only read set", name)
		}
	}
}
