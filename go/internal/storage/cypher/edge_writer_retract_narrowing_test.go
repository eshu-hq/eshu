// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// unmarkedLegacyRetractRows is the batch the nil-fence path hands RetractEdges
// for a per-edge-only partition: every row unmarked, no delta_projection and no
// refresh intent_type. It is the input both halves of wholeScopeRetractDomains
// are discriminated on -- the narrowed half must bind nothing for it, the
// fenced-but-not-narrowed half must bind the batch-wide list.
func unmarkedLegacyRetractRows() []reducer.SharedProjectionIntentRow {
	return []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "legacy-1",
			RepositoryID: "repo-legacy",
			Payload:      map[string]any{"repo_id": "repo-legacy"},
		},
		{
			IntentID:     "legacy-2",
			RepositoryID: "repo-legacy",
			Payload:      map[string]any{"repo_id": "repo-legacy"},
		},
	}
}

// TestWholeScopeRetractDomainsCoversFencedSet ties this package's table to the
// reducer's fence, which is the set the table is meant to mirror.
//
// The two agreed when the table was written, but they are enumerated in
// different packages and nothing compared them. A domain added to
// domainHasRepoWideRetract (reducer/shared_projection_worker_refresh_fence.go)
// and missed here never reaches narrowedWholeScopeRepoIDs or this table, so none
// of the three guard tests below iterate over it: on an unmarked legacy per-edge
// row it binds the batch-wide repository list to a whole-repository DELETE,
// which is the #6166 over-delete the narrowing exists to prevent, and it does so
// with every test here still green. (A domain added to the fence WITHOUT a
// buildRetractStatement case fails loudly at the builder's default, so the
// silent direction is the one worth a test.)
//
// The comparison runs both ways: a fenced domain missing from the table is that
// over-delete, and a table row the reducer does not fence describes a
// whole-scope retract that never happens.
func TestWholeScopeRetractDomainsCoversFencedSet(t *testing.T) {
	t.Parallel()

	fenced := reducer.RepoWideRetractDomains()
	if len(fenced) == 0 {
		t.Fatal("reducer.RepoWideRetractDomains() is empty; the comparison below would pass having checked nothing")
	}
	fencedSet := make(map[string]struct{}, len(fenced))
	for _, domain := range fenced {
		fencedSet[domain] = struct{}{}
		if _, ok := wholeScopeRetractDomains[domain]; !ok {
			t.Errorf("%s is fenced by the reducer's repo-wide retract but absent from wholeScopeRetractDomains; its RetractEdges path binds the batch-wide repo_ids to a whole-repository DELETE (#6166) and no test in this file iterates over it",
				domain)
		}
	}
	for domain := range wholeScopeRetractDomains {
		if _, ok := fencedSet[domain]; !ok {
			t.Errorf("wholeScopeRetractDomains lists %s, which the reducer does not fence behind a repo refresh intent; the table describes a whole-scope retract that does not exist",
				domain)
		}
	}
}

// TestWholeScopeRetractDomainsHalvesAreNonEmpty floors both halves of the
// table.
//
// Every other assertion in this file loops over one of them, and a `range` over
// an empty slice is not an error -- it passes, silently, having checked
// nothing. That is the exact collapse the Ifá mirrors learned to floor against,
// and the reason this test exists rather than a comment saying the halves are
// populated. The numbers are hand-written at today's counts: adding a domain is
// free, removing one is a deliberate act that must edit this line and say why.
func TestWholeScopeRetractDomainsHalvesAreNonEmpty(t *testing.T) {
	t.Parallel()

	narrowed := wholeScopeNarrowedDomains()
	if len(narrowed) < 4 {
		t.Fatalf("wholeScopeNarrowedDomains() = %v (%d); the narrowed half has collapsed and every loop over it passes vacuously",
			narrowed, len(narrowed))
	}
	unnarrowed := wholeScopeUnnarrowedDomains()
	if len(unnarrowed) < 3 {
		t.Fatalf("wholeScopeUnnarrowedDomains() = %v (%d); the fenced-but-not-narrowed half has collapsed and every loop over it passes vacuously",
			unnarrowed, len(unnarrowed))
	}
	if len(narrowed)+len(unnarrowed) != len(wholeScopeRetractDomains) {
		t.Fatalf("halves sum to %d but the table has %d rows; a row is in neither half",
			len(narrowed)+len(unnarrowed), len(wholeScopeRetractDomains))
	}
	for _, domain := range narrowed {
		if !isWholeScopeNarrowedDomain(domain) {
			t.Errorf("isWholeScopeNarrowedDomain(%q) = false for a domain the narrowed half returned", domain)
		}
	}
	for _, domain := range unnarrowed {
		if isWholeScopeNarrowedDomain(domain) {
			t.Errorf("isWholeScopeNarrowedDomain(%q) = true for a domain the unnarrowed half returned", domain)
		}
	}
}

