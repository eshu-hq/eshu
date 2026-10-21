// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestRetirementRetractPlanClassifiesEveryDomain is the closed-table
// firewall: it iterates the reducer domain registry in both directions,
// so a domain added later fails here until someone classifies its 2c
// retraction. It is the same shape as
// TestWholeScopeRetractDomainsCoversFencedSet: the silent direction (a
// domain the runner would skip) is the one worth a test.
func TestRetirementRetractPlanClassifiesEveryDomain(t *testing.T) {
	t.Parallel()

	union := make(map[reducercontract.Domain]struct{})
	for _, domain := range reducercontract.KnownDomains() {
		union[domain] = struct{}{}
	}
	for _, domain := range reducercontract.ProjectionDomains() {
		union[domain] = struct{}{}
	}
	// DomainSearchVectorBuild is a real projection-work domain with its own
	// runner (searchvector/) that neither registry lists: KnownDomains
	// omits it and ProjectionDomains omits it despite the "complete set"
	// contract. The retirement table classifies it anyway; adding it to a
	// registry is a capability-surface change left for a follow-up (P2
	// candidate), not this lane.
	union[reducercontract.DomainSearchVectorBuild] = struct{}{}
	if len(union) == 0 {
		t.Fatal("domain union is empty; the comparison below would pass having checked nothing")
	}
	for domain := range union {
		plan, ok := RetirementDomainPlanFor(domain)
		if !ok {
			t.Errorf("domain %q has no retirement plan; classify it as retract_by_repo, removed_by_node_retract, or not_repository_scoped with a reason", domain)
			continue
		}
		switch plan.Class {
		case RetireDomainRetractByRepo, RetireDomainRemovedByNodeRetract, RetireDomainNotRepositoryScoped:
		default:
			t.Errorf("domain %q has unknown class %q", domain, plan.Class)
		}
		if strings.TrimSpace(plan.Reason) == "" {
			t.Errorf("domain %q has no reason; a classification without its source check is a guess", domain)
		}
	}
	for domain := range retirementDomainPlans {
		if _, ok := union[domain]; !ok {
			t.Errorf("retirement plan lists %q, which is no known reducer domain; the row describes a retraction that does not exist", domain)
		}
	}
}

// TestRetirementSharedWorkerLeaseDomainsMatchWorker ties the shared
// worker's configured domain list to both tables. The worker claims its
// lease with cfg.Domain (a variable, never a literal), so its domains ride
// the shared range instead of the literal lease table — but every one of
// them must still have a retract_by_repo domain plan: a pre-barrier lease
// in one of these domains holds 2b' exactly because repo-keyed work is
// still in flight for it. The literal table keeps only the dedicated
// runners (code_calls, repo_dependency) and the maintenance skips.
func TestRetirementSharedWorkerLeaseDomainsMatchWorker(t *testing.T) {
	t.Parallel()

	domains := RetirementSharedWorkerLeaseDomains()
	if len(domains) == 0 {
		t.Fatal("shared worker domain list is empty; the comparison below would pass having checked nothing")
	}
	for _, domain := range domains {
		plan, ok := RetirementDomainPlanFor(reducercontract.Domain(domain))
		if !ok {
			t.Errorf("shared worker domain %q has no retirement domain plan", domain)
			continue
		}
		if plan.Class != RetireDomainRetractByRepo {
			t.Errorf("shared worker domain %q is %q, want retract_by_repo: its pre-barrier leases hold 2b' for repo-keyed work", domain, plan.Class)
		}
	}
	for _, domain := range []reducercontract.Domain{reducercontract.DomainCodeCalls, reducercontract.DomainRepoDependency} {
		plan, ok := RetirementDomainPlanFor(domain)
		if !ok || plan.Class != RetireDomainRetractByRepo {
			t.Errorf("dedicated lease domain %q must be retract_by_repo, got %+v", domain, plan)
		}
		entry, ok := RetirementLeaseDomainFor(string(domain))
		if !ok || entry.Class != RetirementLeaseRepoGraphWriter {
			t.Errorf("dedicated lease domain %q must be repo_graph_writer in the lease table, got %+v", domain, entry)
		}
	}
}

// retirementLeaseClaimRoots are the trees a new partition-lease claim site
// can hide in. The writer test walks them in full: a ClaimPartitionLease
// call in any other tree fails closed here until the roots grow with a
// reason.
func retirementLeaseClaimRoots(t *testing.T) []string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller cannot locate this test file")
	}
	// .../go/internal/reducer/retirement/plan_test.go -> .../go.
	goRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	return []string{
		filepath.Join(goRoot, "internal", "reducer"),
		filepath.Join(goRoot, "cmd"),
	}
}

