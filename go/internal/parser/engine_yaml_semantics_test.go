// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathYAMLArgoCDApplication(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "application.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: iac-eks-addons
  namespace: argocd
spec:
  project: platform
  source:
    repoURL: https://github.com/myorg/iac-eks-argocd.git
    path: overlays/production/addons/cert-manager
    targetRevision: main
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
      allowEmpty: true
    syncOptions:
      - CreateNamespace=true
      - PruneLast=true
  destination:
    server: https://kubernetes.default.svc
    namespace: cert-manager
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

	assertNamedBucketContains(t, got, "argocd_applications", "iac-eks-addons")
	assertBucketContainsFieldValue(t, got, "argocd_applications", "source_repo", "https://github.com/myorg/iac-eks-argocd.git")
	assertBucketContainsFieldValue(t, got, "argocd_applications", "source_path", "overlays/production/addons/cert-manager")
	assertBucketContainsFieldValue(t, got, "argocd_applications", "source_root", "overlays/")
	assertBucketContainsFieldValue(t, got, "argocd_applications", "dest_server", "https://kubernetes.default.svc")
	assertBucketContainsFieldValue(t, got, "argocd_applications", "dest_namespace", "cert-manager")
	assertBucketContainsFieldValue(t, got, "argocd_applications", "sync_policy", "automated(prune=true,selfHeal=true,allowEmpty=true),syncOptions=CreateNamespace=true|PruneLast=true")
	assertBucketContainsFieldValue(t, got, "argocd_applications", "sync_policy_options", "CreateNamespace=true|PruneLast=true")
}

func TestDefaultEngineParsePathYAMLArgoCDApplicationMultiSource(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "multi-source-application.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: comprehensive-multi-source
  namespace: argocd
spec:
  project: platform
  sources:
    - repoURL: https://github.com/myorg/helm-charts.git
      path: charts/comprehensive-app
      targetRevision: main
      helm:
        valueFiles:
          - $values/production/values.yaml
    - repoURL: https://github.com/myorg/config-repo.git
      targetRevision: main
      ref: values
  destination:
    server: https://kubernetes.default.svc
    namespace: production
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

	assertNamedBucketContains(t, got, "argocd_applications", "comprehensive-multi-source")
	assertBucketContainsFieldValue(t, got, "argocd_applications", "source_repo", "https://github.com/myorg/helm-charts.git")
	assertBucketContainsFieldValue(t, got, "argocd_applications", "source_path", "charts/comprehensive-app")
	assertBucketContainsFieldValue(t, got, "argocd_applications", "source_repos", "https://github.com/myorg/helm-charts.git,https://github.com/myorg/config-repo.git")
	assertBucketContainsFieldValue(t, got, "argocd_applications", "source_paths", "charts/comprehensive-app,")
	assertBucketContainsFieldValue(t, got, "argocd_applications", "source_revisions", "main,main")
	assertBucketContainsFieldValue(t, got, "argocd_applications", "source_roots", "charts/comprehensive-app/,")
	assertBucketContainsFieldValue(t, got, "argocd_applications", "dest_namespace", "production")
}

func TestDefaultEngineParsePathYAMLArgoCDApplicationSetNestedSources(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "applicationset.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: svc-search
  namespace: argocd
spec:
  generators:
    - merge:
        generators:
          - matrix:
              generators:
                - git:
                    repoURL: https://github.com/example-org/deployment-charts
                    files:
                      - path: argocd/svc-search/overlays/*/config.yaml
                - list:
                    elements:
                      - cluster: prod
          - plugin:
              configMapRef:
                name: argocd-generator-plugin
  template:
    spec:
      project: "{{.argocd.project}}"
      sources:
        - repoURL: "{{.git.repoURL}}"
          path: argocd/svc-search/overlays/{{.environment}}
      destination:
        namespace: "{{.helm.namespace}}"
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

	assertNamedBucketContains(t, got, "argocd_applicationsets", "svc-search")
	assertBucketContainsFieldValue(t, got, "argocd_applicationsets", "source_repos", "https://github.com/example-org/deployment-charts")
	assertBucketContainsFieldValue(t, got, "argocd_applicationsets", "source_paths", "argocd/svc-search/overlays/*/config.yaml,argocd/svc-search/overlays/{{.environment}}")
	assertBucketContainsFieldValue(t, got, "argocd_applicationsets", "source_roots", "argocd/svc-search/")
	assertBucketContainsFieldValue(t, got, "argocd_applicationsets", "generators", "git,list,matrix,merge,plugin")
}

