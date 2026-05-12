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
  name: api-node-search
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
                      - path: argocd/api-node-search/overlays/*/config.yaml
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
          path: argocd/api-node-search/overlays/{{.environment}}
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

	assertNamedBucketContains(t, got, "argocd_applicationsets", "api-node-search")
	assertBucketContainsFieldValue(t, got, "argocd_applicationsets", "source_repos", "https://github.com/example-org/deployment-charts")
	assertBucketContainsFieldValue(t, got, "argocd_applicationsets", "source_paths", "argocd/api-node-search/overlays/*/config.yaml,argocd/api-node-search/overlays/{{.environment}}")
	assertBucketContainsFieldValue(t, got, "argocd_applicationsets", "source_roots", "argocd/api-node-search/")
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

func TestDefaultEngineParsePathYAMLCrossplaneResources(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "crossplane.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xiamroles.iam.aws.myorg.io
spec:
  group: iam.aws.myorg.io
  names:
    kind: XIAMRole
    plural: xiamroles
  claimNames:
    kind: IAMRole
    plural: iamroles
---
apiVersion: apiextensions.crossplane.io/v1
kind: Composition
metadata:
  name: iam-role-composition
spec:
  compositeTypeRef:
    apiVersion: iam.aws.myorg.io/v1alpha1
    kind: XIAMRole
  resources:
    - name: iam-role
---
apiVersion: iam.aws.myorg.crossplane.io/v1alpha1
kind: IAMRole
metadata:
  name: my-service-role
  namespace: default
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

	assertNamedBucketContains(t, got, "crossplane_xrds", "xiamroles.iam.aws.myorg.io")
	assertBucketContainsFieldValue(t, got, "crossplane_xrds", "claim_kind", "IAMRole")
	assertNamedBucketContains(t, got, "crossplane_compositions", "iam-role-composition")
	assertBucketContainsFieldValue(t, got, "crossplane_compositions", "composite_kind", "XIAMRole")
	assertNamedBucketContains(t, got, "crossplane_claims", "my-service-role")
	assertBucketContainsFieldValue(t, got, "crossplane_claims", "api_version", "iam.aws.myorg.crossplane.io/v1alpha1")
}

