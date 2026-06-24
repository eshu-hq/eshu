// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hcl

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestTerraformParseResourceMetadata(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "main.tf", `resource "aws_s3_bucket" "logs" {
  count = 2
}

resource "aws_iam_user" "writer" {
  for_each = { alice = "reader" }
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	resources := bucketForTest(t, got, "terraform_resources")
	if len(resources) != 2 {
		t.Fatalf("len(terraform_resources) = %d, want 2", len(resources))
	}

	bucket := namedItemForTest(t, resources, "aws_s3_bucket.logs")
	if got, want := bucket["count"], "2"; got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got, want := bucket["provider"], "aws"; got != want {
		t.Fatalf("provider = %#v, want %#v", got, want)
	}
	if got, want := bucket["resource_service"], "s3"; got != want {
		t.Fatalf("resource_service = %#v, want %#v", got, want)
	}

	user := namedItemForTest(t, resources, "aws_iam_user.writer")
	if got, want := user["for_each"], `{ alice = "reader" }`; got != want {
		t.Fatalf("for_each = %#v, want %#v", got, want)
	}
}

func TestTerraformParsePagerDutyDeclarationsFromModules(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "team-checkout.tf", `module "checkout_pagerduty_service" {
  source  = "sampleorg.pe.jfrog.io/TF__SAMPLE/pagerduty-service/aws"
  version = "~> 2.0"

  name                    = local.checkout_params.pagerduty_service_name
  description             = "Checkout service"
  escalation_policy       = local.checkout_params.pagerduty_escalation_policy
  incident_urgency        = "high"
  acknowledgement_timeout = try(local.checkout_params.pagerduty_acknowledgement_timeout, 0)
  auto_resolve_timeout    = 3600
  event_orchestration     = try(local.checkout_params.pagerduty_event_orchestration, null)
  enable_slack_connection = try(local.checkout_params.slack_enable_pagerduty, false)
  integration_key         = "secret-routing-key"
}

module "pagerduty_runbook" {
  source = "example.com/acme/pagerduty-runbook/aws"
  name   = "unsupported"
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := bucketForTest(t, got, "pagerduty_declarations")
	if len(rows) != 2 {
		t.Fatalf("len(pagerduty_declarations) = %d, want 2: %#v", len(rows), rows)
	}

	service := namedItemForTest(t, rows, "module.checkout_pagerduty_service")
	if got, want := service["source_class"], "declared"; got != want {
		t.Fatalf("source_class = %#v, want %#v", got, want)
	}
	if got, want := service["declaration_kind"], "terraform_module"; got != want {
		t.Fatalf("declaration_kind = %#v, want %#v", got, want)
	}
	if got, want := service["outcome"], "declared"; got != want {
		t.Fatalf("outcome = %#v, want %#v", got, want)
	}
	if got, ok := service["module_source_fingerprint"].(string); !ok || got == "" {
		t.Fatalf("module_source_fingerprint = %#v, want non-empty string", service["module_source_fingerprint"])
	}
	if got, want := service["module_source_redacted"], true; got != want {
		t.Fatalf("module_source_redacted = %#v, want %#v", got, want)
	}
	if got, want := service["service_name"], "local.checkout_params.pagerduty_service_name"; got != want {
		t.Fatalf("service_name = %#v, want %#v", got, want)
	}
	if got, want := service["service_name_resolution"], "reference"; got != want {
		t.Fatalf("service_name_resolution = %#v, want %#v", got, want)
	}
	if got, want := service["incident_urgency"], "high"; got != want {
		t.Fatalf("incident_urgency = %#v, want %#v", got, want)
	}
	if got, want := service["acknowledgement_timeout_resolution"], "unresolved"; got != want {
		t.Fatalf("acknowledgement_timeout_resolution = %#v, want %#v", got, want)
	}
	if got, want := service["event_orchestration_declared"], true; got != want {
		t.Fatalf("event_orchestration_declared = %#v, want %#v", got, want)
	}
	if got, want := service["unresolved_inputs"], "acknowledgement_timeout,enable_slack_connection,event_orchestration"; got != want {
		t.Fatalf("unresolved_inputs = %#v, want %#v", got, want)
	}
	if got, want := service["redacted_inputs"], "integration_key"; got != want {
		t.Fatalf("redacted_inputs = %#v, want %#v", got, want)
	}
	if got, want := service["redaction_state"], "redacted"; got != want {
		t.Fatalf("redaction_state = %#v, want %#v", got, want)
	}

	unsupported := namedItemForTest(t, rows, "module.pagerduty_runbook")
	if got, want := unsupported["outcome"], "unsupported"; got != want {
		t.Fatalf("unsupported outcome = %#v, want %#v", got, want)
	}
	if got, want := unsupported["unsupported_reason"], "unsupported_module_source"; got != want {
		t.Fatalf("unsupported_reason = %#v, want %#v", got, want)
	}
}

func TestTerraformParsePagerDutyDeclarationsFromTFVars(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFileAt(t, filepath.Join("environments", "qa", "terraform.tfvars"), `environment_vars = {
  checkout = {
    pagerduty_service_name            = "QA Checkout"
    pagerduty_service_description     = "Checkout Service"
    pagerduty_escalation_policy       = "Checkout QA"
    pagerduty_incident_urgency        = "low"
    pagerduty_acknowledgement_timeout = 0
    pagerduty_event_orchestration = {
      start = {
        label = "Downgrade all alerts"
      }
    }
    slack_enable_pagerduty = false
  }

  checkout_duplicate = {
    pagerduty_service_name      = "QA Checkout"
    pagerduty_escalation_policy = "Checkout QA"
  }

  malformed = {
    pagerduty_service_name = {
      nested = "not-a-service-name"
    }
  }
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rows := bucketForTest(t, got, "pagerduty_declarations")
	if len(rows) != 3 {
		t.Fatalf("len(pagerduty_declarations) = %d, want 3: %#v", len(rows), rows)
	}

	checkout := namedItemForTest(t, rows, "tfvars.environment_vars.checkout")
	if got, want := checkout["declaration_kind"], "tfvars"; got != want {
		t.Fatalf("declaration_kind = %#v, want %#v", got, want)
	}
	if got, want := checkout["environment"], "qa"; got != want {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
	if got, want := checkout["service_name"], "QA Checkout"; got != want {
		t.Fatalf("service_name = %#v, want %#v", got, want)
	}
	if got, want := checkout["outcome"], "ambiguous"; got != want {
		t.Fatalf("outcome = %#v, want duplicate service ambiguity", got)
	}
	if got, want := checkout["duplicate_service_name"], true; got != want {
		t.Fatalf("duplicate_service_name = %#v, want %#v", got, want)
	}
	if got, want := checkout["event_orchestration_declared"], true; got != want {
		t.Fatalf("event_orchestration_declared = %#v, want %#v", got, want)
	}
	if _, exists := checkout["event_orchestration"]; exists {
		t.Fatalf("event_orchestration raw payload present; want metadata-only declaration")
	}

	duplicate := namedItemForTest(t, rows, "tfvars.environment_vars.checkout_duplicate")
	if got, want := duplicate["outcome"], "ambiguous"; got != want {
		t.Fatalf("duplicate outcome = %#v, want %#v", got, want)
	}

	malformed := namedItemForTest(t, rows, "tfvars.environment_vars.malformed")
	if got, want := malformed["outcome"], "rejected"; got != want {
		t.Fatalf("malformed outcome = %#v, want %#v", got, want)
	}
	if got, want := malformed["malformed_inputs"], "service_name"; got != want {
		t.Fatalf("malformed_inputs = %#v, want %#v", got, want)
	}
}

func TestTerraformParseS3BackendDynamoDBTableMetadata(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "backend.tf", `terraform {
  backend "s3" {
    bucket         = "app-tfstate-prod"
    key            = "services/api/terraform.tfstate"
    region         = "us-east-1"
    dynamodb_table = "tfstate-locks-api"
  }
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	backends := bucketForTest(t, got, "terraform_backends")
	if got, want := len(backends), 1; got != want {
		t.Fatalf("len(terraform_backends) = %d, want %d", got, want)
	}
	backend := backends[0]
	if got, want := backend["dynamodb_table"], "tfstate-locks-api"; got != want {
		t.Fatalf("dynamodb_table = %#v, want %#v", got, want)
	}
	if got, want := backend["dynamodb_table_is_literal"], true; got != want {
		t.Fatalf("dynamodb_table_is_literal = %#v, want %#v", got, want)
	}
}

func TestTerragruntParseHelperPaths(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "terragrunt.hcl", `terraform {
  source = "../modules/app"
}

include "root" {
  path = find_in_parent_folders("root.hcl")
}

locals {
  env = read_terragrunt_config(find_in_parent_folders("env.hcl"))
  runtime = yamldecode(file("${get_repo_root()}/config/runtime.yaml"))
  rendered = templatefile("${path.module}/templates/runtime.json", {})
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	configs := bucketForTest(t, got, "terragrunt_configs")
	if len(configs) != 1 {
		t.Fatalf("len(terragrunt_configs) = %d, want 1", len(configs))
	}
	config := configs[0]
	if got, want := config["terraform_source"], "../modules/app"; got != want {
		t.Fatalf("terraform_source = %#v, want %#v", got, want)
	}
	if got, want := config["include_paths"], "root.hcl"; got != want {
		t.Fatalf("include_paths = %#v, want %#v", got, want)
	}
	if got, want := config["read_config_paths"], "env.hcl"; got != want {
		t.Fatalf("read_config_paths = %#v, want %#v", got, want)
	}
	if got, want := config["local_config_asset_paths"], "config/runtime.yaml,templates/runtime.json"; got != want {
		t.Fatalf("local_config_asset_paths = %#v, want %#v", got, want)
	}
}

func writeHCLTestFile(t *testing.T, name string, body string) string {
	t.Helper()
	return writeHCLTestFileAt(t, name, body)
}

func writeHCLTestFileAt(t *testing.T, name string, body string) string {
	t.Helper()
	filePath := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(filePath), err)
	}
	if err := os.WriteFile(filePath, []byte(body), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", filePath, err)
	}
	return filePath
}

func bucketForTest(t *testing.T, payload map[string]any, key string) []map[string]any {
	t.Helper()
	rows, ok := payload[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any", key, payload[key])
	}
	return rows
}

func namedItemForTest(t *testing.T, rows []map[string]any, name string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if row["name"] == name {
			return row
		}
	}
	t.Fatalf("missing row named %q in %#v", name, rows)
	return nil
}

func TestTerraformParseResourceAttributesTopLevel(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "main.tf", `resource "aws_instance" "web" {
  instance_type = "t3.micro"
  ami           = "ami-0abcdef0"
  tags          = local.common_tags
  user_data     = templatefile("${path.module}/user_data.tmpl", {})
  depends_on    = [aws_iam_role.web]
  count         = 2
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	resources := bucketForTest(t, got, "terraform_resources")
	row := namedItemForTest(t, resources, "aws_instance.web")

	attrs, ok := row["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes type = %T, want map[string]any", row["attributes"])
	}
	if got, want := attrs["instance_type"], "t3.micro"; got != want {
		t.Fatalf("attributes[instance_type] = %#v, want %#v", got, want)
	}
	if got, want := attrs["ami"], "ami-0abcdef0"; got != want {
		t.Fatalf("attributes[ami] = %#v, want %#v", got, want)
	}
	for _, suppressed := range []string{"tags", "user_data", "count", "depends_on"} {
		if _, present := attrs[suppressed]; present {
			t.Fatalf("attributes[%s] present; want absent", suppressed)
		}
	}
	unknown, ok := row["unknown_attributes"].([]string)
	if !ok {
		t.Fatalf("unknown_attributes type = %T, want []string", row["unknown_attributes"])
	}
	// The implementation sorts unknown_attributes; assert exact equality so an
	// extra unexpected entry cannot silently pass.
	if want := []string{"tags", "user_data"}; !reflect.DeepEqual(unknown, want) {
		t.Fatalf("unknown_attributes = %#v, want %#v", unknown, want)
	}
	if got, want := row["count"], "2"; got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
}

func TestTerraformParseResourceAttributesNestedBlocks(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "main.tf", `resource "aws_s3_bucket" "logs" {
  acl = "private"

  versioning {
    enabled = true
  }

  server_side_encryption_configuration {
    rule {
      apply_server_side_encryption_by_default {
        sse_algorithm = "AES256"
      }
    }
  }

  lifecycle {
    prevent_destroy = true
  }

  tags = local.common_tags
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	resources := bucketForTest(t, got, "terraform_resources")
	row := namedItemForTest(t, resources, "aws_s3_bucket.logs")
	attrs, ok := row["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes type = %T, want map[string]any", row["attributes"])
	}

	cases := map[string]string{
		"acl":                "private",
		"versioning.enabled": "true",
		"server_side_encryption_configuration.rule.apply_server_side_encryption_by_default.sse_algorithm": "AES256",
	}
	for path, want := range cases {
		if got := attrs[path]; got != want {
			t.Fatalf("attributes[%q] = %#v, want %#v", path, got, want)
		}
	}
	// lifecycle is reserved — must NOT appear.
	for key := range attrs {
		if strings.HasPrefix(key, "lifecycle.") || key == "lifecycle" {
			t.Fatalf("attributes contains reserved lifecycle key %q", key)
		}
	}
	// tags is unknown (local.*).
	unknown, _ := row["unknown_attributes"].([]string)
	foundTags := false
	for _, u := range unknown {
		if u == "tags" {
			foundTags = true
		}
	}
	if !foundTags {
		t.Fatalf("unknown_attributes = %#v, want to contain %q", unknown, "tags")
	}
	if got, want := len(unknown), 1; got != want {
		t.Fatalf("len(unknown_attributes) = %d, want %d (only %q should be unknown — reserved blocks must not leak)", got, want, "tags")
	}
}

func TestTerraformParseResourceAttributesAbsentWhenEmpty(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "main.tf", `resource "aws_s3_bucket" "empty" {
}
`)
	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	row := namedItemForTest(t, bucketForTest(t, got, "terraform_resources"), "aws_s3_bucket.empty")
	if _, present := row["attributes"]; present {
		t.Fatalf("attributes key present on resource with no attributes")
	}
	if _, present := row["unknown_attributes"]; present {
		t.Fatalf("unknown_attributes key present on resource with no attributes")
	}
}

func TestTerraformParseResourceAttributesHeredocAndEscapes(t *testing.T) {
	t.Parallel()

	filePath := writeHCLTestFile(t, "main.tf", `resource "aws_iam_role" "svc" {
  assume_role_policy = <<-EOT
  {"Version":"2012-10-17"}
  EOT

  name = "svc\"quoted"
}
`)

	got, err := Parse(filePath, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	row := namedItemForTest(t, bucketForTest(t, got, "terraform_resources"), "aws_iam_role.svc")
	attrs, ok := row["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes type = %T, want map[string]any", row["attributes"])
	}
	// <<-EOT heredoc: HCL evaluates to the unindented body with a trailing
	// newline. The state side stores the actual JSON content, which also
	// ends with a newline when the heredoc is the source. The byte-level
	// literalSourceText implementation would have returned "<<-EOT\n  ...\n  EOT",
	// which never matches.
	if got, want := attrs["assume_role_policy"], "{\"Version\":\"2012-10-17\"}\n"; got != want {
		t.Fatalf("attributes[assume_role_policy] = %q, want %q", got, want)
	}
	// Escaped quote: HCL evaluates `"svc\"quoted"` to `svc"quoted`. The old
	// byte-level reader would have returned `svc\"quoted` (backslash preserved).
	if got, want := attrs["name"], `svc"quoted`; got != want {
		t.Fatalf("attributes[name] = %q, want %q (escape must be resolved, not preserved)", got, want)
	}
}
