// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ageOf reads one numeric key out of an entry's JSON text.
func ageOf(t *testing.T, entry Entry, key string) float64 {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal([]byte(entry.JSON), &fields); err != nil {
		t.Fatalf("entry %s json %q: %v", entry.Section, entry.JSON, err)
	}
	value, ok := fields[key].(float64)
	if !ok {
		t.Fatalf("entry %s has no numeric key %q in %q", entry.Section, key, entry.JSON)
	}
	return value
}

func TestAddAgeAdvancesOnlyTheDurationKeys(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Section: "stage", Ordinal: 1, JSON: `{"stage":"reducer","status":"pending","count":4}`},
		{Section: "backlog", Ordinal: 1, JSON: `{"domain":"a","outstanding_count":2,"oldest_outstanding_age_seconds":10.5}`},
		{Section: "queue", Ordinal: 1, JSON: `{"outstanding_count":9,"overdue_claim_count":1,"oldest_outstanding_age_seconds":42}`},
		{Section: "blockage", Ordinal: 1, JSON: `{"domain":"d","blocked_count":3,"oldest_blocked_age_seconds":7.25}`},
		{Section: "failure", Ordinal: 1, JSON: `{"stage":"reducer","updated_at":"2026-10-06T12:00:00Z"}`},
	}
	original := append([]Entry(nil), entries...)

	got, err := AddAge(entries, 20*time.Second)
	if err != nil {
		t.Fatalf("AddAge() error = %v", err)
	}
	if !reflect.DeepEqual(entries, original) {
		t.Fatalf("AddAge mutated its input: %#v", entries)
	}
	if want := 30.5; ageOf(t, got[1], "oldest_outstanding_age_seconds") != want {
		t.Fatalf("backlog age = %v, want %v", ageOf(t, got[1], "oldest_outstanding_age_seconds"), want)
	}
	if want := 62.0; ageOf(t, got[2], "oldest_outstanding_age_seconds") != want {
		t.Fatalf("queue age = %v, want %v", ageOf(t, got[2], "oldest_outstanding_age_seconds"), want)
	}
	if want := 27.25; ageOf(t, got[3], "oldest_blocked_age_seconds") != want {
		t.Fatalf("blockage age = %v, want %v", ageOf(t, got[3], "oldest_blocked_age_seconds"), want)
	}
	for _, i := range []int{0, 4} {
		if got[i] != entries[i] {
			t.Fatalf("entry %d (%s) changed: %#v", i, got[i].Section, got[i])
		}
	}
	if got[2].JSON == entries[2].JSON {
		t.Fatal("queue entry JSON did not change; the age add was dropped")
	}
	if ageOf(t, got[2], "overdue_claim_count") != 1 || ageOf(t, got[2], "outstanding_count") != 9 {
		t.Fatalf("queue counts changed: %s", got[2].JSON)
	}
}

func TestAddAgeLeavesZeroAgesAlone(t *testing.T) {
	t.Parallel()

	// A zero age means nothing was outstanding at as_of (the statement
	// COALESCEs and clamps at zero); adding the read delay would invent an
	// outstanding item.
	entries := []Entry{
		{Section: "queue", Ordinal: 1, JSON: `{"oldest_outstanding_age_seconds":0}`},
		{Section: "backlog", Ordinal: 1, JSON: `{"oldest_outstanding_age_seconds":0.0}`},
	}
	got, err := AddAge(entries, time.Minute)
	if err != nil {
		t.Fatalf("AddAge() error = %v", err)
	}
	if !reflect.DeepEqual(got, entries) {
		t.Fatalf("zero ages changed: %#v", got)
	}
}

func TestAddAgeNonPositiveAgeReturnsEntriesUnchanged(t *testing.T) {
	t.Parallel()

	entries := []Entry{{Section: "queue", Ordinal: 1, JSON: `{"oldest_outstanding_age_seconds":5}`}}
	for _, age := range []time.Duration{0, -3 * time.Second} {
		got, err := AddAge(entries, age)
		if err != nil {
			t.Fatalf("AddAge(%v) error = %v", age, err)
		}
		if !reflect.DeepEqual(got, entries) {
			t.Fatalf("AddAge(%v) = %#v, want unchanged", age, got)
		}
	}
}

func TestAddAgeRejectsAnAgeKeyThatIsNotANumber(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`{"oldest_outstanding_age_seconds":"12"}`,
		`{"oldest_outstanding_age_seconds":null}`,
		`not json`,
	} {
		_, err := AddAge([]Entry{{Section: "queue", Ordinal: 1, JSON: raw}}, time.Second)
		if err == nil {
			t.Fatalf("AddAge(%q) error = nil, want a decode error", raw)
		}
	}
}

func TestAddAgeKeepsLargeAndPreciseNumbersExact(t *testing.T) {
	t.Parallel()

	entries := []Entry{{Section: "queue", Ordinal: 1, JSON: `{"total_count":9007199254740993,"oldest_outstanding_age_seconds":1.000001}`}}
	got, err := AddAge(entries, 2*time.Second)
	if err != nil {
		t.Fatalf("AddAge() error = %v", err)
	}
	var fields map[string]json.Number
	decoder := json.NewDecoder(strings.NewReader(got[0].JSON))
	decoder.UseNumber()
	if err := decoder.Decode(&fields); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fields["total_count"].String() != "9007199254740993" {
		t.Fatalf("total_count = %s, want the exact large integer", fields["total_count"])
	}
	age, err := strconv.ParseFloat(fields["oldest_outstanding_age_seconds"].String(), 64)
	if err != nil || age != 3.000001 {
		t.Fatalf("age = %v (%v), want 3.000001", age, err)
	}
}

// TestAgeKeysCoverTheDecoderDurations pins the key table to the sections the
// production decoder reads as durations; it fails when a section or key is
// added to one side only.
func TestAgeKeysCoverTheDecoderDurations(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"queue":    "oldest_outstanding_age_seconds",
		"backlog":  "oldest_outstanding_age_seconds",
		"blockage": "oldest_blocked_age_seconds",
	}
	if !reflect.DeepEqual(ageKeys, want) {
		t.Fatalf("ageKeys = %v, want %v", ageKeys, want)
	}
}

// TestAddAgeLeavesTheTerraformStateSectionsByteForByte: the Terraform-state
// rows carry absolute observed_at values and no age, so a reader that runs
// AddAge over them at a 20 s row age must change nothing.
func TestAddAgeLeavesTheTerraformStateSectionsByteForByte(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Section: "last_serial", Ordinal: 1, JSON: `{"safe_locator_hash":"h","serial":3,"observed_at":"2026-10-07T09:30:15Z"}`},
		{Section: "recent_warning", Ordinal: 1, JSON: `{"safe_locator_hash":"h","warning_kind":"state_missing","observed_at":null}`},
	}

	aged, err := AddAge(entries, 20*time.Second)
	if err != nil {
		t.Fatalf("AddAge() error = %v", err)
	}
	if !reflect.DeepEqual(aged, entries) {
		t.Fatalf("AddAge() changed Terraform-state entries:\n got %+v\nwant %+v", aged, entries)
	}
}
