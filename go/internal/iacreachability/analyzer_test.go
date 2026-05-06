package iacreachability

import (
	"reflect"
	"sort"
	"testing"
)

func TestAnalyzeClassifiesUsedUnusedAndAmbiguousIaCArtifacts(t *testing.T) {
	t.Parallel()

	rows := Analyze(map[string][]File{
		"terraform-stack": {
			{RepoID: "terraform-stack", RelativePath: "main.tf", Content: `
module "checkout_service" {
  source = "../terraform-modules/modules/checkout-service"
}
module "dynamic_target" {
  source = "../terraform-modules/modules/${var.dynamic_module_name}"
}
variable "dynamic_module_name" {
  default = "dynamic-target"
}`},
		},
		"terraform-modules": {
			{RepoID: "terraform-modules", RelativePath: "modules/checkout-service/main.tf", Content: `resource "aws_ecs_service" "this" {}`},
			{RepoID: "terraform-modules", RelativePath: "modules/orphan-cache/main.tf", Content: `resource "aws_elasticache_cluster" "this" {}`},
			{RepoID: "terraform-modules", RelativePath: "modules/dynamic-target/main.tf", Content: `resource "aws_lambda_function" "this" {}`},
		},
		"helm-controller": {
			{RepoID: "helm-controller", RelativePath: "argocd/applications/checkout-service.yaml", Content: `path: charts/checkout-service`},
			{RepoID: "helm-controller", RelativePath: "argocd/applications/dynamic-target.yaml", Content: `path: "charts/{{service}}"
- service: dynamic-target`},
		},
		"helm-charts": {
			{RepoID: "helm-charts", RelativePath: "charts/checkout-service/Chart.yaml", Content: `name: checkout-service`},
			{RepoID: "helm-charts", RelativePath: "charts/orphan-worker/Chart.yaml", Content: `name: orphan-worker`},
			{RepoID: "helm-charts", RelativePath: "charts/dynamic-target/Chart.yaml", Content: `name: dynamic-target`},
		},
		"ansible-controller": {
			{RepoID: "ansible-controller", RelativePath: ".github/workflows/deploy-ops.yaml", Content: `run: ansible-playbook ../ansible-ops/playbooks/site.yml`},
			{RepoID: "ansible-controller", RelativePath: "jenkins/Jenkinsfile", Content: `sh 'ansible-playbook ../ansible-ops/playbooks/dynamic-role.yml --extra-vars selected_role=dynamic_role'`},
		},
		"ansible-ops": {
			{RepoID: "ansible-ops", RelativePath: "playbooks/site.yml", Content: `roles:
  - checkout_deploy`},
			{RepoID: "ansible-ops", RelativePath: "playbooks/dynamic-role.yml", Content: `roles:
  - "{{ selected_role }}"
vars:
  selected_role: dynamic_role`},
			{RepoID: "ansible-ops", RelativePath: "roles/checkout_deploy/tasks/main.yml", Content: `- debug: msg=used`},
			{RepoID: "ansible-ops", RelativePath: "roles/orphan_maintenance/tasks/main.yml", Content: `- debug: msg=unused`},
			{RepoID: "ansible-ops", RelativePath: "roles/dynamic_role/tasks/main.yml", Content: `- debug: msg=dynamic`},
		},
		"kustomize-controller": {
			{RepoID: "kustomize-controller", RelativePath: "argocd/applications/checkout-prod.yaml", Content: `path: overlays/prod`},
			{RepoID: "kustomize-controller", RelativePath: "argocd/applications/dynamic-target.yaml", Content: `path: "base/{{service}}"
- service: dynamic-target`},
		},
		"kustomize-config": {
			{RepoID: "kustomize-config", RelativePath: "overlays/prod/kustomization.yaml", Content: `resources:
  - ../../base/checkout-service`},
			{RepoID: "kustomize-config", RelativePath: "base/checkout-service/kustomization.yaml", Content: `resources:
  - deployment.yaml`},
			{RepoID: "kustomize-config", RelativePath: "base/orphan-api/kustomization.yaml", Content: `resources:
  - deployment.yaml`},
			{RepoID: "kustomize-config", RelativePath: "base/dynamic-target/kustomization.yaml", Content: `resources:
  - deployment.yaml`},
		},
		"compose-controller": {
			{RepoID: "compose-controller", RelativePath: ".github/workflows/deploy-compose.yaml", Content: `steps:
  - run: docker compose -f ../compose-app/compose.yaml up -d api worker
  - run: docker compose -f ../compose-app/compose.yaml up -d ${SERVICE_NAME}
env:
  SERVICE_NAME: dynamic-target`},
		},
		"compose-app": {
			{RepoID: "compose-app", RelativePath: "compose.yaml", Content: `services:
  api:
    image: example/api:latest
  worker:
    image: example/worker:latest
  orphan-cache:
    image: example/cache:latest
  dynamic-target:
    image: example/dynamic:latest`},
		},
	}, Options{IncludeAmbiguous: true})

	got := map[string]Reachability{}
	for _, row := range rows {
		got[row.ID] = row.Reachability
	}
	want := map[string]Reachability{
		"ansible:ansible-ops:roles/checkout_deploy":            ReachabilityUsed,
		"ansible:ansible-ops:roles/dynamic_role":               ReachabilityAmbiguous,
		"ansible:ansible-ops:roles/orphan_maintenance":         ReachabilityUnused,
		"helm:helm-charts:charts/checkout-service":             ReachabilityUsed,
		"helm:helm-charts:charts/dynamic-target":               ReachabilityAmbiguous,
		"helm:helm-charts:charts/orphan-worker":                ReachabilityUnused,
		"kustomize:kustomize-config:base/checkout-service":     ReachabilityUsed,
		"kustomize:kustomize-config:base/dynamic-target":       ReachabilityAmbiguous,
		"kustomize:kustomize-config:base/orphan-api":           ReachabilityUnused,
		"kustomize:kustomize-config:overlays/prod":             ReachabilityUsed,
		"compose:compose-app:services/api":                     ReachabilityUsed,
		"compose:compose-app:services/dynamic-target":          ReachabilityAmbiguous,
		"compose:compose-app:services/orphan-cache":            ReachabilityUnused,
		"compose:compose-app:services/worker":                  ReachabilityUsed,
		"terraform:terraform-modules:modules/checkout-service": ReachabilityUsed,
		"terraform:terraform-modules:modules/dynamic-target":   ReachabilityAmbiguous,
		"terraform:terraform-modules:modules/orphan-cache":     ReachabilityUnused,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reachability rows = %#v, want %#v", got, want)
	}
}

