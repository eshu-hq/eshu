// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

func testResolver(t *testing.T) (ArtifactResolver, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "testdata/cassettes/awscloud"), 0o750); err != nil {
		t.Fatalf("mkdir cassette: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "fixture.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	snap := goldengate.Snapshot{
		SchemaVersion: "1",
		Graph: goldengate.GraphSnapshot{
			RequiredCorrelations: []goldengate.RequiredCorrelation{{ID: "rc-1"}},
		},
		QueryShapes: goldengate.QueryShapes{
			HTTP: map[string]goldengate.QueryShape{"repo_summary": {}},
			MCP:  map[string]goldengate.QueryShape{"get_repo_summary": {}},
		},
	}
	matrix := capabilitycatalog.Matrix{Capabilities: []capabilitycatalog.MatrixCapability{
		{
			Capability: "cap.profiled",
			Profiles: map[string]capabilitycatalog.MatrixProfile{
				"local_lightweight": {
					Status:          "unsupported",
					MaxTruthLevel:   "unsupported",
					Verification:    []capabilitycatalog.MatrixVerification{{Kind: "go_test", Ref: "./internal/query"}},
					RequiredRuntime: "local_host",
				},
				"production": {
					Status:          "supported",
					MaxTruthLevel:   "exact",
					Verification:    []capabilitycatalog.MatrixVerification{{Kind: "remote_validation", Ref: "prod-profiled"}},
					RequiredRuntime: "deployed_services",
				},
			},
		},
		{
			Capability: "cap.missing_verification",
			Profiles: map[string]capabilitycatalog.MatrixProfile{
				"production": {Status: "supported", MaxTruthLevel: "exact"},
			},
		},
		{
			Capability: "cap.blank_verification",
			Profiles: map[string]capabilitycatalog.MatrixProfile{
				"local_lightweight": {
					Status:          "unsupported",
					MaxTruthLevel:   "unsupported",
					Verification:    []capabilitycatalog.MatrixVerification{{Kind: "go_test", Ref: "./internal/query"}},
					RequiredRuntime: "local_host",
				},
				"production": {
					Status:          "supported",
					MaxTruthLevel:   "exact",
					Verification:    []capabilitycatalog.MatrixVerification{{Kind: "", Ref: "  "}},
					RequiredRuntime: "deployed_services",
				},
			},
		},
		{
			Capability: "cap.unsupported_only",
			Profiles: map[string]capabilitycatalog.MatrixProfile{
				"local_lightweight": {Status: "unsupported", MaxTruthLevel: "unsupported", Verification: []capabilitycatalog.MatrixVerification{{Kind: "go_test", Ref: "./internal/query"}}},
			},
		},
	}}
	productClaims := capabilitycatalog.ProductClaimLedger{Version: "v1", Claims: []capabilitycatalog.ProductClaim{
		{
			ID:           "readme.claim-one",
			ClaimText:    "README claim one",
			Capabilities: []capabilitycatalog.ProductClaimCapability{{ID: "cap.profiled"}},
			Proof: capabilitycatalog.ProductClaimProof{
				Command: "cd go && go test ./internal/query -count=1",
				Signals: []capabilitycatalog.ProductClaimProofSignal{
					{Capability: "cap.profiled", Kind: "go_test", Ref: "./internal/query"},
				},
			},
		},
		{
			ID:           "readme.missing-proof",
			ClaimText:    "README claim without proof",
			Capabilities: []capabilitycatalog.ProductClaimCapability{{ID: "cap.profiled"}},
		},
		{
			ID:        "readme.missing-capability",
			ClaimText: "README claim without capability",
			Proof: capabilitycatalog.ProductClaimProof{
				Command: "cd go && go test ./internal/query -count=1",
				Signals: []capabilitycatalog.ProductClaimProofSignal{
					{Capability: "cap.profiled", Kind: "go_test", Ref: "./internal/query"},
				},
			},
		},
		{
			ID:           "readme.unknown-capability",
			ClaimText:    "README claim with unknown capability",
			Capabilities: []capabilitycatalog.ProductClaimCapability{{ID: "cap.unknown"}},
			Proof: capabilitycatalog.ProductClaimProof{
				Command: "cd go && go test ./internal/query -count=1",
				Signals: []capabilitycatalog.ProductClaimProofSignal{
					{Capability: "cap.unknown", Kind: "go_test", Ref: "./internal/query"},
				},
			},
		},
	}}
	authzProofs := AuthzProofLedger{Version: "v1", Scenarios: []AuthzProofScenario{
		{
			Family:       "repository_content",
			GrantMode:    "in_grant",
			ProofGate:    "authz-scoped-route-tests",
			TestFile:     "go/internal/query/authz_replay_coverage_contract_test.go",
			TestName:     "TestAuthorizationReplayCoverageContract",
			RouteSamples: []string{"POST /api/v0/code/search"},
		},
	}}
	return ArtifactResolver{RepoRoot: root, Snapshot: snap, Matrix: matrix, ProductClaims: productClaims, AuthzProofs: authzProofs}, root
}

func TestResolveCassetteAndFixturePaths(t *testing.T) {
	r, _ := testResolver(t)
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioCassette, Ref: "testdata/cassettes/awscloud"}); !ok {
		t.Error("present cassette dir should resolve")
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioParserFixture, Ref: "fixture.json"}); !ok {
		t.Error("present fixture file should resolve")
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioCassette, Ref: "testdata/cassettes/absent"}); ok {
		t.Error("missing cassette dir must not resolve")
	}
}

