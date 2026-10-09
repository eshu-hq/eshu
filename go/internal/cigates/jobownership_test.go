// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// ownershipErrors runs the full drift check on a hermetic repo and returns
// only the findings the job-ownership check (drift check 13) produced, so a
// fixture does not have to satisfy every unrelated check to prove one rule.
func ownershipErrors(t *testing.T, root string, reg *cigates.Registry) []string {
	t.Helper()
	var out []string
	for _, err := range cigates.DriftCheck(root, reg) {
		if strings.Contains(err.Error(), "job ownership") {
			out = append(out, err.Error())
		}
	}
	return out
}

// ownershipGate returns a blocking gate that owns ci.job in workflow.
func ownershipGate(id, workflow, job string, checkNames ...string) cigates.Gate {
	g := gateWith(id, id, workflow)
	g.Blocking = true
	g.CI.Job = job
	g.CI.CheckNames = checkNames
	return g
}

const orphanWorkflow = "name: Orphan\non: [push]\njobs:\n" +
	"  owned:\n    name: owned-job\n    runs-on: ubuntu-latest\n    steps: []\n" +
	"  static-mirror:\n    name: static mirror\n    runs-on: ubuntu-latest\n    steps: []\n"

func TestJobOwnershipFlagsUnownedJobThenCleanWithOwnerRow(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a"), nil)
	writeWorkflow(t, root, "orphan.yml", orphanWorkflow)
	owned := ownershipGate("a", "orphan.yml", "owned-job")

	errs := ownershipErrors(t, root, minimalReg([]cigates.Gate{owned}, nil, nil))
	if len(errs) != 1 {
		t.Fatalf("want exactly one ownership finding for the unowned job, got %d: %v", len(errs), errs)
	}
	for _, want := range []string{"static-mirror", `"static mirror"`, "orphan.yml"} {
		if !strings.Contains(errs[0], want) {
			t.Errorf("finding must name %q so the fix is obvious, got: %s", want, errs[0])
		}
	}

	// Adding the owner row makes the tree clean.
	root = buildDriftRepo(t, minimalPreCommit("a")+hookEntry("b"), nil)
	writeWorkflow(t, root, "orphan.yml", orphanWorkflow)
	reg := minimalReg([]cigates.Gate{owned, ownershipGate("b", "orphan.yml", "static mirror")}, nil, nil)
	if errs := ownershipErrors(t, root, reg); len(errs) != 0 {
		t.Errorf("an owner row for the job must clear the finding, got: %v", errs)
	}
}

func TestJobOwnershipNeedsTargetOfOwnedJobIsCovered(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a"), nil)
	// prep <- changes <- owned: one direct and one transitive needs target.
	writeWorkflow(t, root, "chain.yml", "name: Chain\non: [push]\njobs:\n"+
		"  prep:\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  changes:\n    needs: prep\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  owned:\n    name: owned-job\n    needs: [changes]\n    runs-on: ubuntu-latest\n    steps: []\n")
	reg := minimalReg([]cigates.Gate{ownershipGate("a", "chain.yml", "owned-job")}, nil, nil)

	if errs := ownershipErrors(t, root, reg); len(errs) != 0 {
		t.Errorf("direct and transitive needs targets of an owned job are covered, got: %v", errs)
	}
}

func TestJobOwnershipAlwaysOwnerDoesNotCoverItsNeeds(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a"), nil)
	// always() runs the owner even when the dependency failed, so the
	// dependency failing would not turn the owned check red.
	writeWorkflow(t, root, "always.yml", "name: Always\non: [push]\njobs:\n"+
		"  dep:\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  owned:\n    name: owned-job\n    needs: dep\n    if: ${{ always() }}\n"+
		"    runs-on: ubuntu-latest\n    steps: []\n")
	reg := minimalReg([]cigates.Gate{ownershipGate("a", "always.yml", "owned-job")}, nil, nil)

	errs := ownershipErrors(t, root, reg)
	if len(errs) != 1 || !strings.Contains(errs[0], `"dep"`) {
		t.Errorf("a needs target of an always() owner is not covered; want one finding for dep, got: %v", errs)
	}
}

