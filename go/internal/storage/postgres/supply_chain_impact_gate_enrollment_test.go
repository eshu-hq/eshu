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
	step := proofStep(t, string(workflow))
	if !strings.Contains(step, "ESHU_POSTGRES_TEST_DSN:") {
		t.Fatalf("the step that runs the supply-chain impact proofs in %s no longer passes ESHU_POSTGRES_TEST_DSN: they would skip in CI", workflowPath)
	}
	if !strings.Contains(step, supplyChainImpactProofRequiredEnv+": \"1\"") {
		t.Fatalf("the step that runs the supply-chain impact proofs in %s must set %s=1 so an unset DSN fails them instead of skipping", workflowPath, supplyChainImpactProofRequiredEnv)
	}
	helper, err := os.ReadFile("supply_chain_impact_live_dsn_test.go")
	if err != nil {
		t.Fatalf("read the live DSN helper: %v", err)
	}
	if !helperFailsOnRequired(helper) {
		t.Fatalf("the live DSN helper no longer calls t.Fatalf for a supplyChainImpactLiveFail outcome (and t.Skip for a skip); an unset DSN would skip in CI again")
	}
	if !helperReadsRequiredEnv(helper) || !bytes.Contains(helper, []byte("supplyChainImpactLiveDecision(dsn, os.Getenv(")) {
		t.Fatalf("the live DSN helper no longer routes %s through supplyChainImpactLiveDecision (which TestSupplyChainImpactLiveDecision pins); the skip-is-failure switch is dead", supplyChainImpactProofRequiredEnv)
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

// TestProofStepScopesTheEnvChecksToTheProofStep is the seeded RED/GREEN pair
// for the step-scoped env check: the keys appearing in another step must not
// count for the step that runs the proofs.
func TestProofStepScopesTheEnvChecksToTheProofStep(t *testing.T) {
	t.Parallel()

	red := "jobs:\n    steps:\n      - name: Other\n        env:\n          ESHU_POSTGRES_TEST_DSN: x\n          " +
		supplyChainImpactProofRequiredEnv + ": \"1\"\n      - name: Run\n        run: go test ./internal/storage/postgres/ -run 'X'\n"
	step := proofStep(t, red)
	if strings.Contains(step, "ESHU_POSTGRES_TEST_DSN:") || strings.Contains(step, supplyChainImpactProofRequiredEnv) {
		t.Fatalf("RED: keys in another step leaked into the proof step: %q", step)
	}
	green := "jobs:\n    steps:\n      - name: Run\n        env:\n          ESHU_POSTGRES_TEST_DSN: x\n          " +
		supplyChainImpactProofRequiredEnv + ": \"1\"\n        run: go test ./internal/storage/postgres/ -run 'X'\n"
	step = proofStep(t, green)
	if !strings.Contains(step, "ESHU_POSTGRES_TEST_DSN:") || !strings.Contains(step, supplyChainImpactProofRequiredEnv+": \"1\"") {
		t.Fatalf("GREEN: the proof step lost its own env keys: %q", step)
	}
}

// failCase and skipCase match the helper acting on each outcome: Fail must
// call t.Fatalf and Skip must call t.Skip, so a helper that decides correctly and
// then skips anyway cannot pass.
var (
	failCase = regexp.MustCompile(`case supplyChainImpactLiveFail:\s*t\.Fatalf\(`)
	skipCase = regexp.MustCompile(`case supplyChainImpactLiveSkip:\s*t\.Skip\(`)
)

// helperFailsOnRequired reports whether the helper source acts on the decision.
func helperFailsOnRequired(source []byte) bool {
	return failCase.Match(source) && skipCase.Match(source)
}

// TestHelperFailsOnRequiredPinsTheOutcomeAction is the seeded pair: a helper that
// maps the fail outcome to t.Skipf must be rejected.
func TestHelperFailsOnRequiredPinsTheOutcomeAction(t *testing.T) {
	t.Parallel()

	good := []byte("case supplyChainImpactLiveFail:\n\t\tt.Fatalf(\"x\")\n\tcase supplyChainImpactLiveSkip:\n\t\tt.Skip(\"y\")\n")
	if !helperFailsOnRequired(good) {
		t.Fatal("GREEN: the real outcome mapping was not recognized")
	}
	bad := []byte("case supplyChainImpactLiveFail:\n\t\tt.Skipf(\"x\")\n\tcase supplyChainImpactLiveSkip:\n\t\tt.Skip(\"y\")\n")
	if helperFailsOnRequired(bad) {
		t.Fatal("RED: a helper that skips on the fail outcome was accepted")
	}
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

// directDSNRead matches a test reading the proof DSN from the environment
// itself, bypassing the helper that applies the required-proof switch.
var directDSNRead = regexp.MustCompile(`os\.(?:Getenv|LookupEnv)\("ESHU_POSTGRES_TEST_DSN"\)`)

// liveProofFiles returns the sorted test files that declare supply-chain impact
// live proofs, and the ones among them that also read the proof DSN directly. A
// file is a proof file when it calls an opener helper (see liveDSNOpeners) or
// calls supplyChainImpactLiveDSN itself, including from a Test function. The
// helper file that defines the mechanism is not a proof file.
func liveProofFiles(sources map[string][]byte) (files, mixed []string) {
	const helperFile = "supply_chain_impact_live_dsn_test.go"
	callees := []string{"supplyChainImpactLiveDSN"}
	for opener := range liveDSNOpeners(sources) {
		callees = append(callees, opener)
	}
	for file, body := range sources {
		if file == helperFile {
			continue
		}
		calls := false
		for _, callee := range callees {
			if bytes.Contains(body, []byte(callee+"(")) {
				calls = true
			}
		}
		if !calls {
			continue
		}
		files = append(files, file)
		if directDSNRead.Match(body) {
			mixed = append(mixed, file)
		}
	}
	sort.Strings(files)
	sort.Strings(mixed)
	return files, mixed
}

// TestLiveProofFilesCoversDirectCallersAndFlagsMixedFiles is the seeded pair for
// proof-file discovery: a Test function that calls the DSN helper directly makes
// its file a proof file, and a proof file that also reads the DSN itself is
// flagged; a file that never touches the helpers is out of scope.
func TestLiveProofFilesCoversDirectCallersAndFlagsMixedFiles(t *testing.T) {
	t.Parallel()

	sources := map[string][]byte{
		"supply_chain_impact_live_dsn_test.go": []byte("func supplyChainImpactLiveDSN(t int) {}\nfunc TestSupplyChainImpactLiveDecision(t int) {}\n"),
		"direct_test.go":                       []byte("func TestSupplyChainImpactDirectLive(t int) {\n\tsupplyChainImpactLiveDSN(t, \"x\")\n}\n"),
		"wrapped_test.go":                      []byte("func open(t int) {\n\tsupplyChainImpactLiveDSN(t, \"x\")\n}\nfunc TestWrappedLive(t int) {\n\topen(t)\n}\n"),
		"mixed_test.go":                        []byte("func TestMixedLive(t int) {\n\tsupplyChainImpactLiveDSN(t, \"x\")\n\t_ = os.Getenv(\"ESHU_POSTGRES_TEST_DSN\")\n}\n"),
		"unrelated_test.go":                    []byte("func TestOtherLive(t int) {\n\t_ = os.Getenv(\"ESHU_POSTGRES_TEST_DSN\")\n}\n"),
	}
	files, mixed := liveProofFiles(sources)
	want := []string{"direct_test.go", "mixed_test.go", "wrapped_test.go"}
	if len(files) != len(want) || files[0] != want[0] || files[1] != want[1] || files[2] != want[2] {
		t.Fatalf("proof files = %v, want %v (the helper file and an unrelated direct-DSN file are out of scope)", files, want)
	}
	if len(mixed) != 1 || mixed[0] != "mixed_test.go" {
		t.Fatalf("mixed-mechanism files = %v, want [mixed_test.go]", mixed)
	}
}

// liveDSNOpeners derives, from the package's own test sources, every helper a
// supply-chain impact live proof opens its database through: the functions that
// call supplyChainImpactLiveDSN, and, transitively, the functions that call one
// of those. Deriving the set (instead of listing names) means a new wrapper is
// covered the day it is written. sources maps file name to contents.
func liveDSNOpeners(sources map[string][]byte) map[string]bool {
	funcDecl := regexp.MustCompile(`(?m)^func (?:\([^)]*\) )?([A-Za-z0-9_]+)\(`)
	bodies := map[string]string{}
	for file, body := range sources {
		if file == "supply_chain_impact_gate_enrollment_test.go" {
			continue
		}
		text := string(body)
		locs := funcDecl.FindAllStringSubmatchIndex(text, -1)
		for i, loc := range locs {
			end := len(text)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			bodies[text[loc[2]:loc[3]]] = text[loc[0]:end]
		}
	}
	openers := map[string]bool{"supplyChainImpactLiveDSN": true}
	for changed := true; changed; {
		changed = false
		for name, body := range bodies {
			if openers[name] || strings.HasPrefix(name, "Test") || name == "supplyChainImpactLiveDecision" {
				continue
			}
			for opener := range openers {
				if strings.Contains(body, opener+"(") {
					openers[name] = true
					changed = true
					break
				}
			}
		}
	}
	delete(openers, "supplyChainImpactLiveDSN")
	return openers
}

// TestLiveDSNOpenersFollowsWrappersTransitively is the seeded RED/GREEN pair for
// opener discovery: a helper that only wraps another opener is itself an opener,
// and an unrelated helper is not.
func TestLiveDSNOpenersFollowsWrappersTransitively(t *testing.T) {
	t.Parallel()

	got := liveDSNOpeners(map[string][]byte{
		"a_test.go": []byte("func base(t int) {\n\tsupplyChainImpactLiveDSN(t, \"x\")\n}\n\nfunc wrapper(t int) {\n\tbase(t)\n}\n\nfunc outer(t int) {\n\twrapper(t)\n}\n\nfunc unrelated(t int) {\n\tother(t)\n}\n"),
	})
	for _, want := range []string{"base", "wrapper", "outer"} {
		if !got[want] {
			t.Errorf("GREEN: %s not discovered as an opener (got %v)", want, got)
		}
	}
	if got["unrelated"] {
		t.Errorf("RED: an unrelated helper was reported as an opener (got %v)", got)
	}
}

// proofStep returns the text of the workflow step that runs the Postgres proofs
// (the one holding the storage/postgres go test command), so the env checks
// cannot be satisfied by the same keys appearing in some other step.
func proofStep(t *testing.T, workflow string) string {
	t.Helper()

	const marker = "go test ./internal/storage/postgres/ -run"
	steps := regexp.MustCompile(`(?m)^      - name:`).Split(workflow, -1)
	for _, step := range steps {
		if strings.Contains(step, marker) {
			return step
		}
	}
	t.Fatalf("no workflow step runs %q; the guard cannot find the proof step", marker)
	return ""
}

// supplyChainImpactLiveProofs discovers every Test function declared in a
// matching live test file, so a new proof is enrolled by being written, not by
// someone remembering to edit the workflow.
func supplyChainImpactLiveProofs(t *testing.T) []string {
	t.Helper()

	const guardFile = "supply_chain_impact_gate_enrollment_test.go"
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("list package test files: %v", err)
	}
	sources := map[string][]byte{}
	for _, entry := range entries {
		file := entry.Name()
		if entry.IsDir() || file == guardFile || !strings.HasSuffix(file, "_test.go") {
			continue
		}
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		sources[file] = body
	}
	files, mixed := liveProofFiles(sources)
	if len(mixed) > 0 {
		t.Fatalf("%v open a supply-chain impact proof database through the helpers and also read ESHU_POSTGRES_TEST_DSN directly: route every DSN read through supplyChainImpactLiveDSN so the required-proof switch applies", mixed)
	}
	if len(files) == 0 {
		t.Fatal("no test file calls a supply-chain impact DSN opener: the discovery scan is broken")
	}
	testFunc := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	var names []string
	for _, file := range files {
		for _, m := range testFunc.FindAllSubmatch(sources[file], -1) {
			name := string(m[1])
			if !strings.HasSuffix(name, "Live") {
				t.Fatalf("%s declares %s: a live proof must end in Live so the gate's filter selects it", file, name)
			}
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
