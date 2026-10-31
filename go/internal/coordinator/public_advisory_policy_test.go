// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestPublicAdvisoryPolicySelectsExactOwnedInstances(t *testing.T) {
	t.Parallel()
	instances := []workflow.DesiredCollectorInstance{
		{
			InstanceID: "public-a", CollectorKind: scope.CollectorVulnerabilityIntelligence,
			Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true,
			Configuration: `{"targets":[],"public_advisory_targets":[]}`,
		},
		{
			InstanceID: "generic-b", CollectorKind: scope.CollectorVulnerabilityIntelligence,
			Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true,
			Configuration: `{"targets":[]}`,
		},
	}
	policy, err := parsePublicAdvisoryPolicy(`{"expected_revision":0,"desired_revision":1,"instances":["public-a"]}`, instances)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := policy.selectInstances(instances)
	if err != nil || len(selected) != 1 || selected[0].InstanceID != "public-a" {
		t.Fatalf("selected = %+v, %v; want public-a only", selected, err)
	}
	for _, raw := range []string{
		`{"expected_revision":0,"desired_revision":1,"instances":["missing"]}`,
		`{"expected_revision":0,"desired_revision":1,"instances":["public-a","public-a"]}`,
		`{"expected_revision":0,"desired_revision":1,"instances":["generic-b"]}`,
		`{"expected_revision":0,"desired_revision":1,"instances":[],"unsafe":true}`,
	} {
		if _, err := parsePublicAdvisoryPolicy(raw, instances); err == nil {
			t.Fatalf("accepted invalid public advisory policy %s", strings.ReplaceAll(raw, "public-a", "instance"))
		}
	}
}

func TestPublicAdvisoryPolicyRejectsInvalidTargetFreshness(t *testing.T) {
	t.Parallel()
	for _, targets := range []string{
		`null`, `{}`, `[{}]`,
		`[{"scope_id":"public-nvd"}]`,
		`[{"scope_id":"public-nvd","max_age_seconds":0}]`,
		`[{"scope_id":"public-nvd","max_age_seconds":2592001}]`,
		`[{"scope_id":"public-nvd","max_age_seconds":"3600"}]`,
	} {
		instance := workflow.DesiredCollectorInstance{
			InstanceID: "public-a", CollectorKind: scope.CollectorVulnerabilityIntelligence,
			Configuration: `{"targets":[],"public_advisory_targets":` + targets + `}`,
		}
		_, err := parsePublicAdvisoryPolicy(
			`{"expected_revision":0,"desired_revision":1,"instances":["public-a"]}`,
			[]workflow.DesiredCollectorInstance{instance},
		)
		if err == nil {
			t.Fatalf("accepted public targets %s without valid freshness", targets)
		}
	}
}
