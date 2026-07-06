// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// This file holds the raw-value coercion and bounded-fallback helpers
// assignField (decode_map.go) uses to turn JSONB-native payload values into the
// map/slice/struct field shapes the typed structs declare. It was split out of
// decode_map.go to keep that file under the repository 500-line cap while
// keeping the hot-path decodeMapInto / assignField core cohesive; the coercion
// helpers here are a self-contained unit with no dependency on the plan cache.

// anyToStringMap coerces a JSONB-native map[string]any of string values (or an
// already-typed map[string]string) into map[string]string. ok is false when
// raw is not one of those two shapes, or when a map[string]any value is not a
// string — the caller falls back to jsonRoundTripValue for those rare cases so
// correctness is never sacrificed for the fast path.
//
// The result is ALWAYS a freshly allocated map that does not alias raw, even
// on the already-typed map[string]string branch: a decode result is a fresh
// owned value the caller may mutate (some reducer consumers normalize the
// decoded map in place), so aliasing an in-memory caller's env.Payload map
// would let that normalization mutate the original payload. The JSONB path
// already allocates; cloning the already-typed path costs nothing in
// production (that branch is not hit on the Postgres decode path) and keeps
// the decode side-effect-free for every input shape.
func anyToStringMap(raw any) (map[string]string, bool) {
	switch v := raw.(type) {
	case map[string]string:
		out := make(map[string]string, len(v))
		for key, value := range v {
			out[key] = value
		}
		return out, true
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

// anyToStringMapSlice coerces a JSONB-native []any of string-valued object maps
// (or an already-typed []map[string]string / []map[string]any) into
// []map[string]string. ok is false when raw is not one of those shapes, or when
// any element is not a string-valued object map — the caller falls back to
// jsonRoundTripValue for those rare cases so correctness is never sacrificed for
// the fast path. It preserves every element and every key/value verbatim (no
// trimming or empty-dropping); shape normalization is the caller's concern, so
// the coercion stays a faithful decode of the wire payload.
//
// The result is ALWAYS a fresh slice of freshly cloned element maps that does
// not alias raw, even on the already-typed []map[string]string branch, for the
// same reason anyToStringMap clones: a decode result is a fresh owned value a
// reducer consumer may normalize in place, so aliasing an in-memory caller's
// env.Payload slice/maps would mutate the original payload. Cloning costs
// nothing on the Postgres decode path (which is []any, not []map[string]string).
func anyToStringMapSlice(raw any) ([]map[string]string, bool) {
	switch v := raw.(type) {
	case []map[string]string:
		out := make([]map[string]string, 0, len(v))
		for _, item := range v {
			cloned := make(map[string]string, len(item))
			for key, value := range item {
				cloned[key] = value
			}
			out = append(out, cloned)
		}
		return out, true
	case []map[string]any:
		out := make([]map[string]string, 0, len(v))
		for _, item := range v {
			m, ok := anyToStringMap(item)
			if !ok {
				return nil, false
			}
			out = append(out, m)
		}
		return out, true
	case []any:
		out := make([]map[string]string, 0, len(v))
		for _, item := range v {
			m, ok := anyToStringMap(item)
			if !ok {
				return nil, false
			}
			out = append(out, m)
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
