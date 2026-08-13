// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// populatedGraphMaterialization returns a second-generation materialization
// shaped like the repositories that dead-lettered in the #4207 full-corpus run:
// a small repository (15 files, nested directories) re-projected into a graph
// that already holds every other repository's File and Directory population.
//
// The retract statements this builds are the ones that timed out. Their cost
// does not come from this repository's own size — a 57-fact repository blew the
// same two-minute budget as a 59,717-fact one — it comes from the graph the
// statements scan.
func populatedGraphMaterialization() projector.CanonicalMaterialization {
	files := make([]projector.FileRow, 0, 15)
	directories := []projector.DirectoryRow{
		{Path: "/corpus/my-repo/src", ParentPath: "/corpus/my-repo", Depth: 1},
		{Path: "/corpus/my-repo/src/api", ParentPath: "/corpus/my-repo/src", Depth: 2},
	}
	for i := 0; i < 15; i++ {
		files = append(files, projector.FileRow{
			Path:    "/corpus/my-repo/src/api/handler" + string(rune('a'+i)) + ".go",
			DirPath: "/corpus/my-repo/src/api",
			RepoID:  "repository:r_0a682efa",
		})
	}
	return projector.CanonicalMaterialization{
		RepoID:          "repository:r_0a682efa",
		RepoPath:        "/corpus/my-repo",
		GenerationID:    "gen-2",
		FirstGeneration: false,
		Files:           files,
		Directories:     directories,
		Entities: []projector.EntityRow{
			{EntityID: "fn-1", Label: "Function", EntityName: "Serve", FilePath: files[0].Path, StartLine: 10},
		},
	}
}

// TestCanonicalRetractStatementsAnchorTraversalsOnIndexedSide is the #4207
// regression. Every retract statement must expand from the side the statement
// can seek by index. A statement that expands into its own anchor from an
// unbound labelled node scans that label's whole population per input row, so
// its cost tracks the corpus rather than the work item — the signature of the
// eleven dead-lettered projector items in the full-corpus run.
func TestCanonicalRetractStatementsAnchorTraversalsOnIndexedSide(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := populatedGraphMaterialization()

	for _, tt := range []struct {
		name       string
		statements []Statement
	}{
		{name: "full refresh", statements: writer.buildRetractStatements(mat)},
		{name: "entity", statements: writer.buildEntityRetractStatements(mat)},
		{name: "delta", statements: deltaRetractStatements(writer, mat)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.statements) == 0 {
				t.Fatal("no retract statements built; the regression would not be exercised")
			}
			for _, stmt := range tt.statements {
				for _, finding := range FindUnanchoredRetractTraversals(stmt.Cypher) {
					t.Errorf(
						"retract statement expands into its indexed anchor %q from an unbound label: %s\nfull statement:\n%s",
						finding.Anchor, finding.Pattern, stmt.Cypher,
					)
				}
			}
		})
	}
}

// deltaRetractStatements exercises the delta branch of the same production
// builder, which the second generation of an incremental repository takes.
func deltaRetractStatements(writer *CanonicalNodeWriter, mat projector.CanonicalMaterialization) []Statement {
	delta := mat
	delta.DeltaProjection = true
	delta.DeltaFilePaths = []string{mat.Files[0].Path}
	delta.DeltaDeletedFilePaths = []string{"/corpus/my-repo/src/api/removed.go"}
	delta.DeltaDeletedDirectoryPaths = []string{"/corpus/my-repo/src/legacy"}
	return writer.buildRetractStatements(delta)
}