// TestFencedButNotNarrowedDomainsStillBindBatchWideRepoIDs is the converse of
// the nil-fence test, and it is what makes narrowing a fifth domain impossible
// to do silently.
//
// domainHasRepoWideRetract (reducer/shared_projection_worker_refresh_fence.go)
// fences SEVEN domains; only four of them narrow. The other three -- handles
// route, runs in, invokes cloud action -- fall through to buildRetractStatement
// with the batch-wide collectRepoIDs list, and MUST keep doing so. Anyone who
// re-derives "which domains narrow" from the fencing predicate gets seven and
// concludes the four is wrong; narrowing one of these three on that reasoning
// is the failure this test catches, because the same unmarked batch that the
// narrowed half must skip has to bind a non-empty list here.
func TestFencedButNotNarrowedDomainsStillBindBatchWideRepoIDs(t *testing.T) {
	t.Parallel()

	for _, domain := range wholeScopeUnnarrowedDomains() {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()

			executor := &probeGuardRecordingExecutor{probeFound: true}
			writer := NewEdgeWriter(executor, 0)

			if err := writer.RetractEdges(context.Background(), domain, unmarkedLegacyRetractRows(), "reducer/test"); err != nil {
				t.Fatalf("RetractEdges: %v", err)
			}

			bound := false
			for _, stmt := range executor.executeCalls {
				raw, ok := stmt.Parameters["repo_ids"]
				if !ok {
					continue
				}
				repoIDs, _ := raw.([]string)
				if len(repoIDs) == 0 {
					t.Fatalf("%s bound an EMPTY repo_ids on an unmarked batch (cypher %q); it is in the unnarrowed half of wholeScopeRetractDomains, so it must retract over the batch-wide list -- either the narrowing spread to it, or the table row is wrong",
						domain, stmt.Cypher)
				}
				bound = true
			}
			if !bound {
				t.Fatalf("%s issued no statement binding repo_ids; a fenced-but-not-narrowed domain must still run its whole-repository retract", domain)
			}
		})
	}
}

// TestNarrowedWholeScopeRepoIDsRejectsUnregisteredDomain covers the branch that
// binds the production dispatch to the table.
//
// The four live call sites all pass a registered domain, so this path is
// unreachable from retractFencedRepoWideDomain today -- which is the point: it is the tripwire
// a fifth narrowing branch hits when its author wires the helper but forgets
// the table. It must return an ERROR, not skip. Skipping would be a silent lost
// retract, the one failure mode this whole mechanism exists to make visible.
func TestNarrowedWholeScopeRepoIDsRejectsUnregisteredDomain(t *testing.T) {
	t.Parallel()

	writer := NewEdgeWriter(&probeGuardRecordingExecutor{probeFound: true}, 0)
	repoIDs, skip, err := writer.narrowedWholeScopeRepoIDs(reducer.DomainCodeCalls, unmarkedLegacyRetractRows(), "reducer/test")
	if err == nil {
		t.Fatalf("narrowedWholeScopeRepoIDs(%q) returned no error; an unregistered domain must fail loudly, not skip its retract (repoIDs=%v skip=%v)",
			reducer.DomainCodeCalls, repoIDs, skip)
	}
	if skip {
		t.Errorf("narrowedWholeScopeRepoIDs(%q) returned skip=true alongside its error; a caller that checks skip before err would lose the retract silently",
			reducer.DomainCodeCalls)
	}
	if !strings.Contains(err.Error(), "wholeScopeRetractDomains") {
		t.Errorf("error does not name the table to register in: %v", err)
	}
}

