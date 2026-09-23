// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scalars

import (
	"database/sql"
	"testing"
	"time"
)

func TestBlankRejectsEmptyAndWhitespace(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "   ", "\t\n "} {
		if !Blank(value) {
			t.Errorf("Blank(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"a", " a ", "0"} {
		if Blank(value) {
			t.Errorf("Blank(%q) = true, want false", value)
		}
	}
}

func TestNullTimeMapsZeroToInvalid(t *testing.T) {
	t.Parallel()

	if got := NullTime(time.Time{}); got.Valid {
		t.Errorf("NullTime(zero) = %#v, want invalid", got)
	}
	stamp := time.Date(2026, time.May, 12, 14, 0, 0, 0, time.UTC)
	if got := NullTime(stamp); !got.Valid || !got.Time.Equal(stamp) {
		t.Errorf("NullTime(stamp) = %#v, want valid %v", got, stamp)
	}
}

func TestNullTimePtrNormalizesToUTC(t *testing.T) {
	t.Parallel()

	if got := NullTimePtr(nil); got.Valid {
		t.Errorf("NullTimePtr(nil) = %#v, want invalid", got)
	}
	zero := time.Time{}
	if got := NullTimePtr(&zero); got.Valid {
		t.Errorf("NullTimePtr(zero) = %#v, want invalid", got)
	}
	eastern := time.FixedZone("EST", -5*3600)
	local := time.Date(2026, time.May, 12, 9, 0, 0, 0, eastern)
	got := NullTimePtr(&local)
	if !got.Valid {
		t.Fatalf("NullTimePtr(local) invalid, want valid")
	}
	if want := local.UTC(); !got.Time.Equal(want) {
		t.Errorf("NullTimePtr = %v, want %v", got.Time, want)
	}
}

func TestTimePtrFromNullRoundTripsUTC(t *testing.T) {
	t.Parallel()

	if got := TimePtrFromNull(sql.NullTime{}); got != nil {
		t.Errorf("TimePtrFromNull(invalid) = %v, want nil", got)
	}
	stamp := time.Date(2026, time.May, 12, 14, 0, 0, 0, time.UTC)
	got := TimePtrFromNull(sql.NullTime{Time: stamp, Valid: true})
	if got == nil || !got.Equal(stamp) {
		t.Errorf("TimePtrFromNull = %v, want %v", got, stamp)
	}
}

func TestDurationFromSecondsClampsNonPositive(t *testing.T) {
	t.Parallel()

	for _, value := range []float64{0, -1, -0.5} {
		if got := DurationFromSeconds(value); got != 0 {
			t.Errorf("DurationFromSeconds(%v) = %v, want 0", value, got)
		}
	}
	if got, want := DurationFromSeconds(1.5), 1500*time.Millisecond; got != want {
		t.Errorf("DurationFromSeconds(1.5) = %v, want %v", got, want)
	}
}

func TestNullableTimeUTCMapsInvalidToZero(t *testing.T) {
	t.Parallel()

	if got := NullableTimeUTC(sql.NullTime{}); !got.IsZero() {
		t.Errorf("NullableTimeUTC(invalid) = %v, want zero", got)
	}
	eastern := time.FixedZone("EST", -5*3600)
	local := time.Date(2026, time.May, 12, 9, 0, 0, 0, eastern)
	got := NullableTimeUTC(sql.NullTime{Time: local, Valid: true})
	if want := local.UTC(); !got.Equal(want) {
		t.Errorf("NullableTimeUTC(valid) = %v, want %v", got, want)
	}
}

func TestNullableTimeBindsZeroAsNil(t *testing.T) {
	t.Parallel()

	if got := NullableTime(time.Time{}); got != nil {
		t.Errorf("NullableTime(zero) = %v, want nil", got)
	}
	eastern := time.FixedZone("EST", -5*3600)
	local := time.Date(2026, time.May, 12, 9, 0, 0, 0, eastern)
	got, ok := NullableTime(local).(time.Time)
	if !ok || !got.Equal(local.UTC()) {
		t.Errorf("NullableTime(local) = %v, want %v", got, local.UTC())
	}
}

func TestStringMapToAnyWidensOrNilsEmpty(t *testing.T) {
	t.Parallel()

	if got := StringMapToAny(nil); got != nil {
		t.Errorf("StringMapToAny(nil) = %#v, want nil", got)
	}
	if got := StringMapToAny(map[string]string{}); got != nil {
		t.Errorf("StringMapToAny(empty) = %#v, want nil", got)
	}
	got := StringMapToAny(map[string]string{"a": "1"})
	if want := (map[string]any{"a": "1"}); len(got) != len(want) || got["a"] != want["a"] {
		t.Errorf("StringMapToAny = %#v, want %#v", got, want)
	}
}

func TestCleanStringSetTrimsDedupesAndDropsBlanks(t *testing.T) {
	t.Parallel()

	got := CleanStringSet([]string{" b ", "a", "b", "", "  ", "a", "c"})
	want := []string{"b", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("CleanStringSet = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("CleanStringSet = %#v, want %#v", got, want)
		}
	}
	if got := CleanStringSet(nil); len(got) != 0 {
		t.Errorf("CleanStringSet(nil) = %#v, want empty", got)
	}
}
