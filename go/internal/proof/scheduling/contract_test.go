// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
)

func probe(kind, term, entity string) codetopicparallel.ProbeRow {
	return codetopicparallel.ProbeRow{SourceKind: kind, MatchedTerm: term, EntityID: &entity}
}

func TestCappedPoolMaySelectDifferentRows(t *testing.T) {
	baseline := []codetopicparallel.ProbeRow{
		probe("entity", "a", "one"), probe("entity", "a", "two"),
	}
	candidate := []codetopicparallel.ProbeRow{
		probe("entity", "a", "one"), probe("entity", "a", "three"),
	}
	if err := validateConditionalPools(baseline, candidate, []string{"a"}, 2); err != nil {
		t.Fatalf("capped pool should allow a different valid subset: %v", err)
	}
}

func TestUncappedPoolMustMatchExactly(t *testing.T) {
	baseline := []codetopicparallel.ProbeRow{probe("entity", "a", "one")}
	candidate := []codetopicparallel.ProbeRow{probe("entity", "a", "two")}
	if err := validateConditionalPools(baseline, candidate, []string{"a"}, 2); err == nil {
		t.Fatal("uncapped mismatch was accepted")
	}
}

func TestCappedPoolRejectsMissingOrDuplicateRows(t *testing.T) {
	baseline := []codetopicparallel.ProbeRow{
		probe("file", "a", "one"), probe("file", "a", "two"),
	}
	for _, test := range []struct {
		name      string
		candidate []codetopicparallel.ProbeRow
	}{
		{"missing", []codetopicparallel.ProbeRow{probe("file", "a", "one")}},
		{"duplicate", []codetopicparallel.ProbeRow{probe("file", "a", "one"), probe("file", "a", "one")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateConditionalPools(baseline, test.candidate, []string{"a"}, 2); err == nil {
				t.Fatal("invalid capped pool was accepted")
			}
		})
	}
}

func TestConditionalPoolsRejectUnexpectedTermAndKind(t *testing.T) {
	for _, row := range []codetopicparallel.ProbeRow{
		probe("entity", "other", "one"),
		probe("unknown", "a", "one"),
	} {
		if err := validateConditionalPools(nil, []codetopicparallel.ProbeRow{row}, []string{"a"}, 2); err == nil {
			t.Fatalf("unexpected row accepted: %+v", row)
		}
	}
}

func TestConditionalPoolsRejectInvalidCap(t *testing.T) {
	if err := validateConditionalPools(nil, nil, []string{"a"}, 0); err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("invalid cap error = %v", err)
	}
}

func TestScopeRejectsUngrantableRows(t *testing.T) {
	allowed := "allowed"
	other := "other"
	for _, repo := range []*string{&other, nil} {
		row := probe("entity", "a", "one")
		row.RepoID = repo
		if err := validateScope([]codetopicparallel.ProbeRow{row}, []string{allowed}, ""); err == nil {
			t.Fatalf("row with repo %v escaped the grant", repo)
		}
	}
	row := probe("entity", "a", "one")
	row.RepoID = &allowed
	if err := validateScope([]codetopicparallel.ProbeRow{row}, []string{allowed}, ""); err != nil {
		t.Fatalf("allowed repo rejected: %v", err)
	}
}

func TestScopeRejectsWrongOrMissingLanguage(t *testing.T) {
	for _, language := range []*string{nil, ptr("python")} {
		row := probe("file", "a", "")
		row.Language = language
		if err := validateScope([]codetopicparallel.ProbeRow{row}, nil, "go"); err == nil {
			t.Fatalf("row with language %v escaped the filter", language)
		}
	}
	row := probe("file", "a", "")
	row.Language = ptr("go")
	if err := validateScope([]codetopicparallel.ProbeRow{row}, nil, "go"); err != nil {
		t.Fatalf("matching language rejected: %v", err)
	}
}

func ptr(value string) *string {
	return &value
}

func TestReaderMustBeRecoveryAndReadOnly(t *testing.T) {
	if err := requireReadOnlyReader(true, "on"); err != nil {
		t.Fatalf("read-only replica rejected: %v", err)
	}
	for _, state := range []struct {
		recovery bool
		readOnly string
	}{
		{false, "on"},
		{true, "off"},
	} {
		if err := requireReadOnlyReader(state.recovery, state.readOnly); err == nil {
			t.Fatalf("unsafe reader state accepted: %+v", state)
		}
	}
}
