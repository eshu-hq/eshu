// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEncodeDecodeEntriesRoundTrip(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Section: "queue", Ordinal: 0, JSON: `{"outstanding":3,"oldest_outstanding_age_seconds":12.5}`},
		{Section: "backlog", Ordinal: 0, JSON: `{"stage":"projector","note":"quote \" and \\ slash and é"}`},
		{Section: "backlog", Ordinal: 1, JSON: `{}`},
		{Section: "blockage", Ordinal: 9223372036854775807, JSON: `[]`},
	}
	encoded, err := EncodeEntries(entries)
	if err != nil {
		t.Fatalf("EncodeEntries() error = %v", err)
	}
	got, err := DecodeEntries(encoded)
	if err != nil {
		t.Fatalf("DecodeEntries() error = %v", err)
	}
	if !reflect.DeepEqual(got, entries) {
		t.Fatalf("round trip = %#v, want %#v", got, entries)
	}
}

func TestEncodeEntriesUsesShimTupleShape(t *testing.T) {
	t.Parallel()

	encoded, err := EncodeEntries([]Entry{{Section: "queue", Ordinal: 2, JSON: `{"a":1}`}})
	if err != nil {
		t.Fatalf("EncodeEntries() error = %v", err)
	}
	// The reader feeds these tuples to the production decoder unchanged, so the
	// payload is exactly [[section, ordinal, section_json_text], ...].
	if want := `[["queue",2,"{\"a\":1}"]]`; string(encoded) != want {
		t.Fatalf("encoded = %s, want %s", encoded, want)
	}
}

func TestEncodeEntriesEmptyIsEmptyArrayNotNull(t *testing.T) {
	t.Parallel()

	for _, in := range [][]Entry{nil, {}} {
		encoded, err := EncodeEntries(in)
		if err != nil {
			t.Fatalf("EncodeEntries(%v) error = %v", in, err)
		}
		if string(encoded) != "[]" {
			t.Fatalf("EncodeEntries(%v) = %s, want []", in, encoded)
		}
	}
}

func TestDecodeEntriesRejectsMalformedPayloads(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"not an array":         `{"a":1}`,
		"null":                 `null`,
		"row is not an array":  `["queue"]`,
		"too few elements":     `[["queue",0]]`,
		"too many elements":    `[["queue",0,"{}","x"]]`,
		"section not string":   `[[1,0,"{}"]]`,
		"ordinal fractional":   `[["queue",1.5,"{}"]]`,
		"ordinal not a number": `[["queue","0","{}"]]`,
		"json not a string":    `[["queue",0,{}]]`,
		"empty section":        `[["",0,"{}"]]`,
		"trailing garbage":     `[["queue",0,"{}"]] x`,
		"empty input":          ``,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodeEntries([]byte(payload))
			if err == nil {
				t.Fatalf("DecodeEntries(%q) = nil error, want ErrDecode", payload)
			}
			if !errors.Is(err, ErrDecode) {
				t.Fatalf("DecodeEntries(%q) error = %v, want errors.Is ErrDecode", payload, err)
			}
		})
	}
}

func TestRowValidate(t *testing.T) {
	t.Parallel()

	valid := Row{
		ModelKey:      ModelActiveWorkSummary,
		SchemaVersion: SchemaVersion,
		SourceSHA256:  strings.Repeat("a", 64),
		AsOf:          time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC),
		RowCount:      1,
		Entries:       []Entry{{Section: "queue", Ordinal: 0, JSON: `{}`}},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate(valid) = %v, want nil", err)
	}

	mismatch := valid
	mismatch.RowCount = 2
	if err := mismatch.Validate(); !errors.Is(err, ErrRowCountMismatch) {
		t.Fatalf("Validate(row_count mismatch) = %v, want ErrRowCountMismatch", err)
	}

	for name, mutate := range map[string]func(*Row){
		"blank model key":  func(r *Row) { r.ModelKey = "  " },
		"zero version":     func(r *Row) { r.SchemaVersion = 0 },
		"blank source sha": func(r *Row) { r.SourceSHA256 = "" },
		"negative count":   func(r *Row) { r.RowCount = -1 },
		"zero as_of":       func(r *Row) { r.AsOf = time.Time{} },
		"negative pass":    func(r *Row) { r.PassDuration = -time.Millisecond },
	} {
		bad := valid
		mutate(&bad)
		if err := bad.Validate(); err == nil || errors.Is(err, ErrRowCountMismatch) {
			t.Fatalf("Validate(%s) = %v, want a non-mismatch error", name, err)
		}
	}
}
