// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNumberSlicePublicDecoders(t *testing.T) {
	for _, token := range []string{"8e1", "80.0"} {
		n := json.Number(token)
		pod, err := DecodeKubernetesLivePodTemplate(Envelope{FactKind: FactKindKubernetesLivePodTemplate, SchemaVersion: "1.0.0", Payload: map[string]any{"object_id": "x", "containers": []any{map[string]any{"ports": []any{n}}}}})
		if err != nil {
			t.Fatalf("pod: %v", err)
		}
		if got := pod.Containers[0].Ports; !reflect.DeepEqual(got, []int32{80}) {
			t.Errorf("ports=%v", got)
		}
		section, err := DecodeDocumentationSection(Envelope{FactKind: FactKindDocumentationSection, SchemaVersion: "1.0.0", Payload: map[string]any{"document_id": "d", "revision_id": "r", "section_id": "s", "ordinal_path": []any{n}}})
		if err != nil {
			t.Fatalf("section: %v", err)
		}
		if !reflect.DeepEqual(section.OrdinalPath, []int{80}) {
			t.Errorf("ordinals=%v", section.OrdinalPath)
		}
		summary, err := DecodeCodeFunctionSummary(Envelope{FactKind: FactKindCodeFunctionSummary, SchemaVersion: "1.0.0", Payload: map[string]any{"function_id": "f", "param_to_return": []any{n}}})
		if err != nil {
			t.Fatalf("summary: %v", err)
		}
		if !reflect.DeepEqual(summary.ParamToReturn, []int{80}) {
			t.Errorf("parameters=%v", summary.ParamToReturn)
		}
	}
}

func TestNumberSliceLegacyMembersAndAtomicity(t *testing.T) {
	for _, destination := range []any{[]int(nil), []int32(nil)} {
		typ := reflect.TypeOf(destination)
		for _, legacy := range []any{float64(81), float32(82), int(83), uint(84), nil, (*int)(nil), "bad", true, float64(1.5)} {
			input := []any{json.Number("8e1"), legacy}
			before := append([]any(nil), input...)
			field := reflect.New(typ).Elem()
			initial := reflect.MakeSlice(typ, 1, 1)
			initial.Index(0).SetInt(7)
			field.Set(initial)
			err := assignField(field, input)
			oracle := reflect.New(typ.Elem()).Elem()
			oldErr := jsonRoundTripValue(oracle, legacy)
			if (err != nil) != (oldErr != nil) {
				t.Fatalf("%s legacy %T: error=%v oracle=%v", typ, legacy, err, oldErr)
			}
			if err == nil {
				if field.Len() != 2 || field.Index(0).Int() != 80 || field.Index(1).Int() != oracle.Int() {
					t.Fatalf("%s values=%v", typ, field.Interface())
				}
			} else if field.Len() != 1 || field.Index(0).Int() != 7 {
				t.Fatal("partial destination mutation")
			}
			if !reflect.DeepEqual(input, before) {
				t.Fatal("input mutation")
			}
		}
	}
}

func TestNumberSliceErrorsAreBoundedAndClassified(t *testing.T) {
	for _, token := range []string{"8.1", "2147483648", "1e99999999999999999", "1e-99999999999999999", "01", "NaN", strings.Repeat("9", 10000)} {
		_, err := DecodeKubernetesLivePodTemplate(Envelope{FactKind: FactKindKubernetesLivePodTemplate, SchemaVersion: "1.0.0", Payload: map[string]any{"object_id": "x", "containers": []any{map[string]any{"ports": []any{json.Number(token)}}}}})
		var classified *DecodeError
		if !errors.As(err, &classified) || classified.Classification != ClassificationInputInvalid {
			t.Fatalf("want classified error, got %v", err)
		}
		if len(err.Error()) > 512 {
			t.Fatalf("unbounded numeric error: %d", len(err.Error()))
		}
	}
}

func TestNumberSliceUnchangedFallback(t *testing.T) {
	for _, input := range []any{nil, []any{}, []any{float64(80), nil}, []int{80}, []int32{80}, []any{float32(80), uint(81)}} {
		for _, destination := range []any{[]int(nil), []int32(nil)} {
			typ := reflect.TypeOf(destination)
			got := reflect.New(typ).Elem()
			want := reflect.New(typ).Elem()
			err := assignField(got, input)
			oldErr := jsonRoundTripValue(want, input)
			if (err != nil) != (oldErr != nil) || !reflect.DeepEqual(got.Interface(), want.Interface()) {
				t.Fatalf("legacy %T to %s differs: got=%v %v want=%v %v", input, typ, got.Interface(), err, want.Interface(), oldErr)
			}
		}
	}
}
