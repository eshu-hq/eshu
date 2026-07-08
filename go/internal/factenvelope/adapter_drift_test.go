// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factenvelope

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

type fieldRole string

const (
	roleConsumed          fieldRole = "consumed by generated adapter"
	roleHostDropped       fieldRole = "validated by SDK and intentionally not persisted"
	roleHostOwned         fieldRole = "host-owned adapter option"
	rolePopulatedFromFact fieldRole = "populated from SDK fact"
	rolePopulatedInternal fieldRole = "populated from internal envelope"
	roleRefField          fieldRole = "carried across source-ref mapping"
)

var sdkFactFields = map[string]fieldRole{
	"Kind":             roleConsumed,
	"SchemaVersion":    roleConsumed,
	"StableKey":        roleConsumed,
	"SourceConfidence": roleConsumed,
	"ObservedAt":       roleConsumed,
	"Tombstone":        roleConsumed,
	"SourceRef":        roleConsumed,
	"Payload":          roleConsumed,
	"Redactions":       roleHostDropped,
}

var internalEnvelopeFields = map[string]fieldRole{
	"FactID":           roleHostOwned,
	"ScopeID":          roleHostOwned,
	"GenerationID":     roleHostOwned,
	"CollectorKind":    roleHostOwned,
	"FencingToken":     roleHostOwned,
	"FactKind":         rolePopulatedFromFact,
	"StableFactKey":    rolePopulatedFromFact,
	"SchemaVersion":    rolePopulatedFromFact,
	"SourceConfidence": rolePopulatedFromFact,
	"ObservedAt":       rolePopulatedFromFact,
	"Payload":          rolePopulatedFromFact,
	"IsTombstone":      rolePopulatedFromFact,
	"SourceRef":        rolePopulatedFromFact,
}

var factschemaEnvelopeFields = map[string]fieldRole{
	"FactKind":         rolePopulatedInternal,
	"SchemaVersion":    rolePopulatedInternal,
	"StableFactKey":    rolePopulatedInternal,
	"ScopeID":          rolePopulatedInternal,
	"GenerationID":     rolePopulatedInternal,
	"CollectorKind":    rolePopulatedInternal,
	"SourceConfidence": rolePopulatedInternal,
	"ObservedAt":       rolePopulatedInternal,
	"IsTombstone":      rolePopulatedInternal,
	"SourceRef":        rolePopulatedInternal,
	"Payload":          rolePopulatedInternal,
}

var sdkSourceRefFields = map[string]fieldRole{
	"SourceSystem": roleRefField,
	"ScopeID":      roleRefField,
	"GenerationID": roleRefField,
	"FactKey":      roleRefField,
	"URI":          roleRefField,
	"RecordID":     roleRefField,
}

var internalRefFields = map[string]fieldRole{
	"SourceSystem":   roleRefField,
	"ScopeID":        roleRefField,
	"GenerationID":   roleRefField,
	"FactKey":        roleRefField,
	"SourceURI":      roleRefField,
	"SourceRecordID": roleRefField,
}

func TestGeneratedAdapterFieldsAllAccountedFor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		typ      reflect.Type
		expected map[string]fieldRole
	}{
		{name: "sdk fact", typ: reflect.TypeOf(sdkcollector.Fact{}), expected: sdkFactFields},
		{name: "internal envelope", typ: reflect.TypeOf(facts.Envelope{}), expected: internalEnvelopeFields},
		{name: "factschema envelope", typ: reflect.TypeOf(factschema.Envelope{}), expected: factschemaEnvelopeFields},
		{name: "sdk source ref", typ: reflect.TypeOf(sdkcollector.SourceRef{}), expected: sdkSourceRefFields},
		{name: "internal source ref", typ: reflect.TypeOf(facts.Ref{}), expected: internalRefFields},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if problems := structFieldDrift(tc.typ, tc.expected); len(problems) > 0 {
				t.Fatalf("%s drifted from generated adapter mapping:\n  %v", tc.name, problems)
			}
		})
	}
}

func TestStructFieldDriftDetectsDrift(t *testing.T) {
	t.Parallel()

	type extraFieldStruct struct {
		Kind    string
		Unknown string
	}
	if problems := structFieldDrift(reflect.TypeOf(extraFieldStruct{}), map[string]fieldRole{"Kind": roleConsumed}); len(problems) != 1 {
		t.Fatalf("structFieldDrift() extra-field problems = %v, want exactly one", problems)
	}

	type missingFieldStruct struct {
		Kind string
	}
	expected := map[string]fieldRole{"Kind": roleConsumed, "Gone": rolePopulatedFromFact}
	if problems := structFieldDrift(reflect.TypeOf(missingFieldStruct{}), expected); len(problems) != 1 {
		t.Fatalf("structFieldDrift() removed-field problems = %v, want exactly one", problems)
	}
}

func TestGeneratedAdapterPopulatesEveryDurableField(t *testing.T) {
	t.Parallel()

	fact := fullySetSDKFact()
	env := InternalFromSDKFact(fact, InternalEnvelopeOptions{
		ComponentID:   "component-1",
		ScopeID:       "scope-1",
		GenerationID:  "generation-1",
		CollectorKind: "extension",
		FencingToken:  7,
	})

	assertMappedFieldsNonZero(t, reflect.ValueOf(env), "")
}

func exportedFieldNames(t reflect.Type) []string {
	var names []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		names = append(names, field.Name)
	}
	sort.Strings(names)
	return names
}

func structFieldDrift(structType reflect.Type, expected map[string]fieldRole) []string {
	var problems []string
	present := make(map[string]struct{}, structType.NumField())
	for _, name := range exportedFieldNames(structType) {
		present[name] = struct{}{}
		if _, ok := expected[name]; !ok {
			problems = append(problems, "unexpected exported field "+name+" has no generated adapter role")
		}
	}
	for name, role := range expected {
		if _, ok := present[name]; !ok {
			problems = append(problems, "expected field "+name+" ("+string(role)+") is no longer present")
		}
	}
	sort.Strings(problems)
	return problems
}

func assertMappedFieldsNonZero(t *testing.T, v reflect.Value, path string) {
	t.Helper()
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		fieldValue := v.Field(i)
		name := path + field.Name
		if fieldValue.Kind() == reflect.Struct && fieldValue.Type() != reflect.TypeOf(time.Time{}) {
			assertMappedFieldsNonZero(t, fieldValue, name+".")
			continue
		}
		if fieldValue.IsZero() {
			t.Fatalf("mapped field %s is zero after mapping a fully populated fact", name)
		}
	}
}

func fullySetSDKFact() sdkcollector.Fact {
	return sdkcollector.Fact{
		Kind:             "aws_resource",
		SchemaVersion:    "1.0.0",
		StableKey:        "aws:resource:res-1",
		SourceConfidence: sdkcollector.SourceConfidenceObserved,
		ObservedAt:       time.Date(2026, time.July, 8, 12, 30, 0, 0, time.UTC),
		Tombstone:        true,
		SourceRef: sdkcollector.SourceRef{
			SourceSystem: "aws",
			ScopeID:      "scope-from-ref",
			GenerationID: "generation-from-ref",
			FactKey:      "aws:resource:res-1",
			URI:          "aws://resource/res-1",
			RecordID:     "res-1",
		},
		Payload:    map[string]any{"resource_id": "res-1"},
		Redactions: []sdkcollector.Redaction{{Field: "secret", Reason: "removed"}},
	}
}
