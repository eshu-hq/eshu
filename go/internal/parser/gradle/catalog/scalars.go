// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import (
	"strconv"

	"github.com/pelletier/go-toml/v2/unstable"
)

// validScalar reports whether go-toml's decoder accepts a scalar value. The
// unstable parser checks only the shape of a number or date; the decoder
// then rejects an integer past int64, a float strconv cannot hold, and a
// date or time that does not exist (1979-02-30, 24:00:00, a +25:00 offset).
// A file holding such a value is not valid TOML, and Gradle's parser refuses
// it too, so Scan reports it malformed with no rows.
//
// The checks follow go-toml v2's decode.go (parseInteger, parseFloat,
// parseLocalDate, parseLocalTime, parseLocalDateTime, parseDateTime) and the
// unmarshaler's rule that a local time or local date-time has no trailing
// bytes. Strings and booleans carry no decoder check and return at once, so
// the common catalog value costs one switch. Integers and dates never
// allocate; a float allocates only in strconv.
func validScalar(value *unstable.Node) bool {
	switch value.Kind {
	case unstable.Integer:
		return validInteger(value.Data)
	case unstable.Float:
		return validFloat(value.Data)
	case unstable.LocalDate:
		return validLocalDate(value.Data)
	case unstable.LocalTime:
		rest, ok := localTime(value.Data)
		return ok && len(rest) == 0
	case unstable.LocalDateTime:
		rest, ok := localDateTime(value.Data)
		return ok && len(rest) == 0
	case unstable.DateTime:
		return validDateTime(value.Data)
	default:
		return true
	}
}

// validInteger accepts a decimal, 0x, 0o or 0b integer that fits int64, with
// TOML's underscore rules and no leading zero on a decimal.
func validInteger(b []byte) bool {
	if len(b) > 2 && b[0] == '0' {
		switch b[1] {
		case 'x':
			return digitsFit(b[2:], 16)
		case 'o':
			return digitsFit(b[2:], 8)
		case 'b':
			return digitsFit(b[2:], 2)
		default:
			// go-toml panics here; the scanner never produces it.
			return false
		}
	}
	start := 0
	if len(b) > 0 && (b[0] == '+' || b[0] == '-') {
		start = 1
	}
	if len(b) > start+1 && b[start] == '0' {
		return false // a leading zero; an underscore after it fails too
	}
	return digitsFit(b, 10)
}

// digitsFit reports whether b, an optional sign and digits of base with
// single underscores between them, parses as an int64 the way go-toml's
// checkAndRemoveUnderscoresIntegers and strconv.ParseInt do.
func digitsFit(b []byte, base uint64) bool {
	neg := false
	if len(b) > 0 && (b[0] == '+' || b[0] == '-') {
		neg = b[0] == '-'
		b = b[1:]
	}
	if len(b) == 0 || b[0] == '_' || b[len(b)-1] == '_' {
		return false
	}
	limit := uint64(1<<63 - 1)
	if neg {
		limit = 1 << 63
	}
	var v uint64
	underscore := false
	for _, c := range b {
		if c == '_' {
			if underscore {
				return false
			}
			underscore = true
			continue
		}
		underscore = false
		d, ok := digitValue(c)
		if !ok || d >= base {
			return false
		}
		if v > (limit-d)/base {
			return false
		}
		v = v*base + d
	}
	return true
}

