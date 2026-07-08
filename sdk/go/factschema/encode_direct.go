// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"fmt"
	"reflect"
	"sync"
	"time"
)

func addStringPtr(payload map[string]any, key string, value *string) {
	if value != nil {
		payload[key] = *value
	}
}

func addBoolPtr(payload map[string]any, key string, value *bool) {
	if value != nil {
		payload[key] = *value
	}
}

func addIntPtr(payload map[string]any, key string, value *int) {
	if value != nil {
		payload[key] = *value
	}
}

func addInt32Ptr(payload map[string]any, key string, value *int32) {
	if value != nil {
		payload[key] = *value
	}
}

func addInt64Ptr(payload map[string]any, key string, value *int64) {
	if value != nil {
		payload[key] = *value
	}
}

func addTimePtr(payload map[string]any, key string, value *time.Time) {
	if value != nil {
		payload[key] = value.UTC()
	}
}

func addStringSlice(payload map[string]any, key string, value []string) {
	if value != nil {
		payload[key] = value
	}
}

func addAnyMap(payload map[string]any, key string, value map[string]any) {
	if value != nil {
		payload[key] = value
	}
}

func addStringMapPtr(payload map[string]any, key string, value *map[string]string) {
	if value != nil {
		payload[key] = *value
	}
}

func addStringMap(payload map[string]any, key string, value map[string]string) {
	if value != nil {
		payload[key] = value
	}
}

type encodeField struct {
	index     int
	key       string
	omitEmpty bool
}

var encodeFieldsCache sync.Map // reflect.Type -> []encodeField

func encodeDirectPayload[T any](value T) (map[string]any, error) {
	encoded, err := encodeDirectValue(reflect.ValueOf(value))
	if err != nil {
		return nil, err
	}
	payload, ok := encoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("factschema: direct payload encode requires struct, got %T", value)
	}
	return payload, nil
}

func encodeDirectValue(value reflect.Value) (any, error) {
	if !value.IsValid() {
		return nil, nil
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, nil
		}
		return encodeDirectValue(value.Elem())
	}
	switch value.Kind() {
	case reflect.Struct:
		return encodeDirectStruct(value)
	case reflect.Slice:
		if value.IsNil() {
			return nil, nil
		}
		if value.Type().Elem().Kind() != reflect.Struct && value.Type().Elem().Kind() != reflect.Pointer {
			return value.Interface(), nil
		}
		out := make([]map[string]any, 0, value.Len())
		for i := 0; i < value.Len(); i++ {
			encoded, err := encodeDirectValue(value.Index(i))
			if err != nil {
				return nil, err
			}
			item, ok := encoded.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("factschema: direct payload slice item encoded as %T", encoded)
			}
			out = append(out, item)
		}
		return out, nil
	default:
		return value.Interface(), nil
	}
}

func encodeDirectStruct(value reflect.Value) (map[string]any, error) {
	valueType := value.Type()
	fields := encodeFieldsOf(valueType)
	payload := make(map[string]any, len(fields))
	for _, field := range fields {
		fieldValue := value.Field(field.index)
		if field.omitEmpty && isDirectOmitEmptyValue(fieldValue) {
			continue
		}
		encoded, err := encodeDirectValue(fieldValue)
		if err != nil {
			return nil, err
		}
		payload[field.key] = encoded
	}
	return payload, nil
}

func encodeFieldsOf(valueType reflect.Type) []encodeField {
	if cached, ok := encodeFieldsCache.Load(valueType); ok {
		return cached.([]encodeField)
	}
	fields := make([]encodeField, 0, valueType.NumField())
	for i := 0; i < valueType.NumField(); i++ {
		field := valueType.Field(i)
		if field.PkgPath != "" || field.Anonymous {
			continue
		}
		key, omitEmpty, skip := parseJSONTag(field.Tag.Get("json"), field.Name)
		if skip {
			continue
		}
		fields = append(fields, encodeField{index: i, key: key, omitEmpty: omitEmpty})
	}
	encodeFieldsCache.Store(valueType, fields)
	return fields
}

// isDirectOmitEmptyValue mirrors encoding/json's omitempty check so direct
// encoding does not drop non-nil pointers to empty scalar or struct values.
func isDirectOmitEmptyValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Invalid:
		return true
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return value.Len() == 0
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Interface, reflect.Pointer:
		return value.IsZero()
	default:
		return false
	}
}
