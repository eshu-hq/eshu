// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// SchemaVersion is the encoding version of the rows payload this package
// writes. Bump it only when the [section, ordinal, section_json_text] tuple
// shape changes; a reader treats any other version as a fallback, never as
// rows to decode.
const SchemaVersion = 1

// ModelActiveWorkSummary is the model key of the active-work summary: the
// result of the status snapshot's active-work statement.
const ModelActiveWorkSummary = "active_work_summary"

// Entry is one tuple of a stored statement result: the section name, the
// ordinal within that section, and the section row as its JSON text. A reader
// feeds the tuples, in stored order, to the same decoder that reads the live
// statement, so Entry carries the text unparsed.
type Entry struct {
	// Section names the summary section the row belongs to (queue, backlog,
	// blockage, and so on).
	Section string
	// Ordinal is the row's position within its section.
	Ordinal int64
	// JSON is the section row as JSON text, exactly as the statement produced it.
	JSON string
}

// Row is one stored model row.
type Row struct {
	// ModelKey identifies which summary the row holds.
	ModelKey string
	// SchemaVersion is the version of the Entries encoding.
	SchemaVersion int
	// SourceSHA256 is the hex sha256 of the statement text the writer ran.
	SourceSHA256 string
	// AsOf is the clock value the writer bound as the statement's $1.
	AsOf time.Time
	// ComputedAt is the database clock when the row was written. Upsert
	// ignores it and the database sets it; Read fills it.
	ComputedAt time.Time
	// PassDuration is the writer's compute time for the pass.
	PassDuration time.Duration
	// RowCount is the number of Entries the row is stored with.
	RowCount int
	// Entries holds the statement result in live order.
	Entries []Entry
}

// Validate reports whether the row can be written or served: it needs a model
// key, a positive schema version, a source digest, a non-negative row count,
// and a row count equal to the number of entries. A count mismatch returns an
// error that satisfies errors.Is(err, ErrRowCountMismatch).
func (r Row) Validate() error {
	switch {
	case strings.TrimSpace(r.ModelKey) == "":
		return errors.New("status summary row: model key is blank")
	case r.SchemaVersion <= 0:
		return fmt.Errorf("status summary row %q: schema version %d is not positive", r.ModelKey, r.SchemaVersion)
	case strings.TrimSpace(r.SourceSHA256) == "":
		return fmt.Errorf("status summary row %q: source sha256 is blank", r.ModelKey)
	case r.RowCount < 0:
		return fmt.Errorf("status summary row %q: row count %d is negative", r.ModelKey, r.RowCount)
	case r.RowCount != len(r.Entries):
		return fmt.Errorf("%w: model %q stores row_count %d for %d entries",
			ErrRowCountMismatch, r.ModelKey, r.RowCount, len(r.Entries))
	}
	return nil
}

// EncodeEntries encodes entries as the stored jsonb payload,
// [[section, ordinal, section_json_text], ...], in the given order. An empty
// input encodes as [] and never as null.
func EncodeEntries(entries []Entry) ([]byte, error) {
	tuples := make([][3]any, len(entries))
	for i, entry := range entries {
		tuples[i] = [3]any{entry.Section, entry.Ordinal, entry.JSON}
	}
	encoded, err := json.Marshal(tuples)
	if err != nil {
		return nil, fmt.Errorf("encode status summary entries: %w", err)
	}
	return encoded, nil
}

// DecodeEntries decodes a stored payload back into entries. It accepts only an
// array of three-element [string, integer, string] tuples with a non-empty
// section and no trailing data; anything else returns an error that satisfies
// errors.Is(err, ErrDecode), so a reader never serves a payload it cannot
// fully understand.
func DecodeEntries(payload []byte) ([]Entry, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var tuples []json.RawMessage
	if err := decoder.Decode(&tuples); err != nil {
		return nil, fmt.Errorf("%w: payload is not an array: %v", ErrDecode, err)
	}
	if tuples == nil {
		return nil, fmt.Errorf("%w: payload is null", ErrDecode)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing data after the payload", ErrDecode)
	}
	entries := make([]Entry, len(tuples))
	for i, raw := range tuples {
		entry, err := decodeTuple(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: tuple %d: %v", ErrDecode, i, err)
		}
		entries[i] = entry
	}
	return entries, nil
}

func decodeTuple(raw json.RawMessage) (Entry, error) {
	var fields []json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Entry{}, errors.New("not an array")
	}
	if len(fields) != 3 {
		return Entry{}, fmt.Errorf("has %d elements, want 3", len(fields))
	}
	var entry Entry
	if err := json.Unmarshal(fields[0], &entry.Section); err != nil || entry.Section == "" {
		return Entry{}, errors.New("section is not a non-empty string")
	}
	if err := json.Unmarshal(fields[1], &entry.Ordinal); err != nil {
		return Entry{}, errors.New("ordinal is not an integer")
	}
	if err := json.Unmarshal(fields[2], &entry.JSON); err != nil {
		return Entry{}, errors.New("section json is not a string")
	}
	return entry, nil
}
