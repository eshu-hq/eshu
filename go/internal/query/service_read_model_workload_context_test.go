package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetServiceContextFallsBackToRepositoryWorkloadIdentity(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runByMatch: map[string][]map[string]any{
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
			},
		},
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{{
				ID:   "repo-serverless",
				Name: "serverless-job",
				Path: "/repos/serverless-job",
			}},
			summary: repositoryReadModelSummary{
				Available:     true,
				WorkloadNames: []string{"serverless-job"},
			},
			entities: []EntityContent{
				{
					RepoID:       "repo-serverless",
					RelativePath: "template.yml",
					EntityType:   "CloudFormationResource",
					EntityName:   "ProcessRecords",
					Language:     "yaml",
					Metadata: map[string]any{
						"resource_type": "AWS::Serverless::Function",
					},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/serverless-job/context", nil)
	req.SetPathValue("service_name", "serverless-job")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp["id"], "workload:serverless-job"; got != want {
		t.Fatalf("id = %#v, want %#v", got, want)
	}
	if got, want := resp["materialization_status"], "identity_only"; got != want {
		t.Fatalf("materialization_status = %#v, want %#v", got, want)
	}
	deploymentEvidence := mapValue(resp, "deployment_evidence")
	familyPaths := mapSliceValue(deploymentEvidence, "delivery_family_paths")
	cloudFormation := requireRepositoryStoryDeliveryFamily(familyPaths, "cloudformation")
	if cloudFormation == nil {
		t.Fatalf("delivery_family_paths = %#v, want cloudformation family", familyPaths)
	}
	if got, want := cloudFormation["mode"], "serverless_delivery"; got != want {
		t.Fatalf("cloudformation.mode = %#v, want %#v", got, want)
	}
}
