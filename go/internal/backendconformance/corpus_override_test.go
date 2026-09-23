// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"context"
	"strings"
	"testing"
)

func overrideTestCase(override BackendOverride) ReadCase {
	return ReadCase{
		Name:       "override case",
		Capability: CapabilityCanonicalWrites,
		Cypher:     "MATCH (m:Module) RETURN m.uid AS uid",
		WantRows:   []map[string]any{},
		Overrides:  map[BackendID]BackendOverride{BackendNornicDB: override},
	}
}

// TestReadCaseOverrideRequiresATrackingIssue: an override without an issue
// reference is an undocumented acceptance of wrong rows, so the corpus refuses
// to run it.
func TestReadCaseOverrideRequiresATrackingIssue(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"uid": "module:x"}}
	for _, divergence := range []string{"", "   ", "NornicDB creates the node", "6968", "#", "see #abc"} {
		tc := overrideTestCase(BackendOverride{Divergence: divergence, WantRows: rows})
		query := &recordingGraphQuery{rows: rows}
		_, err := RunReadCorpusFor(context.Background(), query, BackendNornicDB, []ReadCase{tc})
		if err == nil || !strings.Contains(err.Error(), "tracking issue") {
			t.Errorf("Divergence %q: error = %v, want tracking-issue rejection", divergence, err)
		}
		if len(query.calls) != 0 {
			t.Errorf("Divergence %q: case ran %d queries before validation rejected it", divergence, len(query.calls))
		}
	}
}

// TestReadCaseOverrideRejectsIncompleteOrPointlessEntries covers the other
// ways an override could hide a check: no default rows to diverge from, nil
// override rows (which would disable the comparison), an unknown backend key,
// and override rows equal to the correct rows (a stale override once the bug
// is fixed).
func TestReadCaseOverrideRejectsIncompleteOrPointlessEntries(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"uid": "module:x"}}
	noDefault := overrideTestCase(BackendOverride{Divergence: "#6968", WantRows: rows})
	noDefault.WantRows = nil
	nilRows := overrideTestCase(BackendOverride{Divergence: "#6968"})
	unknown := overrideTestCase(BackendOverride{Divergence: "#6968", WantRows: rows})
	unknown.Overrides = map[BackendID]BackendOverride{"memgraph": {Divergence: "#6968", WantRows: rows}}
	same := overrideTestCase(BackendOverride{Divergence: "#6968", WantRows: []map[string]any{}})

	for name, tc := range map[string]ReadCase{
		"no default WantRows": noDefault,
		"nil override rows":   nilRows,
		"unknown backend":     unknown,
		"same as correct":     same,
	} {
		if err := validateReadCase(tc); err == nil {
			t.Errorf("%s: validateReadCase() = nil, want error", name)
		}
	}
	if err := validateReadCase(overrideTestCase(BackendOverride{Divergence: "#6968", WantRows: rows})); err != nil {
		t.Errorf("valid override: validateReadCase() = %v, want nil", err)
	}
}

// TestRunReadCorpusForSelectsTheBackendsRows: the overridden backend is held
// to its pinned rows, every other backend (and the backend-neutral runner) to
// the correct rows, so a change in either direction fails.
func TestRunReadCorpusForSelectsTheBackendsRows(t *testing.T) {
	t.Parallel()

	pinned := []map[string]any{{"uid": "module:x"}}
	tc := overrideTestCase(BackendOverride{Divergence: "#6968", WantRows: pinned})

	for _, step := range []struct {
		name    string
		backend BackendID
		rows    []map[string]any
		wantErr bool
	}{
		{"nornicdb gives its pinned rows", BackendNornicDB, pinned, false},
		{"nornicdb gives the correct rows", BackendNornicDB, []map[string]any{}, true},
		{"neo4j gives the correct rows", BackendNeo4j, []map[string]any{}, false},
		{"neo4j gives nornicdb's rows", BackendNeo4j, pinned, true},
		{"neutral runner gives the correct rows", "", []map[string]any{}, false},
		{"neutral runner gives nornicdb's rows", "", pinned, true},
	} {
		_, err := RunReadCorpusFor(context.Background(), &recordingGraphQuery{rows: step.rows}, step.backend, []ReadCase{tc})
		if (err != nil) != step.wantErr {
			t.Errorf("%s: error = %v, wantErr %v", step.name, err, step.wantErr)
		}
	}
}

// TestOverrideFailurePrintsBackendDivergenceAndActualRows: a CI failure must
// say which expectation was applied and show the true rows, so the pinned
// rows can be corrected from the log alone.
func TestOverrideFailurePrintsBackendDivergenceAndActualRows(t *testing.T) {
	t.Parallel()

	tc := overrideTestCase(BackendOverride{Divergence: "#6968", WantRows: []map[string]any{{"uid": "module:x"}}})
	actual := []map[string]any{{"uid": "module:actual-value"}}
	_, err := RunReadCorpusFor(context.Background(), &recordingGraphQuery{rows: actual}, BackendNornicDB, []ReadCase{tc})
	if err == nil {
		t.Fatal("RunReadCorpusFor() error = nil, want mismatch")
	}
	for _, want := range []string{"nornicdb", "#6968", "module:actual-value", "module:x"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err.Error(), want)
		}
	}
}

// TestDefaultReadCorpusPassesEachLaneOnItsOwnExpectations runs the whole
// default corpus as each backend against a fake that answers every exact-row
// case with the rows that backend is held to, proving the corpus and its
// overrides are consistent per lane and that the pinned cases report their
// divergence.
func TestDefaultReadCorpusPassesEachLaneOnItsOwnExpectations(t *testing.T) {
	t.Parallel()

	for _, backend := range []BackendID{BackendNornicDB, BackendNeo4j} {
		answers := map[string][]map[string]any{}
		pinned := 0
		for _, c := range DefaultReadCorpus() {
			if c.WantRows == nil {
				continue
			}
			answers[c.Cypher] = c.WantRows
			if o, ok := c.Overrides[backend]; ok {
				answers[c.Cypher] = o.WantRows
				pinned++
			}
		}
		query := &recordingGraphQuery{rows: []map[string]any{{"ok": true}}, byCypher: answers}
		report, err := RunReadCorpusFor(context.Background(), query, backend, DefaultReadCorpus())
		if err != nil {
			t.Fatalf("RunReadCorpusFor(%s) error = %v", backend, err)
		}
		reported := 0
		for _, r := range report.Results {
			if r.Divergence != "" {
				reported++
			}
		}
		if reported != pinned {
			t.Errorf("%s: %d results report a divergence, want %d", backend, reported, pinned)
		}
	}
}
