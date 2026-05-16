package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesSBOMAttestationAttachments(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/sbom-attestations/attachments")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listSBOMAttestationAttachments"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
}
