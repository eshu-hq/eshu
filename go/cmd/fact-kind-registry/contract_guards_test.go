// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFixtureTree materializes a minimal repo tree for the contract
// guard tests. Files are repo-relative slash paths mapped to contents.
func writeFixtureTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, contents := range files {
		if err := writeTestFile(t, filepath.Join(root, filepath.FromSlash(rel)), contents); err != nil {
			t.Fatalf("write fixture %s: %v", rel, err)
		}
	}
	return root
}

// runGuards discovers declarations in a fixture tree and returns the
// combined diagnostics error text (empty when clean).
func runGuards(t *testing.T, root string) string {
	t.Helper()
	families, bindings, diags := discoverDeclarations(root, filepath.Join(root, "go"))
	diags = append(diags, validateContracts(root, families, bindings)...)
	if err := diags.err(); err != nil {
		return err.Error()
	}
	return ""
}

const fixtureFamily = `// SPDX-License-Identifier: MIT

package cloud

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

const TestResourceFactKind = "test_resource"
const TestWarningFactKind = "test_warning"

var testFamily = contract.Declare(contract.Family{
	ID: "test", FactDir: "go/internal/facts/cloud",
	OwnerRepository: "eshu-hq/eshu", LifecycleOwner: "go/internal/facts",
	SchemaVersion: "1.0.0", ReducerDomain: "test_materialization",
	ProjectionHook: "test_projection", AdmissionHook: "facts.ValidateSchemaVersion",
	ReadSurface: "GET /api/v0/test", TruthProfile: "provider_gated",
	Kinds: []contract.Kind{
		{Kind: "test_resource"},
		{Kind: "test_warning", Disposition: "unconsumed",
			NoReducerReason: "warning only", NoReducerEvidence: "fixture ledger"},
	},
	Producers: []contract.Producer{{
		ID: "collector-test", Repository: "eshu-hq/eshu", Path: "go/internal/collector/test",
		AuthorityMode: "core_issued_grant", Kinds: []string{"test_resource", "test_warning"},
	}},
})
`

const fixtureBinding = `// SPDX-License-Identifier: MIT

package reducer

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

var testBinding = contract.Bind(contract.Binding{
	ID: "reducer/reducer_test_materialization", FamilyID: "test",
	ReducerDir: "go/internal/reducer", Symbol: "MaterializeTest",
	Kinds: []string{"test_resource"},
})

func MaterializeTest(kind string) bool { return kind == "test_resource" }
`

func fixtureFiles() map[string]string {
	return map[string]string{
		"go/internal/facts/cloud/fam.go": fixtureFamily,
		"go/internal/reducer/handler.go": fixtureBinding,
	}
}

// TestContractGuardsGreen proves the valid shape passes: one bound
// kind with reducer evidence, one disclosed unbound kind, one
// producer covering both.
func TestContractGuardsGreen(t *testing.T) {
	root := writeFixtureTree(t, fixtureFiles())
	if got := runGuards(t, root); got != "" {
		t.Fatalf("valid declarations produced diagnostics:\n%s", got)
	}
}

