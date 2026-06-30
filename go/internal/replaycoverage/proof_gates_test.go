// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func replayProofRegistry(gates ...cigates.Gate) *cigates.Registry {
	return &cigates.Registry{Gates: gates}
}

func replayProofGate(id string, localCommand string, workflow string) cigates.Gate {
	var local *cigates.Local
	if localCommand != "" {
		local = &cigates.Local{Command: localCommand}
	}
	return cigates.Gate{
		ID:       id,
		Category: cigates.CategoryExactness,
		Tier:     cigates.TierPrePR,
		Blocking: true,
		Triggers: []string{"specs/replay-coverage-manifest.v1.yaml"},
		Local:    local,
		CI:       cigates.CI{Workflow: workflow, Job: id},
	}
}

func TestValidateRequiredProofGatesRejectsUnknownManifestGate(t *testing.T) {
	manifest := Manifest{Coverage: []CoverageEntry{{
		Surface:      "collector:aws",
		Scenario:     ScenarioCassette,
		ScenarioType: ScenarioTypeBaseline,
		Ref:          "testdata/cassettes/awscloud/supply-chain-demo.json",
		ProofGate:    "typo-gate",
	}}}
	registry := replayProofRegistry(replayProofGate("golden-corpus-gate", "bash scripts/verify-golden-corpus-gate.sh", "golden-corpus-gate.yml"))

	errs := ValidateRequiredProofGates(manifest, AuthzProofLedger{}, registry)
	if len(errs) != 1 {
		t.Fatalf("ValidateRequiredProofGates errors=%d, want 1: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), `unknown proof_gate "typo-gate"`) {
		t.Fatalf("error = %v, want unknown proof_gate", errs[0])
	}
}

func TestValidateRequiredProofGatesRejectsGateWithoutRunnableLocalCommand(t *testing.T) {
	manifest := Manifest{Coverage: []CoverageEntry{{
		Surface:      "collector:aws",
		Scenario:     ScenarioCassette,
		ScenarioType: ScenarioTypeBaseline,
		Ref:          "testdata/cassettes/awscloud/supply-chain-demo.json",
		ProofGate:    "golden-corpus-gate",
	}}}
	registry := replayProofRegistry(replayProofGate("golden-corpus-gate", "", "golden-corpus-gate.yml"))

	errs := ValidateRequiredProofGates(manifest, AuthzProofLedger{}, registry)
	if len(errs) != 1 {
		t.Fatalf("ValidateRequiredProofGates errors=%d, want 1: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), `proof_gate "golden-corpus-gate" has no local command`) {
		t.Fatalf("error = %v, want missing local command", errs[0])
	}
}

func TestValidateRequiredProofGatesAllowsDocumentedLocalOnlyGate(t *testing.T) {
	manifest := Manifest{Coverage: []CoverageEntry{{
		Surface:      "capability:local.fixture",
		Scenario:     ScenarioGoTest,
		ScenarioType: ScenarioTypeFault,
		Ref:          "go/internal/replay/localfixture",
		ProofGate:    "local-fixture-proof",
	}}}
	gate := replayProofGate("local-fixture-proof", "cd go && go test ./internal/replay/localfixture -count=1", "")
	gate.LocalOnlyReason = "local fixture runner has no CI service equivalent"

	if errs := ValidateRequiredProofGates(manifest, AuthzProofLedger{}, replayProofRegistry(gate)); len(errs) != 0 {
		t.Fatalf("ValidateRequiredProofGates returned errors: %v", errs)
	}
}

func TestValidateRequiredProofGatesChecksAuthzProofLedger(t *testing.T) {
	manifest := Manifest{Coverage: []CoverageEntry{{
		Surface:      "authz_family:admin_recovery:in_grant",
		Scenario:     ScenarioAuthzScopedRoute,
		ScenarioType: ScenarioTypeBaseline,
		Ref:          "admin_recovery:in_grant",
		ProofGate:    "authz-scoped-route-tests",
	}}}
	ledger := AuthzProofLedger{Scenarios: []AuthzProofScenario{{
		Family:    "admin_recovery",
		GrantMode: "in_grant",
		ProofGate: "stale-authz-gate",
		TestFile:  "go/internal/query/authz_replay_coverage_contract_test.go",
		TestName:  "TestAuthorizationReplayCoverageContractRouteSamplesAllowScopedTokens",
	}}}
	registry := replayProofRegistry(replayProofGate("authz-scoped-route-tests", "cd go && go test ./internal/query -run TestAuthorizationReplayCoverageContract -count=1", "replay-coverage-gate.yml"))

	errs := ValidateRequiredProofGates(manifest, ledger, registry)
	if len(errs) != 1 {
		t.Fatalf("ValidateRequiredProofGates errors=%d, want 1: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), `authorization proof ledger`) {
		t.Fatalf("error = %v, want authorization proof ledger context", errs[0])
	}
}

func TestRunGateDoesNotCountInvalidProofGateAsCovered(t *testing.T) {
	inputs := gateInputs(true)
	inputs.Manifest.Coverage[0].ProofGate = "typo-gate"
	inputs.ProofGates = replayProofRegistry(replayProofGate("golden-corpus-gate", "bash scripts/verify-golden-corpus-gate.sh", "golden-corpus-gate.yml"))

	cov, _, gr := RunGate(inputs)
	var aws SurfaceCoverage
	for _, sc := range cov.Surfaces {
		if sc.Surface.Key == "collector:aws" {
			aws = sc
			break
		}
	}
	if aws.Status != StatusUnresolved {
		t.Fatalf("collector:aws status = %q, want %q", aws.Status, StatusUnresolved)
	}
	if !strings.Contains(aws.Detail, `unknown proof_gate "typo-gate"`) {
		t.Fatalf("collector:aws detail = %q, want proof gate validation detail", aws.Detail)
	}
	if !gr.Failed() {
		t.Fatal("blocking gate must fail when proof_gate validation fails")
	}
}

func TestRunGateDoesNotCountInvalidAuthzProofGateAsCovered(t *testing.T) {
	inputs := gateInputs(true)
	inputs.Inventory = capabilitycatalog.SurfaceInventory{}
	inputs.FactKinds = nil
	inputs.Ledger = ParserLedger{}
	inputs.Authorization = capabilitycatalog.AuthorizationCatalog{
		PermissionFamilies: []capabilitycatalog.PermissionFamily{{
			Family:             "repository_content",
			CapabilityPrefixes: []string{"code_search."},
		}},
	}
	inputs.Manifest = Manifest{Coverage: []CoverageEntry{{
		Surface:      "authz_family:repository_content:in_grant",
		Scenario:     ScenarioAuthzScopedRoute,
		ScenarioType: ScenarioTypeBaseline,
		Ref:          "repository_content:in_grant",
		ProofGate:    "authz-scoped-route-tests",
	}}}
	inputs.AuthzProofs = AuthzProofLedger{Scenarios: []AuthzProofScenario{{
		Family:    "repository_content",
		GrantMode: "in_grant",
		ProofGate: "stale-authz-gate",
		TestFile:  "go/internal/query/authz_replay_coverage_contract_test.go",
		TestName:  "TestAuthorizationReplayCoverageContractRouteSamplesAllowScopedTokens",
	}}}
	inputs.Resolver = stubResolver{present: map[string]bool{"repository_content:in_grant": true}}
	inputs.ProofGates = replayProofRegistry(replayProofGate("authz-scoped-route-tests", "cd go && go test ./internal/query -run TestAuthorizationReplayCoverageContract -count=1", "replay-coverage-gate.yml"))

	cov, _, gr := RunGate(inputs)
	var authz SurfaceCoverage
	for _, sc := range cov.Surfaces {
		if sc.Surface.Key == "authz_family:repository_content:in_grant" {
			authz = sc
			break
		}
	}
	if authz.Status != StatusUnresolved {
		t.Fatalf("authz status = %q, want %q", authz.Status, StatusUnresolved)
	}
	if !strings.Contains(authz.Detail, `unknown proof_gate "stale-authz-gate"`) {
		t.Fatalf("authz detail = %q, want authz proof gate validation detail", authz.Detail)
	}
	if !gr.Failed() {
		t.Fatal("blocking gate must fail when authz proof_gate validation fails")
	}
}
