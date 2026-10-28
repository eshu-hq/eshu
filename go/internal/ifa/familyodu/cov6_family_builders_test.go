// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// cov6RepoRoot resolves the repository root the same way the
// materializededges lockstep tests do: four directories above this package.
func cov6RepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
}

// assertCov6OduWellFormed pins the live-ingest invariants every family Odù
// must hold: a catalog name, at least one fact, and a distinct non-empty
// stable_fact_key per fact (replay derives FactID from the key, so a
// duplicate collapses last-writer-wins at ingest).
func assertCov6OduWellFormed(t *testing.T, wantName string, odu Odu) {
	t.Helper()
	if odu.Name != wantName {
		t.Fatalf("Odù name = %q, want %q", odu.Name, wantName)
	}
	if len(odu.Facts) == 0 {
		t.Fatalf("Odù %q carries no facts", wantName)
	}
	seen := make(map[string]string, len(odu.Facts))
	for _, env := range odu.Facts {
		if env.StableFactKey == "" {
			t.Fatalf("Odù %q fact kind %q carries an empty stable_fact_key", wantName, env.FactKind)
		}
		if prev, dup := seen[env.StableFactKey]; dup {
			t.Fatalf("Odù %q duplicate stable_fact_key %q (fact kinds %s and %s)", wantName, env.StableFactKey, prev, env.FactKind)
		}
		seen[env.StableFactKey] = env.FactKind
	}
}

func TestCov6PureFamilyBuildersWellFormed(t *testing.T) {
	t.Parallel()
	builders := []struct {
		name  string
		build func() CatalogOdu
	}{
		{name: InheritanceFamilyOduName, build: InheritanceFamilyOdu},
		{name: RationaleFamilyOduName, build: RationaleFamilyOdu},
		{name: CloudResourceFamilyOduName, build: CloudResourceFamilyOdu},
		{name: IAMCanAssumeFamilyOduName, build: IAMCanAssumeFamilyOdu},
		{name: IAMInstanceProfileRoleFamilyOduName, build: IAMInstanceProfileRoleFamilyOdu},
		{name: KubernetesNamespaceEnvironmentFamilyOduName, build: KubernetesNamespaceEnvironmentFamilyOdu},
		{name: SymbolRuntimeFamilyOduName, build: SymbolRuntimeFamilyOdu},
	}
	for _, tc := range builders {
		t.Run(strings.TrimPrefix(tc.name, "odu:ifa-"), func(t *testing.T) {
			t.Parallel()
			assertCov6OduWellFormed(t, tc.name, tc.build().Odu)
		})
	}
}

func TestCov6CassetteFamilyLoadersWellFormed(t *testing.T) {
	t.Parallel()
	root := cov6RepoRoot(t)
	loaders := []struct {
		name string
		path string
		load func(string) (Odu, error)
	}{
		{name: RepoDependencyFamilyOduName, path: RepoDependencyFamilyCassetteFullPath(root), load: LoadRepoDependencyFamilyOdu},
		{name: CodeownersFamilyOduName, path: CodeownersFamilyCassetteFullPath(root), load: LoadCodeownersFamilyOdu},
		{name: "odu:ifa-code-call-family", path: CodeCallFamilyCassetteFullPath(root), load: LoadCodeCallFamilyOdu},
		{name: DeployableUnitFamilyOduName, path: DeployableUnitFamilyCassetteFullPath(root), load: LoadDeployableUnitFamilyOdu},
		{name: DocumentationFamilyOduName, path: DocumentationFamilyCassetteFullPath(root), load: LoadDocumentationFamilyOdu},
		{name: SubmodulePinFamilyOduName, path: SubmodulePinFamilyCassetteFullPath(root), load: LoadSubmodulePinFamilyOdu},
		{name: WorkloadDependencyFamilyOduName, path: WorkloadDependencyFamilyCassetteFullPath(root), load: LoadWorkloadDependencyFamilyOdu},
	}
	for _, tc := range loaders {
		t.Run(strings.TrimPrefix(tc.name, "odu:ifa-"), func(t *testing.T) {
			t.Parallel()
			odu, err := tc.load(tc.path)
			if err != nil {
				t.Fatalf("load %s: %v", tc.path, err)
			}
			assertCov6OduWellFormed(t, tc.name, odu)
		})
	}
}

