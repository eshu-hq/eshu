// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
)

// ViolationKind classifies the specific way a payload schema broke
// compatibility without a corresponding major version bump. Contract System
// v1 §5 defines the compatibility rule this enum encodes: "Major = remove/
// rename key, narrow a type, change stable-key derivation, change meaning."
type ViolationKind string

const (
	// ViolationRemovedRequiredField means a field that was required in the
	// baseline schema is absent from the current schema's properties. A
	// rename of a required field surfaces as this same violation kind (the
	// old name is both removed from properties and removed from required),
	// since from a collector's perspective a rename and a removal are
	// indistinguishable breaks: any consumer still emitting the old field
	// name produces a payload the current schema rejects.
	ViolationRemovedRequiredField ViolationKind = "removed_required_field"

	// ViolationRemovedField means an OPTIONAL field present in the baseline
	// schema's properties is absent from the current schema's properties.
	// Under additionalProperties:false this is a real break: a collector
	// still emitting the dropped field now produces a schema-invalid payload
	// (an input_invalid dead letter, per Contract System v1 §3.2), even
	// though the field was never required. A rename of an optional field
	// surfaces as this violation on the old name. This class is only
	// reported when the baseline schema is fail-closed
	// (additionalProperties:false); an open schema accepts the extra field,
	// so removing it from the declared properties is not a break there.
	ViolationRemovedField ViolationKind = "removed_field"

	// ViolationNarrowedType means a field's declared type or value space
	// shrank — for example "type": "string" gained an "enum" constraint, or
	// the "type" itself changed to a strictly narrower JSON Schema type. A
	// previously valid payload value may no longer validate.
	ViolationNarrowedType ViolationKind = "narrowed_type"

	// ViolationWidenedRequired means a field that was optional (present in
	// properties, absent from required) in the baseline became required in
	// the current schema. This breaks any collector still emitting the
	// field's absence path, so it is treated as a major-only change
	// identically to a removal.
	ViolationWidenedRequired ViolationKind = "widened_required"

	// ViolationAddedRequiredField means a brand-new field (absent from the
	// baseline schema's properties entirely) was added to the current
	// schema's required set. Existing collectors that never emitted it now
	// fail validation, so this is a break without a major bump. It is
	// distinct from ViolationWidenedRequired, which covers a field that
	// already existed as optional in the baseline.
	ViolationAddedRequiredField ViolationKind = "added_required_field"
)

// Violation is one detected breaking change: which field, what kind of
// break, and a human/machine-readable explanation naming both so an external
// collector author can act on it without asking a human (issue #4569
// acceptance criteria).
type Violation struct {
	Kind    ViolationKind
	Field   string
	Message string
}

// String renders the violation as a single line naming the field and the
// violation type explicitly, satisfying the "not a generic 'schema changed'
// message" requirement.
func (v Violation) String() string {
	return fmt.Sprintf("%s: field %q %s", v.Kind, v.Field, v.Message)
}

// jsonSchema is the subset of JSON Schema draft 2020-12 this gate reasons
// about: property type/enum shape, the required-field set, the title (which
// carries this repo's "(schema version N)" marker per
// sdk/go/factschema/internal/schemagen.AWSResourceSchema), and
// additionalProperties (which decides whether removing an optional field is
// a break — see ViolationRemovedField). Fields outside this subset
// (descriptions, $id, etc.) are not compared: they cannot express a breaking
// change under Contract System v1 §5's rule.
//
// AdditionalProperties is a *bool so the gate can tell "explicitly set to
// false" (fail-closed; dropping an optional field is a break) apart from
// "absent/open" (nil; the extra field is still accepted, so the drop is not
// a break). The factschema schemas all set it to false today; parsing it
// keeps the rule correct if that ever changes.
type jsonSchema struct {
	Title                string                    `json:"title"`
	Required             []string                  `json:"required"`
	Properties           map[string]schemaProperty `json:"properties"`
	AdditionalProperties *bool                     `json:"additionalProperties"`
}

// failClosed reports whether the schema sets additionalProperties:false, the
// fail-closed mode under which a payload carrying an undeclared property is
// rejected. Only a fail-closed baseline makes optional-field removal a break.
func (s jsonSchema) failClosed() bool {
	return s.AdditionalProperties != nil && !*s.AdditionalProperties
}

// schemaProperty is one property's type-relevant shape.
type schemaProperty struct {
	Type string        `json:"type"`
	Enum []interface{} `json:"enum"`
}

// versionMarkerPattern matches this repo's checked-in schema title
// convention, "... (schema version N)", as emitted by
// sdk/go/factschema/internal/schemagen.AWSResourceSchema. It is intentionally
// scoped to this one convention rather than attempting to parse arbitrary
// semver out of a free-text title.
var versionMarkerPattern = regexp.MustCompile(`\(schema version (\d+)\)`)

