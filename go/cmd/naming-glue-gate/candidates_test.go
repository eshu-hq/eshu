// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"reflect"
	"testing"
)

// fakeGitRunner answers Directories from a canned table keyed by "ref:dir",
// so NewDirectories can be tested without a real git repository.
type fakeGitRunner struct {
	byRefDir map[string][]string
	err      error
}

func (f fakeGitRunner) Directories(ref, dir string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byRefDir[ref+":"+dir], nil
}

func TestNewDirectoriesMergesAcrossDirsAndFiltersViaRefs(t *testing.T) {
	runner := fakeGitRunner{byRefDir: map[string][]string{
		"main:go/internal": {"go/internal/query"},
		"HEAD:go/internal": {"go/internal/query", "go/internal/query/taghistory"},
		"main:go/cmd":      {"go/cmd/api"},
		"HEAD:go/cmd":      {"go/cmd/api", "go/cmd/api/workloadinstance"},
	}}
	got, err := NewDirectories(runner, "main", "HEAD", []string{"go/internal", "go/cmd"})
	if err != nil {
		t.Fatalf("NewDirectories: %v", err)
	}
	want := []Candidate{
		{Path: "go/cmd/api/workloadinstance", Name: "workloadinstance"},
		{Path: "go/internal/query/taghistory", Name: "taghistory"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NewDirectories() = %+v, want %+v", got, want)
	}
}

func TestNewDirectoriesPropagatesRunnerError(t *testing.T) {
	runner := fakeGitRunner{err: fmt.Errorf("boom")}
	_, err := NewDirectories(runner, "main", "HEAD", []string{"go/internal"})
	if err == nil {
		t.Fatal("NewDirectories() error = nil, want non-nil when the runner fails")
	}
}

func TestNewDirectoriesFromListsFindsAdditionsOnly(t *testing.T) {
	old := []string{"go/internal/query", "go/internal/query/entity", "go/internal/reducer"}
	cur := []string{
		"go/internal/query", "go/internal/query/entity",
		"go/internal/reducer", "go/internal/reducer/workloadinstance",
		"go/internal/query/taghistory",
	}
	got := newDirectoriesFromLists(old, cur)
	want := []Candidate{
		{Path: "go/internal/query/taghistory", Name: "taghistory"},
		{Path: "go/internal/reducer/workloadinstance", Name: "workloadinstance"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("newDirectoriesFromLists() = %+v, want %+v", got, want)
	}
}

func TestNewDirectoriesFromListsSkipsTestdataAndFixtures(t *testing.T) {
	old := []string{"go/internal/parser"}
	cur := []string{
		"go/internal/parser",
		"go/internal/parser/testdata/somefamily",
		"go/internal/parser/testdata/somefamily/deepnest",
		"go/internal/parser/fixtures/anotherfamily",
	}
	got := newDirectoriesFromLists(old, cur)
	if len(got) != 0 {
		t.Fatalf("newDirectoriesFromLists() = %+v, want empty (testdata/fixtures subtrees are exempt)", got)
	}
}

func TestNewDirectoriesFromListsSkipsDotDirectories(t *testing.T) {
	old := []string{"go/internal/foo"}
	cur := []string{"go/internal/foo", "go/internal/foo/.cache"}
	got := newDirectoriesFromLists(old, cur)
	if len(got) != 0 {
		t.Fatalf("newDirectoriesFromLists() = %+v, want empty (dot-directories are exempt)", got)
	}
}

func TestNewDirectoriesFromListsSkipsSeparatedNames(t *testing.T) {
	// A name that already uses an underscore or hyphen is not a glued
	// compound by definition (it already has a word boundary), so it is
	// not a candidate for this check -- the existing filename-stutter
	// gate's parent-repeat rule covers separated names.
	old := []string{"go/cmd"}
	cur := []string{"go/cmd", "go/cmd/read-api-latency-gate", "go/cmd/some_underscored_name"}
	got := newDirectoriesFromLists(old, cur)
	if len(got) != 0 {
		t.Fatalf("newDirectoriesFromLists() = %+v, want empty (separated names are not glued compounds)", got)
	}
}

func TestNewDirectoriesFromListsSkipsShortNames(t *testing.T) {
	old := []string{"go/internal"}
	cur := []string{"go/internal", "go/internal/aws", "go/internal/gcp"}
	got := newDirectoriesFromLists(old, cur)
	if len(got) != 0 {
		t.Fatalf("newDirectoriesFromLists() = %+v, want empty (too short to plausibly be a two-word glue)", got)
	}
}

func TestNewDirectoriesFromListsSkipsMixedCase(t *testing.T) {
	// A directory containing an uppercase letter is not the lowercase,
	// no-separator glue shape rule 3 targets (e.g. a fixture mirroring a
	// third-party convention).
	old := []string{"go/internal"}
	cur := []string{"go/internal", "go/internal/SomeThirdPartyDir"}
	got := newDirectoriesFromLists(old, cur)
	if len(got) != 0 {
		t.Fatalf("newDirectoriesFromLists() = %+v, want empty (mixed case is not the glue shape)", got)
	}
}
