// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// supplyChainImpactProofRequiredEnv turns the supply-chain impact live proofs'
// unset-DSN skip into a failure. The reducer contention gate sets it, so a
// renamed DSN variable cannot silently turn these proofs into skips in CI.
const supplyChainImpactProofRequiredEnv = "ESHU_REQUIRE_SUPPLY_CHAIN_IMPACT_PROOF"

// TestSupplyChainImpactLiveProofsRunInTheReducerContentionGate is the
// enrollment guard for the supply-chain impact retraction live proofs (#7141).
// Those proofs read ESHU_POSTGRES_TEST_DSN and skip without it, and they sat
// in the scheduled lane, so a change to the writer, the retraction, the
// fencing admission or the capped-evidence drain could merge without any of
// them running. The reducer contention gate runs a Postgres service on every
// pull request that touches the storage or reducer trees, so they run there.
//
// The coupling is a test NAME matching a regex in a YAML file, which nothing
// else checks, so this test reads the real workflow and the real test files:
// every proof declared in a supply-chain impact live file must be selected by
// the gate's -run filter, and the gate must supply the DSN and the
// skip-is-failure switch. It needs no database and runs everywhere.
func TestSupplyChainImpactLiveProofsRunInTheReducerContentionGate(t *testing.T) {
	t.Parallel()

	workflowPath := filepath.Join("..", "..", "..", "..", ".github", "workflows", "reducer-contention-gate.yml")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read %s: %v", workflowPath, err)
	}
	if !bytes.Contains(workflow, []byte("ESHU_POSTGRES_TEST_DSN:")) {
		t.Fatalf("%s no longer passes ESHU_POSTGRES_TEST_DSN: the supply-chain impact live proofs would skip in CI", workflowPath)
	}
	if !bytes.Contains(workflow, []byte(supplyChainImpactProofRequiredEnv+": \"1\"")) {
		t.Fatalf("%s must set %s=1 so an unset DSN fails the supply-chain impact proofs instead of skipping them", workflowPath, supplyChainImpactProofRequiredEnv)
	}
	helper, err := os.ReadFile("supply_chain_impact_live_dsn_test.go")
	if err != nil {
		t.Fatalf("read the live DSN helper: %v", err)
	}
	if !helperReadsRequiredEnv(helper) {
		t.Fatalf("the live DSN helper no longer reads %s in code; the skip-is-failure switch is dead", supplyChainImpactProofRequiredEnv)
	}

	proofs := supplyChainImpactLiveProofs(t)
	filter := reducerContentionGateRunFilter(t, string(workflow))
	if missing := proofsMissingFromFilter(t, filter, proofs); len(missing) > 0 {
		t.Fatalf("the reducer contention gate's -run filter %q does not select the supply-chain impact live proofs %v", filter, missing)
	}
}

// TestProofsMissingFromFilterRejectsAnUnselectedProof is the seeded RED/GREEN
// pair for the enrollment guard: a filter that omits a proof must be reported,
// and a filter that selects every proof must report nothing. Without the RED
// half the guard above could pass while checking nothing.
func TestProofsMissingFromFilterRejectsAnUnselectedProof(t *testing.T) {
	t.Parallel()

	proofs := []string{"TestSupplyChainImpactWriterRetractsSupersededFindingsLive", "TestSupplyChainImpactCappedScopeConvergesLive"}

	red := proofsMissingFromFilter(t, "^(TestSomethingElseLive)", proofs)
	if len(red) != len(proofs) {
		t.Fatalf("RED: a filter selecting none of the proofs reported %v, want all %d", red, len(proofs))
	}
	partial := proofsMissingFromFilter(t, "^(TestSupplyChainImpactWriter[A-Za-z]*Live)", proofs)
	if len(partial) != 1 || partial[0] != "TestSupplyChainImpactCappedScopeConvergesLive" {
		t.Fatalf("RED: a filter missing the capped-scope proof reported %v, want only that proof", partial)
	}
	// The skip-is-failure switch must be read in code: a helper that only
	// mentions the variable in a comment leaves the switch dead.
	if !helperReadsRequiredEnv([]byte(`if os.Getenv("ESHU_REQUIRE_SUPPLY_CHAIN_IMPACT_PROOF") == "1" {`)) {
		t.Fatal("GREEN: the real switch read was not recognized")
	}
	if helperReadsRequiredEnv([]byte("// ESHU_REQUIRE_SUPPLY_CHAIN_IMPACT_PROOF is \"1\" in CI\nif os.Getenv(\"X\") == \"1\" {")) {
		t.Fatal("RED: a comment mentioning the switch satisfied the check")
	}
	if green := proofsMissingFromFilter(t, "^(TestSupplyChainImpact[A-Za-z]*Live)", proofs); len(green) != 0 {
		t.Fatalf("GREEN: a filter selecting every proof reported %v, want none", green)
	}
}

// requiredEnvRead matches the helper reading the switch from the environment.
// Matching the call, not the name, keeps a comment that mentions the variable
// from satisfying the check.
var requiredEnvRead = regexp.MustCompile(`os\.Getenv\("` + supplyChainImpactProofRequiredEnv + `"\)`)

// helperReadsRequiredEnv reports whether the helper source reads the
// skip-is-failure switch in code.
func helperReadsRequiredEnv(source []byte) bool {
	return requiredEnvRead.Match(source)
}

// proofsMissingFromFilter returns the proofs the -run filter does not select.
func proofsMissingFromFilter(t *testing.T, filter string, proofs []string) []string {
	t.Helper()

	selects, err := regexp.Compile(filter)
	if err != nil {
		t.Fatalf("compile the -run filter %q: %v", filter, err)
	}
	var missing []string
	for _, name := range proofs {
		if !selects.MatchString(name) {
			missing = append(missing, name)
		}
	}
	return missing
}

// supplyChainImpactLiveFileGlobs are the test files whose proofs exercise the
// supply-chain impact writer, retraction, fencing admission or capped-evidence
// drain against real Postgres. The suppression end-to-end proof is not listed:
// it is tracked separately (#7493).
var supplyChainImpactLiveFileGlobs = []string{
	"supply_chain_impact_*_live_test.go",
	"installed_advisory_targets_paging_live_test.go",
}

// supplyChainImpactLiveProofs discovers every Test function declared in a
// matching live test file, so a new proof is enrolled by being written, not by
// someone remembering to edit the workflow.
func supplyChainImpactLiveProofs(t *testing.T) []string {
	t.Helper()

	testFunc := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	var names []string
	for _, glob := range supplyChainImpactLiveFileGlobs {
		files, err := filepath.Glob(glob)
		if err != nil {
			t.Fatalf("glob %s: %v", glob, err)
		}
		if len(files) == 0 {
			t.Fatalf("no file matches %s: the discovery scan is broken", glob)
		}
		for _, file := range files {
			body, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			for _, m := range testFunc.FindAllSubmatch(body, -1) {
				name := string(m[1])
				if !strings.HasSuffix(name, "Live") {
					t.Fatalf("%s declares %s: a live proof must end in Live so the gate's filter selects it", file, name)
				}
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}
