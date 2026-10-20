// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfacesForbidInferenceAndMutation is the acceptance gate the
// issue calls out for a high-redaction scanner: the Bedrock SDK adapter must
// never be able to run inference or mutate Bedrock state. We reflect over both
// adapter-local read interfaces and confirm no inference call (InvokeModel,
// InvokeAgent, Converse, Retrieve, RetrieveAndGenerate) and no mutation
// (Create/Delete/Update/Prepare/Start/Stop/...) is reachable. Because the
// inference calls live only in the separate bedrockruntime and
// bedrockagentruntime modules, which this package never imports, they cannot
// appear on either interface at all; this test fails the build if a future edit
// ever adds one.
func TestAdapterInterfacesForbidInferenceAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// bedrock-runtime inference — never reachable.
		"InvokeModel", "Converse", "ConverseStream", "InvokeModelWithResponseStream",
		// bedrock-agent-runtime inference / retrieval — never reachable.
		"InvokeAgent", "Retrieve", "RetrieveAndGenerate", "InvokeFlow", "InvokeInlineAgent",
		// model invocation logging is a config write surface we do not touch.
		"StartIngestionJob",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Stop", "Start", "Put",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Invoke", "Render", "Retry", "Attach", "Detach", "Prepare",
		"Apply", "Tag", "Untag", "Copy",
	}

	for _, ifaceType := range []reflect.Type{
		reflect.TypeOf((*bedrockAPIClient)(nil)).Elem(),
		reflect.TypeOf((*bedrockAgentAPIClient)(nil)).Elem(),
	} {
		for i := 0; i < ifaceType.NumMethod(); i++ {
			name := ifaceType.Method(i).Name
			for _, banned := range forbiddenSubstrings {
				if strings.Contains(name, banned) {
					t.Fatalf("%s exposes forbidden inference/ingestion method %q; the Bedrock adapter is metadata-only", ifaceType.Name(), name)
				}
			}
			for _, prefix := range forbiddenPrefixes {
				if strings.HasPrefix(name, prefix) {
					t.Fatalf("%s exposes mutation method %q (prefix %q); the Bedrock adapter is metadata-only", ifaceType.Name(), name, prefix)
				}
			}
		}
	}
}

// TestAdapterMethodsAreReadOnly asserts every method on both adapter interfaces
// is a List, Get, or ListTagsForResource read so the read surface stays
// explicit and auditable.
func TestAdapterMethodsAreReadOnly(t *testing.T) {
	for _, ifaceType := range []reflect.Type{
		reflect.TypeOf((*bedrockAPIClient)(nil)).Elem(),
		reflect.TypeOf((*bedrockAgentAPIClient)(nil)).Elem(),
	} {
		if ifaceType.NumMethod() == 0 {
			t.Fatalf("%s has no methods; expected the Bedrock read surface", ifaceType.Name())
		}
		for i := 0; i < ifaceType.NumMethod(); i++ {
			name := ifaceType.Method(i).Name
			if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Get") {
				t.Fatalf("%s method %q is neither a List nor Get read", ifaceType.Name(), name)
			}
		}
	}
}
