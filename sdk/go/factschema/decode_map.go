// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sync"
)

// attributesField is the struct field name the polymorphic Resource and
// Relationship structs use for their untyped service/verb pass-through. A field
// with this name and a map[string]any type collects every payload key that has
// no other json-tagged field, preserving the JSON-native Go types the raw
// map[string]any already carries.
const attributesField = "Attributes"

// decodeMapInto assigns the payload map's values onto the fields of the struct
// pointed to by out, using each field's json tag, WITHOUT a JSON
// marshal/unmarshal round trip. It is the hot-path replacement for the previous
// json.Marshal(map)+json.Unmarshal(struct) decode, which serialized every fact
// twice and dominated the reducer join-index and node-projection benchmarks
// (No-Regression Evidence in the reducer package docs).
//
// The reducer's payload map comes from a Postgres JSONB unmarshal, so its value
// types are exactly the encoding/json defaults: string, float64 for numbers,
// bool, []any, map[string]any, and nil. decodeMapInto coerces those into the
// struct's field types directly for the common shapes (string, *string, []string,
// *bool, *int32, map[string]any) and falls back to a per-value json round trip
// only for a structurally complex field (a *map[string]string, or a slice of
// structs such as []BlockDevice) — which are small and rare, so the bounded
// fallback keeps the hot identity path serialization-free.
//
// A field named Attributes with a map[string]any type receives every remaining
// payload key, replacing the custom UnmarshalJSON the polymorphic structs used
// for the pass-through. out must be a non-nil pointer to a struct.
func decodeMapInto(payload map[string]any, out any) error {
	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("factschema: decode target must be a non-nil pointer, got %T", out)
	}
	sv := rv.Elem()
	if sv.Kind() != reflect.Struct {
		return fmt.Errorf("factschema: decode target must point to a struct, got %s", sv.Kind())
	}

	plan := structPlanFor(sv.Type())

	for _, f := range plan.fields {
		raw, present := payload[f.jsonName]
		if !present || raw == nil {
			continue
		}
		if err := assignField(sv.Field(f.index), raw); err != nil {
			return fmt.Errorf("factschema: field %q: %w", f.jsonName, err)
		}
	}

	if plan.attributesIndex >= 0 {
		remainder := map[string]any{}
		for key, value := range payload {
			if _, isKnown := plan.known[key]; isKnown {
				continue
			}
			remainder[key] = value
		}
		if len(remainder) > 0 {
			sv.Field(plan.attributesIndex).Set(reflect.ValueOf(remainder))
		}
	}
	return nil
}

// plannedField records one json-tagged struct field: the payload key it maps
// from and its struct field index.
type plannedField struct {
	jsonName string
	index    int
}

// structPlan is the once-computed decode plan for a struct type: its
// json-tagged fields, the set of payload keys they own (so the Attributes
// pass-through captures only the remainder), and the index of the Attributes
// map field (or -1). Caching it keeps decodeMapInto off the per-fact reflection
// walk that dominated the hot-path allocations.
type structPlan struct {
	fields          []plannedField
	known           map[string]struct{}
	attributesIndex int
}

var structPlanCache sync.Map // reflect.Type -> *structPlan

// structPlanFor returns the cached decode plan for a struct type, building it on
// first use. The plan is immutable after construction and safe for concurrent
// reads by parallel reducer workers.
func structPlanFor(t reflect.Type) *structPlan {
	if cached, ok := structPlanCache.Load(t); ok {
		return cached.(*structPlan)
	}
	plan := &structPlan{
		known:           make(map[string]struct{}, t.NumField()),
		attributesIndex: -1,
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Name == attributesField && field.Type.Kind() == reflect.Map {
			plan.attributesIndex = i
			continue
		}
		if field.PkgPath != "" {
			continue // unexported field: never serialized
		}
		jsonName, _, skip := parseJSONTag(field.Tag.Get("json"), field.Name)
		if skip {
			continue
		}
		plan.known[jsonName] = struct{}{}
		plan.fields = append(plan.fields, plannedField{jsonName: jsonName, index: i})
	}
	actual, _ := structPlanCache.LoadOrStore(t, plan)
	return actual.(*structPlan)
}