func TestAnalyzeFiltersFamiliesAndAmbiguousRows(t *testing.T) {
	t.Parallel()

	rows := Analyze(map[string][]File{
		"helm-controller": {
			{RepoID: "helm-controller", RelativePath: "app.yaml", Content: `path: "charts/{{service}}"
- service: dynamic-target`},
		},
		"helm-charts": {
			{RepoID: "helm-charts", RelativePath: "charts/dynamic-target/Chart.yaml", Content: `name: dynamic-target`},
			{RepoID: "helm-charts", RelativePath: "charts/orphan-worker/Chart.yaml", Content: `name: orphan-worker`},
		},
		"terraform-modules": {
			{RepoID: "terraform-modules", RelativePath: "modules/orphan-cache/main.tf", Content: `resource "aws_elasticache_cluster" "this" {}`},
		},
	}, Options{Families: map[string]bool{"helm": true}})

	var gotIDs []string
	for _, row := range rows {
		gotIDs = append(gotIDs, row.ID)
		if row.Family != "helm" {
			t.Fatalf("family = %q, want helm", row.Family)
		}
		if row.Reachability == ReachabilityAmbiguous {
			t.Fatalf("ambiguous row %q returned when IncludeAmbiguous=false", row.ID)
		}
	}
	sort.Strings(gotIDs)
	wantIDs := []string{"helm:helm-charts:charts/orphan-worker"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("row ids = %#v, want %#v", gotIDs, wantIDs)
	}
}
