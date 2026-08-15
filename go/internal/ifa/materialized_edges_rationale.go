// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// rationaleExpectedEdgesRelPath is the repo-root-relative path to the
// hand-derived expected rationale edge set (#5998).
//
// It lives under this package's testdata/ tree rather than testdata/cassettes/,
// for the same reason the SQL and documentation families do: it is a gate
// ASSERTION file, and the offline cassette validator globs every
// testdata/cassettes/*/*.json as a replay cassette.
const rationaleExpectedEdgesRelPath = "go/internal/ifa/testdata/rationale/ifa-rationale-family-expected-edges.json"

// rationaleFamilyExpectedEdgesPath joins repoRoot onto the fixture path.
func rationaleFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, rationaleExpectedEdgesRelPath)
}

// rationaleExpectedEdge is one hand-derived expected EXPLAINS edge.
//
// RationaleUID carries the full projected node identity, excerpt hash included.
// The hash is sha256(text) truncated to eight bytes, so the fixture's value is
// computed independently from the comment text rather than captured from a run:
// if the shipped hashing changes, this fixture fails, which is the point. A
// fixture recorded from output would instead follow the code wherever it went.
type rationaleExpectedEdge struct {
	RationaleUID   string `json:"rationale_uid"`
	TargetEntityID string `json:"target_entity_id"`
	TargetPath     string `json:"target_path"`
	RepoID         string `json:"repo_id"`
	CommentKind    string `json:"comment_kind"`
}

type rationaleExpectedEdgesFile struct {
	Odu string `json:"odu"`
	// Note is free-form authoring context every committed rationale
	// expected-edge-set fixture carries (see
	// testdata/rationale/ifa-rationale-family-expected-edges.json); modeled
	// here, rather than left to strict-decode rejection, so the
	// DisallowUnknownFields decoder below does not reject that fixture.
	Note  string                  `json:"note,omitempty"`
	Edges []rationaleExpectedEdge `json:"edges"`
}

