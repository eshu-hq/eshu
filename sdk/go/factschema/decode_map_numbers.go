// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"fmt"
	"math"
)

// jsonNumberToInt64 coerces a JSONB-native number (float64 from encoding/json,
// or an in-memory int/int32/int64) into an int64, matching how the previous
// json.Unmarshal path filled int64 / *int64 size fields. It fails closed on a
// non-integral or out-of-range value, mirroring encoding/json.
func jsonNumberToInt64(raw any) (int64, error) {
	switch n := raw.(type) {
	case float64:
		if math.Trunc(n) != n {
			return 0, fmt.Errorf("want integer, got non-integral number %v", n)
		}
		// int64 uses >= here (unlike jsonNumberToInt32's >) on purpose:
		// math.MaxInt64 is not exactly representable as float64, so the constant
		// rounds up to 2^63 in this comparison. A float64 that reaches 2^63 would
		// overflow int64(n), so it must be rejected; every representable int64
		// value below it (at most 2^63-2048 at this magnitude) still passes.
		if n < math.MinInt64 || n >= math.MaxInt64 {
			return 0, fmt.Errorf("number %v out of int64 range", n)
		}
		return int64(n), nil
	case int:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case int64:
		return n, nil
	default:
		return 0, fmt.Errorf("want number, got %T", raw)
	}
}

// jsonNumberToFloat64 coerces a JSONB-native number (float64 from
// encoding/json, or an in-memory int/int32/int64) into a float64, matching how
// the previous json.Unmarshal path filled float64 / *float64 score fields (for
// example vulnerability.cve's CVSSScore). Unlike jsonNumberToInt64/Int32 this
// never rejects a non-integral value — a float field's whole point is
// fractional precision — but it still fails closed on a non-numeric type
// rather than silently zeroing the field.
func jsonNumberToFloat64(raw any) (float64, error) {
	switch n := raw.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int64:
		return float64(n), nil
	default:
		return 0, fmt.Errorf("want number, got %T", raw)
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

// jsonNumberToInt coerces a JSONB-native number (float64 from encoding/json,
// or an in-memory int/int32/int64) into a platform int, matching how the
// previous json.Unmarshal path filled *int fields (for example secretsiam/v1's
// VaultACLPolicyRule.PathDepth and VaultKVMetadata.PathDepth: a Vault path's
// segment depth, always a small non-negative count).
//
// It fails closed on BOTH a non-integral float (a depth of 3.5) and an
// out-of-range one by delegating to jsonNumberToInt64, matching
// jsonNumberToInt32/Int64 and encoding/json's UnmarshalTypeError behavior. A
// float64 can be integral yet still exceed int64 range (for example 1e300):
// int(n) on such a value is implementation-defined and would silently project
// a wrong depth, so it must dead-letter as input_invalid instead. int is
// 64-bit on every Eshu build target, so the int64->int cast is lossless.
func jsonNumberToInt(raw any) (int, error) {
	n, err := jsonNumberToInt64(raw)
	if err != nil {
		return 0, err
	}
	return int(n), nil
}
