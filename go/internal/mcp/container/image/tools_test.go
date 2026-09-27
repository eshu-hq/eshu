// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimagetools

import (
	"reflect"
	"testing"
)

func TestToolsOwnsIdentityDefinitionsInRegistrationOrder(t *testing.T) {
	t.Parallel()

	var got []string
	for _, definition := range Tools() {
		got = append(got, definition.Name)
	}
	want := []string{"list_container_image_identities", "list_container_image_tag_history"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tools() names = %q, want %q", got, want)
	}
}

func TestAggregateToolsOwnsAggregateDefinitionsInRegistrationOrder(t *testing.T) {
	t.Parallel()

	var got []string
	for _, definition := range AggregateTools() {
		got = append(got, definition.Name)
	}
	want := []string{"count_container_image_identities", "get_container_image_identity_inventory"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AggregateTools() names = %q, want %q", got, want)
	}
}
