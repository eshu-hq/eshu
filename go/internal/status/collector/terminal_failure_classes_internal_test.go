// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// terminalFailureGaugeName is the gauge whose failure_class label the operator
// docs bound in prose. A doc unit that names it and counts "bounded classes" is
// a statement of the label bound.
const terminalFailureGaugeName = "eshu_runtime_coordinator_collector_terminal_failure_instances"

// failureClassBoundDocs are the operator docs that state the failure_class
// label bound in prose, keyed by path relative to this package directory. The
// test reads each one, so both are lockstep test inputs
// (specs/ci-docs-lockstep-paths.txt): editing either doc alone must still run
// this test. The value marks the doc that also spells out how the bounded
// classes split (shared by every kind, written by the extension host, added by
// first-party kinds), so a doc that states only the totals is not forced to
// restate the split.
var failureClassBoundDocs = map[string]bool{
	"../../../../docs/public/reference/telemetry/metrics.md":                       true,
	"../../../../docs/public/observability/telemetry-coverage-runtime-surfaces.md": false,
}

// kindOwnFailureClassCount counts the distinct classes that first-party kinds
// add beyond the common and extension host classes.
func kindOwnFailureClassCount() int {
	shared := make(map[string]struct{})
	for _, class := range commonFailureClasses {
		shared[class] = struct{}{}
	}
	for _, class := range extensionHostFailureClasses {
		shared[class] = struct{}{}
	}
	own := make(map[string]struct{})
	for _, classes := range kindFailureClasses {
		for _, class := range classes {
			if _, ok := shared[class]; !ok {
				own[class] = struct{}{}
			}
		}
	}
	return len(own)
}

// failureClassBoundUnits returns the prose paragraphs and table rows of doc that
// name the terminal-failure gauge and count its bounded classes. Whitespace is
// collapsed, so a phrase the doc wraps across lines still matches.
func failureClassBoundUnits(doc string) []string {
	var units []string
	var paragraph []string
	flush := func() {
		if len(paragraph) > 0 {
			units = append(units, strings.Join(strings.Fields(strings.Join(paragraph, " ")), " "))
			paragraph = nil
		}
	}
	for _, line := range strings.Split(doc, "\n") {
		switch {
		case strings.TrimSpace(line) == "":
			flush()
		case strings.HasPrefix(line, "|"):
			flush()
			units = append(units, strings.Join(strings.Fields(line), " "))
		default:
			paragraph = append(paragraph, line)
		}
	}
	flush()
	var statements []string
	for _, unit := range units {
		if strings.Contains(unit, terminalFailureGaugeName) && strings.Contains(unit, "bounded classes") {
			statements = append(statements, unit)
		}
	}
	return statements
}

// TestTerminalFailureDocCountsMatchTheCode ties the class counts the operator
// docs state for the failure_class label to the code (eshu-hq/eshu#10746). The
// hand-written counts drifted twice: a doc said the label takes 51 values when
// BoundedFailureClasses() holds 51 and the label adds unknown, and another still
// counted the classes before the last additions. Every figure is derived here,
// so adding a class fails this test until the docs are updated with it.
func TestTerminalFailureDocCountsMatchTheCode(t *testing.T) {
	t.Parallel()
	bounded := len(BoundedFailureClasses())
	own := kindOwnFailureClassCount()
	if got := len(commonFailureClasses) + len(extensionHostFailureClasses) + own + 1; got != bounded {
		t.Fatalf("common %d + extension host %d + first-party %d + extension_defined 1 = %d, want %d: "+
			"the class sets overlap, so the split the docs state cannot add up",
			len(commonFailureClasses), len(extensionHostFailureClasses), own, got, bounded)
	}
	checks := []struct {
		name        string
		pattern     *regexp.Regexp
		want        int
		splitOnly   bool
		explanation string
	}{
		{
			"bounded classes", regexp.MustCompile(`(\d+) bounded classes`), bounded, false,
			"len(BoundedFailureClasses())",
		},
		// The label also takes UnknownFailureToken, which BoundedFailureClasses excludes.
		{
			"label values", regexp.MustCompile(`at most (\d+) values`), bounded + 1, false,
			"len(BoundedFailureClasses()) plus " + UnknownFailureToken,
		},
		{
			"classes every kind shares", regexp.MustCompile(`(\d+) classes every kind shares`),
			len(commonFailureClasses), true, "len(commonFailureClasses)",
		},
		{
			"classes the extension host writes", regexp.MustCompile(`(\d+) the extension host writes`),
			len(extensionHostFailureClasses), true, "len(extensionHostFailureClasses)",
		},
		{
			"classes first-party kinds add", regexp.MustCompile(`(\d+) more that individual first-party kinds add`),
			own, true, "the distinct kindFailureClasses outside the common and host sets",
		},
	}
	for docPath, composition := range failureClassBoundDocs {
		raw, err := os.ReadFile(docPath)
		if err != nil {
			t.Fatalf("read %s: %v", docPath, err)
		}
		units := failureClassBoundUnits(string(raw))
		if len(units) == 0 {
			t.Errorf("%s has no paragraph or table row that names %s and counts its bounded classes; "+
				"restore the statement or update this test", docPath, terminalFailureGaugeName)
			continue
		}
		for _, unit := range units {
			for _, check := range checks {
				if check.splitOnly && !composition {
					continue
				}
				matches := check.pattern.FindAllStringSubmatch(unit, -1)
				if len(matches) == 0 {
					t.Errorf("%s does not state the %s (pattern %s); want %d", docPath, check.name, check.pattern, check.want)
					continue
				}
				for _, match := range matches {
					got, err := strconv.Atoi(match[1])
					if err != nil {
						t.Fatalf("%s: %q is not a count: %v", docPath, match[1], err)
					}
					if got != check.want {
						t.Errorf("%s says %d for the %s, want %d (%s)", docPath, got, check.name, check.want, check.explanation)
					}
				}
			}
		}
	}
}

// TestKindFailureClassesNameFirstPartyKinds pins that every key of the
// per-kind map is a collector kind the platform ships, so a misspelled kind
// cannot leave its classes unreachable while the tests for other kinds pass.
func TestKindFailureClassesNameFirstPartyKinds(t *testing.T) {
	t.Parallel()
	for kind, classes := range kindFailureClasses {
		if _, ok := firstPartyKinds[kind]; !ok {
			t.Errorf("kindFailureClasses key %q is not a first-party collector kind", kind)
		}
		if len(classes) == 0 {
			t.Errorf("kindFailureClasses[%q] is empty, want classes or no entry", kind)
		}
	}
}

// TestExtensionFailureClassPatternMatchesTheHost pins that the shape the status
// leaf uses to collapse an extension-defined class equals the shape the
// extension host enforces. The host package is read as source so the status
// leaf stays free of collector runtime imports in shipped code.
func TestExtensionFailureClassPatternMatchesTheHost(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../../collector/extensionhost/failure.go")
	if err != nil {
		t.Fatalf("read extension host failure.go: %v", err)
	}
	m := regexp.MustCompile("failureClassPattern\\s*=\\s*regexp\\.MustCompile\\(`([^`]+)`\\)").FindSubmatch(src)
	if m == nil {
		t.Fatal("extensionhost failureClassPattern declaration not found; update this test with the host")
	}
	if got, want := extensionFailureClassPattern.String(), string(m[1]); got != want {
		t.Fatalf("status pattern %q differs from the extension host pattern %q", got, want)
	}
}