// TestFindUnanchoredRetractTraversalsFlagsTheRegressedShape proves the rule the
// test above depends on actually fires. It runs the pre-fix production text
// through the same helper, so removing the rule's teeth breaks this test too.
func TestFindUnanchoredRetractTraversalsFlagsTheRegressedShape(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		cypher      string
		wantAnchor  string
		wantPattern string
	}{
		{
			name: "directory contains file, pre-fix",
			cypher: `UNWIND $file_paths AS file_path
MATCH (f:File {path: file_path})
MATCH (:Directory)-[r:CONTAINS]->(f)
DELETE r`,
			wantAnchor:  "f",
			wantPattern: "(:Directory)-[r:CONTAINS]->(f)",
		},
		{
			name: "directory parent edges, pre-fix",
			cypher: `UNWIND $rows AS row
MATCH (p:Directory)-[r:CONTAINS]->(d:Directory {path: row.path})
WHERE d.repo_id = $repo_id
DELETE r`,
			wantAnchor:  "d",
			wantPattern: "(p:Directory)-[r:CONTAINS]->(d:Directory {path: row.path})",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			findings := FindUnanchoredRetractTraversals(tt.cypher)
			if len(findings) != 1 {
				t.Fatalf("FindUnanchoredRetractTraversals() = %d findings, want 1: %+v", len(findings), findings)
			}
			if findings[0].Anchor != tt.wantAnchor {
				t.Errorf("Anchor = %q, want %q", findings[0].Anchor, tt.wantAnchor)
			}
			if findings[0].Pattern != tt.wantPattern {
				t.Errorf("Pattern = %q, want %q", findings[0].Pattern, tt.wantPattern)
			}
		})
	}
}

// TestFindUnanchoredRetractTraversalsAcceptsAnchoredShapes keeps the rule from
// flagging the statements that already read from their indexed side, including
// the fixed forms.
func TestFindUnanchoredRetractTraversalsAcceptsAnchoredShapes(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		cypher string
	}{
		{
			name: "outbound from anchored file",
			cypher: `UNWIND $file_paths AS file_path
MATCH (f:File {path: file_path})
MATCH (f)-[r:IMPORTS]->(:Module)
DELETE r`,
		},
		{
			name: "inbound written from the anchored side",
			cypher: `UNWIND $file_paths AS file_path
MATCH (f:File {path: file_path})
MATCH (f)<-[r:CONTAINS]-(:Directory)
DELETE r`,
		},
		{
			name: "anchored repository expands outward",
			cypher: `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
WHERE f.repo_id = $repo_id
DETACH DELETE f`,
		},
		{
			name: "merge patterns are not scanned",
			cypher: `UNWIND $rows AS row
MATCH (d:Directory {path: row.dir_path})
MERGE (d)-[rel:CONTAINS]->(f)`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if findings := FindUnanchoredRetractTraversals(tt.cypher); len(findings) != 0 {
				t.Fatalf("FindUnanchoredRetractTraversals() = %+v, want none", findings)
			}
		})
	}
}

// TestCanonicalRetractStatementsCoverTheTimedOutStatements pins the two
// statements the #4207 run actually failed on, so a future refactor that drops
// them from the retract phase does not quietly make the regression above
// vacuous.
func TestCanonicalRetractStatementsCoverTheTimedOutStatements(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	statements := writer.buildRetractStatements(populatedGraphMaterialization())

	var sawDirectoryFileEdges, sawDirectoryParentEdges bool
	for _, stmt := range statements {
		if strings.Contains(stmt.Cypher, "CONTAINS") && strings.Contains(stmt.Cypher, "$file_paths") {
			sawDirectoryFileEdges = true
		}
		if strings.Contains(stmt.Cypher, "row.parent_path") {
			sawDirectoryParentEdges = true
		}
	}
	if !sawDirectoryFileEdges {
		t.Error("retract phase no longer refreshes Directory CONTAINS File edges")
	}
	if !sawDirectoryParentEdges {
		t.Error("retract phase no longer refreshes Directory parent edges")
	}
}

// --- The anchor-direction rule the tests above assert with ---
//
// The rule lives here rather than in a production file because nothing in the
// production build calls it: it is the regression's assertion, not part of the
// canonical write path.

