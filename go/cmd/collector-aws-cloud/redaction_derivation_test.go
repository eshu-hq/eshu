// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	// Blank import installs the full AWS scanner registry via init side
	// effects so the derived redaction set reflects production registrations.
	// main.go imports the same aggregator; this keeps the test honest even if
	// the command's import is refactored.
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
)

// serviceKindPattern extracts the ServiceKind constant name from a
// runtimebind bind.go source file. The qualifier is either awscloud (root
// constants_*.go) or catalog (#6696 consolidated category files).
var serviceKindPattern = regexp.MustCompile(`ServiceKind:\s*(?:awscloud|catalog)\.(\w+)`)

// TestRedactionKeySetDerivesFromRuntimebindRegistrations proves the command
// derives the redaction-key requirement from the registry rather than a
// hand-maintained switch. The expected set is computed from the
// service/<svc>/runtimebind/bind.go files that set RequiresRedactionKey:
// true, so adding a redaction scanner needs no change here or in config.go.
// A scanner that declares the flag in its binding but is missing from the
// registry-derived set (or vice versa) fails this test.
func TestRedactionKeySetDerivesFromRuntimebindRegistrations(t *testing.T) {
	want := redactionRequiringServiceDirs(t)
	if len(want) == 0 {
		t.Fatalf("redactionRequiringServiceDirs() = empty, want the live redaction scanner set")
	}
	got := awsruntime.ServiceKindsRequiringRedactionKey()
	if !equalStringSets(got, want) {
		t.Fatalf("ServiceKindsRequiringRedactionKey() = %v, want %v (derived from runtimebind RequiresRedactionKey flags)", got, want)
	}
}

