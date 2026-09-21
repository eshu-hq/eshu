// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/coordinator/egress"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestLoadConfigParsesCollectorEgressPolicy(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "active"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":
			return "true"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[{"instance_id":"pagerduty-primary","collector_kind":"pagerduty","mode":"continuous","enabled":true,"claims_enabled":true,"configuration":{"targets":[{"provider":"pagerduty","scope_id":"pagerduty:account:example","account_id":"example","token_env":"PAGERDUTY_TOKEN","incident_limit":25,"log_entry_limit":25,"change_event_limit":25}]}}]`
		case "ESHU_HOSTED_COLLECTOR_EGRESS_POLICY_JSON":
			return `{"mode":"restricted","collectors":[{"collector_kind":"pagerduty","decision":"deny"}]}`
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}

	decision := cfg.CollectorEgressPolicy.Decide(scope.CollectorPagerDuty)
	if got, want := decision.Action, egress.CollectorActionDeny; got != want {
		t.Fatalf("pagerduty action = %q, want %q", got, want)
	}
}
