// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

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
