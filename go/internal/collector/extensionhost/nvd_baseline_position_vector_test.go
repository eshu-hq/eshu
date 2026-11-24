// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"regexp"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/vulnsource"
)

// Shared baseline position vector (eshu-hq/eshu-sdk#97 and #98,
// eshu-hq/eshu-collector-vulnerability-intelligence#199). The vector's
// top-level position block pins the scope id grammar, the CVE ID
// canonical form and the (stamp, CVE ID) order that core and the collector
// each implement. It rides the same file as the verdict-key vector, which
// nvd_verdict_keys_vector_test.go loads and digest-pins, and the collector
// replays the same cases in its TestNVDBaselinePositionMatchesSharedVector,
// so a grammar or order drift on either side fails both suites.

type nvdPositionVector struct {
	ScopeGrammar string                   `json:"scope_grammar"`
	CVEIDPattern string                   `json:"cve_id_pattern"`
	CanonicalIDs []nvdPositionCanonicalID `json:"canonical_ids"`
	RoundTrips   []nvdPositionRoundTrip   `json:"round_trips"`
	Rejections   []nvdPositionRejection   `json:"rejections"`
	Orders       []nvdPositionOrder       `json:"orders"`
}

type nvdPositionCanonicalID struct {
	Name      string `json:"name"`
	Raw       string `json:"raw"`
	Canonical string `json:"canonical"`
	Valid     bool   `json:"valid"`
}

type nvdPositionRoundTrip struct {
	Name        string `json:"name"`
	ScopeID     string `json:"scope_id"`
	Run         string `json:"run"`
	Year        int    `json:"year"`
	StartUnixUS int64  `json:"start_unix_us"`
	FromCVEID   string `json:"from_cve_id"`
}

type nvdPositionRejection struct {
	Name    string `json:"name"`
	ScopeID string `json:"scope_id"`
}

type nvdPositionFields struct {
	StartUnixUS int64  `json:"start_unix_us"`
	FromCVEID   string `json:"from_cve_id"`
}

type nvdPositionOrder struct {
	Name string            `json:"name"`
	A    nvdPositionFields `json:"a"`
	B    nvdPositionFields `json:"b"`
	Cmp  int               `json:"cmp"`
}

// TestNVDBaselinePositionMatchesSharedVector replays the vector's position
// block through core's scope parser, scope builder, canonical-ID predicate
// and position order. An empty block fails, so a vector that loses it cannot
// pass by replaying nothing. Core never canonicalizes a raw record ID (that
// transform is the collector's, on IDs read from the feed); a scope id always
// carries the canonical form as-is, so the canonical-id cases replay against
// the predicate core shares with the checkpoint writer.
func TestNVDBaselinePositionMatchesSharedVector(t *testing.T) {
	t.Parallel()

	position := loadNVDVerdictVector(t).Position
	if len(position.CanonicalIDs) == 0 || len(position.RoundTrips) == 0 || len(position.Rejections) == 0 || len(position.Orders) == 0 {
		t.Fatalf("position block is missing cases: %d canonical ids, %d round trips, %d rejections, %d orders",
			len(position.CanonicalIDs), len(position.RoundTrips), len(position.Rejections), len(position.Orders))
	}
	if want := "vuln-intel://nvd/_baseline/<run>/<year>/<start_unix_us>[/<from_cve_id>]"; position.ScopeGrammar != want {
		t.Errorf("scope grammar = %q, want %q", position.ScopeGrammar, want)
	}
	cveIDPattern, err := regexp.Compile(position.CVEIDPattern)
	if err != nil {
		t.Fatalf("vector cve_id_pattern %q does not compile: %v", position.CVEIDPattern, err)
	}

	for _, c := range position.CanonicalIDs {
		t.Run("canonical/"+c.Name, func(t *testing.T) {
			t.Parallel()
			if got := vulnsource.IsCanonicalNVDBaselineCVEID(c.Canonical); got != c.Valid {
				t.Errorf("IsCanonicalNVDBaselineCVEID(%q) = %v, want %v", c.Canonical, got, c.Valid)
			}
			if got := cveIDPattern.MatchString(c.Canonical); got != c.Valid {
				t.Errorf("vector pattern on canonical %q = %v, want %v", c.Canonical, got, c.Valid)
			}
			// A raw ID that needs no transform is canonical exactly when the
			// case is valid; one that needs trimming or upper-casing is never
			// a canonical scope segment as written.
			if want := c.Valid && c.Raw == c.Canonical; vulnsource.IsCanonicalNVDBaselineCVEID(c.Raw) != want {
				t.Errorf("IsCanonicalNVDBaselineCVEID(raw %q) = %v, want %v", c.Raw, !want, want)
			}
		})
	}
	for _, c := range position.RoundTrips {
		t.Run("round_trip/"+c.Name, func(t *testing.T) {
			t.Parallel()
			runID, year, start, fromCVEID, ok := vulnsource.ParseNVDBaselineScope(c.ScopeID)
			if !ok {
				t.Fatalf("ParseNVDBaselineScope(%q) rejected a scope the vector round-trips", c.ScopeID)
			}
			if runID != c.Run || year != c.Year || start.UnixMicro() != c.StartUnixUS || fromCVEID != c.FromCVEID {
				t.Errorf("parse(%q) = (%q, %d, %d, %q), want (%q, %d, %d, %q)", c.ScopeID,
					runID, year, start.UnixMicro(), fromCVEID, c.Run, c.Year, c.StartUnixUS, c.FromCVEID)
			}
			rebuilt := vulnsource.NVDBaselineScopeID(c.Run, c.Year, time.UnixMicro(c.StartUnixUS).UTC(), c.FromCVEID)
			if rebuilt != c.ScopeID {
				t.Errorf("rebuilt scope = %q, want the one scope id %q", rebuilt, c.ScopeID)
			}
		})
	}
	for _, c := range position.Rejections {
		t.Run("rejection/"+c.Name, func(t *testing.T) {
			t.Parallel()
			if _, _, _, _, ok := vulnsource.ParseNVDBaselineScope(c.ScopeID); ok {
				t.Errorf("ParseNVDBaselineScope(%q) accepted a scope the vector rejects", c.ScopeID)
			}
			if vulnsource.IsNVDBaselineScope(c.ScopeID) {
				t.Errorf("IsNVDBaselineScope(%q) = true, want false", c.ScopeID)
			}
		})
	}
	for _, c := range position.Orders {
		t.Run("order/"+c.Name, func(t *testing.T) {
			t.Parallel()
			a := nvdBaselinePosition{unixMicro: c.A.StartUnixUS, cveID: c.A.FromCVEID}
			b := nvdBaselinePosition{unixMicro: c.B.StartUnixUS, cveID: c.B.FromCVEID}
			if got := a.compare(b); signOf(got) != signOf(c.Cmp) {
				t.Errorf("compare(%+v, %+v) = %d, want sign of %d", a, b, got, c.Cmp)
			}
		})
	}
}

// signOf reduces a comparison result to -1, 0 or 1.
func signOf(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}
