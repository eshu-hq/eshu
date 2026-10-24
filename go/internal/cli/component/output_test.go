// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCLIComponentJSONOpensWithIDAndCarriesVersion pins the part of the
// `eshu component list --json` shape that the Helm reconcile init container
// reads with shell pattern matching (deploy/helm/eshu, the component
// activations script). It splits the listing on `{"id":"` and then looks for
// `"version":"<v>"` inside the same component object. If the field order or
// the key names change, change that script and its test in
// go/internal/runtime together with this one.
func TestCLIComponentJSONOpensWithIDAndCarriesVersion(t *testing.T) {
	t.Parallel()

	body, err := json.Marshal(CLIComponent{ID: "dev.example.collector", Version: "0.1.0", Name: "Example", Publisher: "example"})
	if err != nil {
		t.Fatalf("marshal CLIComponent: %v", err)
	}
	text := string(body)
	if !strings.HasPrefix(text, `{"id":"dev.example.collector",`) {
		t.Fatalf("CLIComponent JSON = %s, want it to open with the id key first", text)
	}
	if !strings.Contains(text, `"version":"0.1.0"`) {
		t.Fatalf("CLIComponent JSON = %s, want a version key holding the version", text)
	}
}