// UnanchoredRetractTraversal names one relationship pattern in a retract or
// refresh statement whose selective, index-backed side sits on the RIGHT of the
// arrow while an unbound labelled node sits on the LEFT.
//
// Direction matters on NornicDB. A pattern written as
// `(:Directory)-[r:CONTAINS]->(f)` puts the unbound `(:Directory)` first, and
// the executor cold-plans it as a full Directory label scan with per-directory
// CONTAINS fanout re-joined to `f`. That is work proportional to the whole
// graph's Directory population on every input row, no matter how few rows the
// statement carries — which is why a 25-path retract chunk that took
// milliseconds against an empty graph exceeded a two-minute budget once the
// corpus was populated (issue #4207). Writing the same relationship anchored on
// the bound variable, `(f)<-[r:CONTAINS]-(:Directory)`, walks `f`'s own
// adjacency and matches the identical edges. The same rewrite was applied to
// the workload catalog queries for issues #3466 and #1731.
type UnanchoredRetractTraversal struct {
	// Anchor is the index-backed variable stranded on the right of the arrow.
	Anchor string
	// Pattern is the offending relationship pattern text.
	Pattern string
}

var (
	// matchClausePattern isolates MATCH / OPTIONAL MATCH clauses. MERGE and
	// CREATE patterns are excluded: their endpoints are already bound by a
	// preceding MATCH, so arrow order carries no scan cost there.
	matchClausePattern = regexp.MustCompile(`(?m)^\s*(?:OPTIONAL\s+)?MATCH\s+(.*)$`)

	// propertyAnchoredNodePattern matches a node that pins a property map and
	// binds a variable, e.g. `(f:File {path: file_path})`.
	propertyAnchoredNodePattern = regexp.MustCompile(`\((\w+):\w+\s*\{[^}]*\}\)`)

	// relationshipPattern splits `(left)-[rel:TYPE]->(right)` into its two node
	// texts. Only the left-to-right arrow form can strand an anchor on the
	// right; `<-` and undirected forms already read from the left-hand node.
	relationshipPattern = regexp.MustCompile(`(\([^()]*\))-\[[^\]]*\]->(\([^()]*\))`)

	// nodeVariablePattern extracts the binding variable from a node pattern.
	nodeVariablePattern = regexp.MustCompile(`^\((\w+)`)
)

// FindUnanchoredRetractTraversals reports every MATCH relationship pattern in
// cypher that expands left-to-right from an unbound labelled node into a node
// the statement anchors on an indexed property.
//
// Both the positive assertion over the production retract statements and the
// negative assertion over a known-bad statement call this function, so the
// regression test cannot pass against a re-implementation of the rule.
func FindUnanchoredRetractTraversals(cypher string) []UnanchoredRetractTraversal {
	anchored := make(map[string]struct{})
	for _, match := range propertyAnchoredNodePattern.FindAllStringSubmatch(cypher, -1) {
		anchored[match[1]] = struct{}{}
	}

	var findings []UnanchoredRetractTraversal
	for _, clause := range matchClausePattern.FindAllStringSubmatch(cypher, -1) {
		for _, rel := range relationshipPattern.FindAllStringSubmatch(clause[1], -1) {
			left, right := rel[1], rel[2]
			if nodeIsAnchored(left, anchored) {
				continue
			}
			if !nodeIsAnchored(right, anchored) {
				continue
			}
			findings = append(findings, UnanchoredRetractTraversal{
				Anchor:  nodeVariable(right),
				Pattern: strings.TrimSpace(rel[0]),
			})
		}
	}
	return findings
}

// nodeIsAnchored reports whether a node pattern can seed an index lookup:
// either it pins a property map inline, or it binds a variable that some other
// clause in the same statement pinned.
func nodeIsAnchored(node string, anchored map[string]struct{}) bool {
	if strings.Contains(node, "{") {
		return true
	}
	variable := nodeVariable(node)
	if variable == "" {
		return false
	}
	_, ok := anchored[variable]
	return ok
}

func nodeVariable(node string) string {
	match := nodeVariablePattern.FindStringSubmatch(node)
	if match == nil {
		return ""
	}
	return match[1]
}
