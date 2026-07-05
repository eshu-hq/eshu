// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"encoding/json"
	"fmt"
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
// use. It handles string, *string, []string, *bool, *int, *int32, *float64,
// map[string]string, and map[string]any directly, and json-round-trips a
// single value for any other type (the rare *map[string]string / []struct
// fields) so correctness is never sacrificed for the fast path.
func assignField(field reflect.Value, raw any) error {
	switch field.Kind() {
	case reflect.String:
		s, ok := raw.(string)
		if !ok {
			return fmt.Errorf("want string, got %T", raw)
		}
		field.SetString(s)
		return nil

	case reflect.Int64:
		n, err := jsonNumberToInt64(raw)
		if err != nil {
			return err
		}
		field.SetInt(n)
		return nil

	case reflect.Float64:
		n, err := jsonNumberToFloat64(raw)
		if err != nil {
			return err
		}
		field.SetFloat(n)
		return nil

	case reflect.Bool:
		b, ok := raw.(bool)
		if !ok {
			return fmt.Errorf("want bool, got %T", raw)
		}
		field.SetBool(b)
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
		case reflect.Int:
			// *int fast path (for example secretsiam/v1's VaultACLPolicyRule.
			// PathDepth and VaultKVMetadata.PathDepth): a direct type-switch
			// coercion, mirroring the Int32/Int64/Float64 pointer cases below,
			// instead of falling through to the default branch's
			// jsonRoundTripValue marshal/unmarshal fallback -- the same gap
			// class the map[string]string (Wave 4b) and float64 (Wave 4c)
			// fast paths closed for their own first-occurrence field shapes.
			n, err := jsonNumberToInt(raw)
			if err != nil {
				return err
			}
			field.Set(reflect.ValueOf(&n))
			return nil
		case reflect.Int32:
			n, err := jsonNumberToInt32(raw)
			if err != nil {
				return err
			}
			field.Set(reflect.ValueOf(&n))
			return nil
		case reflect.Int64:
			n, err := jsonNumberToInt64(raw)
			if err != nil {
				return err
			}
			field.Set(reflect.ValueOf(&n))
			return nil
		case reflect.Float64:
			// *float64 fast path (for example vulnerability.cve's optional
			// CVSSScore): a direct type-switch coercion, mirroring the Int32/
			// Int64 pointer cases above, instead of falling through to the
			// default branch's jsonRoundTripValue marshal/unmarshal fallback.
			n, err := jsonNumberToFloat64(raw)
			if err != nil {
				return err
			}
			field.Set(reflect.ValueOf(&n))
			return nil
		case reflect.Map:
			if field.Type().Elem() == reflect.TypeOf(map[string]string{}) {
				m, ok := anyToStringMap(raw)
				if !ok {
					return jsonRoundTripValue(field, raw)
				}
				field.Set(reflect.ValueOf(&m))
				return nil
			}
			return jsonRoundTripValue(field, raw)
		case reflect.Struct:
			// A pointer to a nested payload struct (for example ImageManifest's
			// *Descriptor config) decodes through the same marshal-free
			// decodeMapInto recursion, so a single nested object never triggers a
			// json round trip. Already-typed (non-map) values fall back so a
			// caller passing the concrete struct still decodes correctly.
			m, ok := asObjectMap(raw)
			if !ok {
				return jsonRoundTripValue(field, raw)
			}
			nested := reflect.New(field.Type().Elem())
			if err := decodeMapInto(m, nested.Interface()); err != nil {
				return err
			}
			field.Set(nested)
			return nil
		default:
			return jsonRoundTripValue(field, raw)
		}

	case reflect.Slice:
		switch field.Type().Elem().Kind() {
		case reflect.String:
			strs, err := anyToStringSlice(raw)
			if err != nil {
				return err
			}
			field.Set(reflect.ValueOf(strs))
			return nil
		case reflect.Struct:
			// A slice of nested payload structs (for example ImageManifest's
			// []Descriptor layers, ImageIndex's []Descriptor manifests, or
			// awsv1.EC2InstancePosture's []BlockDevice) decodes element-by-element
			// through decodeMapInto when every element is an object map, so a
			// many-element list stays off the json round-trip path. A slice whose
			// elements are not object maps (an already-typed []Struct, or a scalar
			// element) falls back to the json round trip so correctness is never
			// sacrificed for the fast path.
			if err, handled := assignStructSlice(field, raw); handled {
				return err
			}
			return jsonRoundTripValue(field, raw)
		default:
			return jsonRoundTripValue(field, raw)
		}

	case reflect.Map:
		// A map[string]any field (other than Attributes, handled by the caller)
		// takes the value as-is when it is already the right map shape.
		if m, ok := raw.(map[string]any); ok && field.Type() == reflect.TypeOf(map[string]any{}) {
			field.Set(reflect.ValueOf(m))
			return nil
		}
		// A map[string]string field (for example a pod-template label selector, a
		// descriptor's annotations, or a manifest's config_labels) takes a fast
		// type-assert/coerce path rather than the marshal/unmarshal fallback:
		// measured 15x faster on the same shape (Benchmark No-Regression Evidence,
		// kubernetes_live wave), with identical output for every value the JSONB
		// decode path can produce (a string-valued map[string]any, or an
		// already-typed map[string]string).
		if field.Type() == reflect.TypeOf(map[string]string{}) {
			if m, ok := anyToStringMap(raw); ok {
				field.Set(reflect.ValueOf(m))
				return nil
			}
		}
		return jsonRoundTripValue(field, raw)

	default:
		return jsonRoundTripValue(field, raw)
	}
}

