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

	// A one-space section is accepted by both Validate and the decoder, so it
	// stays valid; only the empty section is rejected.
	spaced := valid
	spaced.Entries = []Entry{{Section: " ", Ordinal: 0, JSON: `{}`}}
	if err := spaced.Validate(); err != nil {
		t.Fatalf("Validate(one-space section) = %v, want nil", err)
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
		"blank section":    func(r *Row) { r.Entries = []Entry{{Section: "", Ordinal: 0, JSON: `{}`}} },
		"second blank": func(r *Row) {
			r.RowCount = 2
			r.Entries = append(r.Entries, Entry{Section: "", Ordinal: 1, JSON: `{}`})
		},
	} {
		bad := valid
		mutate(&bad)
		if err := bad.Validate(); err == nil || errors.Is(err, ErrRowCountMismatch) {
			t.Fatalf("Validate(%s) = %v, want a non-mismatch error", name, err)
		}
	}
}

// TestValidateAndDecodeAgreeOnEverySection keeps Row.Validate and decodeTuple
// from drifting apart: Upsert stores only what Validate accepts, and Read
// returns only what DecodeEntries accepts, so a row Validate passes but the
// decoder rejects can be written and never read back. For every section value
// both must accept it or both must reject it, and a validated row must survive
// EncodeEntries then DecodeEntries unchanged.
func TestValidateAndDecodeAgreeOnEverySection(t *testing.T) {
	t.Parallel()

	for _, section := range []string{
		"", " ", "\t", "queue", "backlog", "é", "a b", strings.Repeat("s", 4096),
	} {
		row := Row{
			ModelKey:      ModelActiveWorkSummary,
			SchemaVersion: SchemaVersion,
			SourceSHA256:  strings.Repeat("a", 64),
			AsOf:          time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC),
			RowCount:      1,
			Entries:       []Entry{{Section: section, Ordinal: 3, JSON: `{"k":1}`}},
		}
		encoded, err := EncodeEntries(row.Entries)
		if err != nil {
			t.Fatalf("EncodeEntries(section %q) error = %v", section, err)
		}
		decoded, decodeErr := DecodeEntries(encoded)
		validateErr := row.Validate()
		if (validateErr == nil) != (decodeErr == nil) {
			t.Fatalf("section %q: Validate error = %v, DecodeEntries error = %v; they must accept and reject the same rows",
				section, validateErr, decodeErr)
		}
		if validateErr == nil && !reflect.DeepEqual(decoded, row.Entries) {
			t.Fatalf("section %q: round trip = %#v, want %#v", section, decoded, row.Entries)
		}
	}
}
