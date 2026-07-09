// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
	"github.com/eshu-hq/eshu/go/internal/sourcetool"
)

const sourceToolEdgeProperty = "source_tool"

// narrowedCorrelationSourceToolIssues is the static half of the deferred #4959
// (Ifá P1 T11) source_tool consistency check: for every B-12 narrowed required
// correlation that constrains source_tool, the tokens the reducer would derive
// from that rc's evidence kinds (sourceToolForEvidenceKind, the same write-time
// derivation cross_repo_intent_row.go stamps on edges) must all be canonical
// and must all fall inside the rc's allowed source_tool vocabulary.
//
// It is intentionally the STATIC half only. A correlation with no evidence_kinds
// filter is skipped because which evidence facts a materialized edge aggregates
// — and therefore which becomes the primary source_tool — is not decidable
// without running the resolver against a graph. That dynamic half stays the
// golden-corpus gate's job (goldengate.EvaluateEdgeProperty over the real graph)
// and Ifá's post-materialization phases; the Ifá contract layer deliberately
// only asserts the evidence-kind half (see go/internal/ifa/evidence.go).
func narrowedCorrelationSourceToolIssues(rcs []goldengate.RequiredCorrelation) []string {
	var issues []string
	for _, rc := range rcs {
		if len(rc.EvidenceKinds) == 0 {
			continue
		}
		allowed, hasPin := rc.AllowedEdgePropertyValues[sourceToolEdgeProperty]
		constrained := hasPin
		for _, prop := range rc.RequiredEdgeProperties {
			if prop == sourceToolEdgeProperty {
				constrained = true
			}
		}
		if !constrained {
			continue
		}

		allowedSet := make(map[string]struct{}, len(allowed))
		for _, token := range allowed {
			if !sourcetool.IsValid(token) {
				issues = append(issues, fmt.Sprintf("rc %s: allowed source_tool %q is not in sourcetool.Canonical", rc.ID, token))
			}
			allowedSet[token] = struct{}{}
		}

		for _, kind := range rc.EvidenceKinds {
			derived := sourceToolForEvidenceKind(kind)
			if derived == "" {
				issues = append(issues, fmt.Sprintf("rc %s: evidence kind %q derives no source_tool, but the rc pins source_tool", rc.ID, kind))
				continue
			}
			if hasPin {
				if _, ok := allowedSet[derived]; !ok {
					issues = append(issues, fmt.Sprintf("rc %s: evidence kind %q derives source_tool %q, not in the rc's allowed set %v", rc.ID, kind, derived, allowed))
				}
			}
		}
	}
	return issues
}

// TestNarrowedCorrelationSourceToolConsistencyTeeth proves the helper is not
// vacuous: an rc whose evidence kind derives kustomize but whose allowed
// source_tool set names only helm must be reported, naming the rc, the kind, the
// derived token, and the allowed set.
func TestNarrowedCorrelationSourceToolConsistencyTeeth(t *testing.T) {
	t.Parallel()

	inconsistent := goldengate.RequiredCorrelation{
		ID:                        "rc-teeth",
		EvidenceKinds:             []string{"KUSTOMIZE_RESOURCE_REFERENCE"},
		RequiredEdgeProperties:    []string{sourceToolEdgeProperty},
		AllowedEdgePropertyValues: map[string][]string{sourceToolEdgeProperty: {"helm"}},
	}
	issues := narrowedCorrelationSourceToolIssues([]goldengate.RequiredCorrelation{inconsistent})
	if len(issues) == 0 {
		t.Fatal("narrowedCorrelationSourceToolIssues found no issue for an rc whose evidence kind derives a source_tool outside its allowed set")
	}
	if got := issues[0]; !containsAll(got, "rc-teeth", "KUSTOMIZE_RESOURCE_REFERENCE", "kustomize", "helm") {
		t.Errorf("issue = %q, want it to name the rc, kind, derived token, and allowed set", got)
	}
}

// TestSnapshotNarrowedCorrelationSourceToolConsistency locks the real B-12
// snapshot: every narrowed correlation that pins source_tool must pin exactly
// the token(s) its evidence kinds derive to. A hand-edit that made an rc's
// evidence_kinds and allowed source_tool inconsistent would otherwise fail only
// in the Docker-heavy live golden-corpus gate; this catches it statically.
func TestSnapshotNarrowedCorrelationSourceToolConsistency(t *testing.T) {
	t.Parallel()

	snap, err := goldengate.LoadSnapshot(snapshotPath(t))
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if issues := narrowedCorrelationSourceToolIssues(snap.Graph.RequiredCorrelations); len(issues) != 0 {
		t.Fatalf("snapshot narrowed-correlation source_tool inconsistencies (%d):\n%s", len(issues), joinLines(issues))
	}
}

func snapshotPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// This file lives at <repoRoot>/go/internal/reducer/.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(repoRoot, "testdata", "golden", "e2e-20repo-snapshot.json")
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += "  " + l + "\n"
	}
	return out
}
