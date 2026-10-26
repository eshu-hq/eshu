// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eol

import (
	"strconv"
	"strings"
	"time"
)

// Statuses a declaration can resolve to. Missing data is never read as
// supported: anything the snapshot cannot place yields StatusUnknown.
const (
	// StatusSupported marks a cycle still within its support window, or a
	// rolling release whose eol is false.
	StatusSupported = "supported"
	// StatusEOL marks a cycle past its end-of-life day.
	StatusEOL = "eol"
	// StatusSupportEnding marks a cycle past its end of active support but
	// before its end-of-life day.
	StatusSupportEnding = "support-ending"
	// StatusUnknown marks a declaration the snapshot cannot place: an
	// unmapped runtime, a missing product file or cycle, or a version too
	// vague to pick a cycle.
	StatusUnknown = "unknown"
)

// runtimeSlug maps an Eshu canonical runtime to its endoflife.date slug.
// Absent entries (java, swift, and any unmapped tool token) resolve unknown:
// java has no plain slug, only vendor builds, and swift has none at all.
var runtimeSlug = map[string]string{
	"node":      "nodejs",
	"go":        "go",
	"python":    "python",
	"ruby":      "ruby",
	"php":       "php",
	"rust":      "rust",
	"terraform": "terraform",
	"dotnet":    "dotnet",
}

// Answer is the join of one declared runtime version with the snapshot.
type Answer struct {
	// Status is one of the Status constants.
	Status string `json:"status"`
	// Cycle is the matched endoflife.date cycle, empty when unknown.
	Cycle string `json:"cycle,omitempty"`
	// SnapshotDate is the UTC day of the vendored fetch, YYYY-MM-DD.
	SnapshotDate string `json:"snapshot_date"`
	// SnapshotAgeDays is whole days from the snapshot to the query clock,
	// clamped at zero so a future-dated snapshot never reports negative age.
	SnapshotAgeDays int `json:"snapshot_age_days"`
	// Stale is true when the snapshot is past its refresh window.
	Stale bool `json:"stale"`
}

// Resolve joins one declared version against the snapshot. major is the
// declared major version; minor and hasMinor carry the declared minor, which
// is absent for a partial such as "20" or "3". now is the query clock. The
// function performs no I/O and therefore no network access.
func Resolve(sn Snapshot, runtime string, major, minor int, hasMinor bool, now time.Time) Answer {
	ans := Answer{Status: StatusUnknown, SnapshotDate: sn.Date}
	ans.SnapshotAgeDays, ans.Stale = snapshotAge(sn.Date, now)
	slug, ok := runtimeSlug[strings.ToLower(runtime)]
	if !ok {
		return ans
	}
	found, matched := matchCycle(sn.Products[slug], major, minor, hasMinor)
	if !matched {
		return ans
	}
	ans.Cycle = found.Cycle
	ans.Status = cycleStatus(found, now)
	return ans
}

// matchCycle picks the cycle a declared version belongs to. A cycle name is
// major or major.minor; the declaration must supply at least that many
// components and agree on every one, so a bare "3" never picks between
// "3.12" and "3.13".
func matchCycle(cycles []cycle, major, minor int, hasMinor bool) (cycle, bool) {
	for _, c := range cycles {
		parts := strings.Split(c.Cycle, ".")
		nums := make([]int, 0, len(parts))
		valid := true
		for _, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil {
				valid = false
				break
			}
			nums = append(nums, n)
		}
		if !valid || len(nums) == 0 || len(nums) > 2 {
			continue
		}
		if nums[0] != major {
			continue
		}
		if len(nums) == 2 && (!hasMinor || nums[1] != minor) {
			continue
		}
		return c, true
	}
	return cycle{}, false
}

// cycleStatus reads one matched cycle at the query clock. A rolling release
// (eol false) is supported; otherwise the eol day ends support and the
// support day, when the product publishes one, opens the support-ending
// window. An unparseable or missing eol day is unknown, never supported.
func cycleStatus(c cycle, now time.Time) string {
	if c.EOLFalse {
		return StatusSupported
	}
	eol, err := time.Parse(dateLayout, c.EOL)
	if err != nil {
		return StatusUnknown
	}
	today := now.UTC().Truncate(24 * time.Hour)
	eolDay := eol.UTC().Truncate(24 * time.Hour)
	if !today.Before(eolDay.Add(24 * time.Hour)) {
		return StatusEOL
	}
	if c.Support != "" {
		if support, err := time.Parse(dateLayout, c.Support); err == nil {
			if !today.Before(support.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)) {
				return StatusSupportEnding
			}
		}
	}
	return StatusSupported
}

// snapshotAge returns whole days from the snapshot day to the query clock
// with future dates clamped at zero, plus whether the snapshot is past its
// refresh window. A date that cannot parse is treated as maximally stale:
// unreadable provenance must attract attention, never look fresh.
func snapshotAge(date string, now time.Time) (int, bool) {
	day, err := time.Parse(dateLayout, date)
	if err != nil {
		return 0, true
	}
	days := int(now.UTC().Truncate(24*time.Hour).Sub(day.UTC()).Hours() / 24)
	if days < 0 {
		days = 0
	}
	return days, days > RefreshWindowDays
}