// TestContractGuardsSeededViolations proves each guard fails on its
// planted violation with a file:line diagnostic naming the fix.
func TestContractGuardsSeededViolations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(files map[string]string)
		want   string
	}{
		{
			name: "duplicate kind ownership",
			mutate: func(files map[string]string) {
				files["go/internal/facts/cloud/other.go"] = strings.ReplaceAll(
					strings.ReplaceAll(fixtureFamily, `ID: "test"`, `ID: "other"`),
					"testFamily", "otherFamily")
			},
			want: "owned by both",
		},
		{
			name: "wrong fact directory",
			mutate: func(files map[string]string) {
				files["go/internal/facts/cloud/fam.go"] = strings.ReplaceAll(fixtureFamily,
					`FactDir: "go/internal/facts/cloud"`, `FactDir: "go/internal/facts"`)
			},
			want: "move the declaration into",
		},
		{
			name: "non-literal field",
			mutate: func(files map[string]string) {
				files["go/internal/facts/cloud/fam.go"] = strings.ReplaceAll(fixtureFamily,
					`ID: "test"`, `ID: familyID`)
			},
			want: "not a string literal",
		},
		{
			name: "unknown struct field",
			mutate: func(files map[string]string) {
				files["go/internal/facts/cloud/fam.go"] = strings.ReplaceAll(fixtureFamily,
					`ID: "test",`, `ID: "test", BogusField: "x",`)
			},
			want: "unknown field",
		},
		{
			name: "duplicate binding edge",
			mutate: func(files map[string]string) {
				files["go/internal/reducer/handler.go"] = strings.ReplaceAll(fixtureBinding,
					`Kinds: []string{"test_resource"}`, `Kinds: []string{"test_resource", "test_resource"}`)
			},
			want: "duplicate binding edge",
		},
		{
			name: "binding unknown kind",
			mutate: func(files map[string]string) {
				files["go/internal/reducer/handler.go"] = strings.ReplaceAll(fixtureBinding,
					`"test_resource"}`, `"test_nope"}`)
			},
			want: "does not own",
		},
		{
			name: "binding missing symbol",
			mutate: func(files map[string]string) {
				files["go/internal/reducer/handler.go"] = strings.ReplaceAll(fixtureBinding,
					`Symbol: "MaterializeTest"`, `Symbol: "NoSuchSymbol"`)
			},
			want: "does not exist",
		},
		{
			name: "binding symbol behind build tag",
			mutate: func(files map[string]string) {
				files["go/internal/reducer/handler.go"] = strings.ReplaceAll(fixtureBinding,
					`func MaterializeTest(kind string) bool { return kind == "test_resource" }`, "")
				files["go/internal/reducer/tagged.go"] = "//go:build ignore\n\npackage reducer\n\nfunc MaterializeTest(kind string) bool { return kind == \"test_resource\" }\n"
			},
			want: "does not exist",
		},
		{
			name: "reducer evidence behind build tag",
			mutate: func(files map[string]string) {
				files["go/internal/reducer/handler.go"] = strings.ReplaceAll(fixtureBinding,
					`func MaterializeTest(kind string) bool { return kind == "test_resource" }`,
					`func MaterializeTest(kind string) bool { return true }`)
				files["go/internal/reducer/tagged.go"] = "//go:build ignore\n\npackage reducer\n\nfunc taggedReader(kind string) bool { return kind == \"test_resource\" }\n"
			},
			want: "no reducer-consumption evidence",
		},
		{
			name: "binding wrong directory",
			mutate: func(files map[string]string) {
				files["go/internal/reducer/handler.go"] = strings.ReplaceAll(fixtureBinding,
					`ReducerDir: "go/internal/reducer"`, `ReducerDir: "go/internal/reducer/other"`)
			},
			want: "move the binding beside its implementation",
		},
		{
			name: "binding without evidence",
			mutate: func(files map[string]string) {
				files["go/internal/reducer/handler.go"] = strings.ReplaceAll(fixtureBinding,
					`kind == "test_resource"`, `kind == "something_else"`)
			},
			want: "no reducer-consumption evidence",
		},
		{
			name: "kind with neither binding nor disposition",
			mutate: func(files map[string]string) {
				files["go/internal/facts/cloud/fam.go"] = strings.ReplaceAll(fixtureFamily,
					`{Kind: "test_warning", Disposition: "unconsumed",
			NoReducerReason: "warning only", NoReducerEvidence: "fixture ledger"}`,
					`{Kind: "test_warning"}`)
			},
			want: "no reducer binding and no no-reducer disposition",
		},
		{
			name: "kind with both binding and disposition",
			mutate: func(files map[string]string) {
				files["go/internal/facts/cloud/fam.go"] = strings.ReplaceAll(fixtureFamily,
					`{Kind: "test_resource"},`,
					`{Kind: "test_resource", Disposition: "read_only", NoReducerReason: "r", NoReducerEvidence: "e"},`)
			},
			want: "both reducer bindings",
		},
		{
			name: "stale no-reducer disclosure",
			mutate: func(files map[string]string) {
				files["go/internal/reducer/extra.go"] = `package reducer

func readsWarning(kind string) bool { return kind == "test_warning" }
`
			},
			want: "stale disclosure",
		},
		{
			name: "producer subset names unowned kind",
			mutate: func(files map[string]string) {
				files["go/internal/facts/cloud/fam.go"] = strings.ReplaceAll(fixtureFamily,
					`Kinds: []string{"test_resource", "test_warning"}`, `Kinds: []string{"test_resource", "test_nope"}`)
			},
			want: "which the family does not own",
		},
		{
			name: "kind without producer",
			mutate: func(files map[string]string) {
				files["go/internal/facts/cloud/fam.go"] = strings.ReplaceAll(fixtureFamily,
					`Kinds: []string{"test_resource", "test_warning"}`, `Kinds: []string{"test_resource"}`)
			},
			want: "has no declared producer",
		},
		{
			name: "blank metadata",
			mutate: func(files map[string]string) {
				files["go/internal/facts/cloud/fam.go"] = strings.ReplaceAll(fixtureFamily,
					`OwnerRepository: "eshu-hq/eshu",`, ``)
			},
			want: "missing required metadata OwnerRepository",
		},
		{
			name: "duplicate binding id",
			mutate: func(files map[string]string) {
				files["go/internal/reducer/second.go"] = strings.ReplaceAll(fixtureBinding,
					"testBinding", "secondBinding")
			},
			want: "duplicate binding ID",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := fixtureFiles()
			tc.mutate(files)
			root := writeFixtureTree(t, files)
			got := runGuards(t, root)
			if got == "" {
				t.Fatalf("seeded violation %q passed; want diagnostic containing %q", tc.name, tc.want)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("diagnostics = %q, want substring %q", got, tc.want)
			}
			if !strings.Contains(got, ".go:") || !strings.Contains(got, "ACTION:") {
				t.Fatalf("diagnostics must carry file:line and ACTION, got %q", got)
			}
		})
	}
}

