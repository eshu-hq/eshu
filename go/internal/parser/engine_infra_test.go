package parser

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultEngineParsePathJSONPackageJSON(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "package.json")
	writeTestFile(
		t,
		filePath,
		`{
  "name": "demo",
  "scripts": {
    "build": "tsc -p ."
  },
  "dependencies": {
    "react": "^19.0.0"
  }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got["lang"] != "json" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "json")
	}

	assertNamedBucketContains(t, got, "functions", "build")
	assertNamedBucketContains(t, got, "variables", "react")
	assertBucketContainsFieldValue(t, got, "variables", "section", "dependencies")
	assertJSONTopLevelKeysContain(t, got, "name", "scripts", "dependencies")
}

func TestDefaultEngineParsePathJSONCloudFormation(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "template.json")
	writeTestFile(
		t,
		filePath,
		`{
  "AWSTemplateFormatVersion": "2010-09-09",
  "Parameters": {
    "StageName": {
      "Type": "String"
    }
  },
  "Resources": {
    "HelloFunction": {
      "Type": "AWS::Lambda::Function",
      "Properties": {
        "Handler": "index.handler",
        "Runtime": "python3.12"
      }
    }
  },
  "Outputs": {
    "ApiUrl": {
      "Value": "https://example.com"
    }
  }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "cloudformation_resources", "HelloFunction")
	assertNamedBucketContains(t, got, "cloudformation_parameters", "StageName")
	assertNamedBucketContains(t, got, "cloudformation_outputs", "ApiUrl")
	assertBucketContainsFieldValue(t, got, "cloudformation_resources", "resource_type", "AWS::Lambda::Function")
}

func TestDefaultEngineParsePathJSONCloudFormationSAMTransformList(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "serverless.json")
	writeTestFile(
		t,
		filePath,
		`{
  "Transform": ["AWS::Serverless-2016-10-31"],
  "Parameters": {
    "Environment": {
      "Default": "dev"
    }
  },
  "Resources": {
    "ApiFunction": {
      "Type": "Custom::Function",
      "Properties": {
        "VpcId": {
          "Fn::ImportValue": "SharedVpcId"
        }
      }
    }
  },
  "Outputs": {
    "VpcId": {
      "Value": "vpc-12345",
      "Export": {
        "Name": "ServiceVpcId"
      }
    }
  }
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "cloudformation_resources", "ApiFunction")
	assertNamedBucketContains(t, got, "cloudformation_parameters", "Environment")
	assertNamedBucketContains(t, got, "cloudformation_outputs", "VpcId")
	assertNamedBucketContains(t, got, "cloudformation_cross_stack_imports", "SharedVpcId")
	assertNamedBucketContains(t, got, "cloudformation_cross_stack_exports", "ServiceVpcId")
}

func TestDefaultEngineParsePathHCLTerraform(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "main.tf")
	writeTestFile(
		t,
		filePath,
		`terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = "us-east-1"
}

variable "region" {
  type = string
  description = "AWS region"
}

locals {
  environment = "dev"
}

data "aws_caller_identity" "current" {}

resource "aws_s3_bucket" "logs" {}

module "service" {
  source = "./modules/service"
  version = "1.2.3"
}

