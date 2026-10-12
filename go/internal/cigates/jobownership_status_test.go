// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// statusWorkflow is an owner job with the given if: that needs dep. readsDep
// adds a step that reads needs.dep.result.
func statusWorkflow(ifExpr string, readsDep bool) string {
	wf := "name: Status\non: [push]\njobs:\n" +
		"  dep:\n    runs-on: ubuntu-latest\n    steps: []\n" +
		"  owned:\n    name: owned-job\n    needs: dep\n"
	if ifExpr != "" {
		// Double-quoted so a leading `!` is not read as a YAML tag.
		wf += "    if: \"" + ifExpr + "\"\n"
	}
	wf += "    runs-on: ubuntu-latest\n    steps:\n"
	if readsDep {
		wf += "      - run: test \"${{ needs.dep.result }}\" = success\n"
	} else {
		wf += "      - run: echo hi\n"
	}
	return wf
}

// TestJobOwnershipStatusOverrideShapes proves every job-level condition that
// disables the implicit success() is treated as an override. GitHub applies a
// default success() "unless you include one of" the status check functions
// (docs: Contexts and expressions, "Status check functions"), so an owner that
// calls any of them runs after a failed dependency and covers only a
// dependency whose result it reads.
func TestJobOwnershipStatusOverrideShapes(t *testing.T) {
	t.Parallel()

	overrides := map[string]string{
		"always":                   "${{ always() }}",
		"always mixed case":        "${{ Always() }}",
		"always with condition":    "${{ always() && github.event_name == 'push' }}",
		"not cancelled":            "${{ !cancelled() }}",
		"not cancelled bare":       "!cancelled()",
		"success or failure":       "success() || failure()",
		"failure":                  "failure()",
		"cancelled":                "cancelled()",
		"not success":              "${{ !success() }}",
		"success or event":         "success() || github.event_name == 'push'",
		"negated group":            "!(success() && github.event_name == 'push')",
		"failure wrapped in group": "(failure() && github.event_name == 'push')",
	}
	for name, ifExpr := range overrides {
		t.Run("override/"+name, func(t *testing.T) {
			t.Parallel()
			root := buildDriftRepo(t, minimalPreCommit("a"), nil)
			writeWorkflow(t, root, "s.yml", statusWorkflow(ifExpr, false))
			reg := minimalReg([]cigates.Gate{ownershipGate("a", "s.yml", "owned-job")}, nil, nil)
			errs := ownershipErrors(t, root, reg)
			if len(errs) != 1 || !strings.Contains(errs[0], `"dep"`) {
				t.Errorf("if: %s runs after a failed dependency; dep it does not read is uncovered, got: %v", ifExpr, errs)
			}

			root = buildDriftRepo(t, minimalPreCommit("a"), nil)
			writeWorkflow(t, root, "s.yml", statusWorkflow(ifExpr, true))
			if errs := ownershipErrors(t, root, reg); len(errs) != 0 {
				t.Errorf("if: %s reading needs.dep.result covers dep, got: %v", ifExpr, errs)
			}
		})
	}

	implicit := map[string]string{
		"no if":                  "",
		"bare success":           "success()",
		"success and condition":  "${{ success() && github.event_name == 'push' }}",
		"condition and success":  "github.event_name == 'push' && success()",
		"plain condition":        "${{ github.event_name == 'push' }}",
		"needs output condition": "${{ needs.dep.outputs.run == 'true' }}",
		"not equal condition":    "github.ref != 'refs/heads/main'",
	}
	for name, ifExpr := range implicit {
		t.Run("implicit/"+name, func(t *testing.T) {
			t.Parallel()
			root := buildDriftRepo(t, minimalPreCommit("a"), nil)
			writeWorkflow(t, root, "s.yml", statusWorkflow(ifExpr, false))
			reg := minimalReg([]cigates.Gate{ownershipGate("a", "s.yml", "owned-job")}, nil, nil)
			if errs := ownershipErrors(t, root, reg); len(errs) != 0 {
				t.Errorf("if: %q keeps the implicit success(), so a failed dependency skips the owner and dep is covered, got: %v", ifExpr, errs)
			}
		})
	}
}

// TestJobOwnershipOverrideAggregatorDoesNotCoverThroughASkippableNeed pins the
// hazard .github/workflows/test.yml documents above go-race-complete: when an
// override aggregator reads needs.mid.result and mid is skipped because ITS
// dependency failed, an aggregator that accepts `skipped` goes green. Reading
// mid covers mid only, never mid's own dependencies.
func TestJobOwnershipOverrideAggregatorDoesNotCoverThroughASkippableNeed(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a"), nil)
	writeWorkflow(t, root, "chain.yml", "name: Chain\non: [push]\njobs:\n"+
		"  dep:\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  mid:\n    needs: dep\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  owned:\n    name: owned-job\n    needs: mid\n    if: ${{ always() }}\n"+
		"    runs-on: ubuntu-latest\n    steps:\n"+
		"      - run: |\n"+
		"          result=\"${{ needs.mid.result }}\"\n"+
		"          test \"${result}\" = success || test \"${result}\" = skipped\n")
	reg := minimalReg([]cigates.Gate{ownershipGate("a", "chain.yml", "owned-job")}, nil, nil)

	errs := ownershipErrors(t, root, reg)
	if len(errs) != 1 || !strings.Contains(errs[0], `"dep"`) {
		t.Errorf("dep is behind a skippable need of an always() aggregator; want one finding for dep, got: %v", errs)
	}
}

