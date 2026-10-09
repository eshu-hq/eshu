// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// continueWorkflow is an owner job that needs dep, where dep sets the given
// job-level continue-on-error (empty means the key is absent) and may itself
// need a deeper job.
func continueWorkflow(continueOnError string) string {
	wf := "name: Continue\non: [push]\njobs:\n" +
		"  deep:\n    runs-on: ubuntu-latest\n    steps: []\n" +
		"  dep:\n    needs: deep\n    runs-on: ubuntu-latest\n"
	if continueOnError != "" {
		wf += "    continue-on-error: " + continueOnError + "\n"
	}
	return wf + "    steps: []\n" +
		"  owned:\n    name: owned-job\n    needs: dep\n    runs-on: ubuntu-latest\n    steps: []\n"
}

// TestJobOwnershipContinueOnErrorNeedIsNotCoveredThroughNeeds pins F5: a job
// with continue-on-error does not turn the run red when it fails, so its
// dependent still runs and a needs edge proves nothing about it. It must be
// owned directly. Its own dependency is still covered: a failure there skips
// the continue-on-error job, and so the owner.
func TestJobOwnershipContinueOnErrorNeedIsNotCoveredThroughNeeds(t *testing.T) {
	t.Parallel()

	tolerant := map[string]string{
		"true":       "true",
		"expression": "${{ matrix.experimental }}",
		"quoted":     "'true'",
	}
	for name, value := range tolerant {
		t.Run("tolerant/"+name, func(t *testing.T) {
			t.Parallel()
			root := buildDriftRepo(t, minimalPreCommit("a"), nil)
			writeWorkflow(t, root, "c.yml", continueWorkflow(value))
			reg := minimalReg([]cigates.Gate{ownershipGate("a", "c.yml", "owned-job")}, nil, nil)
			errs := ownershipErrors(t, root, reg)
			if len(errs) != 1 || !strings.Contains(errs[0], `"dep"`) {
				t.Errorf("continue-on-error: %s keeps dep from failing the dependent; want one finding for dep, got: %v", value, errs)
			}
		})
	}
	for name, value := range map[string]string{"absent": "", "false": "false"} {
		t.Run("strict/"+name, func(t *testing.T) {
			t.Parallel()
			root := buildDriftRepo(t, minimalPreCommit("a"), nil)
			writeWorkflow(t, root, "c.yml", continueWorkflow(value))
			reg := minimalReg([]cigates.Gate{ownershipGate("a", "c.yml", "owned-job")}, nil, nil)
			if errs := ownershipErrors(t, root, reg); len(errs) != 0 {
				t.Errorf("continue-on-error %q: a failing dep skips the owner, got: %v", value, errs)
			}
		})
	}
	t.Run("owned directly is clean", func(t *testing.T) {
		t.Parallel()
		root := buildDriftRepo(t, minimalPreCommit("a")+hookEntry("b"), nil)
		writeWorkflow(t, root, "c.yml", continueWorkflow("true"))
		reg := minimalReg([]cigates.Gate{
			ownershipGate("a", "c.yml", "owned-job"),
			ownershipGate("b", "c.yml", "dep"),
		}, nil, nil)
		if errs := ownershipErrors(t, root, reg); len(errs) != 0 {
			t.Errorf("a gate row owning the continue-on-error job covers it, got: %v", errs)
		}
	})
}

// TestJobOwnershipContinueOnErrorNeedReadByOverrideReaderIsNotCovered pins the
// override branch of the walk: an always() aggregator that reads the result of
// a continue-on-error dependency does not cover it either, because that job
// passes when it fails.
func TestJobOwnershipContinueOnErrorNeedReadByOverrideReaderIsNotCovered(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a"), nil)
	writeWorkflow(t, root, "c.yml", "name: Continue\non: [push]\njobs:\n"+
		"  dep:\n    continue-on-error: true\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  owned:\n    name: owned-job\n    needs: dep\n    if: ${{ always() }}\n    runs-on: ubuntu-latest\n"+
		"    steps:\n      - run: test \"${{ needs.dep.result }}\" = success\n")
	reg := minimalReg([]cigates.Gate{ownershipGate("a", "c.yml", "owned-job")}, nil, nil)
	errs := ownershipErrors(t, root, reg)
	if len(errs) != 1 || !strings.Contains(errs[0], `"dep"`) {
		t.Errorf("a result read does not cover a continue-on-error dependency; want one finding for dep, got: %v", errs)
	}
}

// TestJobOwnershipContinueOnErrorReaderCoversNothingThroughItsReads pins the
// other half: a continue-on-error job that runs after a failed dependency
// (always()) and reads its result passes when its own step fails, so the read
// covers nothing. Owning the reader, as the finding asks, must not make the
// validator green while a failure behind it still blocks nothing.
func TestJobOwnershipContinueOnErrorReaderCoversNothingThroughItsReads(t *testing.T) {
	t.Parallel()

	root := buildDriftRepo(t, minimalPreCommit("a")+hookEntry("b"), nil)
	writeWorkflow(t, root, "c.yml", "name: Continue\non: [push]\njobs:\n"+
		"  x:\n    runs-on: ubuntu-latest\n    steps: []\n"+
		"  t:\n    needs: x\n    if: always()\n    continue-on-error: true\n    runs-on: ubuntu-latest\n"+
		"    steps:\n      - run: test \"${{ needs.x.result }}\" = success\n"+
		"  owned:\n    name: owned-job\n    needs: t\n    runs-on: ubuntu-latest\n    steps: []\n")
	reg := minimalReg([]cigates.Gate{
		ownershipGate("a", "c.yml", "owned-job"),
		ownershipGate("b", "c.yml", "t"),
	}, nil, nil)
	errs := ownershipErrors(t, root, reg)
	if len(errs) != 1 || !strings.Contains(errs[0], `"x"`) {
		t.Errorf("t passes when it fails, so its read of x blocks nothing; want one finding for x, got: %v", errs)
	}
}
