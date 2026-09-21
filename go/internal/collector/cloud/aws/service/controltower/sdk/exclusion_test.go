// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsMutationAndPayloadReads is the metadata-only
// acceptance gate for the Control Tower adapter: it must never enable, disable,
// reset, create, update, or delete Control Tower state, and it must never read a
// governance payload (manifest, control/baseline parameters) through a verb that
// implies one. We reflect over the adapter's read interface and confirm no
// mutation-prefixed method is reachable. This test fails the build if a future
// edit ever adds one of these to the adapter surface.
func TestAdapterInterfaceForbidsMutationAndPayloadReads(t *testing.T) {
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore", "Reset", "Set",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Control Tower read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Control Tower adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListOrGetReads asserts every adapter method is a List or
// Get read so the read surface stays explicit and auditable. The scanner reads
// landing-zone, enabled-control, and enabled-baseline metadata and resource tags
// only; it never reads operation results or any governance payload body.
func TestAdapterMethodsAreListOrGetReads(t *testing.T) {
	allowed := map[string]struct{}{
		"ListLandingZones":     {},
		"GetLandingZone":       {},
		"ListEnabledControls":  {},
		"ListEnabledBaselines": {},
		"ListTagsForResource":  {},
	}
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Get") {
			t.Fatalf("apiClient method %q is not a List or Get read", name)
		}
		if _, ok := allowed[name]; !ok {
			t.Fatalf("apiClient exposes unexpected method %q; keep the read surface minimal and audited", name)
		}
	}
}