// TestJobOwnershipOverrideAggregatorCoversEachNeedItReadsDirectly keeps the
// go-race-complete shape green: both needs are read directly, so the
// transitive edge between them adds nothing and nothing is flagged.
func TestJobOwnershipOverrideAggregatorCoversEachNeedItReadsDirectly(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a"), nil)
	writeWorkflow(t, root, "race.yml", "name: Race\non: [push]\njobs:\n"+
		"  changes:\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  race:\n    needs: changes\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  owned:\n    name: owned-job\n    needs: [changes, race]\n    if: ${{ always() }}\n"+
		"    runs-on: ubuntu-latest\n    steps:\n"+
		"      - run: |\n"+
		"          test \"${{ needs.changes.result }}\" = success\n"+
		"          test \"${{ needs.race.result }}\" = success\n")
	reg := minimalReg([]cigates.Gate{ownershipGate("a", "race.yml", "owned-job")}, nil, nil)
	if errs := ownershipErrors(t, root, reg); len(errs) != 0 {
		t.Errorf("an aggregator reading every need it lists covers them all, got: %v", errs)
	}
}

// gatingWorkflow is an owner job with the given job-level if: that needs dep,
// with the given steps block. It pins issue #7882: only a result read that
// gates the job covers the dependency.
func gatingWorkflow(jobIf, steps string) string {
	wf := "name: Gating\non: [push]\njobs:\n" +
		"  dep:\n    runs-on: ubuntu-latest\n    steps: []\n" +
		"  owned:\n    name: owned-job\n    needs: dep\n"
	if jobIf != "" {
		// Double-quoted so a leading `!` is not read as a YAML tag.
		wf += "    if: \"" + jobIf + "\"\n"
	}
	return wf + "    runs-on: ubuntu-latest\n    steps:\n" + steps
}

// TestJobOwnershipResultReadMustGate pins #7882: a mention of
// needs.<job>.result that does not gate the job leaves the dependency
// uncovered, while a read in the job if:, a shell test, or a value a shell
// test branches on covers it. A step-level if: is not a gate: skipping one
// step leaves the job green, so the dependency stays uncovered.
func TestJobOwnershipResultReadMustGate(t *testing.T) {
	t.Parallel()

	const always = "${{ always() }}"
	cases := map[string]struct {
		jobIf   string
		steps   string
		covered bool
	}{
		"echo bare mention is not a gate": {
			jobIf:   always,
			steps:   "      - run: echo needs.dep.result\n",
			covered: false,
		},
		"echo interpolated mention is not a gate": {
			jobIf:   always,
			steps:   "      - run: echo \"${{ needs.dep.result }}\"\n",
			covered: false,
		},
		"grep pattern is not a gate": {
			jobIf:   always,
			steps:   "      - run: grep \"needs.dep.result\" file.txt\n",
			covered: false,
		},
		"comment only is not a gate": {
			jobIf: always,
			steps: "      # needs.dep.result is checked by the aggregator\n" +
				"      - run: echo hi\n",
			covered: false,
		},
		"no mention is not a gate": {
			jobIf:   always,
			steps:   "      - run: echo hi\n",
			covered: false,
		},
		"bare job if gates": {
			jobIf:   "always() && needs.dep.result == 'success'",
			steps:   "      - run: echo hi\n",
			covered: true,
		},
		"interpolated job if gates": {
			jobIf:   "${{ always() && needs.dep.result == 'success' }}",
			steps:   "      - run: echo hi\n",
			covered: true,
		},
		"shell test gates": {
			jobIf:   always,
			steps:   "      - run: test \"${{ needs.dep.result }}\" = success\n",
			covered: true,
		},
		"shell test on outcome gates": {
			jobIf:   always,
			steps:   "      - run: test \"${{ needs.dep.outcome }}\" = success\n",
			covered: true,
		},
		"shell variable branched on gates": {
			jobIf: always,
			steps: "      - run: |\n" +
				"          result=\"${{ needs.dep.result }}\"\n" +
				"          test \"$result\" = success\n",
			covered: true,
		},
		"shell variable echoed does not gate": {
			jobIf: always,
			steps: "      - run: |\n" +
				"          result=\"${{ needs.dep.result }}\"\n" +
				"          echo \"$result\"\n",
			covered: false,
		},
		"env value branched on gates": {
			jobIf: always,
			steps: "      - env:\n" +
				"          DEP_RESULT: ${{ needs.dep.result }}\n" +
				"        run: test \"$DEP_RESULT\" = success\n",
			covered: true,
		},
		"env value echoed does not gate": {
			jobIf: always,
			steps: "      - env:\n" +
				"          DEP_RESULT: ${{ needs.dep.result }}\n" +
				"        run: echo \"$DEP_RESULT\"\n",
			covered: false,
		},
		"repo script argument gates": {
			jobIf:   always,
			steps:   "      - run: bash scripts/lib/check.sh \"${{ needs.dep.result }}\"\n",
			covered: true,
		},
		"echoed script path does not gate": {
			jobIf:   always,
			steps:   "      - run: echo \"scripts/lib/check.sh ${{ needs.dep.result }}\"\n",
			covered: false,
		},
		"step if does not gate the job": {
			jobIf: always,
			steps: "      - if: ${{ needs.dep.result == 'success' }}\n" +
				"        run: echo hi\n",
			covered: false,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := buildDriftRepo(t, minimalPreCommit("a"), nil)
			writeWorkflow(t, root, "g.yml", gatingWorkflow(tc.jobIf, tc.steps))
			reg := minimalReg([]cigates.Gate{ownershipGate("a", "g.yml", "owned-job")}, nil, nil)
			errs := ownershipErrors(t, root, reg)
			if tc.covered && len(errs) != 0 {
				t.Errorf("want dep covered, got: %v", errs)
			}
			if !tc.covered && (len(errs) != 1 || !strings.Contains(errs[0], `"dep"`)) {
				t.Errorf("want one finding for dep, got: %v", errs)
			}
		})
	}
}
