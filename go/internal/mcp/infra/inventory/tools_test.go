// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package infrainventorytools

import (
	"reflect"
	"testing"
)

func TestToolsOwnsAggregateDefinitionsInRegistrationOrder(t *testing.T) {
	t.Parallel()

	var got []string
	for _, definition := range Tools() {
		got = append(got, definition.Name)
	}
	want := []string{"count_infra_resources", "get_infra_resource_inventory"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tools() names = %q, want %q", got, want)
	}
}