output "bucket_name" {
  value = aws_s3_bucket.logs.id
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got["lang"] != "hcl" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "hcl")
	}

	assertNamedBucketContains(t, got, "terraform_resources", "aws_s3_bucket.logs")
	assertNamedBucketContains(t, got, "terraform_variables", "region")
	assertNamedBucketContains(t, got, "terraform_outputs", "bucket_name")
	assertNamedBucketContains(t, got, "terraform_modules", "service")
	assertNamedBucketContains(t, got, "terraform_data_sources", "aws_caller_identity.current")
	assertNamedBucketContains(t, got, "terraform_providers", "aws")
	assertNamedBucketContains(t, got, "terraform_locals", "environment")
	callerIdentity := findNamedBucketItem(t, got, "terraform_data_sources", "aws_caller_identity.current")
	if got, want := callerIdentity["provider"], "aws"; got != want {
		t.Fatalf("terraform_data_sources[aws_caller_identity.current].provider = %#v, want %#v", got, want)
	}
	if got, want := callerIdentity["resource_service"], "caller_identity"; got != want {
		t.Fatalf("terraform_data_sources[aws_caller_identity.current].resource_service = %#v, want %#v", got, want)
	}
	if got, want := callerIdentity["resource_category"], "governance"; got != want {
		t.Fatalf("terraform_data_sources[aws_caller_identity.current].resource_category = %#v, want %#v", got, want)
	}
	assertBucketContainsFieldValue(t, got, "terraform_providers", "source", "hashicorp/aws")
	assertBucketContainsFieldValue(t, got, "terraform_modules", "source", "./modules/service")
	if got["artifact_type"] != "terraform_hcl" {
		t.Fatalf("artifact_type = %#v, want %#v", got["artifact_type"], "terraform_hcl")
	}
	if _, ok := got["template_dialect"]; ok {
		t.Fatalf("template_dialect = %#v, want field omitted", got["template_dialect"])
	}
	iacRelevant, ok := got["iac_relevant"].(bool)
	if !ok {
		t.Fatalf("iac_relevant = %T, want bool", got["iac_relevant"])
	}
	if !iacRelevant {
		t.Fatalf("iac_relevant = %#v, want true", got["iac_relevant"])
	}
}

func TestDefaultEngineParsePathHCLTerragrunt(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "terragrunt.hcl")
	writeTestFile(
		t,
		filePath,
		`terraform {
  source = "../modules/app"
}

include "root" {
  path = find_in_parent_folders()
}

locals {
  env = "dev"
}

inputs = {
  image_tag = "latest"
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "terragrunt_configs", "terragrunt")
	assertBucketContainsFieldValue(t, got, "terragrunt_configs", "terraform_source", "../modules/app")
	assertBucketContainsFieldValue(t, got, "terragrunt_configs", "includes", "root")
	assertBucketContainsFieldValue(t, got, "terragrunt_configs", "inputs", "image_tag")
	assertBucketContainsFieldValue(t, got, "terragrunt_configs", "locals", "env")
}

func TestDefaultEngineParsePathHCLTerragruntBuildsFirstClassDependencyLocalAndInputEntities(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "terragrunt.hcl")
	writeTestFile(
		t,
		filePath,
		`terraform {
  source = "../modules/app"
}

dependency "vpc" {
  config_path = "../vpc"
}

locals {
  env = "dev"
}

inputs = {
  image_tag = "latest"
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "terragrunt_dependencies", "vpc")
	assertBucketContainsFieldValue(t, got, "terragrunt_dependencies", "config_path", "../vpc")
	assertNamedBucketContains(t, got, "terragrunt_locals", "env")
	assertBucketContainsFieldValue(t, got, "terragrunt_locals", "value", "dev")
	assertNamedBucketContains(t, got, "terragrunt_inputs", "image_tag")
	assertBucketContainsFieldValue(t, got, "terragrunt_inputs", "value", "latest")
}