// anyToStringMap coerces a JSONB-native map[string]any of string values (or an
// already-typed map[string]string) into map[string]string. ok is false when
// raw is not one of those two shapes, or when a map[string]any value is not a
// string — the caller falls back to jsonRoundTripValue for those rare cases so
// correctness is never sacrificed for the fast path.
func anyToStringMap(raw any) (map[string]string, bool) {
	switch v := raw.(type) {
	case map[string]string:
		return v, true
	case map[string]any:
		out := make(map[string]string, len(v))
		for key, value := range v {
			s, ok := value.(string)
			if !ok {
				return nil, false
			}
			out[key] = s
		}
		return out, true
	default:
		return nil, false
	}
}

// asObjectMap returns raw as a map[string]any when it is one, coercing the
// JSONB-native map[string]any shape a Postgres payload carries. It reports false
// for any other shape (a scalar, an already-typed struct value) so the caller
// can fall back to the json round trip.
func asObjectMap(raw any) (map[string]any, bool) {
	m, ok := raw.(map[string]any)
	return m, ok
}

// assignStructSlice decodes a slice of object maps into a slice of nested
// payload structs, decoding each element through the marshal-free decodeMapInto.
// It accepts both the JSONB-native []any-of-map shape a Postgres payload carries
// and the []map[string]any shape in-memory callers build.
//
// It returns handled=false (without touching field) when raw is not a slice, or
// when any element is not an object map, so the caller falls back to the json
// round trip for an already-typed or scalar-element slice — correctness is never
// sacrificed for the fast path. When handled is true the returned error is the
// decode result (nil on success). The (error, bool) order mirrors the caller's
// `if err, handled := …; handled { return err }` fast-path guard.
func assignStructSlice(field reflect.Value, raw any) (err error, handled bool) {
	rv := reflect.ValueOf(raw)
	if rv.Kind() != reflect.Slice {
		return nil, false
	}
	out := reflect.MakeSlice(field.Type(), 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		m, ok := asObjectMap(rv.Index(i).Interface())
		if !ok {
			return nil, false
		}
		elem := reflect.New(field.Type().Elem())
		if decodeErr := decodeMapInto(m, elem.Interface()); decodeErr != nil {
			return decodeErr, true
		}
		out = reflect.Append(out, elem.Elem())
	}
	field.Set(out)
	return nil, true
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
