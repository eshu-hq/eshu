// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"

	statuscollector "github.com/eshu-hq/eshu/go/internal/status/collector"
)

// terminalFailureDocPath is the operator doc whose class table must match the
// code. The path is relative to this package directory.
const terminalFailureDocPath = "../../../../docs/public/reference/http-api/collector-terminal-failure.md"

// failureClassCardinality is the number of distinct non-unknown values the
// failure_class label of eshu_runtime_coordinator_collector_terminal_failure_instances
// can take (the label adds unknown on top). Raising it is a deliberate
// decision: update the docs and the metric description with it.
const failureClassCardinality = 41

// TestBoundedFailureClassesStayBounded pins the label cardinality and the
// shape of every class: the set stays a small closed list of lowercase
// tokens, so the failure_class label can never grow with persisted text.
func TestBoundedFailureClassesStayBounded(t *testing.T) {
	t.Parallel()
	classes := statuscollector.BoundedFailureClasses()
	if len(classes) != failureClassCardinality {
		t.Fatalf("len(BoundedFailureClasses()) = %d, want %d (%v)", len(classes), failureClassCardinality, classes)
	}
	if slices.Contains(classes, statuscollector.UnknownFailureToken) {
		t.Fatalf("BoundedFailureClasses() holds %q, want it reported separately", statuscollector.UnknownFailureToken)
	}
	shape := regexp.MustCompile(`^[a-z][a-z_]{1,47}$`)
	for _, class := range classes {
		if !shape.MatchString(class) {
			t.Errorf("class %q is not a short lowercase snake_case token", class)
		}
	}
	if !slices.IsSorted(classes) || len(slices.Compact(slices.Clone(classes))) != len(classes) {
		t.Fatalf("BoundedFailureClasses() = %v, want sorted and unique", classes)
	}
	// Every kind's own set is inside the union, so no kind reports a class the
	// OpenAPI enum does not publish.
	for _, kind := range append(scope.AllCollectorKinds(), "hosted_vendor") {
		for _, class := range statuscollector.FailureClassesForKind(string(kind)) {
			if !slices.Contains(classes, class) {
				t.Errorf("kind %q reports %q, outside the published set", kind, class)
			}
		}
	}
}

// TestTerminalFailureDocTableMatchesTheCode ties the class table in
// collector-terminal-failure.md to the code: the table lists exactly the
// bounded classes, and each row names the kinds that can report it.
func TestTerminalFailureDocTableMatchesTheCode(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(terminalFailureDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", terminalFailureDocPath, err)
	}
	doc := string(raw)
	if strings.Contains(doc, "#8896") {
		t.Errorf("the doc still carries the #8896 known limit, want it removed")
	}
	rows := map[string]string{}
	for _, line := range strings.Split(doc, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			continue
		}
		rows[strings.Trim(strings.TrimSpace(cells[1]), "`")] = cells[2]
	}
	classes := statuscollector.BoundedFailureClasses()
	got := make([]string, 0, len(rows))
	for class := range rows {
		got = append(got, class)
	}
	slices.Sort(got)
	if !slices.Equal(got, classes) {
		t.Fatalf("doc class table = %v, want %v", got, classes)
	}
	for _, class := range classes {
		cell := rows[class]
		owners := 0
		for _, kind := range scope.AllCollectorKinds() {
			if !slices.Contains(statuscollector.FailureClassesForKind(string(kind)), class) {
				continue
			}
			owners++
			if !strings.Contains(cell, "every kind") && !strings.Contains(cell, "`"+string(kind)+"`") {
				t.Errorf("doc row %q kinds %q does not name %q", class, strings.TrimSpace(cell), kind)
			}
		}
		if slices.Contains(statuscollector.FailureClassesForKind("hosted_vendor"), class) &&
			!strings.Contains(cell, "every kind") && !strings.Contains(cell, "extension") {
			t.Errorf("doc row %q kinds %q does not name the extension host", class, strings.TrimSpace(cell))
		}
		if owners == 0 && !strings.Contains(cell, "extension") {
			t.Errorf("doc row %q has no first-party owner and does not name the extension host", class)
		}
	}
}