func TestDefaultEngineParsePathHCLTerragruntIncludesEmptyLocalsAndInputs(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "terragrunt.hcl")
	writeTestFile(
		t,
		filePath,
		`terraform {
  source = "../modules/app"
}

include "root" {
  path = find_in_parent_folders()
}
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertNamedBucketContains(t, got, "terragrunt_configs", "terragrunt")
	assertBucketContainsFieldValue(t, got, "terragrunt_configs", "includes", "root")

	configs, ok := got["terragrunt_configs"].([]map[string]any)
	if !ok {
		t.Fatalf("terragrunt_configs = %T, want []map[string]any", got["terragrunt_configs"])
	}
	if len(configs) != 1 {
		t.Fatalf("len(terragrunt_configs) = %d, want 1", len(configs))
	}

	localsValue, hasLocals := configs[0]["locals"]
	if !hasLocals {
		t.Fatalf("terragrunt config missing \"locals\" field: %#v", configs[0])
	}
	if gotLocals, wantLocals := localsValue, any(""); gotLocals != wantLocals {
		t.Fatalf("terragrunt locals = %#v, want %#v", gotLocals, wantLocals)
	}

	inputsValue, hasInputs := configs[0]["inputs"]
	if !hasInputs {
		t.Fatalf("terragrunt config missing \"inputs\" field: %#v", configs[0])
	}
	if gotInputs, wantInputs := inputsValue, any(""); gotInputs != wantInputs {
		t.Fatalf("terragrunt inputs = %#v, want %#v", gotInputs, wantInputs)
	}
}

func TestDefaultEngineParsePathDockerfile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "Dockerfile")
	writeTestFile(
		t,
		filePath,
		`FROM golang:1.24 AS builder
ARG TARGETOS=linux
ENV CGO_ENABLED=0
RUN go build ./...

FROM alpine:3.20
COPY --from=builder /out/app /app
LABEL org.opencontainers.image.source="github.com/example/repo"
EXPOSE 8080/tcp
ENTRYPOINT ["/app"]
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got["lang"] != "dockerfile" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "dockerfile")
	}

	assertNamedBucketContains(t, got, "dockerfile_stages", "builder")
	assertNamedBucketContains(t, got, "dockerfile_stages", "alpine")
	assertNamedBucketContains(t, got, "dockerfile_args", "TARGETOS")
	assertNamedBucketContains(t, got, "dockerfile_envs", "CGO_ENABLED")
	assertNamedBucketContains(t, got, "dockerfile_ports", "alpine:8080")
	assertNamedBucketContains(t, got, "dockerfile_labels", "org.opencontainers.image.source")
	assertBucketContainsFieldValue(t, got, "dockerfile_stages", "copies_from", "builder")
}

func TestDefaultEngineParsePathYAMLKubernetes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "deployment.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo
  namespace: prod
  labels:
    tier: backend
    app: demo
spec:
  template:
    spec:
      containers:
        - name: app
          image: ghcr.io/example/app:1.0.0
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got["lang"] != "yaml" {
		t.Fatalf("lang = %#v, want %#v", got["lang"], "yaml")
	}

	assertNamedBucketContains(t, got, "k8s_resources", "demo")
	assertBucketContainsFieldValue(t, got, "k8s_resources", "kind", "Deployment")
	assertBucketContainsFieldValue(t, got, "k8s_resources", "container_images", "ghcr.io/example/app:1.0.0")
	assertBucketContainsFieldValue(t, got, "k8s_resources", "labels", "app=demo,tier=backend")
}

func assertJSONTopLevelKeysContain(
	t *testing.T,
	payload map[string]any,
	wantKeys ...string,
) {
	t.Helper()

	metadata, ok := payload["json_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("json_metadata = %T, want map[string]any", payload["json_metadata"])
	}
	rawKeys, ok := metadata["top_level_keys"].([]string)
	if ok {
		for _, wantKey := range wantKeys {
			if !slicesContain(rawKeys, wantKey) {
				t.Fatalf("json_metadata.top_level_keys = %#v, want key %q", rawKeys, wantKey)
			}
		}
		return
	}

	interfaces, ok := metadata["top_level_keys"].([]any)
	if !ok {
		t.Fatalf("json_metadata.top_level_keys = %T, want []string or []any", metadata["top_level_keys"])
	}
	keys := make([]string, 0, len(interfaces))
	for _, item := range interfaces {
		keys = append(keys, item.(string))
	}
	for _, wantKey := range wantKeys {
		if !slicesContain(keys, wantKey) {
			t.Fatalf("json_metadata.top_level_keys = %#v, want key %q", keys, wantKey)
		}
	}
}

func slicesContain(values []string, want string) bool {
	return reflect.ValueOf(values).IsValid() && strings.Contains(","+strings.Join(values, ",")+",", ","+want+",")
}
