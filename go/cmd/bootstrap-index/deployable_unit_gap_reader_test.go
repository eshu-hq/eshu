// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"reflect"
	"testing"
)

// scriptIncidentRecord answers Get from a fixed map: the scripted stand-in
// for a Bolt driver record.
type scriptIncidentRecord struct {
	values map[string]any
}

func (r scriptIncidentRecord) Get(key string) (any, bool) {
	value, ok := r.values[key]
	return value, ok
}

// TestCountDeployableUnitIncidentReposSkipsMisshapenRows proves the #8371
// review F4 contract: valid rows count, while rows with a missing, empty,
// or non-string repo_id are skipped. A drift that emptied the counts would
// surface every anchor as a gap (bounded replay, self-healing) rather than
// hide a loss behind a miscount.
func TestCountDeployableUnitIncidentReposSkipsMisshapenRows(t *testing.T) {
	t.Parallel()

	rows := []deployableUnitIncidentRecord{
		scriptIncidentRecord{values: map[string]any{"repo_id": "repo-a"}},
		scriptIncidentRecord{values: map[string]any{"repo_id": "repo-a"}},
		scriptIncidentRecord{values: map[string]any{"repo_id": "repo-b"}},
		scriptIncidentRecord{values: map[string]any{}},
		scriptIncidentRecord{values: map[string]any{"repo_id": ""}},
		scriptIncidentRecord{values: map[string]any{"repo_id": 42}},
		scriptIncidentRecord{values: map[string]any{"other_key": "repo-c"}},
	}
	if got, want := countDeployableUnitIncidentRepos(rows), map[string]int64{"repo-a": 2, "repo-b": 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("counts = %#v, want %#v", got, want)
	}
	if got := countDeployableUnitIncidentRepos(nil); len(got) != 0 {
		t.Fatalf("empty counts = %#v, want empty", got)
	}
}
