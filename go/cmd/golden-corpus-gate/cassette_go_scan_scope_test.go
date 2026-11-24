// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The Go scan facts a cassette records must sit in a go-reachability scope, the
// way the scanner worker writes them. A supply-chain pass reads Go module
// evidence, call reachability and the go_reachability warning only through the
// cross-scope Go stage, which reads scopes named go-reachability://repository:<id>
// (storage/postgres/active_go_reachability.go). A cassette that records them in
// the vulnerability-intelligence scope drives a shape production never has, so
// the golden check for CVE-2026-9401 read module_only while the join was fine
// (#10030).

const (
	goReachabilityScopePrefix = "go-reachability://"
	goReachabilityAnalyzer    = "go_reachability"
)

var goScopeRepositoryPattern = regexp.MustCompile(`^repository:r_[0-9a-f]{8}$`)

// goScanScopeDoc is the slice of a cassette document the guard reads.
type goScanScopeDoc struct {
	Scopes []struct {
		ScopeID string `json:"scope_id"`
		Facts   []struct {
			FactKind string         `json:"fact_kind"`
			Payload  map[string]any `json:"payload"`
		} `json:"facts"`
	} `json:"scopes"`
}

// goScanFactKind reports whether a cassette fact is Go scan evidence: module
// evidence, call reachability, or a warning from the go_reachability analyzer.
// The warning kind is shared with image analyzers, which belong to their own
// scopes, so only the go_reachability analyzer counts.
func goScanFactKind(kind string, payload map[string]any) bool {
	switch kind {
	case "vulnerability.go_module_evidence", "vulnerability.go_call_reachability":
		return true
	case "scanner_worker.warning":
		analyzer, _ := payload["analyzer"].(string)
		return analyzer == goReachabilityAnalyzer
	default:
		return false
	}
}

// goScanScopeViolations lists every Go scan fact that sits outside a
// go-reachability scope with a canonical repository id.
func goScanScopeViolations(doc goScanScopeDoc) []string {
	var violations []string
	for _, scope := range doc.Scopes {
		repository, inGoScope := strings.CutPrefix(scope.ScopeID, goReachabilityScopePrefix)
		inGoScope = inGoScope && goScopeRepositoryPattern.MatchString(repository)
		for _, fact := range scope.Facts {
			if goScanFactKind(fact.FactKind, fact.Payload) && !inGoScope {
				violations = append(violations, fact.FactKind+" in scope "+scope.ScopeID)
			}
		}
	}
	return violations
}

// TestGoScanScopeGuardFlagsASeededViolation proves the guard is not vacuous: a
// module-evidence row and a go_reachability warning in a vulnerability scope are
// flagged, an image-analyzer warning is not Go scan evidence, and the same facts
// in a canonical go-reachability scope pass.
func TestGoScanScopeGuardFlagsASeededViolation(t *testing.T) {
	t.Parallel()

	build := func(scopeID string) goScanScopeDoc {
		var doc goScanScopeDoc
		raw := `{"scopes":[{"scope_id":"` + scopeID + `","facts":[
			{"fact_kind":"vulnerability.go_module_evidence","payload":{}},
			{"fact_kind":"scanner_worker.warning","payload":{"analyzer":"go_reachability"}},
			{"fact_kind":"scanner_worker.warning","payload":{"analyzer":"trivy"}},
			{"fact_kind":"vulnerability.cve","payload":{}}]}]}`
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	if got := goScanScopeViolations(build("vulnerability_intelligence:orders-api:go-alias")); len(got) != 2 {
		t.Fatalf("violations in a vulnerability scope = %v, want the module evidence and the go_reachability warning", got)
	}
	if got := goScanScopeViolations(build("go-reachability://repository:r_ea78e8bb")); len(got) != 0 {
		t.Fatalf("violations in a canonical go-reachability scope = %v, want none", got)
	}
	if got := goScanScopeViolations(build("go-reachability://https://example.test/repo")); len(got) != 2 {
		t.Fatalf("violations in a non-canonical go-reachability scope = %v, want 2", got)
	}
}

// TestCassettesHoldGoScanFactsInGoReachabilityScopes walks every committed
// cassette and fails on a Go scan fact outside a go-reachability scope.
func TestCassettesHoldGoScanFactsInGoReachabilityScopes(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..", "testdata", "cassettes")
	checked := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var doc goScanScopeDoc
		if json.Unmarshal(raw, &doc) != nil || len(doc.Scopes) == 0 {
			return nil
		}
		checked++
		for _, violation := range goScanScopeViolations(doc) {
			t.Errorf("%s: Go scan fact outside a go-reachability scope: %s", path, violation)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk cassettes: %v", err)
	}
	if checked == 0 {
		t.Fatal("no cassette with scopes found: the guard checked nothing")
	}
}
