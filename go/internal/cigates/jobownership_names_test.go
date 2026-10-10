// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// TestJobOwnershipFullyTemplatedNameIsNotOwnedByAnotherJobsName pins the
// name-resolution contract with `ci-gates await`: await waits for a check by
// its exact name, so a job whose whole name is a ${{ }} expression is owned
// only by a declared name that no other job already produces. The pattern
// `^.+$` of such a name fits every declared name, including the literal name
// of an unrelated job, which must not count.
func TestJobOwnershipFullyTemplatedNameIsNotOwnedByAnotherJobsName(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a"), nil)
	writeWorkflow(t, root, "rogue.yml", "name: Rogue\non: [push]\njobs:\n"+
		"  owned:\n    name: owned-job\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  rogue:\n    name: ${{ matrix.label }}\n    runs-on: ubuntu-latest\n"+
		"    strategy:\n      matrix:\n        label: [x]\n    steps: []\n")
	reg := minimalReg([]cigates.Gate{ownershipGate("a", "rogue.yml", "owned-job")}, nil, nil)

	errs := ownershipErrors(t, root, reg)
	if len(errs) != 1 || !strings.Contains(errs[0], `"rogue"`) {
		t.Errorf("a fully templated job is not owned by another job's literal name; want one finding for rogue, got: %v", errs)
	}
}

// TestJobOwnershipFullyTemplatedNameIsOwnedByUnclaimedDeclaredName keeps the
// append_gate dispatch shape green: the dispatch job's display names are
// declared by rows and are no other job's name.
func TestJobOwnershipFullyTemplatedNameIsOwnedByUnclaimedDeclaredName(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a")+hookEntry("b"), nil)
	writeWorkflow(t, root, "dispatch.yml", "name: Dispatch\non: [push]\njobs:\n"+
		"  owned:\n    name: owned-job\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  gate:\n    name: ${{ matrix.display }}\n    runs-on: ubuntu-latest\n    steps: []\n")
	reg := minimalReg([]cigates.Gate{
		ownershipGate("a", "dispatch.yml", "owned-job"),
		ownershipGate("b", "dispatch.yml", "Verify X gate"),
	}, nil, nil)
	if errs := ownershipErrors(t, root, reg); len(errs) != 0 {
		t.Errorf("a declared name no other job produces owns the dispatch job, got: %v", errs)
	}
}

// TestJobOwnershipCIJobIsDroppedWhenCheckNamesIsSet pins await's precedence
// (go/cmd/ci-gates/await.go resolveRequiredGateWorkflows): a row with a
// non-empty check_names list waits for those names only and ignores ci.job.
func TestJobOwnershipCIJobIsDroppedWhenCheckNamesIsSet(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a"), nil)
	writeWorkflow(t, root, "both.yml", "name: Both\non: [push]\njobs:\n"+
		"  a:\n    name: a-job\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  b:\n    name: b-job\n    runs-on: ubuntu-latest\n    steps: []\n")
	reg := minimalReg([]cigates.Gate{ownershipGate("a", "both.yml", "a-job", "b-job")}, nil, nil)

	errs := ownershipErrors(t, root, reg)
	if len(errs) != 1 || !strings.Contains(errs[0], `"a-job"`) {
		t.Errorf("ci.job is ignored when check_names is set, so a-job is unowned; got: %v", errs)
	}
}

