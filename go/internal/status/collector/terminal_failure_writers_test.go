// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/vulnsource"

	statuscollector "github.com/eshu-hq/eshu/go/internal/status/collector"
)

// writerConst is one failure-class constant a collector writer declares. value
// is the string literal in the source, or empty when the constant is built from
// an expression (for example string(sdk.FailureDeleted)) that the AST cannot
// evaluate; the test then resolves it from the imported constant of the same
// name.
type writerConst struct {
	name  string
	value string
}

// internalDir is the go/internal directory, relative to this package.
const internalDir = "../.."

// parseNonTestFiles parses the non-test Go files of the package at relDir
// (relative to go/internal). Build tags are ignored on purpose: a class
// declared behind a tag is still a class the writer can persist.
func parseNonTestFiles(t *testing.T, relDir string) []*ast.File {
	t.Helper()
	dir := filepath.Join(internalDir, relDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", filepath.Join(dir, name), err)
		}
		files = append(files, file)
	}
	return files
}

// writerConsts returns every constant in the package at relDir that match
// accepts. It reads the writer's own declarations, so a class added to the
// writer shows up here without anyone editing this test, and a renamed literal
// shows up with its new value.
func writerConsts(t *testing.T, relDir string, match func(name string, typeName string) bool) []writerConst {
	t.Helper()
	var out []writerConst
	for _, file := range parseNonTestFiles(t, relDir) {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				typeName := ""
				if id, ok := vs.Type.(*ast.Ident); ok {
					typeName = id.Name
				}
				for i, id := range vs.Names {
					if !match(id.Name, typeName) {
						continue
					}
					c := writerConst{name: id.Name}
					if i < len(vs.Values) {
						if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							c.value, _ = strconv.Unquote(lit.Value)
						}
					}
					out = append(out, c)
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no failure-class constants matched in %s, want the writer's declarations", relDir)
	}
	return out
}

// declared returns the sorted class values of the matching constants in relDir.
// A constant built from an expression must have an entry in resolver, and every
// resolver entry must still name a declared constant, so an added or removed
// writer constant fails instead of passing unseen.
func declared(t *testing.T, relDir string, match func(string, string) bool, resolver map[string]string) []string {
	t.Helper()
	seen := map[string]bool{}
	var out []string
	for _, c := range writerConsts(t, relDir, match) {
		seen[c.name] = true
		value := c.value
		if value == "" {
			var ok bool
			if value, ok = resolver[c.name]; !ok {
				t.Errorf("%s constant %s has no literal value and no resolver entry; register it", relDir, c.name)
				continue
			}
		}
		out = append(out, value)
	}
	for name := range resolver {
		if !seen[name] {
			t.Errorf("resolver names %s, which %s no longer declares", name, relDir)
		}
	}
	return sortedUnique(out)
}

func sortedUnique(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}

// minusClasses returns the sorted values of a that are in none of the others.
func minusClasses(a []string, others ...[]string) []string {
	var out []string
	for _, v := range a {
		if !slices.ContainsFunc(others, func(o []string) bool { return slices.Contains(o, v) }) {
			out = append(out, v)
		}
	}
	return sortedUnique(out)
}

func namePrefix(prefix string) func(string, string) bool {
	return func(name string, _ string) bool { return strings.HasPrefix(name, prefix) }
}

func typedAs(typeName string) func(string, string) bool {
	return func(_ string, got string) bool { return got == typeName }
}

// methodCount counts methods named name declared in the non-test files of the
// package at relDir.
func methodCount(t *testing.T, relDir string, name string) int {
	t.Helper()
	count := 0
	for _, file := range parseNonTestFiles(t, relDir) {
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv != nil && fn.Name.Name == name {
				count++
			}
		}
	}
	return count
}

