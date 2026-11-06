// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// disabledRegistryYAML returns a one-gate registry. The marker lines are spliced
// in so each case changes only the field under test.
func disabledRegistryYAML(tier, blocking, marker string) string {
	return `version: v1
gates:
  - id: replay-tier
    name: Replay tier
    category: exactness
    tier: ` + tier + `
    blocking: ` + blocking + `
    triggers:
      - "go/**"
    local:
      command: "bash scripts/verify-replay-tier.sh"
    ci:
      workflow: replay.yml
      job: "Offline replay tier"
    requirements:
      - go
    ci_only_reason: ""
` + marker
}

const disabledMarker = `    disabled:
      reason: "Disabled by owner order."
      issue: "#7331"
`

func TestLoadParsesDisabledMarker(t *testing.T) {
	t.Parallel()
	reg, err := cigates.Load(writeYAML(t, disabledRegistryYAML("manual", "false", disabledMarker)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := reg.Gates[0].Disabled
	if got == nil || got.Reason != "Disabled by owner order." || got.Issue != "#7331" {
		t.Fatalf("Disabled = %+v, want reason and issue #7331", got)
	}
}

func TestLoadWithoutDisabledMarkerLeavesItNil(t *testing.T) {
	t.Parallel()
	reg, err := cigates.Load(writeYAML(t, disabledRegistryYAML("manual", "false", "")))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reg.Gates[0].Disabled != nil {
		t.Fatalf("Disabled = %+v, want nil", reg.Gates[0].Disabled)
	}
}

func TestLoadRejectsMalformedDisabledMarker(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		tier, blocking, marker, want string
	}{
		"blank reason":       {"manual", "false", "    disabled:\n      reason: \"  \"\n      issue: \"#7331\"\n", "disabled.reason"},
		"missing issue":      {"manual", "false", "    disabled:\n      reason: \"why\"\n", "disabled.issue"},
		"issue without hash": {"manual", "false", "    disabled:\n      reason: \"why\"\n      issue: \"7331\"\n", "disabled.issue"},
		"blocking row":       {"manual", "true", disabledMarker, "manual and non-blocking"},
		"live tier row":      {"ci-heavy", "false", disabledMarker, "manual and non-blocking"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := cigates.Load(writeYAML(t, disabledRegistryYAML(tc.tier, tc.blocking, tc.marker)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func disabledGate(workflow string) cigates.Gate {
	gate := gateWith("replay-tier", "replay-tier", workflow)
	gate.Tier = cigates.TierManual
	gate.Blocking = false
	gate.CI.Job = "Offline replay tier"
	gate.Disabled = &cigates.Disabled{Reason: "owner order", Issue: "#7331"}
	return gate
}

func disabledWorkflow(condition string) string {
	if condition != "" {
		condition = "    if: " + condition + "\n"
	}
	return "name: replay\non: [push]\njobs:\n  replay-tier:\n" + condition +
		"    name: Offline replay tier\n    runs-on: ubuntu-latest\n    steps: []\n"
}

func TestWorkflowJobDisabled(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		condition string
		want      bool
	}{
		"literal expression": {"${{ false }}", true},
		"bare false":         {"false", true},
		"tight expression":   {"${{false}}", true},
		"event condition":    {"${{ github.event_name != 'pull_request' }}", false},
		"always true":        {"${{ true }}", false},
		"no condition":       {"", false},
		"false in a guard":   {"${{ false && github.ref == 'x' }}", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := buildDriftRepo(t, minimalPreCommit("replay-tier"), nil)
			writeWorkflow(t, root, "replay.yml", disabledWorkflow(tc.condition))
			got, err := cigates.WorkflowJobDisabled(root, disabledGate("replay.yml"))
			if err != nil || got != tc.want {
				t.Fatalf("WorkflowJobDisabled = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestWorkflowJobDisabledErrorsWhenTheJobCannotBeFound(t *testing.T) {
	t.Parallel()
	root := buildDriftRepo(t, minimalPreCommit("replay-tier"), nil)
	writeWorkflow(t, root, "replay.yml", disabledWorkflow("${{ false }}"))
	gate := disabledGate("replay.yml")
	gate.CI.Job = "No such job"
	if _, err := cigates.WorkflowJobDisabled(root, gate); err == nil {
		t.Fatal("a job that is not in the workflow must be an error, not a disabled job")
	}
	gate = disabledGate("missing.yml")
	if _, err := cigates.WorkflowJobDisabled(root, gate); err == nil {
		t.Fatal("a missing workflow file must be an error")
	}
}

func TestDriftCheckRefusesDisabledMarkerOnALiveJob(t *testing.T) {
	t.Parallel()
	root := buildDriftRepo(t, minimalPreCommit("replay-tier"), nil)
	writeWorkflow(t, root, "replay.yml", disabledWorkflow("${{ github.event_name != 'pull_request' }}"))
	reg := minimalReg([]cigates.Gate{disabledGate("replay.yml")}, nil, nil)
	var found bool
	for _, err := range cigates.DriftCheck(root, reg) {
		if strings.Contains(err.Error(), "replay-tier") && strings.Contains(err.Error(), "disabled marker") {
			found = true
		}
	}
	if !found {
		t.Fatal("a gate marked disabled whose workflow job can still run must fail the drift check")
	}
}

func TestDriftCheckAcceptsDisabledMarkerOnADisabledJob(t *testing.T) {
	t.Parallel()
	root := buildDriftRepo(t, minimalPreCommit("replay-tier"), nil)
	writeWorkflow(t, root, "replay.yml", disabledWorkflow("${{ false }}"))
	reg := minimalReg([]cigates.Gate{disabledGate("replay.yml")}, nil, nil)
	for _, err := range cigates.DriftCheck(root, reg) {
		if strings.Contains(err.Error(), "disabled marker") {
			t.Fatalf("unexpected disabled-marker error: %v", err)
		}
	}
}
