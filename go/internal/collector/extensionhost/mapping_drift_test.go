// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

// These drift-lock tests guard the hand-written translation in
// (*Source).envelopesForResult, which copies sdkcollector.Fact into
// facts.Envelope field by field (including the nested SourceRef -> Ref submap).
// A field added to either side of that mapping is otherwise silently dropped:
// the code still compiles, the existing behavioural tests still pass, and the
// new field never reaches the durable envelope. Each expected-field map below
// records the role every exported field plays in the mapping, so adding or
// removing a field on Fact, Envelope, SourceRef, or Ref fails the matching
// lock until the mapping and this map are updated together.

// fieldRole documents why an exported struct field is accounted for by the
// extensionhost mapping. It is descriptive only; the lock asserts the set of
// field names, and the role appears in failure messages so a drift points the
// author at the mapping decision they must revisit.
type fieldRole string

const (
	// roleConsumed marks a sdkcollector.Fact field the mapping reads.
	roleConsumed fieldRole = "consumed by envelopesForResult"
	// roleHostDropped marks a sdkcollector.Fact field that intentionally has no
	// durable facts.Envelope counterpart. Redactions is enforced and counted by
	// the SDK validation path (sdk/go/collector/validation.go) before emission,
	// so it never rides into the envelope.
	roleHostDropped fieldRole = "intentionally not carried into the envelope"
	// rolePopulatedFromFact marks a facts.Envelope field the mapping fills from
	// the incoming sdkcollector.Fact.
	rolePopulatedFromFact fieldRole = "populated from the sdkcollector.Fact"
	// roleHostOwned marks a facts.Envelope field the host fills from the
	// WorkItem or synthesizes, not from the Fact.
	roleHostOwned fieldRole = "host-owned (from WorkItem or synthesized)"
	// roleRefField marks a field on the nested SourceRef/Ref submap.
	roleRefField fieldRole = "carried across the SourceRef -> Ref submap"
)

// factMappedFields is every exported field of sdkcollector.Fact and the role it
// plays in envelopesForResult. Redactions is the only field not carried into
// the envelope; everything else is copied.
var factMappedFields = map[string]fieldRole{
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

// envelopeMappedFields is every exported field of facts.Envelope and the role
// it plays in envelopesForResult. The host-owned fields are filled from the
// WorkItem (ScopeID, GenerationID, CollectorKind, FencingToken) or synthesized
// (FactID); the rest come from the Fact.
var envelopeMappedFields = map[string]fieldRole{
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

// sdkSourceRefFields is every exported field of sdkcollector.SourceRef; the
// mapping copies all of them into facts.Ref.
var sdkSourceRefFields = map[string]fieldRole{
	"SourceSystem": roleRefField,
	"ScopeID":      roleRefField,
	"GenerationID": roleRefField,
	"FactKey":      roleRefField,
	"URI":          roleRefField,
	"RecordID":     roleRefField,
}

// factsRefFields is every exported field of facts.Ref; the mapping fills all of
// them from sdkcollector.SourceRef.
var factsRefFields = map[string]fieldRole{
	"SourceSystem":   roleRefField,
	"ScopeID":        roleRefField,
	"GenerationID":   roleRefField,
	"FactKey":        roleRefField,
	"SourceURI":      roleRefField,
	"SourceRecordID": roleRefField,
}

// exportedFieldNames returns the sorted names of every exported field of a
// struct type, descending into anonymous embedded structs.
func exportedFieldNames(t reflect.Type) []string {
	var names []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue // unexported
		}
		names = append(names, field.Name)
	}
	sort.Strings(names)
	return names
}

// structFieldDrift compares the exported fields of a struct type against the
// expected role map and returns human-readable drift problems: exported fields
// with no expected role (a new field the mapping would silently drop) and
// expected fields absent from the struct (a removed field the map still lists).
// Both the positive locks and the negative self-test call this one helper, so a
// regression in the detector fails the negative test rather than passing
// silently.
func structFieldDrift(structType reflect.Type, expected map[string]fieldRole) []string {
	var problems []string
	present := make(map[string]struct{}, structType.NumField())
	for _, name := range exportedFieldNames(structType) {
		present[name] = struct{}{}
		if _, ok := expected[name]; !ok {
			problems = append(problems, "unexpected exported field "+name+
				" is not accounted for by the extensionhost mapping")
		}
	}
	for name, role := range expected {
		if _, ok := present[name]; !ok {
			problems = append(problems, "expected field "+name+
				" ("+string(role)+") is no longer present on the struct")
		}
	}
	sort.Strings(problems)
	return problems
}

func TestExtensionHostMappingFactFieldsAllAccountedFor(t *testing.T) {
	t.Parallel()

	if problems := structFieldDrift(reflect.TypeOf(sdkcollector.Fact{}), factMappedFields); len(problems) > 0 {
		t.Fatalf("sdkcollector.Fact drifted from the extensionhost mapping; update envelopesForResult and factMappedFields together:\n  %v", problems)
	}
}