// TestRetirementLeaseDomainsMatchLeaseWriters enumerates every non-test
// caller of ClaimPartitionLease from source, resolves the
// projection_domain argument, and requires each value to be classified in
// the lease table (or in the shared worker range). A new lease writer
// fails here until someone classifies it as repo_graph_writer or
// maintenance_lease with a reason.
func TestRetirementLeaseDomainsMatchLeaseWriters(t *testing.T) {
	t.Parallel()

	type claimSite struct {
		file   string
		line   int
		domain string
		shared bool
	}
	var sites []claimSite
	for _, root := range retirementLeaseClaimRoots(t) {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			consts := packageStringConsts(t, filepath.Dir(path))
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			ast.Inspect(parsed, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) < 2 {
					return true
				}
				name := callFunName(call.Fun)
				if name != "ClaimPartitionLease" {
					return true
				}
				position := fset.Position(call.Pos())
				domain, shared := resolveLeaseDomainArg(call.Args[1], consts)
				sites = append(sites, claimSite{file: path, line: position.Line, domain: domain, shared: shared})
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(sites) == 0 {
		t.Fatal("no ClaimPartitionLease call sites found; the comparison below would pass having checked nothing")
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].file != sites[j].file {
			return sites[i].file < sites[j].file
		}
		return sites[i].line < sites[j].line
	})
	shared := make(map[string]struct{})
	for _, domain := range RetirementSharedWorkerLeaseDomains() {
		shared[domain] = struct{}{}
	}
	reducerRoot := retirementLeaseClaimRoots(t)[0]
	for _, site := range sites {
		rel, _ := filepath.Rel(reducerRoot, site.file)
		if rel == "" {
			rel = site.file
		}
		label := rel + ":" + strconv.Itoa(site.line)
		if site.shared {
			continue
		}
		if site.domain == "" {
			t.Errorf("%s passes an unresolvable lease domain; resolve it to a literal or register its derivation", label)
			continue
		}
		if _, ok := shared[site.domain]; ok {
			t.Errorf("%s passes shared-worker domain %q as a literal; the worker range already covers it", label, site.domain)
			continue
		}
		entry, ok := RetirementLeaseDomainFor(site.domain)
		if !ok {
			t.Errorf("%s claims a lease for unclassified domain %q; classify it as repo_graph_writer or maintenance_lease", label, site.domain)
			continue
		}
		if strings.TrimSpace(entry.Reason) == "" {
			t.Errorf("lease domain %q has no reason", site.domain)
		}
	}
}

// callFunName returns the function name of a call expression: either a
// plain identifier or the selector of a package or receiver value.
func callFunName(fun ast.Expr) string {
	switch fn := fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	default:
		return ""
	}
}

// resolveLeaseDomainArg resolves the projection_domain argument (the
// second ClaimPartitionLease parameter) to its value. cfg.Domain is the
// shared worker's configured domain and resolves as shared range; a
// string literal resolves to itself; any other identifier or selector
// resolves through the calling package's string constants, following one
// alias hop into the reducer contract package (package reducer aliases
// several domain constants). Anything else resolves empty so the test
// fails closed on an unrecognized derivation.
func resolveLeaseDomainArg(arg ast.Expr, consts map[string]string) (string, bool) {
	switch expr := arg.(type) {
	case *ast.BasicLit:
		if expr.Kind == token.STRING {
			return strings.Trim(expr.Value, `"`), false
		}
	case *ast.SelectorExpr:
		if ident, ok := expr.X.(*ast.Ident); ok && ident.Name == "cfg" && expr.Sel.Name == "Domain" {
			return "", true
		}
		if value, ok := consts[expr.Sel.Name]; ok {
			return value, false
		}
		if value, ok := contractStringConsts()[expr.Sel.Name]; ok {
			return value, false
		}
		if ident, ok := expr.X.(*ast.Ident); ok {
			if value, ok := consts[ident.Name+"."+expr.Sel.Name]; ok {
				return value, false
			}
		}
	case *ast.Ident:
		if value, ok := consts[expr.Name]; ok {
			return value, false
		}
		// A package-level alias (Name = reducercontract.Other) resolves
		// through the contract table below.
		if value, ok := contractStringConsts()[expr.Name]; ok {
			return value, false
		}
	}
	return "", false
}

// contractConstsOnce guards the one parse of the reducer contract package
// the lease-domain resolver needs for selector and alias resolution.
var contractConstsOnce sync.Once

// contractConstsCache holds DomainX = "value" pairs from the contract
// package, keyed by Go identifier.
var contractConstsCache map[string]string

// contractStringConsts returns the reducer contract package's string
// constants, parsed once. Test failures here name the parse error; the
// lease-domain assertions never run on a half-read table.
func contractStringConsts() map[string]string {
	contractConstsOnce.Do(func() {
		contractConstsCache = make(map[string]string)
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			return
		}
		dir := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "contract"))
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if err != nil {
				continue
			}
			for _, decl := range parsed.Decls {
				generic, ok := decl.(*ast.GenDecl)
				if !ok || generic.Tok != token.CONST {
					continue
				}
				for _, spec := range generic.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
						continue
					}
					literal, ok := value.Values[0].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					contractConstsCache[value.Names[0].Name] = strings.Trim(literal.Value, `"`)
				}
			}
		}
	})
	return contractConstsCache
}

// packageStringConsts collects the package's string constants (Name =
// "value", including package-qualified keys for selector resolution) from
// every non-test file in the directory.
func packageStringConsts(t *testing.T, dir string) map[string]string {
	t.Helper()
	consts := make(map[string]string)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", filepath.Join(dir, name), err)
		}
		for _, decl := range parsed.Decls {
			generic, ok := decl.(*ast.GenDecl)
			if !ok || generic.Tok != token.CONST {
				continue
			}
			for _, spec := range generic.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
					continue
				}
				literal, ok := value.Values[0].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				consts[value.Names[0].Name] = strings.Trim(literal.Value, `"`)
			}
		}
	}
	return consts
}