func TestDefaultEngineParsePathYAMLKustomizeAndHelm(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	kustomizePath := filepath.Join(repoRoot, "kustomization.yaml")
	writeTestFile(
		t,
		kustomizePath,
		`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: production
resources:
  - ../base
  - ../app
patches:
  - path: patches/replicas.yaml
`,
	)

	chartPath := filepath.Join(repoRoot, "Chart.yaml")
	writeTestFile(
		t,
		chartPath,
		`name: my-api-chart
version: 0.1.0
appVersion: 1.0.0
dependencies:
  - name: redis
    repository: https://charts.example.test/redis
`,
	)

	valuesPath := filepath.Join(repoRoot, "values.yaml")
	writeTestFile(
		t,
		valuesPath,
		`replicaCount: 2
image:
  repository: ghcr.io/example/app
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	kustomizePayload, err := engine.ParsePath(repoRoot, kustomizePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", kustomizePath, err)
	}
	assertNamedBucketContains(t, kustomizePayload, "kustomize_overlays", "kustomization")
	assertBucketContainsFieldValue(t, kustomizePayload, "kustomize_overlays", "namespace", "production")
	kustomizeOverlays := kustomizePayload["kustomize_overlays"].([]map[string]any)
	if len(kustomizeOverlays) != 1 {
		t.Fatalf("kustomize_overlays = %#v, want one overlay", kustomizeOverlays)
	}
	bases, ok := kustomizeOverlays[0]["bases"].([]string)
	if !ok {
		t.Fatalf("kustomize_overlays[0].bases = %T, want []string", kustomizeOverlays[0]["bases"])
	}
	if len(bases) != 2 || bases[0] != "../app" || bases[1] != "../base" {
		t.Fatalf("kustomize_overlays[0].bases = %#v, want [../app ../base]", bases)
	}
	chartPayload, err := engine.ParsePath(repoRoot, chartPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", chartPath, err)
	}
	assertNamedBucketContains(t, chartPayload, "helm_charts", "my-api-chart")
	assertBucketContainsFieldValue(t, chartPayload, "helm_charts", "dependencies", "redis")
	assertBucketContainsFieldValue(t, chartPayload, "helm_charts", "dependency_repositories", "https://charts.example.test/redis")

	valuesPayload, err := engine.ParsePath(repoRoot, valuesPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", valuesPath, err)
	}
	assertNamedBucketContains(t, valuesPayload, "helm_values", "values")
	assertBucketContainsFieldValue(t, valuesPayload, "helm_values", "image_repositories", "ghcr.io/example/app")
	assertBucketContainsFieldValue(t, valuesPayload, "helm_values", "top_level_keys", "image,replicaCount")
}

func TestDefaultEngineParsePathYAMLKustomizePatchTargets(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "kustomization.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
patches:
  - target:
      kind: Deployment
      name: comprehensive-app
    patch: |-
      - op: replace
        path: /spec/replicas
        value: 1
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}

	overlays := payload["kustomize_overlays"].([]map[string]any)
	if len(overlays) != 1 {
		t.Fatalf("kustomize_overlays = %#v, want one overlay", overlays)
	}
	patchTargets, ok := overlays[0]["patch_targets"].([]string)
	if !ok {
		t.Fatalf("kustomize_overlays[0].patch_targets = %T, want []string", overlays[0]["patch_targets"])
	}
	if len(patchTargets) != 1 || patchTargets[0] != "Deployment/comprehensive-app" {
		t.Fatalf("kustomize_overlays[0].patch_targets = %#v, want [Deployment/comprehensive-app]", patchTargets)
	}
}

func TestDefaultEngineParsePathYAMLKustomizeTypedDeployReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "kustomization.yaml")
	writeTestFile(
		t,
		filePath,
		`apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../base
  - https://github.com/myorg/shared-manifests.git//payments?ref=main
components:
  - shared/component
helmCharts:
  - name: nginx
    repo: https://charts.bitnami.com/bitnami
    releaseName: ingress-nginx
images:
  - name: nginx
    newName: ghcr.io/example/nginx
`,
	)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	payload, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", filePath, err)
	}

	overlays := payload["kustomize_overlays"].([]map[string]any)
	if len(overlays) != 1 {
		t.Fatalf("kustomize_overlays = %#v, want one overlay", overlays)
	}

	resourceRefs, ok := overlays[0]["resource_refs"].([]string)
	if !ok {
		t.Fatalf("kustomize_overlays[0].resource_refs = %T, want []string", overlays[0]["resource_refs"])
	}
	if len(resourceRefs) != 2 || resourceRefs[0] != "https://github.com/myorg/shared-manifests.git//payments?ref=main" || resourceRefs[1] != "shared/component" {
		t.Fatalf("kustomize_overlays[0].resource_refs = %#v, want [https://github.com/myorg/shared-manifests.git//payments?ref=main shared/component]", resourceRefs)
	}

	helmRefs, ok := overlays[0]["helm_refs"].([]string)
	if !ok {
		t.Fatalf("kustomize_overlays[0].helm_refs = %T, want []string", overlays[0]["helm_refs"])
	}
	if len(helmRefs) != 3 || helmRefs[0] != "https://charts.bitnami.com/bitnami" || helmRefs[1] != "ingress-nginx" || helmRefs[2] != "nginx" {
		t.Fatalf("kustomize_overlays[0].helm_refs = %#v, want [https://charts.bitnami.com/bitnami ingress-nginx nginx]", helmRefs)
	}

	imageRefs, ok := overlays[0]["image_refs"].([]string)
	if !ok {
		t.Fatalf("kustomize_overlays[0].image_refs = %T, want []string", overlays[0]["image_refs"])
	}
	if len(imageRefs) != 2 || imageRefs[0] != "ghcr.io/example/nginx" || imageRefs[1] != "nginx" {
		t.Fatalf("kustomize_overlays[0].image_refs = %#v, want [ghcr.io/example/nginx nginx]", imageRefs)
	}
}

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
