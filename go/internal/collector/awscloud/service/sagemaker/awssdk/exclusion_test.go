// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterAPIClientForbidsInvokeAndMutation is the acceptance gate the
// issue calls out: the SageMaker SDK adapter must never be able to run
// inference or mutate SageMaker state. We reflect over the adapter-local
// apiClient interface and confirm no inference call (InvokeEndpoint,
// InvokeEndpointAsync) and no lifecycle mutation (Create/Delete/Stop/Update/
// Start of any SageMaker resource) is reachable. Because the inference calls
// live only in the separate aws-sdk-go-v2/service/sagemakerruntime module,
// which this package never imports, they cannot appear on apiClient at all;
// this test fails the build if a future edit ever adds one.
func TestAdapterAPIClientForbidsInvokeAndMutation(t *testing.T) {
	forbiddenExact := []string{
		// Inference / endpoint invocation — never call these.
		"InvokeEndpoint", "InvokeEndpointAsync", "InvokeEndpointWithResponseStream",
	}
	// Any method whose name starts with one of these mutation verbs is a write
	// or lifecycle operation and must not exist on the metadata-only adapter.
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Stop", "Start", "Put",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Invoke", "Render", "Retry", "Attach", "Detach",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		for _, banned := range forbiddenExact {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden inference method %q; SageMaker adapter must never invoke endpoints", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); SageMaker adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreReadOnly asserts every method on the apiClient interface
// is a List or Describe read so the read surface stays explicit and auditable.
func TestAdapterMethodsAreReadOnly(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	if iface.NumMethod() == 0 {
		t.Fatalf("apiClient interface has no methods; expected the SageMaker read surface")
	}
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is neither a List nor Describe read", name)
		}
	}
}