// TestJobOwnershipAmbiguousTemplatedNameOwnsNoJob pins the fail-closed rule:
// a declared name that no job claims as a literal is owned by a templated job
// only when exactly one template fits it, or when it is a display an
// append_gate call in the steps of that job or of a job it needs names. When two or more templates
// fit, `ci-gates await` still waits for exactly one real check, so crediting
// any template (by literal length or otherwise) can hand the name to a job
// that never produces it and leave that job's real cells unblocked.
func TestJobOwnershipAmbiguousTemplatedNameOwnsNoJob(t *testing.T) {
	t.Parallel()

	const det = "  det:\n    name: Det (${{ matrix.b }})\n    runs-on: ubuntu-latest\n" +
		"    strategy:\n      matrix:\n        b: [neo4j]\n    steps: []\n"
	const owned = "  owned:\n    name: owned-job\n    runs-on: ubuntu-latest\n    steps: []\n"
	const rogue = "  rogue:\n    name: ${{ matrix.label }}\n    runs-on: ubuntu-latest\n" +
		"    strategy:\n      matrix:\n        label: [x]\n    steps: []\n"

	check := func(t *testing.T, jobs string, extra []cigates.Gate, want ...string) {
		t.Helper()
		root := buildDriftRepo(t, minimalPreCommit("a")+hookEntry("b"), nil)
		writeWorkflow(t, root, "p.yml", "name: P\non: [push]\njobs:\n"+jobs)
		reg := minimalReg(append([]cigates.Gate{ownershipGate("a", "p.yml", "owned-job")}, extra...), nil, nil)
		errs := ownershipErrors(t, root, reg)
		if len(errs) != len(want) {
			t.Fatalf("want findings for %v, got %d: %v", want, len(errs), errs)
		}
		for i, key := range want {
			if !strings.Contains(errs[i], `"`+key+`"`) {
				t.Errorf("finding %d must name job %q, got: %s", i, key, errs[i])
			}
		}
	}

	t.Run("a single fitting template owns its cell", func(t *testing.T) {
		t.Parallel()
		check(t, owned+det, []cigates.Gate{ownershipGate("b", "p.yml", "det", "Det (neo4j)")})
	})
	t.Run("rogue and matrix job both fit a cell, so neither owns it", func(t *testing.T) {
		t.Parallel()
		check(t, owned+det+rogue, []cigates.Gate{ownershipGate("b", "p.yml", "det", "Det (neo4j)")}, "det", "rogue")
	})
	// det still owns itself through Det (nornicdb), which only its template fits.
	t.Run("a partly templated newcomer cannot take another job's cells", func(t *testing.T) {
		t.Parallel()
		newcomer := "  newcomer:\n    name: Det (neo${{ matrix.v }})\n    runs-on: ubuntu-latest\n" +
			"    strategy:\n      matrix:\n        v: [5]\n    steps: []\n"
		check(t, owned+det+newcomer,
			[]cigates.Gate{ownershipGate("b", "p.yml", "det", "Det (neo4j)", "Det (nornicdb)")}, "newcomer")
	})
	t.Run("two templates fit a display without an append_gate call", func(t *testing.T) {
		t.Parallel()
		jobs := owned +
			"  gate:\n    name: ${{ matrix.display }}\n    runs-on: ubuntu-latest\n    steps: []\n" +
			"  rogue:\n    name: Rogue ${{ matrix.label }}\n    runs-on: ubuntu-latest\n    steps: []\n"
		check(t, jobs, []cigates.Gate{ownershipGate("b", "p.yml", "Rogue display gate")}, "gate", "rogue")
	})
	t.Run("equal literal text is a tie and owns neither job", func(t *testing.T) {
		t.Parallel()
		jobs := owned +
			"  left:\n    name: Run ${{ matrix.x }}\n    runs-on: ubuntu-latest\n    steps: []\n" +
			"  right:\n    name: Run ${{ matrix.y }}\n    runs-on: ubuntu-latest\n    steps: []\n"
		check(t, jobs, []cigates.Gate{ownershipGate("b", "p.yml", "Run one")}, "left", "right")
	})
	t.Run("a tied name does not stop the umbrella key from owning a job", func(t *testing.T) {
		t.Parallel()
		jobs := owned +
			"  left:\n    name: Run ${{ matrix.x }}\n    runs-on: ubuntu-latest\n    steps: []\n" +
			"  right:\n    name: Run ${{ matrix.y }}\n    runs-on: ubuntu-latest\n    steps: []\n"
		check(t, jobs, []cigates.Gate{ownershipGate("b", "p.yml", "left")}, "right")
	})
}

// TestJobOwnershipAppendGateDisplaysBelongToTheirDispatchJob is the real
// static-contract-gates.yml shape: a job builds the matrix with append_gate
// calls and the fully templated dispatch job needs it. A new matrix job whose template fits some of
// those displays must not take them, and is itself unowned.
func TestJobOwnershipAppendGateDisplaysBelongToTheirDispatchJob(t *testing.T) {
	t.Parallel()

	// The real shape: the append_gate calls live in the job that builds the
	// matrix, and the fully templated dispatch job needs it.
	const dispatch = "name: Dispatch\non: [push]\njobs:\n" +
		"  changes:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - run: |\n" +
		"          append_gate \"${{ steps.f.outputs.a }}\" \"a\" \"Verify Ifa alpha gate\" \"cmd\" \"cmd\"\n" +
		"          append_gate \"${{ steps.f.outputs.b }}\" \"b\" \"Verify other gate\" \"cmd\" \"cmd\"\n" +
		"  gate:\n    name: ${{ matrix.display }}\n    needs: changes\n    runs-on: ubuntu-latest\n    steps: []\n"
	gates := func() []cigates.Gate {
		return []cigates.Gate{
			ownershipGate("a", "p.yml", "Verify Ifa alpha gate"),
			ownershipGate("b", "p.yml", "Verify other gate"),
		}
	}

	root := buildDriftRepo(t, minimalPreCommit("a")+hookEntry("b"), nil)
	writeWorkflow(t, root, "p.yml", dispatch)
	if errs := ownershipErrors(t, root, minimalReg(gates(), nil, nil)); len(errs) != 0 {
		t.Fatalf("the dispatch job owns its append_gate displays, got: %v", errs)
	}

	root = buildDriftRepo(t, minimalPreCommit("a")+hookEntry("b"), nil)
	writeWorkflow(t, root, "p.yml", dispatch+
		"  verify-ifa-extra:\n    name: Verify Ifa ${{ matrix.check }} gate\n    runs-on: ubuntu-latest\n"+
		"    strategy:\n      matrix:\n        check: [newcheck]\n    steps:\n      - run: exit 1\n")
	errs := ownershipErrors(t, root, minimalReg(gates(), nil, nil))
	if len(errs) != 1 || !strings.Contains(errs[0], `"verify-ifa-extra"`) {
		t.Errorf("a newcomer whose template fits dispatch displays owns none of them; want one finding, got: %v", errs)
	}
}