func TestExtensionHostMappingEnvelopeFieldsAllAccountedFor(t *testing.T) {
	t.Parallel()

	if problems := structFieldDrift(reflect.TypeOf(facts.Envelope{}), envelopeMappedFields); len(problems) > 0 {
		t.Fatalf("facts.Envelope drifted from the extensionhost mapping; update envelopesForResult and envelopeMappedFields together:\n  %v", problems)
	}
}

func TestExtensionHostMappingSourceRefFieldsAllAccountedFor(t *testing.T) {
	t.Parallel()

	if problems := structFieldDrift(reflect.TypeOf(sdkcollector.SourceRef{}), sdkSourceRefFields); len(problems) > 0 {
		t.Fatalf("sdkcollector.SourceRef drifted from the extensionhost mapping; update the SourceRef submap and sdkSourceRefFields together:\n  %v", problems)
	}
	if problems := structFieldDrift(reflect.TypeOf(facts.Ref{}), factsRefFields); len(problems) > 0 {
		t.Fatalf("facts.Ref drifted from the extensionhost mapping; update the SourceRef submap and factsRefFields together:\n  %v", problems)
	}
}

// TestStructFieldDriftDetectsNewField proves the lock actually trips: a struct
// with an exported field missing from the expected map must be reported. If
// structFieldDrift stops detecting drift, this negative test fails, so the
// positive locks above cannot silently rot into no-ops.
func TestStructFieldDriftDetectsDrift(t *testing.T) {
	t.Parallel()

	type roguedStruct struct {
		Kind    string
		Unknown string // deliberately absent from expected
	}
	expected := map[string]fieldRole{"Kind": roleConsumed}

	problems := structFieldDrift(reflect.TypeOf(roguedStruct{}), expected)
	if len(problems) != 1 {
		t.Fatalf("structFieldDrift() problems = %v, want exactly one (the Unknown field)", problems)
	}

	// A removed expected field must also be reported.
	type missingFieldStruct struct {
		Kind string
	}
	withExtraExpected := map[string]fieldRole{"Kind": roleConsumed, "Gone": rolePopulatedFromFact}
	if problems := structFieldDrift(reflect.TypeOf(missingFieldStruct{}), withExtraExpected); len(problems) != 1 {
		t.Fatalf("structFieldDrift() problems for removed field = %v, want exactly one (the Gone field)", problems)
	}
}

// TestExtensionHostMappingPopulatesEveryEnvelopeField runs the real mapping with
// a Fact and WorkItem whose every mapped input is non-zero, then asserts every
// exported facts.Envelope field is non-zero. This ties the name-level locks to
// real behaviour: a field the drift map calls "populated" that the mapping
// leaves at its zero value fails here, and it exercises envelopesForResult
// rather than a re-implementation of it.
func TestExtensionHostMappingPopulatesEveryEnvelopeField(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	fact := fullySetSDKFact(item)
	src := mustNewSource(t, &recordingRunner{result: completeResult(item, fact)}, nil)

	envelopes := src.envelopesForResult(item, completeResult(item, fact))
	if len(envelopes) != 1 {
		t.Fatalf("envelopesForResult() returned %d envelopes, want 1", len(envelopes))
	}

	value := reflect.ValueOf(envelopes[0])
	typ := value.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		if role := envelopeMappedFields[field.Name]; role == "" {
			t.Fatalf("envelope field %s has no role in envelopeMappedFields; the name lock and this test disagree", field.Name)
		}
	}

	// Assert every mapped scalar landed non-zero, recursing into nested struct
	// fields (the SourceRef -> facts.Ref submap) so a single dropped subfield —
	// e.g. SourceRecordID left empty while SourceURI stays set — fails here.
	// reflect.Value.IsZero on a struct is true only when every subfield is zero,
	// so a top-level SourceRef check alone would miss a partial drop; the name
	// lock only checks field names exist, not that each is copied.
	assertMappedFieldsNonZero(t, value, "")
}

// assertMappedFieldsNonZero fails if any exported field reachable from v is its
// zero value, descending one level into nested structs so a dropped subfield in
// the SourceRef -> facts.Ref submap is caught. time.Time is treated as a leaf: a
// valid UTC timestamp carries a nil location, so recursing into its unexported
// fields would spuriously read as partly zero.
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
			t.Fatalf("mapped field %s is zero after mapping a fully-populated fact; the mapping silently drops it", name)
		}
	}
}

// fullySetSDKFact returns a fact whose every mapped field, including every
// SourceRef field, is non-zero so the populate check can assert the whole
// envelope is filled. Tombstone is true so IsTombstone is non-zero.
func fullySetSDKFact(item workflow.WorkItem) sdkcollector.Fact {
	fact := testSDKFact(item)
	fact.Tombstone = true
	fact.SourceRef = sdkcollector.SourceRef{
		SourceSystem: item.SourceSystem,
		ScopeID:      item.ScopeID,
		GenerationID: item.GenerationID,
		FactKey:      "scorecard-check:binary-artifacts",
		URI:          "https://example.invalid/scorecard/results.json",
		RecordID:     "binary-artifacts",
	}
	return fact
}
