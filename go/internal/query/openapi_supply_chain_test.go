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

func TestOpenAPISpecIncludesSupplyChainImpactFindings(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/impact/findings")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listSupplyChainImpactFindings"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	responses := mustMapField(t, get, "responses")
	twoHundred := mustMapField(t, responses, "200")
	content := mustMapField(t, twoHundred, "content")
	appJSON := mustMapField(t, content, "application/json")
	schema := mustMapField(t, appJSON, "schema")
	properties := mustMapField(t, schema, "properties")
	readiness, ok := properties["readiness"].(map[string]any)
	if !ok {
		t.Fatalf("properties[readiness] = %T, want map describing readiness envelope", properties["readiness"])
	}
	readinessProps := mustMapField(t, readiness, "properties")
	for _, key := range []string{
		"readiness_state",
		"target_scope",
		"evidence_sources",
		"source_snapshots",
		"source_states",
		"missing_evidence",
		"incomplete_reasons",
		"freshness",
		"counts",
	} {
		if _, ok := readinessProps[key]; !ok {
			t.Fatalf("readiness.properties missing %q field", key)
		}
	}
	if _, ok := readinessProps["unsupported_targets"]; ok {
		t.Fatalf("readiness.properties must not include unsupported_targets; field was dropped pending a real producer")
	}
	freshness := mustMapField(t, readinessProps, "freshness")
	enum := mustStringSliceField(t, freshness, "enum")
	for _, want := range []string{"fresh", "stale", "unknown", "pending", "rate_limited", "failed", "partial"} {
		if !containsOpenAPIEnumString(enum, want) {
			t.Fatalf("readiness.freshness enum = %#v, want %q", enum, want)
		}
	}
}

func TestOpenAPISpecIncludesSupplyChainImpactExplain(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/impact/explain")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "explainSupplyChainImpact"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	parameters, ok := get["parameters"].([]any)
	if !ok {
		t.Fatalf("parameters = %T, want []any", get["parameters"])
	}
	var sawFindingID bool
	for _, parameter := range parameters {
		param, ok := parameter.(map[string]any)
		if !ok {
			t.Fatalf("parameter = %T, want map[string]any", parameter)
		}
		if param["name"] == "finding_id" {
			sawFindingID = true
		}
	}
	if !sawFindingID {
		t.Fatal("parameters missing finding_id")
	}
}

func TestOpenAPISpecIncludesContainerImageIdentities(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/supply-chain/container-images/identities")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listContainerImageIdentities"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
}

func mustStringSliceField(t *testing.T, m map[string]any, key string) []string {
	t.Helper()
	values, ok := m[key].([]any)
	if !ok {
		t.Fatalf("%s = %T, want []any", key, m[key])
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("%s element = %T, want string", key, value)
		}
		out = append(out, text)
	}
	return out
}

func containsOpenAPIEnumString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