func TestResolveGoTestPath(t *testing.T) {
	r, root := testResolver(t)
	goTest := filepath.Join(root, "internal", "replay", "fault_test.go")
	if err := os.MkdirAll(filepath.Dir(goTest), 0o750); err != nil {
		t.Fatalf("mkdir go test dir: %v", err)
	}
	if err := os.WriteFile(goTest, []byte("package replay\n"), 0o600); err != nil {
		t.Fatalf("write go test file: %v", err)
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioGoTest, Ref: "internal/replay/fault_test.go"}); !ok {
		t.Error("present go_test file should resolve")
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioGoTest, Ref: "internal/replay/missing_test.go"}); ok {
		t.Error("missing go_test file must not resolve")
	}
}

func TestResolveProofArtifactPath(t *testing.T) {
	r, root := testResolver(t)
	proof := filepath.Join(root, "specs", "capability-budget-proof.v1.yaml")
	if err := os.MkdirAll(filepath.Dir(proof), 0o750); err != nil {
		t.Fatalf("mkdir proof dir: %v", err)
	}
	if err := os.WriteFile(proof, []byte("version: v1\n"), 0o600); err != nil {
		t.Fatalf("write proof artifact: %v", err)
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioProofArtifact, Ref: "specs/capability-budget-proof.v1.yaml"}); !ok {
		t.Error("present proof_artifact file should resolve")
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioProofArtifact, Ref: "specs/missing.yaml"}); ok {
		t.Error("missing proof_artifact file must not resolve")
	}
}

func TestResolveSnapshotBackedScenarios(t *testing.T) {
	r, _ := testResolver(t)
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioCorrelation, Ref: "rc-1"}); !ok {
		t.Error("rc-1 present in snapshot should resolve")
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioCorrelation, Ref: "rc-99"}); ok {
		t.Error("rc-99 absent from snapshot must not resolve")
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioAPIMCPGolden, Ref: "repo_summary"}); !ok {
		t.Error("http query shape should resolve")
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioAPIMCPGolden, Ref: "get_repo_summary"}); !ok {
		t.Error("mcp query shape should resolve")
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioAPIMCPGolden, Ref: "missing_shape"}); ok {
		t.Error("absent query shape must not resolve")
	}
}

func TestResolveCapabilityClaimUsesMatrixProfileProofs(t *testing.T) {
	r, _ := testResolver(t)
	ok, detail := r.Resolve(CoverageEntry{Scenario: ScenarioCapabilityClaim, Ref: "cap.profiled"})
	if !ok {
		t.Fatalf("profiled capability claim should resolve: %s", detail)
	}
	for _, want := range []string{"supported=1", "refusal=1"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail %q missing %q", detail, want)
		}
	}

	if ok, detail := r.Resolve(CoverageEntry{Scenario: ScenarioCapabilityClaim, Ref: "cap.missing_verification"}); ok || !strings.Contains(detail, "missing verification") {
		t.Fatalf("capability with missing profile verification resolved=%v detail=%q", ok, detail)
	}
	if ok, detail := r.Resolve(CoverageEntry{Scenario: ScenarioCapabilityClaim, Ref: "cap.blank_verification"}); ok || !strings.Contains(detail, "missing verification") {
		t.Fatalf("capability with blank profile verification resolved=%v detail=%q", ok, detail)
	}
	if ok, detail := r.Resolve(CoverageEntry{Scenario: ScenarioCapabilityClaim, Ref: "cap.unsupported_only"}); ok || !strings.Contains(detail, "no supported or experimental profile") {
		t.Fatalf("unsupported-only capability resolved=%v detail=%q", ok, detail)
	}
	if ok, detail := r.Resolve(CoverageEntry{Scenario: ScenarioCapabilityClaim, Ref: "cap.absent"}); ok || !strings.Contains(detail, "no capability") {
		t.Fatalf("absent capability resolved=%v detail=%q", ok, detail)
	}
}