// TestJobOwnershipTwoJobsNamingADisplayThroughAppendGateOwnNeither pins the
// branch where two templated jobs both qualify for one append_gate display: the
// legitimate dispatch job and a newcomer that also needs `changes`, so the
// calls in `changes` are visible to both. The display is then ambiguous and
// owns neither job. Crediting either of them (map order picks one) would hand
// the newcomer a display it does not produce, or strip the dispatch job of it.
func TestJobOwnershipTwoJobsNamingADisplayThroughAppendGateOwnNeither(t *testing.T) {
	t.Parallel()

	workflow := func(extraDisplays string) string {
		return "name: Dispatch\non: [push]\njobs:\n" +
			"  changes:\n    runs-on: ubuntu-latest\n    steps:\n" +
			"      - run: |\n" +
			"          append_gate \"${{ steps.f.outputs.a }}\" \"a\" \"Verify Ifa alpha gate\" \"cmd\" \"cmd\"\n" +
			extraDisplays +
			"  gate:\n    name: ${{ matrix.display }}\n    needs: changes\n    runs-on: ubuntu-latest\n    steps: []\n" +
			"  verify-ifa-extra:\n    name: Verify Ifa ${{ matrix.check }} gate\n    needs: changes\n" +
			"    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        check: [newcheck]\n" +
			"    steps:\n      - run: exit 1\n"
	}
	const otherDisplay = "          append_gate \"${{ steps.f.outputs.b }}\" \"b\" \"Verify other gate\" \"cmd\" \"cmd\"\n"

	t.Run("dispatch job keeps the displays only it fits", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a")+hookEntry("b"), nil)
		writeWorkflow(t, root, "p.yml", workflow(otherDisplay))
		reg := minimalReg([]cigates.Gate{
			ownershipGate("a", "p.yml", "Verify Ifa alpha gate"),
			ownershipGate("b", "p.yml", "Verify other gate"),
		}, nil, nil)
		errs := ownershipErrors(t, root, reg)
		if len(errs) != 1 || !strings.Contains(errs[0], `"verify-ifa-extra"`) {
			t.Errorf("only the newcomer is unowned; want one finding for verify-ifa-extra, got: %v", errs)
		}
	})
	t.Run("a display both jobs name leaves both unowned", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a"), nil)
		writeWorkflow(t, root, "p.yml", workflow(""))
		reg := minimalReg([]cigates.Gate{ownershipGate("a", "p.yml", "Verify Ifa alpha gate")}, nil, nil)
		errs := ownershipErrors(t, root, reg)
		// changes is flagged too: it is only reachable through the unowned jobs.
		if len(errs) != 3 || !strings.Contains(errs[0], `"changes"`) ||
			!strings.Contains(errs[1], `"gate"`) || !strings.Contains(errs[2], `"verify-ifa-extra"`) {
			t.Errorf("two qualifying jobs make the display ambiguous; want findings for changes, gate, verify-ifa-extra, got: %v", errs)
		}
	})
}

// TestJobOwnershipBroadNewcomerDoesNotUnownTheDispatchJob is the false-red side
// of the same shape: a newcomer whose template fits EVERY display (`Verify
// ${{ matrix.surface }} gate`) must not strip the dispatch job of the displays
// its needed job names in append_gate calls, or the finding would point at the
// legitimate gate job instead of the newcomer.
func TestJobOwnershipBroadNewcomerDoesNotUnownTheDispatchJob(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a")+hookEntry("b"), nil)
	writeWorkflow(t, root, "p.yml", "name: Dispatch\non: [push]\njobs:\n"+
		"  changes:\n    runs-on: ubuntu-latest\n    steps:\n"+
		"      - run: |\n"+
		"          append_gate \"${{ steps.f.outputs.a }}\" \"a\" \"Verify alpha gate\" \"cmd\" \"cmd\"\n"+
		"  gate:\n    name: ${{ matrix.display }}\n    needs: changes\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  verify-surfaces:\n    name: Verify ${{ matrix.surface }} gate\n    runs-on: ubuntu-latest\n"+
		"    strategy:\n      matrix:\n        surface: [x]\n    steps: []\n")
	reg := minimalReg([]cigates.Gate{ownershipGate("a", "p.yml", "Verify alpha gate")}, nil, nil)
	errs := ownershipErrors(t, root, reg)
	if len(errs) != 1 || !strings.Contains(errs[0], `"verify-surfaces"`) {
		t.Errorf("only the newcomer is unowned; want one finding for verify-surfaces, got: %v", errs)
	}
}
