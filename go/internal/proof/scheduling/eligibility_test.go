// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
)

func oracleString(value string) *string { return &value }
func oracleInt(value int64) *int64      { return &value }

func oracleEntity(id string) codetopicparallel.ProbeRow {
	return codetopicparallel.ProbeRow{
		SourceKind: "entity", MatchedTerm: "needle", RepoID: oracleString("repo-a"),
		RelativePath: oracleString("src/" + id + ".go"), EntityID: oracleString(id),
		EntityName: oracleString("needle"), EntityType: oracleString("Function"),
		Language: oracleString(""), StartLine: oracleInt(1), EndLine: oracleInt(2),
	}
}

func oracleFile(path string) codetopicparallel.ProbeRow {
	return codetopicparallel.ProbeRow{
		SourceKind: "file", MatchedTerm: "needle", RepoID: oracleString("repo-a"),
		RelativePath: oracleString(path), EntityID: oracleString(""),
		EntityName: oracleString(""), EntityType: oracleString(""),
		Language: oracleString(""), StartLine: oracleInt(1), EndLine: oracleInt(2),
	}
}

func oracleFixture(entities, paths, contents int) (oracleSamples, map[string]oracleVerified) {
	samples := oracleSamples{}
	verified := make(map[string]oracleVerified)
	for index := range entities {
		row := oracleEntity(fmt.Sprintf("e%03d", index))
		samples.entities = append(samples.entities, row)
		verified[oracleIdentity(row)] = oracleVerified{row: row}
	}
	for index := range paths {
		row := oracleFile(fmt.Sprintf("needle/path-%03d", index))
		samples.paths = append(samples.paths, row)
		verified[oracleIdentity(row)] = oracleVerified{row: row, pathMatch: true}
	}
	for index := range contents {
		row := oracleFile(fmt.Sprintf("content-%03d", index))
		samples.contents = append(samples.contents, row)
		verified[oracleIdentity(row)] = oracleVerified{row: row}
	}
	return samples, verified
}

func TestOracleEntityBoundaries(t *testing.T) {
	for _, count := range []int{0, 249, 250, 251} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			samples, verified := oracleFixture(count, 0, 0)
			selected := samples.entities
			if len(selected) > 250 {
				selected = selected[:250]
			}
			if err := validateOracleTerm("needle", selected, samples, verified, 250); err != nil {
				t.Fatalf("entity count %d: %v", count, err)
			}
		})
	}
}

func TestOracleExactBoundaryAndOmission(t *testing.T) {
	for _, count := range []int{249, 250} {
		samples, verified := oracleFixture(count, 0, 0)
		replacement := oracleEntity("replacement")
		selected := append([]codetopicparallel.ProbeRow(nil), samples.entities...)
		selected[0] = replacement
		verified[oracleIdentity(replacement)] = oracleVerified{row: replacement}
		if err := validateOracleTerm("needle", selected, samples, verified, 250); err == nil || !strings.Contains(err.Error(), "omitted") {
			t.Fatalf("entity count %d: want omission, got %v", count, err)
		}
	}
}

func TestOraclePathFirstAllocation(t *testing.T) {
	samples, verified := oracleFixture(0, 249, 2)
	selected := append([]codetopicparallel.ProbeRow(nil), samples.paths...)
	selected = append(selected, samples.contents[0])
	if err := validateOracleTerm("needle", selected, samples, verified, 250); err != nil {
		t.Fatal(err)
	}
	selected[0] = samples.contents[1]
	if err := validateOracleTerm("needle", selected, samples, verified, 250); err == nil {
		t.Fatal("replacing an eligible path with content must fail")
	}
	samples, verified = oracleFixture(0, 250, 1)
	if err := validateOracleTerm("needle", samples.paths, samples, verified, 250); err != nil {
		t.Fatalf("exactly 250 paths: %v", err)
	}
	samples, verified = oracleFixture(0, 251, 1)
	if err := validateOracleTerm("needle", samples.paths[:250], samples, verified, 250); err != nil {
		t.Fatalf("251 paths: %v", err)
	}
	samples, verified = oracleFixture(0, 0, 250)
	if err := validateOracleTerm("needle", samples.contents, samples, verified, 250); err != nil {
		t.Fatalf("exactly 250 content-only files: %v", err)
	}
	replacement := oracleFile("replacement")
	selected = append([]codetopicparallel.ProbeRow(nil), samples.contents...)
	selected[0] = replacement
	verified[oracleIdentity(replacement)] = oracleVerified{row: replacement}
	if err := validateOracleTerm("needle", selected, samples, verified, 250); err == nil {
		t.Fatal("content-only omission at exactly 250 must fail")
	}
}

func TestOracleRejectsMalformedDuplicateAndOutOfScopeRows(t *testing.T) {
	samples, verified := oracleFixture(1, 0, 0)
	valid := samples.entities[0]
	for _, test := range []struct {
		name     string
		selected []codetopicparallel.ProbeRow
		verified map[string]oracleVerified
	}{
		{name: "malformed projection", selected: func() []codetopicparallel.ProbeRow {
			row := valid
			row.Language = oracleString("wrong")
			return []codetopicparallel.ProbeRow{row}
		}(), verified: verified},
		{name: "duplicate identity", selected: []codetopicparallel.ProbeRow{valid, valid}, verified: verified},
		{name: "scope or raw match absent", selected: []codetopicparallel.ProbeRow{valid}, verified: map[string]oracleVerified{}},
		{name: "missing primary key", selected: func() []codetopicparallel.ProbeRow {
			row := valid
			row.EntityID = nil
			return []codetopicparallel.ProbeRow{row}
		}(), verified: verified},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateOracleTerm("needle", test.selected, samples, test.verified, 250); err == nil {
				t.Fatal("invalid probe accepted")
			}
		})
	}
}

func TestOracleRejectsMalformedFileProjection(t *testing.T) {
	samples, verified := oracleFixture(0, 1, 0)
	malformed := samples.paths[0]
	malformed.EndLine = oracleInt(81)
	if err := validateOracleTerm("needle", []codetopicparallel.ProbeRow{malformed}, samples, verified, 250); err == nil {
		t.Fatal("unclamped file end line accepted")
	}
}

func TestOracleDuplicateErrorDoesNotExposePersistedIdentity(t *testing.T) {
	for _, test := range []struct {
		name    string
		row     codetopicparallel.ProbeRow
		secrets []string
	}{
		{
			name:    "entity",
			row:     oracleEntity("private-entity-canary"),
			secrets: []string{"private-entity-canary"},
		},
		{
			name:    "file",
			row:     oracleFile("private/path-canary.go"),
			secrets: []string{"repo-a", "private/path-canary.go"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			verified := map[string]oracleVerified{oracleIdentity(test.row): {row: test.row}}
			err := validateOracleTerm("needle", []codetopicparallel.ProbeRow{test.row, test.row}, oracleSamples{}, verified, 250)
			if err == nil || !strings.Contains(err.Error(), "duplicate") {
				t.Fatalf("want duplicate rejection, got %v", err)
			}
			for _, secret := range test.secrets {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("duplicate error exposed persisted identity %q: %v", secret, err)
				}
			}
		})
	}
}