// TestContractGuardsSharedReducerPackage proves two families sharing
// one reducer package is valid (the proven-theory fixture case).
func TestContractGuardsSharedReducerPackage(t *testing.T) {
	files := fixtureFiles()
	files["go/internal/facts/cloud/other.go"] = `// SPDX-License-Identifier: MIT

package cloud

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

var otherFamily = contract.Declare(contract.Family{
	ID: "other", FactDir: "go/internal/facts/cloud",
	OwnerRepository: "eshu-hq/eshu", LifecycleOwner: "go/internal/facts",
	SchemaVersion: "1.0.0", ReducerDomain: "test_materialization",
	ProjectionHook: "test_projection", AdmissionHook: "facts.ValidateSchemaVersion",
	ReadSurface: "GET /api/v0/test", TruthProfile: "provider_gated",
	Kinds: []contract.Kind{{Kind: "other_resource"}},
	Producers: []contract.Producer{{
		ID: "collector-test", Repository: "eshu-hq/eshu", Path: "go/internal/collector/test",
		AuthorityMode: "core_issued_grant", Kinds: []string{"other_resource"},
	}},
})
`
	files["go/internal/reducer/other.go"] = `// SPDX-License-Identifier: MIT

package reducer

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

var otherBinding = contract.Bind(contract.Binding{
	ID: "reducer/reducer_other_materialization", FamilyID: "other",
	ReducerDir: "go/internal/reducer", Symbol: "MaterializeOther",
	Kinds: []string{"other_resource"},
})

func MaterializeOther(kind string) bool { return kind == "other_resource" }
`
	root := writeFixtureTree(t, files)
	if got := runGuards(t, root); got != "" {
		t.Fatalf("shared reducer package produced diagnostics:\n%s", got)
	}
}

