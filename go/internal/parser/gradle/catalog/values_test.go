// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"fmt"
	"testing"
)

// scalarCases are integer, float, date and time values, legal and not. The
// TOML scanner accepts the shape of each one; whether the value itself is
// legal (a real date, an int64, a float in range) is decided by the decoder.
var scalarCases = []string{
	// Integers.
	"0", "+0", "-0", "42", "-17", "1_000", "9223372036854775807", "-9223372036854775808",
	"9223372036854775808", "-9223372036854775809", "99999999999999999999",
	"0xDEADBEEF", "0xdead_beef", "0x7fffffffffffffff", "0x8000000000000000", "0xffffffffffffffff",
	"0o755", "0o1_7", "0o777777777777777777777", "0o1000000000000000000000",
	"0b1101", "0b1_0", "0b" + repeatByte('1', 63), "0b1" + repeatByte('0', 63),
	"01", "007", "+01", "1__0", "_1", "1_", "0x_1", "0x1_",
	// Floats.
	"1.0", "-0.5", "3.1415", "5e+22", "1e06", "-2E-2", "6.626e-34", "224_617.445_991",
	"1e400", "-1e400", "1e-400", "1.7976931348623157e308", "1.8e308",
	"inf", "+inf", "-inf", "nan", "+nan", "-nan", "01.5", "1._5", "1_.5", "1e_5", "1_e5", "00.1",
	// Local dates.
	"1979-05-27", "1980-02-29", "2000-02-29", "1979-02-29", "1900-02-29", "1979-02-30",
	"1979-04-31", "1979-13-01", "1979-00-10", "1979-01-00", "1979-01-32",
	// Local times.
	"07:32:00", "00:32:00.999999", "23:59:59", "24:00:00", "07:60:00", "07:32:60",
	"07:32:59.1234567891234", "07:32:00.5", "07:32:00Z", "07:32:00+07:00", "07:32:00.5Z", "07:32:00.",
	// Local date-times.
	"1979-05-27T07:32:00", "1979-05-27 07:32:00.999", "1979-05-27t07:32:00",
	"1979-02-29T07:32:00", "1979-05-27T24:00:00", "1979-05-27T07:32:60", "1979-05-27T07:32",
	"1979-05-27 07:32:00Z",
	// Offset date-times.
	"1979-05-27T07:32:00Z", "1979-05-27T07:32:00z", "1979-05-27T00:32:00-07:00",
	"1979-05-27T00:32:00.999999+07:00", "1979-05-27T07:32:00+23:59", "1979-05-27T07:32:00+24:00",
	"1979-05-27T07:32:00+25:00", "1979-05-27T07:32:00-07:60", "1979-13-01T00:00:00Z",
	"1979-02-30T00:00:00Z", "1979-05-27T07:32:61Z",
	// Booleans and strings, which carry no decoder check.
	"true", "false", `"1979-02-30"`, `'99999999999999999999'`,
}

// repeatByte returns n copies of c.
func repeatByte(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

// A value the scanner accepts but go-toml's decoder rejects (an impossible
// date, an integer past int64, a float past float64) makes the file
// malformed with no rows, wherever it sits: [libraries], [versions],
// [metadata], an inline table or an array. A legal value of every kind
// leaves the file parsed with the same rows. The reference is a real
// go-toml decode into a map; Scan must agree with it on every case.
func TestScanRejectsExactlyTheScalarValuesTOMLRejects(t *testing.T) {
	t.Parallel()
	placements := map[string]string{
		"libraries":       "[libraries]\nx = \"g:a:1.0\"\ny = %s\n",
		"metadata":        "[libraries]\nx = \"g:a:1.0\"\n[metadata]\nz = %s\n",
		"versions":        "[versions]\nv = %s\n[libraries]\nx = \"g:a:1.0\"\n",
		"root":            "z = %s\n[libraries]\nx = \"g:a:1.0\"\n",
		"inline":          "[libraries]\nx = \"g:a:1.0\"\n[metadata]\nz = { a = %s }\n",
		"array":           "[libraries]\nx = \"g:a:1.0\"\n[bundles]\nb = [%s]\n",
		"nested in array": "[libraries]\nx = \"g:a:1.0\"\n[metadata]\nz = [{ a = [%s] }]\n",
	}
	var rejected, accepted int
	for _, scalar := range scalarCases {
		for place, format := range placements {
			text := fmt.Sprintf(format, scalar)
			result := Scan([]byte(text))
			if tomlRejects(text) {
				rejected++
				if result.State != StateMalformed || len(result.Rows) != 0 || result.ParseError == "" {
					t.Errorf("%s %s: State=%q rows=%d err=%q, want malformed with none (TOML rejects it)",
						place, scalar, result.State, len(result.Rows), result.ParseError)
				}
				continue
			}
			accepted++
			if result.State != StateParsed || len(result.Rows) != 1 || result.Rows[0]["name"] != "g:a" {
				t.Errorf("%s %s: State=%q (%s) rows=%d, want parsed with the one library row (TOML accepts it)",
					place, scalar, result.State, result.ParseError, len(result.Rows))
			}
		}
	}
	if rejected == 0 || accepted == 0 {
		t.Fatalf("cases are lopsided: %d rejected, %d accepted", rejected, accepted)
	}
}

// The three shapes the review minimized, pinned by name.
func TestScanInvalidScalarMinimizedShapesAreMalformed(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"impossible date":   "[libraries]\nx=\"g:a:1.0\"\ny=1979-02-30\n",
		"decimal overflow":  "[libraries]\nx=\"g:a:1.0\"\ny=99999999999999999999\n",
		"metadata datetime": "[libraries]\nx=\"g:a:1.0\"\n[metadata]\nz=1979-13-01T00:00:00Z\n",
	} {
		if !tomlRejects(text) {
			t.Fatalf("%s: the reference decode accepts this text", name)
		}
		result := Scan([]byte(text))
		if result.State != StateMalformed || len(result.Rows) != 0 || result.ParseError != reasonInvalidValue {
			t.Errorf("%s: State=%q rows=%d err=%q, want malformed with none and %q",
				name, result.State, len(result.Rows), result.ParseError, reasonInvalidValue)
		}
	}
}