// schemaVersionMajor extracts the major version number embedded in a
// schema's title, returning ok=false when no version marker is present.
func schemaVersionMajor(title string) (int, bool) {
	m := versionMarkerPattern.FindStringSubmatch(title)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// compareSchemas diffs a baseline JSON Schema against the current (working
// tree) version of the same schema file and returns every breaking change
// that lacks a corresponding major version bump. name is the schema's file
// name, used only in returned error messages.
//
// The major-bump escape hatch: if schemaVersionMajor(current.Title) is
// strictly greater than schemaVersionMajor(baseline.Title) (or the baseline
// had no parseable marker at all, i.e. this is a schema's first tagged
// appearance under the new convention), every violation below is suppressed
// — a major bump is exactly how Contract System v1 §5 says a payload family
// is allowed to break its shape. Missing version markers on both sides
// compare as no bump (0, false vs 0, false), so an unversioned schema still
// gets full breaking-change enforcement.
func compareSchemas(name string, baseline, current []byte) ([]Violation, error) {
	var base, cur jsonSchema
	if err := json.Unmarshal(baseline, &base); err != nil {
		return nil, fmt.Errorf("factschema-diff: parse baseline schema %s: %w", name, err)
	}
	if err := json.Unmarshal(current, &cur); err != nil {
		return nil, fmt.Errorf("factschema-diff: parse current schema %s: %w", name, err)
	}

	baseMajor, baseOK := schemaVersionMajor(base.Title)
	curMajor, curOK := schemaVersionMajor(cur.Title)
	if curOK && (!baseOK || curMajor > baseMajor) {
		return nil, nil
	}

	baseRequired := toSet(base.Required)

	var violations []Violation

	// Removal of ANY baseline property absent from the current schema is a
	// break (Contract System v1 §5/§6.1: "remove/rename key", "removed,
	// renamed, or narrowed field" — field, not just required field). The
	// removal half of a rename surfaces here on the old name. A required
	// field is always a break; an optional field is a break only when the
	// baseline is fail-closed (additionalProperties:false), because an open
	// schema still accepts a collector emitting the dropped field.
	for field := range base.Properties {
		if _, stillPresent := cur.Properties[field]; stillPresent {
			continue
		}
		if baseRequired[field] {
			violations = append(violations, Violation{
				Kind:    ViolationRemovedRequiredField,
				Field:   field,
				Message: "was required in the baseline schema and has been removed",
			})
			continue
		}
		if base.failClosed() {
			violations = append(violations, Violation{
				Kind:    ViolationRemovedField,
				Field:   field,
				Message: "was an optional field in the fail-closed baseline schema and has been removed",
			})
		}
	}

	for _, field := range cur.Required {
		if baseRequired[field] {
			continue
		}
		if _, wasPresent := base.Properties[field]; wasPresent {
			// Was optional in the baseline, is now required.
			violations = append(violations, Violation{
				Kind:    ViolationWidenedRequired,
				Field:   field,
				Message: "was optional in the baseline schema and is now required",
			})
			continue
		}
		// Absent from the baseline properties entirely and now required:
		// a brand-new required field existing collectors never emitted.
		violations = append(violations, Violation{
			Kind:    ViolationAddedRequiredField,
			Field:   field,
			Message: "is newly required but was not present in the baseline schema",
		})
	}

	for field, baseProp := range base.Properties {
		curProp, stillPresent := cur.Properties[field]
		if !stillPresent {
			continue // already reported above if it was required
		}
		if narrowed, reason := isNarrowedType(baseProp, curProp); narrowed {
			violations = append(violations, Violation{
				Kind:    ViolationNarrowedType,
				Field:   field,
				Message: reason,
			})
		}
	}

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Field != violations[j].Field {
			return violations[i].Field < violations[j].Field
		}
		return violations[i].Kind < violations[j].Kind
	})

	return violations, nil
}

// isNarrowedType reports whether curProp's value space is a strict subset of
// baseProp's: an enum constraint added where none existed, or the base type
// changed to a different type entirely (treated conservatively as narrowing,
// since this gate cannot prove type-widening is ever safe for a payload
// contract).
func isNarrowedType(base, cur schemaProperty) (bool, string) {
	if len(base.Enum) == 0 && len(cur.Enum) > 0 {
		return true, "gained an enum constraint, narrowing its value space"
	}
	if len(base.Enum) > 0 && len(cur.Enum) > 0 && !enumSupersetOf(cur.Enum, base.Enum) {
		return true, "enum constraint narrowed to a smaller value set"
	}
	if base.Type != "" && cur.Type != "" && base.Type != cur.Type {
		return true, fmt.Sprintf("type changed from %q to %q", base.Type, cur.Type)
	}
	return false, ""
}

// enumSupersetOf reports whether every value in sub also appears in super.
func enumSupersetOf(super, sub []interface{}) bool {
	set := make(map[string]bool, len(super))
	for _, v := range super {
		set[fmt.Sprintf("%v", v)] = true
	}
	for _, v := range sub {
		if !set[fmt.Sprintf("%v", v)] {
			return false
		}
	}
	return true
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
}