// assignField coerces one JSONB-native value onto one struct field, mirroring
// encoding/json's default map decoding for the shapes the fact-payload structs
// use. It handles string, *string, []string, *bool, *int32, and map[string]any
// directly, and json-round-trips a single value for any other type (the rare
// *map[string]string / []struct fields) so correctness is never sacrificed for
// the fast path.
func assignField(field reflect.Value, raw any) error {
	switch field.Kind() {
	case reflect.String:
		s, ok := raw.(string)
		if !ok {
			return fmt.Errorf("want string, got %T", raw)
		}
		field.SetString(s)
		return nil

	case reflect.Ptr:
		switch field.Type().Elem().Kind() {
		case reflect.String:
			s, ok := raw.(string)
			if !ok {
				return fmt.Errorf("want string, got %T", raw)
			}
			field.Set(reflect.ValueOf(&s))
			return nil
		case reflect.Bool:
			b, ok := raw.(bool)
			if !ok {
				return fmt.Errorf("want bool, got %T", raw)
			}
			field.Set(reflect.ValueOf(&b))
			return nil
		case reflect.Int32:
			n, err := jsonNumberToInt32(raw)
			if err != nil {
				return err
			}
			field.Set(reflect.ValueOf(&n))
			return nil
		default:
			return jsonRoundTripValue(field, raw)
		}

	case reflect.Slice:
		if field.Type().Elem().Kind() == reflect.String {
			strs, err := anyToStringSlice(raw)
			if err != nil {
				return err
			}
			field.Set(reflect.ValueOf(strs))
			return nil
		}
		return jsonRoundTripValue(field, raw)

	case reflect.Map:
		// A map[string]any field (other than Attributes, handled by the caller)
		// takes the value as-is when it is already the right map shape.
		if m, ok := raw.(map[string]any); ok && field.Type() == reflect.TypeOf(map[string]any{}) {
			field.Set(reflect.ValueOf(m))
			return nil
		}
		return jsonRoundTripValue(field, raw)

	default:
		return jsonRoundTripValue(field, raw)
	}
}

// jsonNumberToInt32 coerces a JSONB-native number (float64 from encoding/json,
// or an in-memory int/int32/int64) into an int32, matching how the previous
// json.Unmarshal path filled *int32 port/hop-limit fields.
//
// It fails closed: a non-integral float (for example a port of 8080.5) or a
// value outside the int32 range is rejected with an error rather than silently
// truncated or wrapped, so a malformed number dead-letters the fact as
// input_invalid instead of projecting a wrong port/hop-limit into the graph.
// This mirrors encoding/json, which returns an UnmarshalTypeError when a JSON
// number does not fit the target int32.
func jsonNumberToInt32(raw any) (int32, error) {
	switch n := raw.(type) {
	case float64:
		if math.Trunc(n) != n {
			return 0, fmt.Errorf("want integer, got non-integral number %v", n)
		}
		if n < math.MinInt32 || n > math.MaxInt32 {
			return 0, fmt.Errorf("number %v out of int32 range", n)
		}
		return int32(n), nil
	case int:
		if int64(n) < math.MinInt32 || int64(n) > math.MaxInt32 {
			return 0, fmt.Errorf("number %v out of int32 range", n)
		}
		return int32(n), nil
	case int32:
		return n, nil
	case int64:
		if n < math.MinInt32 || n > math.MaxInt32 {
			return 0, fmt.Errorf("number %v out of int32 range", n)
		}
		return int32(n), nil
	default:
		return 0, fmt.Errorf("want number, got %T", raw)
	}
}

// anyToStringSlice coerces a JSONB []any of strings (or an already-typed
// []string) into []string, the shape every string-slice payload field uses.
func anyToStringSlice(raw any) ([]string, error) {
	switch v := raw.(type) {
	case []string:
		return v, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("want []string element, got %T", item)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("want []string, got %T", raw)
	}
}

// jsonRoundTripValue is the bounded fallback for a structurally complex field
// (a *map[string]string, or a slice of structs). It marshals only the single
// value and unmarshals it into the field, so the rare complex fields still
// decode exactly as encoding/json would while the common scalar/slice fields on
// the hot path stay serialization-free.
func jsonRoundTripValue(field reflect.Value, raw any) error {
	data, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal value: %w", err)
	}
	target := reflect.New(field.Type())
	if err := json.Unmarshal(data, target.Interface()); err != nil {
		return fmt.Errorf("unmarshal value: %w", err)
	}
	field.Set(target.Elem())
	return nil
}