func TestCov6RepoDependencyCassetteCountsAndCoordinates(t *testing.T) {
	t.Parallel()
	root := cov6RepoRoot(t)
	odu, err := LoadRepoDependencyFamilyOdu(RepoDependencyFamilyCassetteFullPath(root))
	if err != nil {
		t.Fatalf("LoadRepoDependencyFamilyOdu: %v", err)
	}
	// The loader fails closed unless the cassette carries exactly 18 facts.
	if len(odu.Facts) != 18 {
		t.Fatalf("repo-dependency Odù carries %d facts, want 18", len(odu.Facts))
	}
	scopeID, generationID, err := RepoDependencyFamilySourceCoordinates(odu)
	if err != nil {
		t.Fatalf("RepoDependencyFamilySourceCoordinates: %v", err)
	}
	if strings.TrimSpace(scopeID) == "" || strings.TrimSpace(generationID) == "" {
		t.Fatalf("source coordinates = (%q,%q), want non-blank", scopeID, generationID)
	}
	if _, _, err := RepoDependencyFamilySourceCoordinates(Odu{}); err == nil {
		t.Fatal("RepoDependencyFamilySourceCoordinates(empty) = nil error, want fail-closed")
	}
	if _, err := LoadRepoDependencyFamilyOdu(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("LoadRepoDependencyFamilyOdu(missing) = nil error, want fail-closed")
	}
}

func TestCov6CassetteFullPathsJoinRepoRoot(t *testing.T) {
	t.Parallel()
	root := cov6RepoRoot(t)
	cases := map[string]string{
		RepoDependencyFamilyCassetteFullPath(root):     "testdata/cassettes/repodependency/ifa-repo-dependency-family.json",
		CodeCallFamilyCassetteFullPath(root):           "testdata/cassettes/codecalls/ifa-code-call-family.json",
		CodeownersFamilyCassetteFullPath(root):         "testdata/cassettes/codeowners/ifa-codeowners-family.json",
		DeployableUnitFamilyCassetteFullPath(root):     "testdata/cassettes/deployableunit/ifa-deployable-unit-family.json",
		DocumentationFamilyCassetteFullPath(root):      "testdata/cassettes/documentation/ifa-documentation-family.json",
		SubmodulePinFamilyCassetteFullPath(root):       "testdata/cassettes/submodulepin/ifa-submodule-pin-family.json",
		WorkloadDependencyFamilyCassetteFullPath(root): "testdata/cassettes/workloaddependency/ifa-workload-dependency-family.json",
		SymbolRuntimeFamilyCassetteFullPath(root):      "testdata/cassettes/symbolruntime/ifa-symbol-runtime-family.json",
	}
	for got, rel := range cases {
		if want := filepath.Join(root, rel); got != want {
			t.Fatalf("cassette path = %q, want %q", got, want)
		}
	}
}

func TestCov6FamilyStableKeyHelpers(t *testing.T) {
	t.Parallel()
	if got := iamCanAssumeFamilyStableFactKey("aws_resource", "arn:example"); !strings.Contains(got, "aws_resource") || !strings.Contains(got, "arn:example") {
		t.Fatalf("iamCanAssumeFamilyStableFactKey = %q, want key carrying kind and identity", got)
	}
	if got := iamInstanceProfileRoleFamilyStableFactKey("aws_iam_role", "role-1"); !strings.Contains(got, "aws_iam_role") || !strings.Contains(got, "role-1") {
		t.Fatalf("iamInstanceProfileRoleFamilyStableFactKey = %q, want key carrying type and id", got)
	}
	if got := kubernetesNamespaceEnvironmentFamilyStableFactKey("prod"); !strings.Contains(got, "prod") {
		t.Fatalf("kubernetesNamespaceEnvironmentFamilyStableFactKey = %q, want key carrying namespace", got)
	}
	entry := shellExecFamilyFunctionEntry("deploy", "uid-1", 10)
	if entry["name"] != "deploy" || entry["uid"] != "uid-1" || entry["line_number"] != float64(10) || entry["end_line"] != float64(11) {
		t.Fatalf("shellExecFamilyFunctionEntry = %v, want name/uid/line/end_line", entry)
	}
	command := shellExecFamilyCommand("deploy", 10, 12, "exec", "sh")
	if command["function_name"] != "deploy" || command["line_number"] != float64(12) || command["api"] != "exec" || command["language"] != "sh" {
		t.Fatalf("shellExecFamilyCommand = %v, want function/line/api/language", command)
	}
	followup := shellExecFamilyFollowupFact()
	if followup.FactKind != SharedFollowupFactKind {
		t.Fatalf("shellExecFamilyFollowupFact kind = %q, want %q", followup.FactKind, SharedFollowupFactKind)
	}
}

func TestCov6LoadCassetteEnvelopesReadsCommittedCassette(t *testing.T) {
	t.Parallel()
	root := cov6RepoRoot(t)
	envs, err := LoadCassetteEnvelopes(WorkloadDependencyFamilyCassetteFullPath(root))
	if err != nil {
		t.Fatalf("LoadCassetteEnvelopes: %v", err)
	}
	if len(envs) == 0 {
		t.Fatal("LoadCassetteEnvelopes returned no envelopes, want file-order facts")
	}
	if _, err := LoadCassetteEnvelopes(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("LoadCassetteEnvelopes(missing) = nil error, want fail-closed")
	}
}
