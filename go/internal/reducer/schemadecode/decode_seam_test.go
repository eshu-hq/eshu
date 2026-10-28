// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
)

// decodeCase describes one Decode* seam function: the envelope kind it
// serves, a zero SDK struct whose type drives payload construction, the
// seam call itself, one doc-named required field for the negative test,
// and scalar spot-checks that prove values round-trip (not just nil
// errors) on the success path.
type decodeCase struct {
	name          string
	kind          string
	target        any
	decode        func(facts.Envelope) (any, error)
	requiredField string
	spotFields    map[string]any
	// mutate adjusts the populated payload before decode for kinds
	// whose decoders validate formats beyond presence.
	mutate func(map[string]any)
	// typeMismatch switches the negative path from deleting the
	// required field to stuffing a wrong-typed value into it, for
	// kinds whose every field is optional (there is nothing to
	// delete, so malformation must come from a type error).
	typeMismatch bool
}

// jsonTagName splits a json struct tag into its key name and whether the
// field is optional. ok is false for skipped or untagged fields.
func jsonTagName(tag string) (name string, optional bool, ok bool) {
	if tag == "" {
		return "", false, false
	}
	head, _, _ := strings.Cut(tag, ",")
	if head == "" || head == "-" {
		return "", false, false
	}
	return head, strings.Contains(tag, "omitempty"), true
}

// populateDummy builds a type-appropriate dummy payload value for one
// struct field type: strings name themselves, collections carry one
// element, nested structs recurse, and time.Time renders as RFC 3339.
func populateDummy(field reflect.StructField) any {
	return dummyForType(field.Type, field.Name)
}

func dummyForType(typ reflect.Type, name string) any {
	if typ == nil {
		return "test-" + name
	}
	if typ.PkgPath() == "time" && typ.Name() == "Time" {
		return "2026-03-04T05:06:07Z"
	}
	if typ.Name() == "RawMessage" {
		return map[string]any{"k": "v"}
	}
	switch typ.Kind() {
	case reflect.String:
		return "test-" + name
	case reflect.Bool:
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return 1
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return uint64(1)
	case reflect.Float32, reflect.Float64:
		return 1.0
	case reflect.Slice:
		return []any{dummyForType(typ.Elem(), name)}
	case reflect.Array:
		return []any{dummyForType(typ.Elem(), name)}
	case reflect.Map:
		return map[string]any{"k": dummyForType(typ.Elem(), name)}
	case reflect.Struct:
		return populatedPayloadForType(typ)
	case reflect.Pointer:
		return dummyForType(typ.Elem(), name)
	case reflect.Interface:
		return "test-" + name
	default:
		return "test-" + name
	}
}

// populatedPayloadForType builds a payload map carrying every required
// (non-omitempty) field of the struct type: the minimal shape whose
// absence of any one key must dead-letter.
func populatedPayloadForType(typ reflect.Type) map[string]any {
	out := map[string]any{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, optional, ok := jsonTagName(field.Tag.Get("json"))
		if !ok || optional {
			continue
		}
		out[name] = populateDummy(field)
	}
	return out
}

// decodeTestEnvelope wraps a payload in the internal envelope shape the
// seam accepts. The schema version stays empty so every case also covers
// the version-less normalization path in FactschemaEnvelope.
func decodeTestEnvelope(kind string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:   "fact-seam-" + strings.ReplaceAll(kind, ".", "-"),
		FactKind: kind,
		Payload:  payload,
	}
}

// decodedFieldByJSON extracts one decoded struct field by its json key,
// dereferencing pointers, so table tests can assert round-trips without
// per-kind type switches.
func decodedFieldByJSON(t *testing.T, decoded any, name string) any {
	t.Helper()
	value := reflect.ValueOf(decoded)
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	typ := value.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		jsonName, _, ok := jsonTagName(field.Tag.Get("json"))
		if !ok || jsonName != name {
			continue
		}
		got := value.Field(i)
		for got.Kind() == reflect.Pointer {
			if got.IsNil() {
				return nil
			}
			got = got.Elem()
		}
		return got.Interface()
	}
	t.Fatalf("decoded %T has no json field %q", decoded, name)
	return nil
}

func scalarToFloat(value any) (float64, bool) {
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}

