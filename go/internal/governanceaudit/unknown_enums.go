// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package governanceaudit

// UnknownEnum names one enum field of a stored event whose value is outside
// this build's registry, with the value NormalizeStoredEvent kept. Field is
// the stored column name, which is also the wire name: "event_type",
// "actor_class", "scope_class", or "decision".
type UnknownEnum struct {
	Field string
	Value string
}

// UnknownEnums lists the enum fields of an event whose values this build does
// not know, in column order, so a reader that tolerated them can tell an
// operator which fields a newer build wrote (#6574). It reports a value only
// when the value is also a bounded lowercase token, the same shape
// NormalizeStoredEvent keeps, so every reported value is safe to log no matter
// how the caller obtained the event. A value that fails the shape check is
// never reported: the write-path validator rejects it and the read path never
// returns it, so an unvalidated event cannot leak a raw principal or URL
// through this helper. An event whose four enums are all in the registry
// yields nil.
func UnknownEnums(event Event) []UnknownEnum {
	var unknown []UnknownEnum
	report := func(field, value string, known bool) {
		if known || !validBoundedToken(value) {
			return
		}
		unknown = append(unknown, UnknownEnum{Field: field, Value: value})
	}
	report("event_type", string(event.Type), validEventType(event.Type))
	report("actor_class", string(event.ActorClass), validActorClass(event.ActorClass))
	report("scope_class", string(event.ScopeClass), validScopeClass(event.ScopeClass))
	report("decision", string(event.Decision), validDecision(event.Decision))
	return unknown
}