func TestJobOwnershipAlwaysOwnerThatReadsNeedsResultCoversThem(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a"), nil)
	// The go-race-complete shape: an always() aggregator that fails the job
	// when needs.<job>.result is not success gates that job, so it is covered.
	// A sibling it never reads stays uncovered.
	writeWorkflow(t, root, "aggregator.yml", "name: Aggregator\non: [push]\njobs:\n"+
		"  shard:\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  unread:\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  owned:\n    name: owned-job\n    needs: [shard, unread]\n    if: ${{ always() }}\n"+
		"    runs-on: ubuntu-latest\n    steps:\n"+
		"      - run: |\n"+
		"          result=\"${{ needs.shard.result }}\"\n"+
		"          test \"${result}\" = success\n")
	reg := minimalReg([]cigates.Gate{ownershipGate("a", "aggregator.yml", "owned-job")}, nil, nil)

	errs := ownershipErrors(t, root, reg)
	if len(errs) != 1 || !strings.Contains(errs[0], `"unread"`) {
		t.Errorf("only the needs target the always() owner reads is covered; want one finding for unread, got: %v", errs)
	}
}

func TestJobOwnershipMatrixJobs(t *testing.T) {
	t.Parallel()

	const matrix = "name: Matrix\non: [push]\njobs:\n" +
		"  cell:\n    name: cell (${{ matrix.shard }}/2)\n    runs-on: ubuntu-latest\n" +
		"    strategy:\n      matrix:\n        shard: [1, 2]\n    steps: []\n"

	t.Run("owned through check_names", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a"), nil)
		writeWorkflow(t, root, "matrix.yml", matrix)
		g := ownershipGate("a", "matrix.yml", "cell", "cell (1/2)", "cell (2/2)")
		if errs := ownershipErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
			t.Errorf("check_names must own the templated matrix job, got: %v", errs)
		}
	})
	t.Run("owned through the job key", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a"), nil)
		writeWorkflow(t, root, "matrix.yml", matrix)
		g := ownershipGate("a", "matrix.yml", "cell")
		if errs := ownershipErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
			t.Errorf("the job key is the umbrella name of a templated job, got: %v", errs)
		}
	})
	t.Run("unowned when another job is owned", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a"), nil)
		writeWorkflow(t, root, "matrix.yml", matrix+"  other:\n    name: other\n    runs-on: ubuntu-latest\n    steps: []\n")
		g := ownershipGate("a", "matrix.yml", "other")
		errs := ownershipErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil))
		if len(errs) != 1 || !strings.Contains(errs[0], `"cell"`) {
			t.Errorf("an unowned matrix job must be reported by its key, got: %v", errs)
		}
	})
}

func TestJobOwnershipNameOverrideIsOwnedByNameNotKey(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a"), nil)
	writeWorkflow(t, root, "named.yml", "name: Named\non: [push]\njobs:\n"+
		"  mcp-tool-count:\n    name: Verify ReadOnlyTools count\n    runs-on: ubuntu-latest\n    steps: []\n")

	byName := ownershipGate("a", "named.yml", "Verify ReadOnlyTools count")
	if errs := ownershipErrors(t, root, minimalReg([]cigates.Gate{byName}, nil, nil)); len(errs) != 0 {
		t.Errorf("a gate naming the display name owns the job, got: %v", errs)
	}

	// await matches the check NAME, so a gate naming the key does not own a job
	// whose GitHub check is called by its display name.
	byKey := ownershipGate("a", "named.yml", "mcp-tool-count")
	if errs := ownershipErrors(t, root, minimalReg([]cigates.Gate{byKey}, nil, nil)); len(errs) != 1 {
		t.Errorf("a gate naming only the job key must not own a name-overridden job, got: %v", errs)
	}
}