// requireRoundTrip fails when a decoded scalar differs from the payload
// value that produced it, tolerating Go numeric conversions across the
// payload map and the typed struct.
func requireRoundTrip(t *testing.T, kind, field string, decoded, want any) {
	t.Helper()
	if wantFloat, ok := scalarToFloat(want); ok {
		gotFloat, ok := scalarToFloat(decoded)
		if !ok || gotFloat != wantFloat {
			t.Fatalf("%s: decoded field %q = %v (%T), want %v", kind, field, decoded, decoded, want)
		}
		return
	}
	if fmt.Sprintf("%v", decoded) != fmt.Sprintf("%v", want) {
		t.Fatalf("%s: decoded field %q = %v (%T), want %v", kind, field, decoded, decoded, want)
	}
}

// requireInputInvalidQuarantine pins the seam's error contract at the
// package boundary: a malformed payload must unwrap to a non-retryable
// *factdecode.FactDecodeError classified input_invalid, itself wrapping a
// *factschema.DecodeError that names the offending kind and field.
func requireInputInvalidQuarantine(t *testing.T, err error, kind, field string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: decode error = nil, want input_invalid quarantine for missing %q", kind, field)
	}
	var factDecodeErr *factdecode.FactDecodeError
	if !errors.As(err, &factDecodeErr) {
		t.Fatalf("%s: error = %v (%T), want it to unwrap to *factdecode.FactDecodeError", kind, err, err)
	}
	if factDecodeErr.Retryable() {
		t.Errorf("%s: FactDecodeError.Retryable() = true, want false: a malformed payload can never succeed on replay", kind)
	}
	if got, want := factDecodeErr.FailureClass(), factschema.ClassificationInputInvalid; got != want {
		t.Errorf("%s: FailureClass() = %q, want %q", kind, got, want)
	}
	var decodeErr *factschema.DecodeError
	if !errors.As(err, &decodeErr) {
		t.Fatalf("%s: error = %v, want it to unwrap to *factschema.DecodeError", kind, err)
	}
	if got := decodeErr.FactKind; got != kind {
		t.Errorf("%s: DecodeError.FactKind = %q, want %q", kind, got, kind)
	}
	if got := decodeErr.Field; got != field {
		t.Errorf("%s: DecodeError.Field = %q, want %q", kind, got, field)
	}
}

// runDecodeCases drives every case through its success path (full
// required payload decodes, spot fields round-trip) and its quarantine
// path (one required field removed dead-letters naming that field).
func runDecodeCases(t *testing.T, cases []decodeCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name+"/Success", func(t *testing.T) {
			t.Parallel()
			payload := populatedPayloadForType(reflect.TypeOf(tc.target).Elem())
			for field, value := range tc.spotFields {
				payload[field] = value
			}
			if tc.mutate != nil {
				tc.mutate(payload)
			}
			decoded, err := tc.decode(decodeTestEnvelope(tc.kind, payload))
			if err != nil {
				t.Fatalf("%s: decode error = %v, want nil (payload=%v)", tc.kind, err, payload)
			}
			for field, want := range tc.spotFields {
				requireRoundTrip(t, tc.kind, field, decodedFieldByJSON(t, decoded, field), want)
			}
		})
		t.Run(tc.name+"/MissingFieldQuarantines", func(t *testing.T) {
			t.Parallel()
			payload := populatedPayloadForType(reflect.TypeOf(tc.target).Elem())
			for field, value := range tc.spotFields {
				if field != tc.requiredField {
					payload[field] = value
				}
			}
			if tc.mutate != nil {
				tc.mutate(payload)
			}
			if tc.typeMismatch {
				payload[tc.requiredField] = 12345
			} else {
				delete(payload, tc.requiredField)
			}
			_, err := tc.decode(decodeTestEnvelope(tc.kind, payload))
			// A wrong-typed optional value fails assignment without
			// field attribution in the contracts module, so the
			// quarantine carries an empty field while keeping the
			// kind, classification, and non-retryable shape.
			wantField := tc.requiredField
			if tc.typeMismatch {
				wantField = ""
			}
			requireInputInvalidQuarantine(t, err, tc.kind, wantField)
		})
	}
}