// digitValue returns the value of a hexadecimal digit.
func digitValue(c byte) (uint64, bool) {
	switch {
	case c >= '0' && c <= '9':
		return uint64(c - '0'), true
	case c >= 'a' && c <= 'f':
		return uint64(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return uint64(c-'A') + 10, true
	}
	return 0, false
}

// validFloat follows go-toml's parseFloat: signed nan, the underscore rules,
// a digit on both sides of the point, no leading zero, and a value
// strconv.ParseFloat accepts (1e400 is out of range and rejected).
func validFloat(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	if len(b) == 4 && (b[0] == '+' || b[0] == '-') && b[1] == 'n' && b[2] == 'a' && b[3] == 'n' {
		return true
	}
	cleaned, ok := floatWithoutUnderscores(b)
	if !ok || len(cleaned) == 0 || cleaned[0] == '.' || cleaned[len(cleaned)-1] == '.' {
		return false
	}
	dot := false
	for i, c := range cleaned {
		if c != '.' {
			continue
		}
		if dot || !isDigit(cleaned[i-1]) || !isDigit(cleaned[i+1]) {
			return false
		}
		dot = true
	}
	start := 0
	if cleaned[0] == '+' || cleaned[0] == '-' {
		start = 1
	}
	if start < len(cleaned) && cleaned[start] == '0' && len(cleaned) > start+1 && isDigit(cleaned[start+1]) {
		return false
	}
	_, err := strconv.ParseFloat(string(cleaned), 64)
	return err == nil
}

// floatWithoutUnderscores follows go-toml's checkAndRemoveUnderscoresFloats.
// It returns b itself when b holds no underscore.
func floatWithoutUnderscores(b []byte) ([]byte, bool) {
	if b[0] == '_' || b[len(b)-1] == '_' {
		return nil, false
	}
	first := -1
	for i, c := range b {
		if c == '_' {
			first = i
			break
		}
	}
	if first < 0 {
		return b, true
	}
	cleaned := make([]byte, 0, len(b))
	before := false
	for i, c := range b {
		switch c {
		case '_':
			if !before || (i < len(b)-1 && (b[i+1] == 'e' || b[i+1] == 'E')) {
				return nil, false
			}
			before = false
		case '+', '-':
			cleaned = append(cleaned, c)
			before = false
		case 'e', 'E':
			if i < len(b)-1 && b[i+1] == '_' {
				return nil, false
			}
			cleaned = append(cleaned, c)
		case '.':
			if (i < len(b)-1 && b[i+1] == '_') || (i > 0 && b[i-1] == '_') {
				return nil, false
			}
			cleaned = append(cleaned, c)
		default:
			before = true
			cleaned = append(cleaned, c)
		}
	}
	return cleaned, true
}

// validLocalDate accepts YYYY-MM-DD naming a day that exists.
func validLocalDate(b []byte) bool {
	if len(b) != 10 || b[4] != '-' || b[7] != '-' {
		return false
	}
	year, ok1 := decimal(b[0:4])
	month, ok2 := decimal(b[5:7])
	day, ok3 := decimal(b[8:10])
	return ok1 && ok2 && ok3 && month > 0 && month < 13 && day > 0 && day <= daysIn(month, year)
}

// localTime reads HH:MM:SS[.fraction] with hour < 24, minute < 60 and
// second < 60, and returns the bytes after it.
func localTime(b []byte) ([]byte, bool) {
	if len(b) < 8 || b[2] != ':' || b[5] != ':' {
		return nil, false
	}
	hour, ok1 := decimal(b[0:2])
	minute, ok2 := decimal(b[3:5])
	second, ok3 := decimal(b[6:8])
	if !ok1 || !ok2 || !ok3 || hour > 23 || minute > 59 || second > 59 {
		return nil, false
	}
	b = b[8:]
	if len(b) == 0 || b[0] != '.' {
		return b, true
	}
	digits := 0
	for digits+1 < len(b) && isDigit(b[digits+1]) {
		digits++
	}
	if digits == 0 {
		return nil, false
	}
	return b[1+digits:], true
}

// localDateTime reads a date, a T, t or space, and a local time, and returns
// the bytes after it.
func localDateTime(b []byte) ([]byte, bool) {
	if len(b) < 11 || !validLocalDate(b[:10]) {
		return nil, false
	}
	if sep := b[10]; sep != 'T' && sep != 't' && sep != ' ' {
		return nil, false
	}
	return localTime(b[11:])
}

// validDateTime accepts a local date-time followed by Z, z or an offset
// +HH:MM or -HH:MM with hours < 24 and minutes < 60, and nothing after it.
func validDateTime(b []byte) bool {
	rest, ok := localDateTime(b)
	if !ok || len(rest) == 0 {
		return false
	}
	if rest[0] == 'Z' || rest[0] == 'z' {
		return len(rest) == 1
	}
	if len(rest) != 6 || (rest[0] != '+' && rest[0] != '-') || rest[3] != ':' {
		return false
	}
	hours, ok1 := decimal(rest[1:3])
	minutes, ok2 := decimal(rest[4:6])
	return ok1 && ok2 && hours <= 23 && minutes <= 59
}

// decimal parses a short run of ASCII digits.
func decimal(b []byte) (int, bool) {
	v := 0
	for _, c := range b {
		if !isDigit(c) {
			return 0, false
		}
		v = v*10 + int(c-'0')
	}
	return v, true
}

// isDigit reports whether c is an ASCII digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// daysIn returns the days in month (1-12) of year, with Gregorian leap years.
func daysIn(month, year int) int {
	switch month {
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	default:
		return 31
	}
}
