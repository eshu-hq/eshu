// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hcl

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestTFVarsJSONParsesLikeTFVars pins #9048: a *.tfvars.json body parses with
// HCL's JSON syntax and yields the same variable payload as the equivalent
// *.tfvars body.
func TestTFVarsJSONParsesLikeTFVars(t *testing.T) {
	t.Parallel()

	hclPath := writeHCLTestFile(t, "vars.tfvars", `region = "us-east-1"
replicas = 2
enabled = true
tags = {
  name  = "web"
  owner = "platform"
}
notif = {
  pagerduty_service_name           = "web"
  pagerduty_acknowledgement_timeout = 1800
  pagerduty_enable_slack_connection = true
}
`)
	jsonPath := writeHCLTestFile(t, "vars.tfvars.json", `{
  "region": "us-east-1",
  "replicas": 2,
  "enabled": true,
  "tags": {
    "name": "web",
    "owner": "platform"
  },
  "notif": {
    "pagerduty_service_name": "web",
    "pagerduty_acknowledgement_timeout": 1800,
    "pagerduty_enable_slack_connection": true
  }
}
`)

	want, err := Parse(hclPath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	got, err := Parse(jsonPath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	normalizePayloadPaths(got, jsonPath, hclPath)
	normalizePayloadPaths(want, hclPath, hclPath)
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("tfvars.json payload differs from tfvars payload:\n got  %s\n want %s", gotJSON, wantJSON)
	}
}

// TestTFVarsJSONRejectsNonObjectBody pins that a *.tfvars.json file must hold
// a JSON object: HCL's JSON syntax admits no top-level list or scalar.
func TestTFVarsJSONRejectsNonObjectBody(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"list":   "[{\"region\": \"us-east-1\"}]\n",
		"scalar": "\"us-east-1\"\n",
		"broken": "{\"region\":\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			filePath := writeHCLTestFile(t, "vars.tfvars.json", body)
			if _, err := Parse(filePath, false, shared.Options{}); err == nil {
				t.Errorf("Parse() error = nil, want parse error for %s body", name)
			}
		})
	}
}

// normalizePayloadPaths rewrites every path value from one file to another so
// payloads from different fixture files compare by content, not identity.
// It also zeroes line numbers: synthesized *.tfvars.json expressions carry
// no source ranges, so only values (not lines) pin parity here.
func normalizePayloadPaths(value any, from, to string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if key == "path" {
				if path, ok := item.(string); ok {
					typed[key] = strings.ReplaceAll(path, from, to)
				}
				continue
			}
			if key == "line_number" {
				typed[key] = 0
				continue
			}
			normalizePayloadPaths(item, from, to)
		}
	case []map[string]any:
		for _, item := range typed {
			normalizePayloadPaths(item, from, to)
		}
	case []any:
		for _, item := range typed {
			normalizePayloadPaths(item, from, to)
		}
	}
}
