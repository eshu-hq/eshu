// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"strings"
)

func parseSelfTestTriggers(
	registryPath string,
	gateID string,
	declared *[]string,
	primary map[string]struct{},
) ([]string, error) {
	if declared == nil {
		return nil, nil
	}
	if len(*declared) == 0 {
		return nil, fmt.Errorf("ci-gates registry %s: gate %q has empty self_test_triggers (omit the field for fail-closed always-run behavior)", registryPath, gateID)
	}
	result := make([]string, 0, len(*declared))
	seen := make(map[string]struct{}, len(*declared))
	for _, rawTrigger := range *declared {
		trigger := strings.TrimSpace(rawTrigger)
		if trigger == "" {
			return nil, fmt.Errorf("ci-gates registry %s: gate %q has blank self_test_triggers entry", registryPath, gateID)
		}
		if _, duplicate := seen[trigger]; duplicate {
			return nil, fmt.Errorf("ci-gates registry %s: gate %q has duplicate self_test_trigger %q", registryPath, gateID, trigger)
		}
		if _, covered := primary[trigger]; !covered {
			return nil, fmt.Errorf("ci-gates registry %s: gate %q self_test_trigger %q must also appear in triggers", registryPath, gateID, trigger)
		}
		seen[trigger] = struct{}{}
		result = append(result, trigger)
	}
	return result, nil
}

// parsePrePush validates and parses a gate's local.pre_push /
// local.pre_push_reason fields, extracted out of Load to keep that function
// under the repo's function-length lint cap. gf.Local must be non-nil; the
// caller only invokes this inside its `if gf.Local != nil` branch.
//
// Deferring a gate from the fast `--pre-push` lane (scripts/dev/pre-push.sh)
// must move its enforcement to CI, never remove it: required-gates.yml
// aggregates every blocking:true gate automatically, so a real CI
// workflow/job proves that; an advisory (blocking:false) gate is never part
// of the required merge gate in the first place, so deferring it removes
// nothing required-gates.yml enforces, whether or not it happens to carry
// its own separate, non-required CI workflow.
func parsePrePush(registryPath, gateID string, gf gateFile) (deferred, floor bool, reason string, err error) {
	prePush := strings.TrimSpace(gf.Local.PrePush)
	reason = strings.TrimSpace(gf.Local.PrePushReason)
	switch prePush {
	case "":
		if reason != "" {
			return false, false, "", fmt.Errorf("ci-gates registry %s: gate %q has pre_push_reason but no pre_push value", registryPath, gateID)
		}
		return false, false, "", nil
	case "floor":
		return false, true, reason, nil
	case "deferred":
		if reason == "" {
			return false, false, "", fmt.Errorf("ci-gates registry %s: gate %q has pre_push: deferred but empty pre_push_reason (required: state the measured cost and where it still runs)", registryPath, gateID)
		}
		hasCIDestination := strings.TrimSpace(gf.CI.Workflow) != "" && strings.TrimSpace(gf.CI.Job) != ""
		if gf.Blocking && !hasCIDestination {
			return false, false, "", fmt.Errorf(
				"ci-gates registry %s: gate %q has pre_push: deferred but is blocking:true with no ci.workflow/ci.job -- deferring it from --pre-push would leave it enforced nowhere; give it a real CI destination or drop the deferral",
				registryPath, gateID,
			)
		}
		return true, false, reason, nil
	default:
		return false, false, "", fmt.Errorf("ci-gates registry %s: gate %q has invalid pre_push %q (want \"floor\", \"deferred\", or omit the field)", registryPath, gateID, gf.Local.PrePush)
	}
}

// validateLocalAndCIBackstop checks the local/CI-backstop invariants shared
// by every gate, extracted out of Load to keep that function under the
// repo's function-length lint cap. Returns the trimmed ci_only_reason and
// local_only_reason on success.
//
//   - local==nil requires a non-empty ci_only_reason (CI-only gate).
//   - local!=nil requires at least one of Command/TestCommand: a local block
//     with neither is representable but meaningless -- executeGates
//     (go/cmd/ci-gates/execute.go) runs zero steps for it and still prints
//     "PASS <gate>", indistinguishable from a gate that actually ran and
//     passed (#6149 follow-up item 8 review, P1).
//   - self_test_triggers requires a distinct, non-empty test_command.
//   - a blocking:false gate with no CI backstop at all (ci.workflow AND
//     ci.job both empty) runs ONLY through a developer's local `make pre-pr`
//     -- unlike every other gate, a skip here is not harmless, because
//     nothing else ever runs the check. That gap is fine as a deliberate,
//     temporary staging decision, but it must be DECLARED via
//     local_only_reason, not merely possible (#6149 follow-up item 5). A
//     blocking gate is exempt: blocking:true with no CI backstop is a
//     different, likely-worse defect this rule does not attempt to
//     characterize -- it would fail CI itself wherever the required-status
//     manifest expects a context, which is a different check's signal.
func validateLocalAndCIBackstop(
	registryPath, gateID string,
	gf gateFile,
	local *Local,
	selfTestTriggers []string,
) (ciOnlyReason, localOnlyReason string, err error) {
	ciOnlyReason = strings.TrimSpace(gf.CIOnlyReason)
	if local == nil && ciOnlyReason == "" {
		return "", "", fmt.Errorf("ci-gates registry %s: gate %q has local==null but empty ci_only_reason (required when local is absent)", registryPath, gateID)
	}
	if local != nil && local.Command == "" && local.TestCommand == "" {
		return "", "", fmt.Errorf(
			"ci-gates registry %s: gate %q declares a local block with neither command nor test_command -- either give it one, or declare local==null with a ci_only_reason instead",
			registryPath, gateID,
		)
	}
	if len(selfTestTriggers) > 0 && (local == nil || local.TestCommand == "" || local.TestCommand == local.Command) {
		return "", "", fmt.Errorf("ci-gates registry %s: gate %q declares self_test_triggers without a distinct test_command", registryPath, gateID)
	}
	localOnlyReason = strings.TrimSpace(gf.LocalOnlyReason)
	ciWorkflow := strings.TrimSpace(gf.CI.Workflow)
	ciJob := strings.TrimSpace(gf.CI.Job)
	if !gf.Blocking && ciWorkflow == "" && ciJob == "" && localOnlyReason == "" {
		return "", "", fmt.Errorf(
			"ci-gates registry %s: gate %q is blocking:false with no CI backstop (ci.workflow and ci.job both empty) but has empty local_only_reason (required when a gate has no CI backstop at all -- state why, and what would close the gap)",
			registryPath, gateID,
		)
	}
	return ciOnlyReason, localOnlyReason, nil
}
