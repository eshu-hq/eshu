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