// TestWholeScopeNarrowingHasOneSanctionedCallSite is the guard against a fifth
// narrowing branch that bypasses the helper entirely.
//
// Everything else here is behavioural, but behaviour cannot see a branch that
// hand-rolls `collectWholeScopeRefreshRepoIDs` + `logWholeScopeRetractSkipped`
// for a brand-new domain: such a branch would be consistent with itself and
// invisible to both loops. So this counts non-test call sites in the package
// and names the one deliberate exception.
//
// It reads the files from DISK rather than through a build overlay on purpose:
// an overlay does not reach go/parser, so a mutation applied that way passes
// vacuously. Prove this guard by editing the real file.
func TestWholeScopeNarrowingHasOneSanctionedCallSite(t *testing.T) {
	t.Parallel()

	// collectWholeScopeRefreshRepoIDs has one sanctioned narrowing caller and
	// one named exception; logWholeScopeRetractSkipped has only the former.
	wantCallers := map[string]map[string]int{
		"collectWholeScopeRefreshRepoIDs": {
			"narrowedWholeScopeRepoIDs": 1,
			// #5998 review F6, extended to every narrowed domain by
			// #6216/#6299: each domain's DELTA branch also retracts the
			// full-generation sibling repositories whose refresh rows carry no
			// delta_projection at all, which collectDeltaFilePaths never sees.
			// Those tails narrow for a reason unrelated to the domain dispatch
			// and deliberately log no skip -- an empty list there is the
			// ordinary all-delta batch -- so they call the collector directly.
			// One per narrowed domain.
			"retractFencedRepoWideDomain": 4,
		},
		"logWholeScopeRetractSkipped": {
			"narrowedWholeScopeRepoIDs": 1,
		},
	}

	got := map[string]map[string]int{}
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++
		countNarrowingCallers(file, got)
	}
	// Floor: a glob or suffix filter that stops matching would leave `got`
	// empty and every comparison below trivially satisfied by an empty want.
	if scanned < 50 {
		t.Fatalf("parsed only %d non-test file(s) in this package; the file walk has collapsed and this guard is checking nothing", scanned)
	}

	for callee, want := range wantCallers {
		if diff := len(got[callee]); diff == 0 {
			t.Fatalf("%s has no call sites at all in this package; either it was renamed or the walk above stopped seeing it", callee)
		}
		for caller, wantN := range want {
			if got[callee][caller] != wantN {
				t.Errorf("%s is called %d time(s) from %s, want %d", callee, got[callee][caller], caller, wantN)
			}
		}
		for caller, gotN := range got[callee] {
			if _, ok := want[caller]; !ok {
				t.Errorf("%s is called %d time(s) from UNSANCTIONED caller %s; whole-scope narrowing must route through narrowedWholeScopeRepoIDs so the domain is checked against wholeScopeRetractDomains, or be added to wantCallers here with the reason",
					callee, gotN, caller)
			}
		}
	}
}

// countNarrowingCallers tallies, per callee of interest, how many times each
// enclosing function in file calls it.
func countNarrowingCallers(file *ast.File, got map[string]map[string]int) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var callee string
			switch target := call.Fun.(type) {
			case *ast.Ident:
				callee = target.Name
			case *ast.SelectorExpr:
				callee = target.Sel.Name
			default:
				return true
			}
			if callee != "collectWholeScopeRefreshRepoIDs" && callee != "logWholeScopeRetractSkipped" {
				return true
			}
			if got[callee] == nil {
				got[callee] = map[string]int{}
			}
			got[callee][fn.Name.Name]++
			return true
		})
	}
}
