package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHelmIngesterRendersSCIPWorkersDefault(t *testing.T) {
	t.Parallel()

	manifests := renderHelmChart(t)
	ingester := requireHelmManifest(t, manifests, "StatefulSet", "eshu")
	env := helmEnvByName(requireHelmContainer(t, ingester, "ingester"))

	assertHelmLiteralEnv(t, env, "SCIP_WORKERS", "4")
}

func TestHelmIngesterRendersSCIPWorkersOverride(t *testing.T) {
	t.Parallel()

	manifests := renderHelmChart(t, "--set", "ingester.scipWorkers=2")
	ingester := requireHelmManifest(t, manifests, "StatefulSet", "eshu")
	env := helmEnvByName(requireHelmContainer(t, ingester, "ingester"))

	assertHelmLiteralEnv(t, env, "SCIP_WORKERS", "2")
}

func TestHelmSchemaPlacesSCIPWorkersUnderIngester(t *testing.T) {
	t.Parallel()

	schemaPath := filepath.Join(repositoryRoot(t), "deploy", "helm", "eshu", "values.schema.json")
	content, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read values.schema.json: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(content, &schema); err != nil {
		t.Fatalf("decode values.schema.json: %v", err)
	}

	apiProperties := helmTopLevelSchemaProperties(t, schema, "api")
	if _, ok := apiProperties["scipWorkers"]; ok {
		t.Fatal("api.scipWorkers present in values.schema.json, want ingester-owned setting")
	}
	ingesterProperties := helmTopLevelSchemaProperties(t, schema, "ingester")
	if _, ok := ingesterProperties["scipWorkers"]; !ok {
		t.Fatal("ingester.scipWorkers missing from values.schema.json")
	}
}

func helmTopLevelSchemaProperties(t *testing.T, schema map[string]any, componentName string) map[string]any {
	t.Helper()
	topProperties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("values.schema.json properties type = %T, want object", schema["properties"])
	}
	component, ok := topProperties[componentName].(map[string]any)
	if !ok {
		t.Fatalf("values.schema.json properties[%s] type = %T, want object", componentName, topProperties[componentName])
	}
	properties, ok := component["properties"].(map[string]any)
	if !ok {
		t.Fatalf("values.schema.json properties[%s].properties type = %T, want object", componentName, component["properties"])
	}
	return properties
}