func TestResolveProductClaimUsesLedgerProof(t *testing.T) {
	r, _ := testResolver(t)
	ok, detail := r.Resolve(CoverageEntry{Scenario: ScenarioProductClaim, Ref: "readme.claim-one"})
	if !ok {
		t.Fatalf("product claim should resolve: %s", detail)
	}
	for _, want := range []string{"readme.claim-one", "signals=1"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail %q missing %q", detail, want)
		}
	}

	if ok, detail := r.Resolve(CoverageEntry{Scenario: ScenarioProductClaim, Ref: "readme.missing-proof"}); ok || !strings.Contains(detail, "missing deterministic proof") {
		t.Fatalf("claim with missing proof resolved=%v detail=%q", ok, detail)
	}
	if ok, detail := r.Resolve(CoverageEntry{Scenario: ScenarioProductClaim, Ref: "readme.missing-capability"}); ok || !strings.Contains(detail, "no referenced capabilities") {
		t.Fatalf("claim with no capabilities resolved=%v detail=%q", ok, detail)
	}
	if ok, detail := r.Resolve(CoverageEntry{Scenario: ScenarioProductClaim, Ref: "readme.unknown-capability"}); ok || !strings.Contains(detail, "unknown capability") {
		t.Fatalf("claim with unknown capability resolved=%v detail=%q", ok, detail)
	}
	if ok, detail := r.Resolve(CoverageEntry{Scenario: ScenarioProductClaim, Ref: "readme.absent"}); ok || !strings.Contains(detail, "no product claim") {
		t.Fatalf("absent product claim resolved=%v detail=%q", ok, detail)
	}
}

func TestResolveAuthorizationScopedRouteUsesProofLedger(t *testing.T) {
	r, root := testResolver(t)
	testFile := filepath.Join(root, "go", "internal", "query", "authz_replay_coverage_contract_test.go")
	if err := os.MkdirAll(filepath.Dir(testFile), 0o750); err != nil {
		t.Fatalf("mkdir authz test dir: %v", err)
	}
	if err := os.WriteFile(testFile, []byte("package query\n"), 0o600); err != nil {
		t.Fatalf("write authz test file: %v", err)
	}

	ok, detail := r.Resolve(CoverageEntry{
		Scenario:  ScenarioAuthzScopedRoute,
		Ref:       "repository_content:in_grant",
		ProofGate: "authz-scoped-route-tests",
	})
	if !ok {
		t.Fatalf("authz scoped-route proof should resolve: %s", detail)
	}
	for _, want := range []string{"repository_content", "in_grant", "POST /api/v0/code/search"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail %q missing %q", detail, want)
		}
	}

	if ok, detail := r.Resolve(CoverageEntry{
		Scenario:  ScenarioAuthzScopedRoute,
		Ref:       "repository_content:out_of_grant",
		ProofGate: "authz-scoped-route-tests",
	}); ok || !strings.Contains(detail, "no authorization scoped-route proof") {
		t.Fatalf("missing authz proof resolved=%v detail=%q", ok, detail)
	}
	if ok, detail := r.Resolve(CoverageEntry{
		Scenario:  ScenarioAuthzScopedRoute,
		Ref:       "repository_content:in_grant",
		ProofGate: "wrong-gate",
	}); ok || !strings.Contains(detail, "proof_gate") {
		t.Fatalf("wrong proof gate resolved=%v detail=%q", ok, detail)
	}
	if ok, detail := r.Resolve(CoverageEntry{
		Scenario:  ScenarioAuthzScopedRoute,
		Ref:       "repository_content:tenant_wide",
		ProofGate: "authz-scoped-route-tests",
	}); ok || !strings.Contains(detail, "unknown grant mode") {
		t.Fatalf("unknown grant mode resolved=%v detail=%q", ok, detail)
	}
}

func TestResolveRejectsPathEscape(t *testing.T) {
	r, _ := testResolver(t)
	// A ref that escapes the repo root must not resolve, even if the target
	// happens to exist on disk: coverage artifacts live inside the repo.
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioCassette, Ref: "../../etc/hosts"}); ok {
		t.Error("path-escaping ref must not resolve")
	}
}
