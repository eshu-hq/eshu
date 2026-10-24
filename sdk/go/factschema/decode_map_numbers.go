// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// jsonNumberToInt64 coerces a number-preserving json.Number, a JSONB-native
// float64, or an in-memory int/int32/int64 into an int64. json.Number uses
// exact decimal value semantics; the historical input types retain their
// previous behavior.
func jsonNumberToInt64(raw any) (int64, error) {
	switch n := raw.(type) {
	case json.Number:
		return parseJSONInteger(string(n), 64)
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

// jsonNumberToFloat64 coerces a number-preserving json.Number, a JSONB-native
// float64, or an in-memory float32/int/int32/int64 into a float64. json.Number
// must have valid JSON number grammar and a finite float64 value. Historical
// input types retain their previous behavior.
func jsonNumberToFloat64(raw any) (float64, error) {
	switch n := raw.(type) {
	case json.Number:
		if _, ok := scanJSONNumber(string(n)); !ok {
			return 0, fmt.Errorf("invalid JSON number")
		}
		value, err := n.Float64()
		if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
			return 0, fmt.Errorf("JSON number out of float64 range")
		}
		return value, nil
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

// jsonNumberToInt32 coerces a number-preserving json.Number, a JSONB-native
// float64, or an in-memory int/int32/int64 into an int32.
//
// It fails closed: a non-integral float (for example a port of 8080.5) or a
// value outside the int32 range is rejected with an error rather than silently
// truncated or wrapped, so a malformed number dead-letters the fact as
// input_invalid instead of projecting a wrong port/hop-limit into the graph.
// This mirrors encoding/json, which returns an UnmarshalTypeError when a JSON
// number does not fit the target int32.
func jsonNumberToInt32(raw any) (int32, error) {
	switch n := raw.(type) {
	case json.Number:
		value, err := parseJSONInteger(string(n), 32)
		return int32(value), err
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

// jsonNumberToInt coerces a number-preserving json.Number, a JSONB-native
// float64, or an in-memory int/int32/int64 into a platform int (for example
// secretsiam/v1's VaultACLPolicyRule.PathDepth).
//
// The json.Number path checks the platform width; historical inputs delegate
// to jsonNumberToInt64 and retain their prior behavior. A
// float64 can be integral yet still exceed int64 range (for example 1e300):
// int(n) on such a value is implementation-defined and would silently project
// a wrong depth, so it must dead-letter as input_invalid instead. int is
// 64-bit on every Eshu build target, so the int64->int cast is lossless.
func jsonNumberToInt(raw any) (int, error) {
	if number, ok := raw.(json.Number); ok {
		value, err := parseJSONInteger(string(number), strconv.IntSize)
		return int(value), err
	}
	n, err := jsonNumberToInt64(raw)
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// numberShape records only bounded indices and an exponent clamped to token
// length. Clamping cannot change an int64 result: beyond that limit a nonzero
// mantissa is necessarily out of range or fractional.
type numberShape struct {
	mantissaEnd int
	fracDigits  int
	exponent    int
}

// scanJSONNumber validates the complete JSON number grammar. It keeps scanning
// after exponent saturation, so malformed suffixes cannot hide behind overflow.
func scanJSONNumber(s string) (numberShape, bool) {
	var shape numberShape
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	if i == len(s) {
		return shape, false
	}
	if s[i] == '0' {
		i++
	} else if s[i] >= '1' && s[i] <= '9' {
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	} else {
		return shape, false
	}
	if i < len(s) && s[i] == '.' {
		i++
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return shape, false
		}
		shape.fracDigits = i - start
	}
	shape.mantissaEnd = i
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		negative := false
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			negative = s[i] == '-'
			i++
		}
		start := i
		// A token this long cannot be represented in memory; the guard keeps
		// the cap arithmetic safe even at the maximum theoretical string size.
		cap := len(s)
		if cap <= math.MaxInt-20 {
			cap += 20
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			digit := int(s[i] - '0')
			if shape.exponent < cap {
				if shape.exponent > (cap-digit)/10 {
					shape.exponent = cap
				} else {
					shape.exponent = shape.exponent*10 + digit
				}
			}
			i++
		}
		if i == start {
			return shape, false
		}
		if negative {
			shape.exponent = -shape.exponent
		}
	}
	return shape, i == len(s)
}

// parseJSONInteger converts a mathematically integral JSON decimal without
// float rounding, exponent expansion, or storage proportional to token length.
// A second linear scan identifies significant digits; only up to 19 digits
// plus a sign are copied before ParseInt checks the target width.
func parseJSONInteger(s string, bits int) (int64, error) {
	shape, ok := scanJSONNumber(s)
	if !ok {
		return 0, fmt.Errorf("invalid JSON number")
	}
	first, last, digits := -1, -1, 0
	for i := 0; i < shape.mantissaEnd; i++ {
		if s[i] < '0' || s[i] > '9' {
			continue
		}
		if s[i] != '0' {
			if first < 0 {
				first = digits
			}
			last = digits
		}
		digits++
	}
	if first < 0 {
		return 0, nil
	}
	shift := -math.MaxInt
	if shape.exponent >= -math.MaxInt+shape.fracDigits {
		shift = shape.exponent - shape.fracDigits
	}
	trailing := digits - 1 - last
	if shift < -trailing {
		return 0, fmt.Errorf("non-integral JSON number")
	}
	significant := last - first + 1
	if significant > 19 || shift > 19-significant-trailing {
		return 0, fmt.Errorf("JSON number out of int%d range", bits)
	}
	zeros := trailing + shift
	var buf [21]byte
	pos := 0
	if s[0] == '-' {
		buf[pos] = '-'
		pos++
	}
	for i, digit := 0, 0; i < shape.mantissaEnd; i++ {
		if s[i] < '0' || s[i] > '9' {
			continue
		}
		if digit >= first && digit <= last {
			buf[pos] = s[i]
			pos++
		}
		digit++
	}
	for i := 0; i < zeros; i++ {
		buf[pos] = '0'
		pos++
	}
	value, err := strconv.ParseInt(string(buf[:pos]), 10, bits)
	if err != nil {
		return 0, fmt.Errorf("JSON number out of int%d range", bits)
	}
	return value, nil
}
