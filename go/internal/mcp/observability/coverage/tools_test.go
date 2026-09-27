// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package observabilitycoveragetools

import (
	"reflect"
	"testing"
)

func TestToolsOwnsCoverageDefinitionInRegistrationOrder(t *testing.T) {
	t.Parallel()

	var got []string
	for _, definition := range Tools() {
		got = append(got, definition.Name)
	}
	want := []string{"list_observability_coverage_correlations"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tools() names = %q, want %q", got, want)
	}
}
