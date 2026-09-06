// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package governanceaudit_test

import (
	"reflect"
	"strings"
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

// TestUnknownEnumsSkipsValuesThatAreNotBoundedTokens: the helper is safe by
// construction, not by caller discipline. An out-of-registry value that fails
// the bounded-token shape (a URL, a principal with spaces, mixed case, an
// over-length string) must never be reported, because the store logs the
// reported value and a caller that skipped NormalizeStoredEvent must not be
// able to leak it (#6584 review).
func TestUnknownEnumsSkipsValuesThatAreNotBoundedTokens(t *testing.T) {
	t.Parallel()

	unsafe := []string{
		"https://example.com/principal",
		"alice smith",
		"Future_Class",
		"user@example.com",
		"../../etc/passwd",
		strings.Repeat("a", 65),
		"",
	}
	for _, value := range unsafe {
		event := storedEventFixture()
		event.Type = governanceaudit.EventType(value)
		event.ActorClass = governanceaudit.ActorClass(value)
		event.ScopeClass = governanceaudit.ScopeClass(value)
		event.Decision = governanceaudit.Decision(value)
		if got := governanceaudit.UnknownEnums(event); got != nil {
			t.Fatalf("UnknownEnums(unsafe %q) = %v, want nil", value, got)
		}
	}

	// A safe unknown token beside an unsafe one: only the token is reported.
	event := storedEventFixture()
	event.ActorClass = "future_class"
	event.ScopeClass = "https://example.com/scope"
	want := []governanceaudit.UnknownEnum{{Field: "actor_class", Value: "future_class"}}
	if got := governanceaudit.UnknownEnums(event); !reflect.DeepEqual(got, want) {
		t.Fatalf("UnknownEnums(mixed) = %v, want %v", got, want)
	}
}
