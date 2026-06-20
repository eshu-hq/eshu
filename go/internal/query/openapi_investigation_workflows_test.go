package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesInvestigationWorkflowRoutes(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths := spec["paths"].(map[string]any)
	list := paths["/api/v0/investigation-workflows"].(map[string]any)["get"].(map[string]any)
	if got, want := list["operationId"], "listInvestigationWorkflows"; got != want {
		t.Fatalf("list operationId = %#v, want %#v", got, want)
	}
	resolve := paths["/api/v0/investigation-workflows/resolve"].(map[string]any)["post"].(map[string]any)
	if got, want := resolve["operationId"], "resolveInvestigationWorkflow"; got != want {
		t.Fatalf("resolve operationId = %#v, want %#v", got, want)
	}
}