// TestShardPathMapping pins the arbiter filename policies: strip an
// exact case-sensitive "<family>_" / "<directory>_" prefix once.
func TestShardPathMapping(t *testing.T) {
	cases := []struct{ got, want string }{
		{kindShardPath("aws", "aws_resource"), "specs/fact-kind-registry-contracts.d/kinds/aws/resource.yaml"},
		{kindShardPath("terraform_state", "terraform_state_resource"), "specs/fact-kind-registry-contracts.d/kinds/terraform_state/resource.yaml"},
		{kindShardPath("ci_cd_run", "ci.run"), "specs/fact-kind-registry-contracts.d/kinds/ci_cd_run/ci.run.yaml"},
		{kindShardPath("aws", "aws_aws_thing"), "specs/fact-kind-registry-contracts.d/kinds/aws/aws_thing.yaml"},
		{bindingShardPath("aws/aws_cloud_image_materialization"), "specs/fact-kind-registry-contracts.d/bindings/aws/cloud_image_materialization.yaml"},
		{bindingShardPath("reducer/other_handler"), "specs/fact-kind-registry-contracts.d/bindings/reducer/other_handler.yaml"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Fatalf("shard path = %q, want %q", tc.got, tc.want)
		}
	}
}

// TestValidateOutputSetGuards proves path and size guards fail on
// planted violations and pass on a clean set.
func TestValidateOutputSetGuards(t *testing.T) {
	clean := []outputFile{{rel: "specs/fact-kind-registry-contracts.d/families/aws.yaml", data: []byte("x\n")}}
	if err := validateOutputSet(clean); err != nil {
		t.Fatalf("clean output set rejected: %v", err)
	}
	big := []outputFile{{rel: "specs/fact-kind-registry-contracts.d/families/big.yaml", data: []byte(strings.Repeat("x\n", 500))}}
	if err := validateOutputSet(big); err == nil || !strings.Contains(err.Error(), "lines") {
		t.Fatalf("500-line output error = %v, want line-limit rejection", err)
	}
	dup := []outputFile{
		{rel: "specs/fact-kind-registry-contracts.d/kinds/aws/resource.yaml", data: []byte("x\n")},
		{rel: "specs/fact-kind-registry-contracts.d/kinds/aws/resource.yaml", data: []byte("y\n")},
	}
	if err := validateOutputSet(dup); err == nil || !strings.Contains(err.Error(), "is produced by more than one declaration") {
		t.Fatalf("duplicate destination error = %v, want single-output duplicate rejection", err)
	} else {
		if !strings.Contains(err.Error(), "rename one source ID") {
			t.Fatalf("duplicate destination error = %v, want rename-one-source-ID action", err)
		}
		if n := strings.Count(err.Error(), "specs/fact-kind-registry-contracts.d/kinds/aws/resource.yaml"); n != 1 {
			t.Fatalf("duplicate destination error names the output %d times, want once: %v", n, err)
		}
	}
	conflict := []outputFile{
		{rel: "specs/fact-kind-registry-contracts.d/kinds/aws", data: []byte("x\n")},
		{rel: "specs/fact-kind-registry-contracts.d/kinds/aws/resource.yaml", data: []byte("y\n")},
	}
	if err := validateOutputSet(conflict); err == nil || !strings.Contains(err.Error(), "directory prefix") {
		t.Fatalf("file/dir conflict error = %v", err)
	}
	unsafe := []outputFile{{rel: "specs/fact-kind-registry-contracts.d/kinds/../evil.yaml", data: []byte("x\n")}}
	if err := validateOutputSet(unsafe); err == nil || !strings.Contains(err.Error(), "unsafe path component") {
		t.Fatalf("unsafe component error = %v", err)
	}
}

// TestDiscoveryNeverExecutesInit proves discovery is parse-only by
// construction: a fixture whose init would create a marker file and
// panic is scanned successfully and leaves no marker.
func TestDiscoveryNeverExecutesInit(t *testing.T) {
	files := fixtureFiles()
	files["go/internal/facts/cloud/fam.go"] += `
func init() { panic("discovery executed package init") }
`
	root := writeFixtureTree(t, files)
	if got := runGuards(t, root); got != "" {
		t.Fatalf("parse-only discovery failed on panicking-init fixture:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(root, "init-ran")); err == nil {
		t.Fatal("fixture init left a marker; discovery executed code")
	}
}
