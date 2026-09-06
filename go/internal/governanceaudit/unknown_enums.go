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
// operator which fields a newer build wrote (#6574). It validates nothing:
// call it on an event NormalizeStoredEvent already accepted, whose unknown
// values are therefore bounded lowercase tokens safe to log. An event whose
// four enums are all in the registry yields nil.
func UnknownEnums(event Event) []UnknownEnum {
	var unknown []UnknownEnum
	if !validEventType(event.Type) {
		unknown = append(unknown, UnknownEnum{Field: "event_type", Value: string(event.Type)})
	}
	if !validActorClass(event.ActorClass) {
		unknown = append(unknown, UnknownEnum{Field: "actor_class", Value: string(event.ActorClass)})
	}
	if !validScopeClass(event.ScopeClass) {
		unknown = append(unknown, UnknownEnum{Field: "scope_class", Value: string(event.ScopeClass)})
	}
	if !validDecision(event.Decision) {
		unknown = append(unknown, UnknownEnum{Field: "decision", Value: string(event.Decision)})
	}
	return unknown
}
