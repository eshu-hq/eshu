// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"os"
	"strings"
	"testing"
)

// TestW1bEncodePathsUseDirectMaps locks issue #4788 to the #4785 emit-path
// decision: every Encode function in the adopted Azure, GCP, and Kubernetes
// live families must build the payload map directly instead of taking the
// slower JSON round trip through encodeToPayload.
func TestW1bEncodePathsUseDirectMaps(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"decode_azure.go",
		"decode_gcp.go",
		"decode_kuberneteslive.go",
	} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if strings.Contains(string(raw), "encodeToPayload(") {
				t.Fatalf("%s still calls encodeToPayload; rewrite #4788 Encode functions to direct-map form", path)
			}
		})
	}
}
