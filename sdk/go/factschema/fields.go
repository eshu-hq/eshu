// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// payloadKeySet is the reflectively derived key contract for one payload
// struct type: the single source of truth decodeAndValidate reads for its
// required-field check and the drift tests compare against the generated JSON
// Schema. Deriving it from the struct — rather than repeating the key list in
// a hand-maintained map — means a new fact kind cannot ship an unwired or
// drifted required set: there is nothing to wire by hand.
//
// Both slices preserve struct field order so error messages and any future
// ordered consumer stay deterministic.
type payloadKeySet struct {
	// Required lists the JSON key names a payload MUST carry: every exported,
	// non-skipped field whose json tag does not include "omitempty". This
	// matches the rule the schema generator's reflector uses to populate a
	// schema's "required" array (github.com/invopop/jsonschema derives
	// required from the absence of "omitempty" only), so the decode validator
	// and the generated schema agree by construction. TestPayloadStructShape-
	// Convention bans the two struct shapes where that rule would be
	// ambiguous, keeping "required" and "optional" unambiguous per field.
	Required []string

	// Known lists every JSON key name the struct serializes: all exported,
	// non-skipped fields regardless of omitempty. It is the full set the
	// generated schema's "properties" object mirrors, and the seam a future
	// open-object kind (one that captures unmodeled top-level keys into an
	// Attributes catch-all) would read to know which keys are already modeled.
	// No such kind exists today, so Known has exactly one consumer now: the
	// schema-vs-derived drift test.
	Known []string
}

// payloadKeySets caches payloadKeySetOf's result per reflect.Type. Deriving
// the set walks the struct's fields once; the cache keeps repeated decodes of
// the same kind off the reflection path. Keyed by reflect.Type, which is
// comparable and stable for the life of the process.
var payloadKeySets sync.Map // reflect.Type -> payloadKeySet

// payloadKeySetOf returns the derived key set for a flat payload struct type,
// computing it once and caching the result. It panics if t is not a struct or
// carries an anonymous (embedded) field: payload structs are flat by contract
// rule, and supporting embedding would require reimplementing encoding/json's
// field-promotion and visibility rules, a silent-divergence risk this module
// exists to avoid. The panic is a programming-error guard, not a runtime input
// path — every payload type is a package-internal struct literal.
func payloadKeySetOf(t reflect.Type) payloadKeySet {
	if cached, ok := payloadKeySets.Load(t); ok {
		return cached.(payloadKeySet)
	}
	ks := derivePayloadKeySet(t)
	payloadKeySets.Store(t, ks)
	return ks
}

// derivePayloadKeySet walks t's fields once, in declared order, building the
// required and known key sets. It is separate from payloadKeySetOf only so the
// cache-miss path is testable without the sync.Map.
func derivePayloadKeySet(t reflect.Type) payloadKeySet {
	if t.Kind() != reflect.Struct {
		panic(fmt.Sprintf("factschema: payloadKeySetOf requires a struct type, got %s", t.Kind()))
	}

	var ks payloadKeySet
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Anonymous {
			panic(fmt.Sprintf(
				"factschema: payload struct %s has embedded field %s; payload structs must be flat",
				t.Name(), field.Name))
		}
		if field.PkgPath != "" {
			continue // unexported field: never serialized
		}

		name, omitEmpty, skip := parseJSONTag(field.Tag.Get("json"), field.Name)
		if skip {
			continue
		}

		ks.Known = append(ks.Known, name)
		if !omitEmpty {
			ks.Required = append(ks.Required, name)
		}
	}
	return ks
}

// requiredPayloadKeys returns the required JSON key names for payload struct
// type T, derived reflectively and cached. decodeAndValidate calls it in place
// of a per-kind hand-maintained map, so the required set for a fact kind is
// always exactly what the struct declares.
func requiredPayloadKeys[T any]() []string {
	return payloadKeySetOf(reflect.TypeFor[T]()).Required
}

// parseJSONTag splits a struct's json tag into its serialized field name and
// options, following encoding/json's own rules so the derived key set matches
// what json.Marshal actually emits:
//
//   - The tag "-" skips the field entirely (skip is true, name is empty).
//   - The tag "-," names the field literally "-" (encoding/json's documented
//     escape for a field whose JSON name is a single dash).
//   - An empty name element (a bare "" or ",omitempty") defaults the name to
//     fieldName, the Go field's own name.
//   - "omitempty" may appear in any option position, not only last.
//   - All other options (for example ",string") are recognized as options and
//     ignored for key-set purposes.
func parseJSONTag(tag, fieldName string) (name string, omitEmpty, skip bool) {
	if tag == "-" {
		return "", false, true
	}

	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = fieldName
	}
	for _, option := range parts[1:] {
		if option == "omitempty" {
			omitEmpty = true
		}
	}
	return name, omitEmpty, false
}