// TestRedactionKeyErrorListsDerivedServices proves the missing-key error
// message lists exactly the registry-derived set, joined as "a, b, ..., or z".
// The expectation is recomputed from the registry the same way the production
// code builds the phrase, so the assertion stays meaningful as scanners change
// instead of pinning a brittle literal.
func TestRedactionKeyErrorListsDerivedServices(t *testing.T) {
	kinds := awsruntime.ServiceKindsRequiringRedactionKey()
	if len(kinds) < 3 {
		t.Fatalf("ServiceKindsRequiringRedactionKey() = %v, want at least three redaction scanners for the join check", kinds)
	}

	// Drive the real error path with the first redaction-requiring service and
	// no ESHU_AWS_REDACTION_KEY set.
	getenv := mapEnv(map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": `[{
			"instance_id":"collector-aws-1",
			"collector_kind":"aws",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"target_scopes":[{
					"account_id":"123456789012",
					"allowed_regions":["us-east-1"],
					"allowed_services":["` + kinds[0] + `"],
					"credentials":{
						"mode":"local_workload_identity"
					}
				}]
			}
		}]`,
		"ESHU_AWS_COLLECTOR_INSTANCE_ID": "collector-aws-1",
	})

	_, err := loadRuntimeConfig(getenv)
	if err == nil {
		t.Fatalf("loadRuntimeConfig() error = nil, want missing redaction key rejection")
	}

	wantPhrase := expectedRedactionPhrase(kinds)
	wantMsg := "ESHU_AWS_REDACTION_KEY is required when " + wantPhrase + " service scans are enabled"
	if err.Error() != wantMsg {
		t.Fatalf("loadRuntimeConfig() error = %q, want %q", err.Error(), wantMsg)
	}
	// Every redaction-requiring kind must appear in the message so an operator
	// can see which scanner forced the requirement.
	for _, kind := range kinds {
		if !strings.Contains(err.Error(), kind) {
			t.Fatalf("error %q missing redaction service %q", err.Error(), kind)
		}
	}
}

// expectedRedactionPhrase mirrors redactionKeyServicesPhrase so the test
// recomputes the expected list instead of hardcoding it. Both must render the
// sorted set as "a, b, ..., or z".
func expectedRedactionPhrase(kinds []string) string {
	switch len(kinds) {
	case 0:
		return "a redaction-requiring"
	case 1:
		return kinds[0]
	case 2:
		return kinds[0] + " or " + kinds[1]
	default:
		return strings.Join(kinds[:len(kinds)-1], ", ") + ", or " + kinds[len(kinds)-1]
	}
}

// redactionRequiringServiceDirs returns the sorted set of service kinds whose
// service/<svc>/runtimebind/bind.go declares RequiresRedactionKey: true. It
// reads the source on disk so the expected set is not a hand-maintained list:
// the registry-derived set must match what the bindings actually register.
//
// Every bind.go's kind resolves uniformly from its own ServiceKind constant
// reference, looked up in the Service* constants parsed from the awscloud
// root and the awscloud/catalog consolidation directory: directory names
// never stand in for kinds, so renamed and nested service packages resolve
// exactly like top-level ones. A bind.go with no ServiceKind reference, or a
// reference to an unknown constant, fails loudly instead of silently
// corrupting the derived set.
func redactionRequiringServiceDirs(t *testing.T) []string {
	t.Helper()
	servicesDir := awsServicesDir(t)
	kindsByConst := serviceKindsByConstName(t, filepath.Dir(servicesDir))
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		t.Fatalf("read service dir %q: %v", servicesDir, err)
	}
	var services []string
	check := func(bindFile string) {
		data, readErr := os.ReadFile(bindFile)
		if os.IsNotExist(readErr) {
			return
		}
		if readErr != nil {
			t.Fatalf("read bind file %q: %v", bindFile, readErr)
		}
		if strings.Contains(string(data), "RequiresRedactionKey: true") {
			services = append(services, serviceKindOfBindFile(t, kindsByConst, bindFile))
		}
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		top := filepath.Join(servicesDir, entry.Name())
		check(filepath.Join(top, "runtimebind", "bind.go"))
		nested, nestErr := os.ReadDir(top)
		if nestErr != nil {
			t.Fatalf("read service dir %q: %v", top, nestErr)
		}
		for _, child := range nested {
			if !child.IsDir() {
				continue
			}
			bindFile := filepath.Join(top, child.Name(), "runtimebind", "bind.go")
			if _, statErr := os.Stat(bindFile); os.IsNotExist(statErr) {
				continue
			} else if statErr != nil {
				t.Fatalf("stat bind file %q: %v", bindFile, statErr)
			}
			check(bindFile)
		}
	}
	sort.Strings(services)
	return services
}

// serviceKindOfBindFile resolves the registry service kind for a bind.go from
// its ServiceKind constant reference, looked up in the constants table built
// by serviceKindsByConstName. It fails the test on an unresolvable reference:
// falling back to a directory name that is not the kind would silently corrupt
// the derived set.
func serviceKindOfBindFile(t *testing.T, kindsByConst map[string]string, bindFile string) string {
	t.Helper()
	data, readErr := os.ReadFile(bindFile)
	if readErr != nil {
		t.Fatalf("read bind file %q: %v", bindFile, readErr)
	}
	m := serviceKindPattern.FindStringSubmatch(string(data))
	if m == nil {
		t.Fatalf("bind file %q declares no awscloud ServiceKind", bindFile)
	}
	kind, ok := kindsByConst[m[1]]
	if !ok {
		t.Fatalf("bind file %q uses ServiceKind constant %q with no matching awscloud constant", bindFile, m[1])
	}
	return kind
}

// serviceKindsByConstName parses every constants_*.go file in the awscloud
// root directory plus every category file in the awscloud/catalog directory
// (#6696 consolidation target) and maps Service* constant names to their
// string values. The redaction walk resolves each bind.go through this table
// so the mapping stays derived from source instead of hand-maintained.
func serviceKindsByConstName(t *testing.T, awscloudDir string) map[string]string {
	t.Helper()
	kinds := map[string]string{}
	fset := token.NewFileSet()
	entries, err := os.ReadDir(awscloudDir)
	if err != nil {
		t.Fatalf("read awscloud dir %q: %v", awscloudDir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "constants_") || !strings.HasSuffix(name, ".go") {
			continue
		}
		collectServiceKinds(t, fset, filepath.Join(awscloudDir, name), kinds)
	}
	catalogDir := filepath.Join(awscloudDir, "catalog")
	catalogEntries, err := os.ReadDir(catalogDir)
	if err != nil {
		t.Fatalf("read catalog dir %q: %v", catalogDir, err)
	}
	for _, entry := range catalogEntries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		collectServiceKinds(t, fset, filepath.Join(catalogDir, name), kinds)
	}
	if len(kinds) == 0 {
		t.Fatalf("parsed zero Service* constants under %q; check directory resolution", awscloudDir)
	}
	return kinds
}

// collectServiceKinds parses one constants source file and records its
// Service* string constants into kinds. A duplicate constant name fails
// loudly: two files must never declare the same service kind.
func collectServiceKinds(t *testing.T, fset *token.FileSet, path string, kinds map[string]string) {
	t.Helper()
	file, parseErr := goparser.ParseFile(fset, path, nil, 0)
	if parseErr != nil {
		t.Fatalf("parse constants file %q: %v", path, parseErr)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			val, ok := spec.(*ast.ValueSpec)
			if !ok || len(val.Names) != 1 || len(val.Values) != 1 {
				continue
			}
			constName := val.Names[0].Name
			if !strings.HasPrefix(constName, "Service") {
				continue
			}
			lit, ok := val.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Fatalf("constants file %q: %s is not a string literal", path, constName)
			}
			str, unquoteErr := strconv.Unquote(lit.Value)
			if unquoteErr != nil {
				t.Fatalf("unquote %s in %q: %v", constName, path, unquoteErr)
			}
			if prev, dup := kinds[constName]; dup && prev != str {
				t.Fatalf("constants file %q: %s = %q conflicts with %q", path, constName, str, prev)
			}
			kinds[constName] = str
		}
	}
}

// awsServicesDir resolves go/internal/collector/awscloud/service from this
// test file's location so the walk does not depend on the working directory.
func awsServicesDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	// This file lives in go/cmd/collector-aws-cloud/; service is under
	// go/internal/collector/awscloud/service.
	return filepath.Join(
		filepath.Dir(currentFile),
		"..", "..",
		"internal", "collector", "awscloud", "service",
	)
}

// equalStringSets reports whether two sorted slices hold the same elements.
func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
