// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/coordinator/egress"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestLoadConfigParsesExtensionEgressPolicy(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "active"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":
			return "true"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[{"instance_id":"scorecard-primary","collector_kind":"scorecard","mode":"scheduled","enabled":true,"claims_enabled":true,"configuration":{"schema_version":"eshu.component.instance.v1","component_id":"dev.eshu.examples.scorecard","component_version":"0.1.0","manifest_digest":"sha256:1234","config_handle":"component-config:abcd","runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"oci"}}}]`
		case "ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON":
			return `{"mode":"restricted","extensions":[{"component_id":"dev.eshu.examples.scorecard","decision":"deny"}]}`
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}

	decision := cfg.ExtensionEgressPolicy.Decide(egress.ExtensionRequest{
		ComponentID:   "dev.eshu.examples.scorecard",
		InstanceID:    "scorecard-primary",
		CollectorKind: scope.CollectorKind("scorecard"),
	})
	if got, want := decision.Action, egress.ExtensionActionDeny; got != want {
		t.Fatalf("scorecard action = %q, want %q", got, want)
	}
}
