// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"reflect"
	"testing"
)

func TestEncodeDirectPayloadMatchesJSONOmitEmptyPointers(t *testing.T) {
	t.Parallel()

	type nested struct {
		Name *string `json:"name,omitempty"`
	}
	type sample struct {
		Required        string            `json:"required"`
		OptionalPointer *string           `json:"optional_pointer,omitempty"`
		OptionalSlice   []string          `json:"optional_slice,omitempty"`
		OptionalMap     map[string]string `json:"optional_map,omitempty"`
		OptionalNested  *nested           `json:"optional_nested,omitempty"`
	}

	empty := ""
	value := sample{
		Required:        "required",
		OptionalPointer: &empty,
		OptionalSlice:   []string{},
		OptionalMap:     map[string]string{},
		OptionalNested:  &nested{},
	}

	want, err := encodeToPayload(value)
	if err != nil {
		t.Fatalf("encodeToPayload() error = %v", err)
	}
	got, err := encodeDirectPayload(value)
	if err != nil {
		t.Fatalf("encodeDirectPayload() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("encodeDirectPayload() = %#v, want JSON payload %#v", got, want)
	}
}
