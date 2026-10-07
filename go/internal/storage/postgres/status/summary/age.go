// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// ageKeys names, per section, the one key the production decoder reads as a
// duration in seconds. The writer stores each age as evaluated at as_of, so a
// reader must advance exactly these keys by the row's age; every other key is
// a count, flag, identifier, or timestamp that stays true at as_of.
// TestAgeKeysCoverTheDecoderDurations and the postgres package's pin test fail
// when this table and the decoder drift apart.
var ageKeys = map[string]string{
	"queue":    "oldest_outstanding_age_seconds",
	"backlog":  "oldest_outstanding_age_seconds",
	"blockage": "oldest_blocked_age_seconds",
}

// AddAge returns a copy of entries whose age keys are advanced by age, the
// time between the row's as_of and the read. The statement clamps an empty
// set's age to zero, so a zero age is left alone: adding the delay would
// invent an outstanding item. An entry with no age key passes through
// untouched and byte for byte. A non-positive age returns the entries
// unchanged. An age key that is not a JSON number, or an entry that is not a
// JSON object, returns an error so the caller falls back instead of serving a
// value it cannot age. The input is never modified.
func AddAge(entries []Entry, age time.Duration) ([]Entry, error) {
	if age <= 0 {
		return entries, nil
	}
	aged := make([]Entry, len(entries))
	copy(aged, entries)
	for i, entry := range entries {
		key, ok := ageKeys[entry.Section]
		if !ok {
			continue
		}
		next, err := addAgeToEntry(entry, key, age)
		if err != nil {
			return nil, err
		}
		aged[i] = next
	}
	return aged, nil
}

func addAgeToEntry(entry Entry, key string, age time.Duration) (Entry, error) {
	decoder := json.NewDecoder(bytes.NewReader([]byte(entry.JSON)))
	decoder.UseNumber()
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil {
		return Entry{}, fmt.Errorf("age %s row %d: %w", entry.Section, entry.Ordinal, err)
	}
	raw, present := fields[key]
	if !present {
		return entry, nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil || len(raw) == 0 || raw[0] == '"' || string(raw) == "null" {
		return Entry{}, fmt.Errorf("age %s row %d: %s is not a number", entry.Section, entry.Ordinal, key)
	}
	seconds, err := strconv.ParseFloat(number.String(), 64)
	if err != nil {
		return Entry{}, fmt.Errorf("age %s row %d: %s: %w", entry.Section, entry.Ordinal, key, err)
	}
	if seconds <= 0 {
		return entry, nil
	}
	fields[key] = json.RawMessage(strconv.FormatFloat(seconds+age.Seconds(), 'f', -1, 64))
	encoded, err := json.Marshal(fields)
	if err != nil {
		return Entry{}, fmt.Errorf("age %s row %d: %w", entry.Section, entry.Ordinal, err)
	}
	return Entry{Section: entry.Section, Ordinal: entry.Ordinal, JSON: string(encoded)}, nil
}
