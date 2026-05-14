package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
)

func TestHandleDeadIaCReturnsScopedDerivedFindings(t *testing.T) {
	t.Parallel()

	handler := &IaCHandler{
		Profile: ProfileLocalAuthoritative,
		Content: fakeIaCDeadContentStore{
			files: map[string][]FileContent{
				"terraform-stack": {
					{RepoID: "terraform-stack", RelativePath: "main.tf", Content: `
variable "dynamic_module_name" {
  default = "dynamic-target"
}
module "checkout_service" {
  source = "../terraform-modules/modules/checkout-service"
}
module "dynamic_target" {
  source = "../terraform-modules/modules/${var.dynamic_module_name}"
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
					{RepoID: "helm-controller", RelativePath: ".github/workflows/deploy-worker.yaml", Content: `run: helm upgrade --install worker-service ../helm-charts/charts/worker-service`},
				},
				"helm-charts": {
					{RepoID: "helm-charts", RelativePath: "charts/checkout-service/Chart.yaml", Content: `name: checkout-service`},
					{RepoID: "helm-charts", RelativePath: "charts/worker-service/Chart.yaml", Content: `name: worker-service`},
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
					{RepoID: "ansible-ops", RelativePath: "playbooks/orphan-maintenance.yml", Content: `roles:
  - orphan_maintenance`},
					{RepoID: "ansible-ops", RelativePath: "roles/checkout_deploy/tasks/main.yml", Content: `- debug: msg=used`},
					{RepoID: "ansible-ops", RelativePath: "roles/orphan_maintenance/tasks/main.yml", Content: `- debug: msg=unused`},
					{RepoID: "ansible-ops", RelativePath: "roles/dynamic_role/tasks/main.yml", Content: `- debug: msg=dynamic`},
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
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/iac/dead", bytes.NewBufferString(`{
		"repo_ids": ["terraform-stack", "terraform-modules", "helm-controller", "helm-charts", "ansible-controller", "ansible-ops", "compose-controller", "compose-app"],
		"include_ambiguous": true
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if resp.Truth == nil {
		t.Fatal("truth envelope missing")
	}
	if got, want := resp.Truth.Level, TruthLevelDerived; got != want {
		t.Fatalf("truth.level = %q, want %q", got, want)
	}
	if got, want := resp.Truth.Capability, "iac_quality.dead_iac"; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", resp.Data)
	}
	rawFindings, ok := data["findings"].([]any)
	if !ok {
		t.Fatalf("findings type = %T, want []any", data["findings"])
	}
	var gotIDs []string
	gotReachability := map[string]string{}
	for _, raw := range rawFindings {
		finding, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("finding type = %T, want map[string]any", raw)
		}
		id := finding["id"].(string)
		gotIDs = append(gotIDs, id)
		gotReachability[id] = finding["reachability"].(string)
	}
	sort.Strings(gotIDs)
	wantIDs := []string{
		"ansible:ansible-ops:roles/dynamic_role",
		"ansible:ansible-ops:roles/orphan_maintenance",
		"compose:compose-app:services/dynamic-target",
		"compose:compose-app:services/orphan-cache",
		"helm:helm-charts:charts/dynamic-target",
		"helm:helm-charts:charts/orphan-worker",
		"terraform:terraform-modules:modules/dynamic-target",
		"terraform:terraform-modules:modules/orphan-cache",
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("finding ids = %#v, want %#v", gotIDs, wantIDs)
	}
	for _, id := range []string{
		"ansible:ansible-ops:roles/dynamic_role",
		"compose:compose-app:services/dynamic-target",
		"helm:helm-charts:charts/dynamic-target",
		"terraform:terraform-modules:modules/dynamic-target",
	} {
		if got, want := gotReachability[id], "ambiguous"; got != want {
			t.Fatalf("%s reachability = %q, want %q", id, got, want)
		}
	}
	for _, id := range []string{
		"ansible:ansible-ops:roles/orphan_maintenance",
		"compose:compose-app:services/orphan-cache",
		"helm:helm-charts:charts/orphan-worker",
		"terraform:terraform-modules:modules/orphan-cache",
	} {
		if got, want := gotReachability[id], "unused"; got != want {
			t.Fatalf("%s reachability = %q, want %q", id, got, want)
		}
	}
}