func TestJobOwnershipAppendGateDisplayOwnsDispatchJob(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a"), nil)
	writeWorkflow(t, root, "dispatch.yml", "name: Dispatch\non: [push]\njobs:\n"+
		"  gate:\n    name: ${{ matrix.display }}\n    runs-on: ubuntu-latest\n    steps:\n"+
		"      - run: |\n"+
		"          append_gate \"${{ steps.filter.outputs.x }}\" \"x\" \"Verify X gate\" \"cmd\" \"cmd\"\n")
	g := ownershipGate("a", "dispatch.yml", "Verify X gate")
	if errs := ownershipErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil)); len(errs) != 0 {
		t.Errorf("an append_gate display owns the dispatch job, got: %v", errs)
	}
}

func TestJobOwnershipOnlyBlockingWorkflowsAreInScope(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a"), nil)
	writeWorkflow(t, root, "orphan.yml", orphanWorkflow)
	advisory := ownershipGate("a", "orphan.yml", "owned-job")
	advisory.Blocking = false
	if errs := ownershipErrors(t, root, minimalReg([]cigates.Gate{advisory}, nil, nil)); len(errs) != 0 {
		t.Errorf("a workflow no blocking gate names is out of scope, got: %v", errs)
	}
}

func TestJobOwnershipAdvisoryRowCountsAsOwnerInBlockingWorkflow(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a")+hookEntry("b"), nil)
	writeWorkflow(t, root, "orphan.yml", orphanWorkflow)
	advisory := ownershipGate("b", "orphan.yml", "static mirror")
	advisory.Blocking = false
	reg := minimalReg([]cigates.Gate{ownershipGate("a", "orphan.yml", "owned-job"), advisory}, nil, nil)
	if errs := ownershipErrors(t, root, reg); len(errs) != 0 {
		t.Errorf("an explicit advisory row is a deliberate owner decision, got: %v", errs)
	}
}

func TestJobOwnershipRequiredStatusCheckJobIsOwned(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a"), nil)
	writeWorkflow(t, root, "orphan.yml", orphanWorkflow)
	reg := minimalReg([]cigates.Gate{ownershipGate("a", "orphan.yml", "owned-job")}, nil, nil)
	reg.RequiredStatusChecks = []cigates.RequiredStatusCheck{{
		Context:       "static-mirror-complete",
		Workflow:      "orphan.yml",
		Job:           "static mirror",
		IntegrationID: 15368,
	}}
	if errs := ownershipErrors(t, root, reg); len(errs) != 0 {
		t.Errorf("a job the ruleset requires directly is owned, got: %v", errs)
	}
}

func TestJobOwnershipUnparseableBlockingWorkflowIsAnError(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a"), nil)
	writeWorkflow(t, root, "broken.yml", "name: Broken\njobs: [unterminated\n")
	g := ownershipGate("a", "broken.yml", "x")
	errs := ownershipErrors(t, root, minimalReg([]cigates.Gate{g}, nil, nil))
	if len(errs) != 1 || !strings.Contains(errs[0], "cannot be parsed") {
		t.Errorf("an unverifiable workflow must not read as owned, got: %v", errs)
	}
}

func TestJobOwnershipFindingsAreSorted(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a"), nil)
	writeWorkflow(t, root, "many.yml", "name: Many\non: [push]\njobs:\n"+
		"  zeta:\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  alpha:\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  owned:\n    name: owned-job\n    runs-on: ubuntu-latest\n    steps: []\n")
	reg := minimalReg([]cigates.Gate{ownershipGate("a", "many.yml", "owned-job")}, nil, nil)
	errs := ownershipErrors(t, root, reg)
	if len(errs) != 2 || !strings.Contains(errs[0], `"alpha"`) || !strings.Contains(errs[1], `"zeta"`) {
		t.Errorf("findings must be deterministic and sorted by job key, got: %v", errs)
	}
}

// hookEntry returns one more local hook entry to append to a minimalPreCommit
// config so a fixture with two gates satisfies the hook-registration check.
func hookEntry(id string) string {
	return `      - id: ` + id + `
        name: test hook
        entry: scripts/check.sh
        language: script
        pass_filenames: false
`
}
