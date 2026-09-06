// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package governanceaudit_test

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// TestUnknownEnumsNamesOnlyFieldsOutsideRegistry: the helper the store logs
// from must name exactly the enum fields this build's registry lacks, with
// the stored value, in column order, and nothing for an all-known event.
func TestUnknownEnumsNamesOnlyFieldsOutsideRegistry(t *testing.T) {
	t.Parallel()

	if got := governanceaudit.UnknownEnums(storedEventFixture()); got != nil {
		t.Fatalf("UnknownEnums(known event) = %v, want nil", got)
	}

	event := storedEventFixture()
	event.Type = "future_event"
	event.ActorClass = "future_class"
	event.ScopeClass = "future_scope"
	event.Decision = "future_decision"
	want := []governanceaudit.UnknownEnum{
		{Field: "event_type", Value: "future_event"},
		{Field: "actor_class", Value: "future_class"},
		{Field: "scope_class", Value: "future_scope"},
		{Field: "decision", Value: "future_decision"},
	}
	if got := governanceaudit.UnknownEnums(event); !reflect.DeepEqual(got, want) {
		t.Fatalf("UnknownEnums(all unknown) = %v, want %v", got, want)
	}

	event = storedEventFixture()
	event.ActorClass = "future_class"
	want = []governanceaudit.UnknownEnum{{Field: "actor_class", Value: "future_class"}}
	if got := governanceaudit.UnknownEnums(event); !reflect.DeepEqual(got, want) {
		t.Fatalf("UnknownEnums(one unknown) = %v, want %v", got, want)
	}
}