// loadRationaleExpectedEdges reads and parses the expected-edge fixture. The
// decoder disallows unknown fields so a typo (e.g. "target_pth" instead of
// "target_path") fails loudly at load time instead of silently decoding to a
// zero value -- the same class of gap #5992 review found and fixed for the
// SQL family's loader (loadSQLRelationshipExpectedEdges). json.Decoder.Decode
// reads exactly one JSON value and stops, unlike the json.Unmarshal this
// replaces, so a second Decode requiring io.EOF closes the trailing-content
// gap switching decoders would otherwise reopen.
func loadRationaleExpectedEdges(path string) ([]rationaleExpectedEdge, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return nil, fmt.Errorf("ifa: read rationale expected edges %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var parsed rationaleExpectedEdgesFile
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("ifa: parse rationale expected edges %s: %w", path, err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("ifa: rationale expected edges %s has trailing content after its JSON object", path)
	}
	if len(parsed.Edges) == 0 {
		return nil, fmt.Errorf("ifa: rationale expected edges %s declares no edges; an empty expected set would make the gate vacuous", path)
	}
	return parsed.Edges, nil
}

// rationaleRowsToExpectedEdges projects extractor rows onto the comparable
// identity.
func rationaleRowsToExpectedEdges(rows []map[string]any) []rationaleExpectedEdge {
	out := make([]rationaleExpectedEdge, 0, len(rows))
	for _, row := range rows {
		out = append(out, rationaleExpectedEdge{
			RationaleUID:   anyToStringValue(row["rationale_uid"]),
			TargetEntityID: anyToStringValue(row["target_entity_id"]),
			TargetPath:     anyToStringValue(row["target_path"]),
			RepoID:         anyToStringValue(row["repo_id"]),
			CommentKind:    anyToStringValue(row["comment_kind"]),
		})
	}
	return out
}

func rationaleEdgeKey(e rationaleExpectedEdge) string {
	return strings.Join([]string{e.RationaleUID, e.TargetEntityID, e.TargetPath, e.RepoID, e.CommentKind}, "\x00")
}

func rationaleEdgeLabel(e rationaleExpectedEdge) string {
	return fmt.Sprintf("%s -[EXPLAINS]-> %s (%s, %s)", e.RationaleUID, e.TargetEntityID, e.CommentKind, e.RepoID)
}

// compareRationaleExpectedSets reports the exact-set mismatch in both
// directions.
//
// MISSING means the extractor stopped projecting an edge the graph is supposed
// to carry. EXTRA means it started projecting one nobody derived — the
// direction a "contains all expected" check would miss, and the one that
// silently grows the graph.
func compareRationaleExpectedSets(oduName string, expected, actual []rationaleExpectedEdge) string {
	expectedSet := make(map[string]struct{}, len(expected))
	for _, e := range expected {
		expectedSet[rationaleEdgeKey(e)] = struct{}{}
	}
	actualSet := make(map[string]struct{}, len(actual))
	for _, e := range actual {
		actualSet[rationaleEdgeKey(e)] = struct{}{}
	}

	var missing, extra []string
	for _, e := range expected {
		if _, ok := actualSet[rationaleEdgeKey(e)]; !ok {
			missing = append(missing, rationaleEdgeLabel(e))
		}
	}
	for _, e := range actual {
		if _, ok := expectedSet[rationaleEdgeKey(e)]; !ok {
			extra = append(extra, rationaleEdgeLabel(e))
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) == 0 && len(extra) == 0 {
		if len(actual) != len(expected) {
			return fmt.Sprintf("odù %q: extractor produced %d row(s) for %d distinct expected edge(s); the extractor's own dedup should have collapsed them",
				oduName, len(actual), len(expected))
		}
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "odù %q: rationale edge set does not match %d hand-derived edge(s)", oduName, len(expected))
	if len(missing) > 0 {
		fmt.Fprintf(&b, "; MISSING (the extractor no longer projects these): %s", strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		fmt.Fprintf(&b, "; EXTRA (nobody derived these): %s", strings.Join(extra, ", "))
	}
	return b.String()
}

// resolveRationaleEdgeMaterializedEdges is the rationale_edges family's vacuity
// guard (#5998).
//
// It runs the production extractor over the Odù's facts and asserts the result
// equals the hand-derived set exactly, then checks the repository fan-out the
// extractor reports alongside the rows: an edge set that is right while the
// repo list is wrong would still mis-scope the durable intents built from it.
func resolveRationaleEdgeMaterializedEdges(odu Odu, expectedEdgesPath string) (bool, string) {
	expected, err := loadRationaleExpectedEdges(expectedEdgesPath)
	if err != nil {
		return false, err.Error()
	}

	repoIDs, rows := reducer.ExtractRationaleEdgeRows(odu.Facts)
	actual := rationaleRowsToExpectedEdges(rows)
	if mismatch := compareRationaleExpectedSets(odu.Name, expected, actual); mismatch != "" {
		return false, mismatch
	}

	// Every expected edge names a repo, so the reported repo set must be exactly
	// the distinct repos those edges belong to. A stale or empty list would scope
	// the emitted intents to the wrong repositories while the edges themselves
	// looked correct.
	wantRepos := map[string]struct{}{}
	for _, e := range expected {
		wantRepos[e.RepoID] = struct{}{}
	}
	gotRepos := map[string]struct{}{}
	for _, r := range repoIDs {
		gotRepos[r] = struct{}{}
	}
	if len(gotRepos) != len(wantRepos) {
		return false, fmt.Sprintf("odù %q: extractor reports %d repository/ies for an edge set spanning %d; the durable intents would be scoped to the wrong repositories",
			odu.Name, len(gotRepos), len(wantRepos))
	}
	for repo := range wantRepos {
		if _, ok := gotRepos[repo]; !ok {
			return false, fmt.Sprintf("odù %q: expected edges name repository %q, which the extractor does not report", odu.Name, repo)
		}
	}

	// Deliberately NOT reporting a count of "inputs that derived no edge".
	// len(Facts)-len(rows) would be wrong here: three of this fixture's seven
	// negative cases are comments INSIDE a fact, not facts of their own, so that
	// subtraction undercounts them and would state a number this function never
	// computed. The negative cases are pinned by their own test instead.
	return true, fmt.Sprintf("odù %q: ExtractRationaleEdgeRows reproduces the expected %d-edge set exactly across %d repository/ies, from %d fact envelope(s)",
		odu.Name, len(expected), len(wantRepos), len(odu.Facts))
}