func TestDefaultEngineParsePathYAMLArgoCDApplicationSetPreservesGeneratorAndTemplateSources(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "applicationset-sources.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: platform-appset
  namespace: argocd
spec:
  generators:
    - git:
        repoURL: https://github.com/myorg/platform-config.git
        files:
          - path: argocd/platform/*/config.yaml
  template:
    spec:
      project: platform
      source:
        repoURL: https://github.com/myorg/platform-runtime.git
        path: deploy/overlays/prod
      destination:
        server: https://kubernetes.default.svc
        namespace: platform
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

	assertNamedBucketContains(t, got, "argocd_applicationsets", "platform-appset")
	assertBucketContainsFieldValue(t, got, "argocd_applicationsets", "generator_source_repos", "https://github.com/myorg/platform-config.git")
	assertBucketContainsFieldValue(t, got, "argocd_applicationsets", "generator_source_paths", "argocd/platform/*/config.yaml")
	assertBucketContainsFieldValue(t, got, "argocd_applicationsets", "template_source_repos", "https://github.com/myorg/platform-runtime.git")
	assertBucketContainsFieldValue(t, got, "argocd_applicationsets", "template_source_paths", "deploy/overlays/prod")
	assertBucketContainsFieldValue(t, got, "argocd_applicationsets", "template_source_roots", "deploy/")
	assertBucketContainsFieldValue(t, got, "argocd_applicationsets", "dest_server", "https://kubernetes.default.svc")
}

// TestDefaultEngineParsePathYAMLCrossplaneResources moved to
// engine_yaml_semantics_crossplane_test.go to keep this file under the
// repo's 500-line package-file cap (issue #5347).
//
// TestDefaultEngineParsePathYAMLKustomizeAndHelm,
// TestDefaultEngineParsePathYAMLKustomizePatchTargets,
// TestDefaultEngineParsePathYAMLKustomizeTypedDeployReferences, and
// TestDefaultEngineParsePathYAMLKustomizeImageOverrides moved to
// engine_yaml_semantics_kustomize_test.go for the same reason (issue #5440).

func TestDefaultEngineParsePathYAMLCloudFormation(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "stack.yaml")
	writeTestFile(
		t,
		filePath,
		`AWSTemplateFormatVersion: "2010-09-09"
Conditions:
  EnableNested: !Equals [!Ref Env, prod]
Parameters:
  Env:
    Type: String
    Default: dev
Resources:
  DataBucket:
    Type: AWS::S3::Bucket
  RolePolicy:
    Type: AWS::IAM::Policy
    DependsOn:
      - DataBucket
  NestedStack:
    Type: AWS::CloudFormation::Stack
    Condition: EnableNested
    Properties:
      TemplateURL: https://example.com/nested-stack.yaml
      Parameters:
        ImportedValue: !ImportValue SharedVpcId
Outputs:
  BucketArn:
    Value: !GetAtt DataBucket.Arn
    Export:
      Name: Stack-BucketArn
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

	assertNamedBucketContains(t, got, "cloudformation_resources", "DataBucket")
	assertBucketContainsFieldValue(t, got, "cloudformation_resources", "resource_type", "AWS::S3::Bucket")
	assertBucketContainsFieldValue(t, got, "cloudformation_resources", "depends_on", "DataBucket")
	assertBucketContainsFieldValue(t, got, "cloudformation_resources", "template_url", "https://example.com/nested-stack.yaml")
	assertNamedBucketContains(t, got, "cloudformation_parameters", "Env")
	assertNamedBucketContains(t, got, "cloudformation_outputs", "BucketArn")
	assertNamedBucketContains(t, got, "cloudformation_conditions", "EnableNested")
	conditions := got["cloudformation_conditions"].([]map[string]any)
	if gotValue, want := conditions[0]["evaluated"], true; gotValue != want {
		t.Fatalf("cloudformation_conditions[0][evaluated] = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := conditions[0]["evaluated_value"], false; gotValue != want {
		t.Fatalf("cloudformation_conditions[0][evaluated_value] = %#v, want %#v", gotValue, want)
	}
	resources := got["cloudformation_resources"].([]map[string]any)
	var nestedStack map[string]any
	for _, resource := range resources {
		if resource["name"] == "NestedStack" {
			nestedStack = resource
			break
		}
	}
	if nestedStack == nil {
		t.Fatal("NestedStack resource not found")
	}
	if gotValue, want := nestedStack["condition_evaluated"], true; gotValue != want {
		t.Fatalf("NestedStack condition_evaluated = %#v, want %#v", gotValue, want)
	}
	if gotValue, want := nestedStack["condition_value"], false; gotValue != want {
		t.Fatalf("NestedStack condition_value = %#v, want %#v", gotValue, want)
	}
	assertNamedBucketContains(t, got, "cloudformation_cross_stack_imports", "SharedVpcId")
	assertNamedBucketContains(t, got, "cloudformation_cross_stack_exports", "Stack-BucketArn")
	assertBucketContainsFieldValue(t, got, "cloudformation_outputs", "export_name", "Stack-BucketArn")
}
