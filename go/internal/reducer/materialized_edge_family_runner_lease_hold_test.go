// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strings"
	"testing"
)

// checkRunnerLeaseHoldLockstep validates the registry contract for the
// runner_lease_hold blocker. This blocker holds the shared projection
// partition lease, so its wait must observe the runner queue for the same
// materialized-edge family that the shared projection worker drains.
func checkRunnerLeaseHoldLockstep(
	declaredBlockerKinds, waitStages, waitKeys map[string]string,
) error {
	return checkRunnerLeaseHoldLockstepWithDomains(
		declaredBlockerKinds,
		waitStages,
		waitKeys,
		sharedProjectionDomains,
	)
}

func checkRunnerLeaseHoldLockstepWithDomains(
	declaredBlockerKinds, waitStages, waitKeys map[string]string,
	sharedDomains []string,
) error {
	shared := make(map[string]struct{}, len(sharedDomains))
	for _, domain := range sharedDomains {
		shared[domain] = struct{}{}
	}

	checked := 0
	for family, rawKind := range declaredBlockerKinds {
		if rawKind != string(blockerRunnerLeaseHold) {
			continue
		}
		checked++
		if waitStages[family] != "runner" {
			return fmt.Errorf("family %q declares blocker_kind=%q but wait_stage=%q; runner_lease_hold must wait at the shared projection runner", family, rawKind, waitStages[family])
		}
		if waitKeys[family] != family {
			return fmt.Errorf("family %q declares blocker_kind=%q but wait_key=%q; runner_lease_hold must wait on its own projection domain", family, rawKind, waitKeys[family])
		}
		if _, ok := shared[family]; !ok {
			return fmt.Errorf("family %q declares blocker_kind=%q but is not in sharedProjectionDomains; allProjectionDomains alone does not prove that the shared runner drains this family", family, rawKind)
		}
	}
	if checked == 0 {
		return fmt.Errorf("registry declares no live %q row; the runner lease blocker lockstep would pass vacuously", blockerRunnerLeaseHold)
	}
	return nil
}

func TestClassifyBlockerKindRecognizesRunnerLeaseHold(t *testing.T) {
	t.Parallel()

	got, ok := classifyBlockerKind(string(blockerRunnerLeaseHold))
	if !ok || got != blockerRunnerLeaseHold {
		t.Fatalf("classifyBlockerKind(%q) = (%q, %t), want (%q, true)", blockerRunnerLeaseHold, got, ok, blockerRunnerLeaseHold)
	}
}

func TestRunnerLeaseHoldLockstepCatchesWrongStage(t *testing.T) {
	t.Parallel()

	err := checkRunnerLeaseHoldLockstepWithDomains(
		map[string]string{"family_a": string(blockerRunnerLeaseHold)},
		map[string]string{"family_a": "handler"},
		map[string]string{"family_a": "family_a"},
		[]string{"family_a"},
	)
	if err == nil || !strings.Contains(err.Error(), "wait_stage") {
		t.Fatalf("checkRunnerLeaseHoldLockstep(wrong stage) = %v, want wait_stage error", err)
	}
}

func TestRunnerLeaseHoldLockstepCatchesWrongKey(t *testing.T) {
	t.Parallel()

	err := checkRunnerLeaseHoldLockstepWithDomains(
		map[string]string{"family_a": string(blockerRunnerLeaseHold)},
		map[string]string{"family_a": "runner"},
		map[string]string{"family_a": "code_calls"},
		[]string{"family_a"},
	)
	if err == nil || !strings.Contains(err.Error(), "wait_key") {
		t.Fatalf("checkRunnerLeaseHoldLockstep(wrong key) = %v, want wait_key error", err)
	}
}

func TestRunnerLeaseHoldLockstepRejectsDedicatedProjectionDomain(t *testing.T) {
	t.Parallel()

	// repo_dependency is in allProjectionDomains but is not drained by the
	// shared projection runner. The blocker must require the narrower,
	// production sharedProjectionDomains set.
	err := checkRunnerLeaseHoldLockstepWithDomains(
		map[string]string{DomainRepoDependency: string(blockerRunnerLeaseHold)},
		map[string]string{DomainRepoDependency: "runner"},
		map[string]string{DomainRepoDependency: DomainRepoDependency},
		[]string{DomainCodeCalls},
	)
	if err == nil || !strings.Contains(err.Error(), "sharedProjectionDomains") {
		t.Fatalf("checkRunnerLeaseHoldLockstep(dedicated projection domain) = %v, want sharedProjectionDomains error", err)
	}
}

func TestRunnerLeaseHoldLockstepRejectsVacuousRegistry(t *testing.T) {
	t.Parallel()

	err := checkRunnerLeaseHoldLockstepWithDomains(
		map[string]string{"family_a": string(blockerNone)},
		map[string]string{"family_a": "runner"},
		map[string]string{"family_a": "family_a"},
		[]string{"family_a"},
	)
	if err == nil || !strings.Contains(err.Error(), "pass vacuously") {
		t.Fatalf("checkRunnerLeaseHoldLockstep(no runner_lease_hold row) = %v, want non-vacuity error", err)
	}
}

func TestMaterializedEdgeFamilyRunnerLeaseHoldLockstep(t *testing.T) {
	t.Parallel()

	rowsDir := ifaFamilyRegistryRowsDir(t)
	declared := parseIfaFamilyRegistryBlockerKinds(t, rowsDir)
	waitStages := parseIfaFamilyRegistryWaitStages(t, rowsDir)
	waitKeys := parseIfaFamilyRegistryWaitKeys(t, rowsDir)
	if err := checkRunnerLeaseHoldLockstep(declared, waitStages, waitKeys); err != nil {
		t.Fatal(err)
	}
}
