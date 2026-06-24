// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/coordinator"
)

var composeVariablePattern = regexp.MustCompile(`\$\{([A-Z0-9_]+)(?::([-?])([^}]*))?\}`)

func TestRemoteE2EComposeDefaultsAllowDisabledHostedCoordinatorStartup(t *testing.T) {
	t.Parallel()

	instancesJSON := remoteE2ECollectorInstancesJSON(t)
	cfg, err := coordinator.LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "active"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":
			return "true"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return instancesJSON
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}

	assertRemoteE2EHostedInstanceDisabled(t, cfg, "remote-e2e-pagerduty", `"account_id": ""`)
	assertRemoteE2EHostedInstanceDisabled(t, cfg, "remote-e2e-jira", `"site_id": ""`)
	assertRemoteE2EHostedInstanceDisabled(t, cfg, "remote-e2e-grafana", `"base_url":""`)
	assertRemoteE2EHostedInstanceDisabled(t, cfg, "remote-e2e-prometheus-mimir", `"base_url":""`)
	assertRemoteE2EHostedInstanceDisabled(t, cfg, "remote-e2e-loki", `"base_url":""`)
	assertRemoteE2EHostedInstanceDisabled(t, cfg, "remote-e2e-tempo", `"base_url":""`)
}

func TestRemoteE2EComposeJiraUsesJQLEnvReference(t *testing.T) {
	t.Parallel()

	compose := readRemoteE2EComposeSource(t)
	if !strings.Contains(compose, `"jql_env": "ESHU_JIRA_JQL"`) {
		t.Fatal(`docker-compose.remote-e2e.yaml must configure Jira with "jql_env": "ESHU_JIRA_JQL"`)
	}
	if strings.Contains(compose, `"jql": "${ESHU_JIRA_JQL`) {
		t.Fatal("docker-compose.remote-e2e.yaml must not interpolate ESHU_JIRA_JQL directly inside collector JSON")
	}

	doc := readComposeDocument(t, "docker-compose.remote-e2e.yaml")
	service := requireComposeService(t, doc, "collector-jira")
	assertComposeEnv(t, service, "ESHU_JIRA_JQL", "${ESHU_JIRA_JQL:-}")
}

func remoteE2ECollectorInstancesJSON(t *testing.T) string {
	t.Helper()

	doc := readComposeDocument(t, "docker-compose.remote-e2e.yaml")
	service := requireComposeService(t, doc, "workflow-coordinator")
	raw, ok := service.Environment["ESHU_COLLECTOR_INSTANCES_JSON"].(string)
	if !ok {
		t.Fatalf("workflow-coordinator ESHU_COLLECTOR_INSTANCES_JSON = %#v, want string", service.Environment["ESHU_COLLECTOR_INSTANCES_JSON"])
	}
	base := strings.TrimSuffix(strings.TrimSpace(renderComposeVariables(t, raw)), "]")

	observabilityDoc := readComposeDocument(t, "docker-compose.remote-e2e.observability.yaml")
	observabilityService := requireComposeService(t, observabilityDoc, "workflow-coordinator")
	observabilityRaw, ok := observabilityService.Environment["ESHU_OBSERVABILITY_COLLECTOR_INSTANCES_JSON"].(string)
	if !ok {
		t.Fatalf("observability workflow-coordinator ESHU_OBSERVABILITY_COLLECTOR_INSTANCES_JSON = %#v, want string", observabilityService.Environment["ESHU_OBSERVABILITY_COLLECTOR_INSTANCES_JSON"])
	}
	observability := strings.TrimPrefix(strings.TrimSpace(renderComposeVariables(t, observabilityRaw)), "[")

	return base + "," + observability
}

func renderComposeVariables(t *testing.T, raw string) string {
	t.Helper()

	return composeVariablePattern.ReplaceAllStringFunc(raw, func(expr string) string {
		matches := composeVariablePattern.FindStringSubmatch(expr)
		key := matches[1]
		if value, ok := remoteE2ERequiredValue(key); ok {
			return value
		}
		operator := matches[2]
		value := matches[3]
		if operator == "-" {
			return value
		}
		if operator == "?" {
			t.Fatalf("missing test value for required Compose variable %s", key)
		}
		return ""
	})
}

func remoteE2ERequiredValue(key string) (string, bool) {
	values := map[string]string{
		"ESHU_AWS_E2E_ACCOUNT_ID":        "123456789012",
		"ESHU_AWS_E2E_REGION":            "us-east-1",
		"ESHU_ECR_OCI_REGION":            "us-east-1",
		"ESHU_ECR_OCI_REGISTRY_ID":       "123456789012",
		"ESHU_ECR_OCI_REPOSITORY":        "team/api",
		"ESHU_SECURITY_ALERT_REPOSITORY": "owner/repository",
		"ESHU_TFSTATE_S3_BUCKET":         "remote-e2e-tfstate",
		"ESHU_TFSTATE_S3_KEY":            "services/api/terraform.tfstate",
		"ESHU_TFSTATE_S3_REGION":         "us-east-1",
	}
	value, ok := values[key]
	return value, ok
}

func assertRemoteE2EHostedInstanceDisabled(t *testing.T, cfg coordinator.Config, instanceID, blankField string) {
	t.Helper()

	for _, instance := range cfg.CollectorInstances {
		if instance.InstanceID != instanceID {
			continue
		}
		if instance.Enabled {
			t.Fatalf("%s Enabled = true, want false", instanceID)
		}
		if !strings.Contains(instance.Configuration, blankField) {
			t.Fatalf("%s configuration missing blank private field %s: %s", instanceID, blankField, instance.Configuration)
		}
		return
	}
	t.Fatalf("collector instance %s missing", instanceID)
}