// TestBoundedFailureClassesMatchTheCollectorWriters ties the status leaf's
// failure classes to the classes the collector writers declare, as sets in both
// directions: a class added to a writer, removed from it, or renamed fails here
// instead of silently reading unknown. The writer's constants are read from its
// source with go/parser, so neither this test nor the writer needs an edit for
// a new class to be noticed. This test file is the one place the status leaf
// names collector runtime packages; the shipped code never imports them.
//
// Not derived from a constant, so pinned by value here: the claim runner's
// collect_failure and commit_failure, which are string literals passed to
// failTerminal in collector/claimed_service.go, and terraform_state's class,
// which comes from one method (checked for a second implementer rather than
// enumerated).
func TestBoundedFailureClassesMatchTheCollectorWriters(t *testing.T) {
	t.Parallel()
	providerFive := []string{
		string(sdk.FailureAuthDenied), string(sdk.FailureNotFound), string(sdk.FailureRateLimited),
		string(sdk.FailureRetryable), string(sdk.FailureTerminal),
	}
	hostWriter := declared(t, "collector/extensionhost", namePrefix("FailureClass"), nil)
	host := minusClasses(hostWriter, []string{extensionhost.FailureClassIdentityMismatch})
	common := sortedUnique(append(slices.Clone(providerFive),
		collector.FailureClassAttemptBudgetExhausted, extensionhost.FailureClassIdentityMismatch,
		"collect_failure", "commit_failure"))
	hostedVendor := statuscollector.FailureClassesForKind("hosted_vendor")
	wantHosted := sortedUnique(append(append(slices.Clone(common), host...), statuscollector.ExtensionDefinedFailureToken))
	if !slices.Equal(hostedVendor, wantHosted) {
		t.Errorf("extension-hosted kind reports %v, writers declare %v", hostedVendor, wantHosted)
	}
	// ownOnly is what a first-party kind reports beyond the common and host
	// classes: exactly its kindFailureClasses entry.
	ownOnly := func(kind scope.CollectorKind) []string {
		return minusClasses(statuscollector.FailureClassesForKind(string(kind)), hostedVendor)
	}
	for _, kind := range scope.AllCollectorKinds() {
		if missing := minusClasses(append(slices.Clone(common), host...), statuscollector.FailureClassesForKind(string(kind))); len(missing) > 0 {
			t.Errorf("kind %q does not report the common or host classes %v", kind, missing)
		}
	}
	check := func(kind scope.CollectorKind, writer []string) {
		got := minusClasses(writer, common)
		if want := ownOnly(kind); !slices.Equal(got, want) {
			t.Errorf("%s: writer declares %v, status leaf reports %v; add, remove, or rename the class in both", kind, got, want)
		}
	}

	// The Jira writer lives in the extracted satellite
	// (eshu-hq/eshu-collector-jira), so no in-tree writer declares its
	// classes anymore. The status leaf's Jira classes stay pinned below
	// against the in-tree SDK policy classes the satellite still emits.
	check(scope.CollectorAWS, declared(t, "collector/awscloud/awsruntime", namePrefix("FailureClass"), map[string]string{
		"FailureClassPermissionDenied":      awsruntime.FailureClassPermissionDenied,
		"FailureClassUnsupportedPermission": awsruntime.FailureClassUnsupportedPermission,
		"FailureClassStaleFence":            awsruntime.FailureClassStaleFence,
	}))
	registry := declared(t, "collector", namePrefix("RegistryFailure"), nil)
	check(scope.CollectorOCIRegistry, registry)
	check(scope.CollectorSBOMAttestation, registry)
	check(scope.CollectorPackageRegistry, append(slices.Clone(registry),
		declared(t, "collector/registry/package/runtime", namePrefix("failureClass"), nil)...))
	check(scope.CollectorVulnerabilityIntelligence, declared(t, "vulnsource", namePrefix("ErrorClass"), nil))
	check(scope.CollectorScannerWorker, declared(t, "collector/scannercomponent", typedAs("FailureClass"), nil))

	if got := methodCount(t, "collector/terraformstate", "FailureClass"); got != 1 {
		t.Errorf("terraformstate declares %d FailureClass methods, want 1; tie the new class here", got)
	}
	check(scope.CollectorTerraformState, []string{terraformstate.WaitingOnGitGenerationError{}.FailureClass()})

	// The SDK holds the five provider classes plus the Jira policy classes.
	sdkWriter := declared(t, "collector/sdk", typedAs("FailureClass"), nil)
	if got, want := minusClasses(sdkWriter, providerFive), ownOnly(scope.CollectorJira); !slices.Equal(got, want) {
		t.Errorf("sdk declares %v beyond the provider classes, jira reports %v", got, want)
	}
	if got := vulnsource.ErrorClassNonRetryable; !slices.Contains(ownOnly(scope.CollectorVulnerabilityIntelligence), got) {
		t.Errorf("vulnerability_intelligence does not report %q", got)
	}
}
