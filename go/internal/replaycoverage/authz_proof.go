// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// AuthzProofFileName is the authorization replay proof ledger inside specs/.
const AuthzProofFileName = "authorization-replay-coverage.v1.yaml"

// AuthzProofLedger lists the concrete scoped-route tests that prove each live
// authorization-catalog permission family in both grant modes.
type AuthzProofLedger struct {
	Version   string               `json:"version" yaml:"version"`
	Scenarios []AuthzProofScenario `json:"scenarios" yaml:"scenarios"`
}

// AuthzProofScenario binds one permission family and grant mode to the test that
// proves the scoped route behavior. RouteSamples are reader-facing breadcrumbs
// for the coverage dashboard and review packet; the named test is the executable
// proof.
type AuthzProofScenario struct {
	Family       string   `json:"family" yaml:"family"`
	GrantMode    string   `json:"grant_mode" yaml:"grant_mode"`
	ProofGate    string   `json:"proof_gate" yaml:"proof_gate"`
	TestFile     string   `json:"test_file" yaml:"test_file"`
	TestName     string   `json:"test_name" yaml:"test_name"`
	RouteSamples []string `json:"route_samples" yaml:"route_samples"`
}

// LoadAuthzProofLedger reads the authorization replay proof ledger. A missing
// ledger is an empty ledger so the replay-coverage gate reports uncovered or
// unresolved authz surfaces rather than failing before it can print the worklist.
func LoadAuthzProofLedger(path string) (AuthzProofLedger, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the repo-owned proof ledger under specs/.
	if err != nil {
		if os.IsNotExist(err) {
			return AuthzProofLedger{}, nil
		}
		return AuthzProofLedger{}, fmt.Errorf("read authorization replay proof ledger %s: %w", path, err)
	}
	var ledger AuthzProofLedger
	if err := yaml.Unmarshal(raw, &ledger); err != nil {
		return AuthzProofLedger{}, fmt.Errorf("parse authorization replay proof ledger %s: %w", path, err)
	}
	return ledger, nil
}

func (ledger AuthzProofLedger) scenario(ref string) (AuthzProofScenario, bool, error) {
	family, mode, ok := strings.Cut(strings.TrimSpace(ref), ":")
	if !ok || strings.TrimSpace(family) == "" || strings.TrimSpace(mode) == "" {
		return AuthzProofScenario{}, false, fmt.Errorf("authz ref %q must be <family>:<grant_mode>", ref)
	}
	family = strings.TrimSpace(family)
	mode = strings.TrimSpace(mode)
	if !validAuthorizationGrantMode(mode) {
		return AuthzProofScenario{}, false, fmt.Errorf("authz ref %q uses unknown grant mode %q", ref, mode)
	}
	for _, scenario := range ledger.Scenarios {
		if strings.TrimSpace(scenario.Family) == family && strings.TrimSpace(scenario.GrantMode) == mode {
			return scenario, true, nil
		}
	}
	return AuthzProofScenario{}, false, nil
}

func validAuthorizationGrantMode(mode string) bool {
	for _, valid := range authorizationGrantModes {
		if mode == valid {
			return true
		}
	}
	return false
}
