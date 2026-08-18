// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pgarray

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
)

// StringArray is a one-dimensional Postgres text array. As a driver.Valuer it
// renders the array literal Postgres parses for text[]; as a sql.Scanner it
// parses the literal Postgres emits for one.
type StringArray []string

// Value implements driver.Valuer. A nil slice becomes SQL NULL; an empty
// non-nil slice becomes the empty array "{}". Every element is double-quoted
// with backslash escaping of `"` and `\`, so a comma, brace, whitespace, empty
// string, or the literal word NULL inside an element survives the round trip
// as data rather than syntax.
func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	if len(a) == 0 {
		return "{}", nil
	}
	b := make([]byte, 1, 1+3*len(a))
	b[0] = '{'
	for i, s := range a {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendQuoted(b, s)
	}
	return string(append(b, '}')), nil
}

// Scan implements sql.Scanner for text-format array output. A SQL NULL leaves
// the receiver nil. An empty array keeps a non-nil receiver at length zero.
// A NULL element is an error, because a Go string cannot carry the
// distinction and dropping it would corrupt positional data.
func (a *StringArray) Scan(src any) error {
	var raw string
	switch src := src.(type) {
	case []byte:
		raw = string(src)
	case string:
		raw = src
	case nil:
		*a = nil
		return nil
	default:
		return fmt.Errorf("pgarray: cannot convert %T to StringArray", src)
	}
	elems, err := parseLinearArray(raw, "StringArray")
	if err != nil {
		return err
	}
	if *a != nil && len(elems) == 0 {
		*a = (*a)[:0]
		return nil
	}
	out := make(StringArray, len(elems))
	for i, e := range elems {
		if e.null {
			return fmt.Errorf("pgarray: parsing array element index %d: cannot convert nil to string", i)
		}
		out[i] = e.text
	}
	*a = out
	return nil
}

// Float64Array is a one-dimensional Postgres double precision array.
type Float64Array []float64

// Value implements driver.Valuer. A nil slice becomes SQL NULL; elements are
// rendered with strconv's shortest round-trip 'f' formatting, unquoted.
func (a Float64Array) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	if len(a) == 0 {
		return "{}", nil
	}
	b := make([]byte, 1, 1+2*len(a))
	b[0] = '{'
	for i, f := range a {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendFloat(b, f, 'f', -1, 64)
	}
	return string(append(b, '}')), nil
}

// Scan implements sql.Scanner for text-format array output. A SQL NULL leaves
// the receiver nil; a NULL element or unparsable number is an error.
func (a *Float64Array) Scan(src any) error {
	var raw string
	switch src := src.(type) {
	case []byte:
		raw = string(src)
	case string:
		raw = src
	case nil:
		*a = nil
		return nil
	default:
		return fmt.Errorf("pgarray: cannot convert %T to Float64Array", src)
	}
	elems, err := parseLinearArray(raw, "Float64Array")
	if err != nil {
		return err
	}
	if *a != nil && len(elems) == 0 {
		*a = (*a)[:0]
		return nil
	}
	out := make(Float64Array, len(elems))
	for i, e := range elems {
		if e.null {
			return fmt.Errorf("pgarray: parsing array element index %d: cannot convert nil to float64", i)
		}
		f, err := strconv.ParseFloat(e.text, 64)
		if err != nil {
			return fmt.Errorf("pgarray: parsing array element index %d: %v", i, err)
		}
		out[i] = f
	}
	*a = out
	return nil
}

// Array wraps a Go slice as a query argument or scan target for the matching
// Postgres array type. It accepts []string, *[]string, []float64 and
// *[]float64 -- the element types Eshu stores -- and mirrors the classic
// lib/pq shape: a slice value is copied into a fresh typed array (so a nil
// slice still encodes as SQL NULL and an empty one as "{}"), while a pointer
// is aliased so Scan writes through to the caller's variable.
//
// Any other type is not silently accepted. The returned wrapper fails at
// Value or Scan time with a typed error naming the offending Go type, which
// surfaces as the statement's error rather than as a wrong write.
func Array(a any) interface {
	driver.Valuer
	sql.Scanner
} {
	switch a := a.(type) {
	case []string:
		return (*StringArray)(&a)
	case *[]string:
		return (*StringArray)(a)
	case []float64:
		return (*Float64Array)(&a)
	case *[]float64:
		return (*Float64Array)(a)
	}
	return unsupportedArray{value: a}
}

// unsupportedArray is what Array returns for an element type this package does
// not encode. Both methods fail loudly so an unsupported type can never reach
// the wire as a wrong literal.
type unsupportedArray struct{ value any }

func (u unsupportedArray) Value() (driver.Value, error) {
	return nil, fmt.Errorf("pgarray: unsupported array type %T", u.value)
}

func (u unsupportedArray) Scan(any) error {
	return fmt.Errorf("pgarray: unsupported array scan target %T", u.value)
}

// QuoteIdentifier returns name as a double-quoted SQL identifier with every
// embedded double quote doubled, for the few places Eshu interpolates a
// schema or table name into DDL. The result is case-sensitive in the query.
// A zero byte terminates the identifier, matching what the wire protocol can
// carry.
func QuoteIdentifier(name string) string {
	if end := strings.IndexByte(name, 0); end >= 0 {
		name = name[:end]
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// appendQuoted appends s as a double-quoted array element, escaping `"` and
// `\` with a backslash. Every element is quoted, whether or not it needs to be;
// Postgres accepts that and it keeps the encoder free of a which-characters-
// need-quoting table that could drift from the server's parser.
func appendQuoted(b []byte, s string) []byte {
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		if c := s[i]; c == '"' || c == '\\' {
			b = append(b, '\\', c)
		} else {
			b = append(b, c)
		}
	}
	return append(b, '"')
}
