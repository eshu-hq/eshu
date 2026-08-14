// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"os"
	"path/filepath"
	"testing"
)

// Fixture drive for the scanners in doc_lockstep_trigger_path_test.go, split
// out the way doc_lockstep_switch_fixtures_test.go is split from
// doc_lockstep_switch_test.go. Every case here is a shape that once passed, or
// a neighbour of one, run over a throwaway package on disk so the scanner under
// test reads a real file the way it reads this one.

// writeTriggerFixture writes body as the sole non-test file of a fresh
// directory.
func writeTriggerFixture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte("package fixture\n\n"+body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return dir
}

// TestDocLockstepTriggerPathScannersReportRewrites is the negative half. Each
// fixture is one of the two rewrites that widened the accepted set with
// triggerAllowed untouched, plus the neighbouring shapes.
func TestDocLockstepTriggerPathScannersReportRewrites(t *testing.T) {
	t.Parallel()

	const claudeMerge = "func MergeClaudePreToolUseInput(input *Input, payload ClaudePreToolUseInput) {\n" +
		"\tinput.Trigger = triggerFromClaudeTool(payload.ToolName)\n}\n"
	const baseOut = "func baseOutput(input Input) Output {\n\treturn Output{Trigger: input.Trigger}\n}\n"

	writeCases := []struct {
		name    string
		body    string
		wantBad int
	}{
		{
			name: "clean_package_reports_nothing",
			body: "func normalizeInput(input Input) Input {\n" +
				"\tinput.Trigger = strings.ToLower(strings.TrimSpace(input.Trigger))\n\treturn input\n}\n\n" +
				baseOut + "\n" + claudeMerge,
			wantBad: 0,
		},
		{
			name: "normalize_remaps_a_class",
			body: "func normalizeInput(input Input) Input {\n" +
				"\tinput.Trigger = strings.ToLower(strings.TrimSpace(input.Trigger))\n" +
				"\tinput.Trigger = canonicalTrigger(input.Trigger)\n\treturn input\n}\n\n" +
				baseOut + "\n" + claudeMerge,
			wantBad: 1,
		},
		{
			name: "normalize_writes_a_literal",
			body: "func normalizeInput(input Input) Input {\n\tinput.Trigger = \"read\"\n\treturn input\n}\n\n" +
				baseOut + "\n" + claudeMerge,
			wantBad: 1,
		},
		{
			name: "an_unlisted_function_writes_the_trigger",
			body: "func normalizeInput(input Input) Input {\n" +
				"\tinput.Trigger = strings.TrimSpace(input.Trigger)\n\treturn input\n}\n\n" +
				baseOut + "\n" + claudeMerge + "\n" +
				"func widen(input *Input) {\n\tinput.Trigger = \"read\"\n}\n",
			wantBad: 1,
		},
		{
			name: "base_output_rewrites_the_wire_trigger",
			body: "func normalizeInput(input Input) Input {\n" +
				"\tinput.Trigger = strings.TrimSpace(input.Trigger)\n\treturn input\n}\n\n" +
				"func baseOutput(input Input) Output {\n\treturn Output{Trigger: canonicalTrigger(input.Trigger)}\n}\n\n" +
				claudeMerge,
			wantBad: 1,
		},
		{
			name: "merge_bypasses_the_tool_mapping",
			body: "func normalizeInput(input Input) Input {\n" +
				"\tinput.Trigger = strings.TrimSpace(input.Trigger)\n\treturn input\n}\n\n" + baseOut + "\n" +
				"func MergeClaudePreToolUseInput(input *Input, payload ClaudePreToolUseInput) {\n" +
				"\tinput.Trigger = strings.ToLower(payload.ToolName)\n}\n",
			wantBad: 1,
		},
		{
			name: "a_method_named_like_a_permitted_writer",
			body: "func normalizeInput(input Input) Input {\n" +
				"\tinput.Trigger = strings.TrimSpace(input.Trigger)\n\treturn input\n}\n\n" +
				baseOut + "\n" + claudeMerge + "\n" +
				"type widener struct{}\n\n" +
				"func (w widener) normalizeInput(input *Input) {\n\tinput.Trigger = \"read\"\n}\n",
			wantBad: 1,
		},
	}
	if len(writeCases) < 7 {
		t.Fatalf("trigger-write cases = %d, want one per shape that rewrites a class outside triggerAllowed", len(writeCases))
	}

	for _, tc := range writeCases {
		writes, err := scanTriggerWrites(writeTriggerFixture(t, tc.body), docTriggerWriters())
		if err != nil {
			t.Fatalf("%s: scan fixture: %v", tc.name, err)
		}
		bad := 0
		for _, write := range writes {
			if !write.OK {
				bad++
			}
		}
		if bad != tc.wantBad {
			t.Errorf("%s: %d bad writes out of %d, want %d", tc.name, bad, len(writes), tc.wantBad)
		}
	}

	gateCases := []struct {
		name      string
		body      string
		wantGates int
		wantSkips int
	}{
		{
			name: "gate_consults_trigger_allowed",
			body: "func Evaluate(input Input) Output {\n\tnormalized := normalizeInput(input)\n\tout := baseOutput(normalized)\n" +
				"\tswitch {\n\tcase !triggerAllowed(normalized.Trigger):\n\t\treturn skip(out, \"a\", \"b\")\n\t}\n\treturn out\n}\n",
			wantGates: 1,
		},
		{
			name: "gate_points_at_a_twin",
			body: "func Evaluate(input Input) Output {\n\tnormalized := normalizeInput(input)\n\tout := baseOutput(normalized)\n" +
				"\tswitch {\n\tcase !triggerEligible(normalized.Trigger):\n\t\treturn skip(out, \"a\", \"b\")\n\t}\n\treturn out\n}\n",
			wantGates: 0,
		},
		{
			name: "gate_argument_is_rewritten_in_place",
			body: "func Evaluate(input Input) Output {\n\tnormalized := normalizeInput(input)\n\tout := baseOutput(normalized)\n" +
				"\tswitch {\n\tcase !triggerAllowed(canonicalTrigger(normalized.Trigger)):\n\t\treturn skip(out, \"a\", \"b\")\n\t}\n\treturn out\n}\n",
			wantGates: 0,
		},
		{
			name: "a_clause_advises_instead_of_skipping",
			body: "func Evaluate(input Input) Output {\n\tnormalized := normalizeInput(input)\n\tout := baseOutput(normalized)\n" +
				"\tswitch {\n\tcase normalized.Trigger == \"list\":\n\t\treturn advise(out)\n" +
				"\tcase !triggerAllowed(normalized.Trigger):\n\t\treturn skip(out, \"a\", \"b\")\n\t}\n\treturn out\n}\n",
			wantGates: 1,
			wantSkips: 1,
		},
		{
			name: "gate_reads_a_rewritten_copy",
			body: "func Evaluate(input Input) Output {\n\tnormalized := normalizeInput(input)\n\tout := baseOutput(normalized)\n" +
				"\tgate := triggerGate{canonicalTrigger(normalized.Trigger)}\n" +
				"\tswitch {\n\tcase !triggerAllowed(gate.Trigger):\n\t\treturn skip(out, \"a\", \"b\")\n\t}\n\treturn out\n}\n",
			wantGates: 0,
		},
	}
	if len(gateCases) < 5 {
		t.Fatalf("gate cases = %d, want one per way the eligibility case stops asking triggerAllowed", len(gateCases))
	}

	for _, tc := range gateCases {
		gates, _, nonSkip, normalized, err := scanEvaluateTriggerGate(writeTriggerFixture(t, tc.body), "Evaluate")
		if err != nil {
			t.Fatalf("%s: scan fixture: %v", tc.name, err)
		}
		if normalized != "normalized" {
			t.Errorf("%s: normalizeInput's result binds to %q, want %q; every fixture carries that binding, so an empty result means the scanner stopped finding it", tc.name, normalized, "normalized")
		}
		if gates != tc.wantGates {
			t.Errorf("%s: gates = %d, want %d", tc.name, gates, tc.wantGates)
		}
		if nonSkip != tc.wantSkips {
			t.Errorf("%s: non-skip clauses = %d, want %d", tc.name, nonSkip, tc.wantSkips)
		}
	}
}
