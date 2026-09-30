// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"errors"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
)

// Patterns that read the shipped statements; the tests compare what they
// extract, never a hand-frozen copy of the SQL.
var (
	markerStatusListPattern = regexp.MustCompile(`(?m)^\s*AND status IN \(([^)]*)\)`)
	markerSetPattern        = regexp.MustCompile(`(?m)^SET projection_write_started_at = (.+)$`)
	lockedStatusListPattern = regexp.MustCompile(`generation\.status IN \(([^)]*)\)`)
)

// TestWriteMarkerStatusAndSetDerivedFromShippedQuery pins the #7389 marker
// rules on the shipped constants: a failed generation is markable (a
// dead-lettered generation can be replayed and activate), the marker keeps the
// latest write start (GREATEST, never backwards), and the heartbeat's locked
// gate still only supersedes pending or active generations.
func TestWriteMarkerStatusAndSetDerivedFromShippedQuery(t *testing.T) {
	t.Parallel()
	statuses := markerStatusListPattern.FindAllStringSubmatch(markProjectionWriteStartedQuery, -1)
	if len(statuses) != 1 || statuses[0][1] != "'pending', 'active', 'failed'" {
		t.Fatalf("marker generation status lists = %v, want exactly one: ('pending', 'active', 'failed')", statuses)
	}
	set := markerSetPattern.FindAllStringSubmatch(markProjectionWriteStartedQuery, -1)
	if len(set) != 1 || set[0][1] != "GREATEST(COALESCE(projection_write_started_at, $5), $5)" {
		t.Fatalf("marker SET clauses = %v, want GREATEST(COALESCE(projection_write_started_at, $5), $5)", set)
	}
	locked := lockedStatusListPattern.FindAllStringSubmatch(lockedGatePredicate(t), -1)
	if len(locked) != 1 || locked[0][1] != "'pending', 'active'" {
		t.Fatalf("heartbeat locked gate status lists = %v, want exactly one: ('pending', 'active')", locked)
	}
	retired := make([]string, 0, len(writeMarkerRetiredGenerationStatuses))
	for status, ok := range writeMarkerRetiredGenerationStatuses {
		if ok {
			retired = append(retired, status)
		}
	}
	sort.Strings(retired)
	if strings.Join(retired, ",") != "completed,superseded" {
		t.Fatalf("marker retired generation statuses = %v, want completed and superseded (plus a missing row)", retired)
	}
}

func markerNullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

// TestWriteMarkerRefusalClassification is the classifier table from the
// #7389 ruling.
func TestWriteMarkerRefusalClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                    string
		generationStatus, workStatus, workClass string
		wantSuperseded                          bool
		wantClass                               string
	}{
		{
			name: "superseded_owned_keeps_work_class", generationStatus: "superseded", workStatus: "running",
			workClass: "projection_retryable", wantSuperseded: true, wantClass: "projection_retryable",
		},
		{
			name: "superseded_owned_no_class", generationStatus: "superseded", workStatus: "running",
			wantSuperseded: true, wantClass: projectorWriteMarkerGenerationRetiredClass,
		},
		{
			name: "completed", generationStatus: "completed", workStatus: "running",
			wantSuperseded: true, wantClass: projectorWriteMarkerGenerationRetiredClass,
		},
		{name: "missing_row", wantSuperseded: true, wantClass: projectorWriteMarkerGenerationRetiredClass},
		{name: "failed_not_owned", generationStatus: "failed", workStatus: "dead_letter", workClass: "projection_bug"},
		{name: "pending_not_owned", generationStatus: "pending", workStatus: "running"},
		{
			name: "failed_owned_work_superseded", generationStatus: "failed", workStatus: "superseded",
			workClass: "projector_superseded_by_newer_generation", wantSuperseded: true,
			wantClass: "projector_superseded_by_newer_generation",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := writeMarkerRefusal("gen-x", markerNullString(tc.generationStatus), markerNullString(tc.workStatus), markerNullString(tc.workClass))
			if !tc.wantSuperseded {
				if !errors.Is(err, ErrProjectorClaimRejected) || errors.Is(err, failure.ErrWorkSuperseded) {
					t.Fatalf("writeMarkerRefusal() = %v, want ErrProjectorClaimRejected", err)
				}
				return
			}
			var superseded projectorWorkSupersededError
			if !errors.As(err, &superseded) || superseded.FailureClass() != tc.wantClass {
				t.Fatalf("writeMarkerRefusal() = %v, want ErrWorkSuperseded with class %s", err, tc.wantClass)
			}
		})
	}
}
