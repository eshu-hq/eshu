package parser

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultEngineParsePathJSONPreservesDocumentOrderForMetadataAndConfigBuckets(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "package.json")
	writeTestFile(
		t,
		filePath,
		`{
  "zeta": true,
  "alpha": true,
  "scripts": {
    "lint": "eslint .",
    "build": "tsc -p ."
  },
  "dependencies": {
    "zlib": "^1.0.0",
    "alpha-lib": "^2.0.0"
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

	if got, want := jsonTopLevelKeys(t, got), []string{"zeta", "alpha", "scripts", "dependencies"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("json top-level keys = %#v, want %#v", got, want)
	}
	if got, want := bucketNames(t, got, "functions"), []string{"lint", "build"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("function names = %#v, want %#v", got, want)
	}
	if got, want := bucketNames(t, got, "variables"), []string{"zlib", "alpha-lib"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("variable names = %#v, want %#v", got, want)
	}
}

func TestDefaultEngineParsePathJSONWarehouseReplay(t *testing.T) {
	t.Parallel()

	filePath := repoFixturePath("ecosystems", "warehouse_replay_comprehensive", "warehouse_replay.json")
	repoRoot := filepath.Dir(filePath)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got, want := bucketNames(t, got, "query_executions"), []string{"daily_revenue_build", "revenue_dashboard_lookup"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("query executions = %#v, want %#v", got, want)
	}
	assertRelationshipPresent(t, got, "RUNS_QUERY_AGAINST", "daily_revenue_build", "analytics.finance.revenue")
	assertCoverageState(t, got, "complete")
}

func TestDefaultEngineParsePathJSONBIReplay(t *testing.T) {
	t.Parallel()

	filePath := repoFixturePath("ecosystems", "bi_replay_comprehensive", "bi_replay.json")
	repoRoot := filepath.Dir(filePath)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got, want := bucketNames(t, got, "dashboard_assets"), []string{"Revenue Overview"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dashboard assets = %#v, want %#v", got, want)
	}
	assertRelationshipPresent(t, got, "POWERS", "analytics.finance.daily_revenue", "Revenue Overview")
	assertCoverageState(t, got, "complete")
}

func TestDefaultEngineParsePathJSONSemanticReplay(t *testing.T) {
	t.Parallel()

	filePath := repoFixturePath("ecosystems", "semantic_replay_comprehensive", "semantic_replay.json")
	repoRoot := filepath.Dir(filePath)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got, want := bucketNames(t, got, "data_assets"), []string{"semantic.finance.revenue_semantic"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("data assets = %#v, want %#v", got, want)
	}
	if got, want := bucketNames(t, got, "data_columns"), []string{
		"semantic.finance.revenue_semantic.customer_tier",
		"semantic.finance.revenue_semantic.gross_amount",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("data columns = %#v, want %#v", got, want)
	}
	assertRelationshipPresent(t, got, "ASSET_DERIVES_FROM", "semantic.finance.revenue_semantic", "analytics.finance.daily_revenue")
	assertRelationshipPresent(t, got, "COLUMN_DERIVES_FROM", "semantic.finance.revenue_semantic.gross_amount", "analytics.finance.daily_revenue.gross_amount")
	assertCoverageState(t, got, "complete")
}

func TestDefaultEngineParsePathJSONQualityReplay(t *testing.T) {
	t.Parallel()

	filePath := repoFixturePath("ecosystems", "quality_replay_comprehensive", "quality_replay.json")
	repoRoot := filepath.Dir(filePath)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got, want := bucketNames(t, got, "data_quality_checks"), []string{
		"daily_revenue_freshness",
		"gross_amount_non_negative",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("data quality checks = %#v, want %#v", got, want)
	}
	assertRelationshipPresent(t, got, "ASSERTS_QUALITY_ON", "daily_revenue_freshness", "analytics.finance.daily_revenue")
	assertRelationshipPresent(t, got, "ASSERTS_QUALITY_ON", "gross_amount_non_negative", "analytics.finance.daily_revenue.gross_amount")
	assertCoverageState(t, got, "complete")
}

func TestDefaultEngineParsePathJSONGovernanceReplay(t *testing.T) {
	t.Parallel()

	filePath := repoFixturePath("ecosystems", "governance_replay_comprehensive", "governance_replay.json")
	repoRoot := filepath.Dir(filePath)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	if got, want := bucketNames(t, got, "data_owners"), []string{"Finance Analytics"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("data owners = %#v, want %#v", got, want)
	}
	if got, want := bucketNames(t, got, "data_contracts"), []string{"daily_revenue_contract"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("data contracts = %#v, want %#v", got, want)
	}
	assertRelationshipPresent(t, got, "OWNS", "Finance Analytics", "analytics.finance.daily_revenue")
	assertCoverageState(t, got, "complete")
	assertGovernanceAnnotationPresent(t, got, "analytics.finance.daily_revenue.customer_email")
}

func TestDefaultEngineParsePathJSONStripsHelmDirectivePreamble(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "base.json")
	writeTestFile(
		t,
		filePath,
		`{{- $env := required "env is required" .Values.env | trim -}}
{{- $accountId := required "accountId is required" .Values.accountId | trim -}}
{
  "sample-service-api": {
    "client": {
      "hostname": "sample-service-api.{{ $env }}.example.test",
      "port": 3081
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

	if got, want := jsonTopLevelKeys(t, got), []string{"sample-service-api"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("json top-level keys = %#v, want %#v", got, want)
	}
}

func bucketNames(t *testing.T, payload map[string]any, key string) []string {
	t.Helper()

	items, ok := payload[key].([]map[string]any)
	if ok {
		names := make([]string, 0, len(items))
		for _, item := range items {
			name, _ := item["name"].(string)
			names = append(names, name)
		}
		return names
	}

	rawItems, ok := payload[key].([]any)
	if !ok {
		t.Fatalf("%s = %T, want []map[string]any or []any", key, payload[key])
	}
	names := make([]string, 0, len(rawItems))
	for _, rawItem := range rawItems {
		item := rawItem.(map[string]any)
		name, _ := item["name"].(string)
		names = append(names, name)
	}
	return names
}

func jsonTopLevelKeys(t *testing.T, payload map[string]any) []string {
	t.Helper()

	metadata, ok := payload["json_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("json_metadata = %T, want map[string]any", payload["json_metadata"])
	}
	if keys, ok := metadata["top_level_keys"].([]string); ok {
		return keys
	}
	rawKeys, ok := metadata["top_level_keys"].([]any)
	if !ok {
		t.Fatalf("json_metadata.top_level_keys = %T, want []string or []any", metadata["top_level_keys"])
	}
	keys := make([]string, 0, len(rawKeys))
	for _, rawKey := range rawKeys {
		keys = append(keys, rawKey.(string))
	}
	return keys
}

func assertRelationshipPresent(t *testing.T, payload map[string]any, relationshipType string, sourceName string, targetName string) {
	t.Helper()

	items, ok := payload["data_relationships"].([]map[string]any)
	if !ok {
		rawItems, ok := payload["data_relationships"].([]any)
		if !ok {
			t.Fatalf("data_relationships = %T, want []map[string]any or []any", payload["data_relationships"])
		}
		items = make([]map[string]any, 0, len(rawItems))
		for _, rawItem := range rawItems {
			items = append(items, rawItem.(map[string]any))
		}
	}

	for _, item := range items {
		if item["type"] == relationshipType && item["source_name"] == sourceName && item["target_name"] == targetName {
			return
		}
	}
	t.Fatalf("missing relationship type=%q source=%q target=%q in %#v", relationshipType, sourceName, targetName, items)
}

func assertCoverageState(t *testing.T, payload map[string]any, want string) {
	t.Helper()

	coverage, ok := payload["data_intelligence_coverage"].(map[string]any)
	if !ok {
		t.Fatalf("data_intelligence_coverage = %T, want map[string]any", payload["data_intelligence_coverage"])
	}
	if got := coverage["state"]; got != want {
		t.Fatalf("data_intelligence_coverage.state = %#v, want %#v", got, want)
	}
}

func assertGovernanceAnnotationPresent(t *testing.T, payload map[string]any, targetName string) {
	t.Helper()

	items, ok := payload["data_governance_annotations"].([]map[string]any)
	if !ok {
		rawItems, ok := payload["data_governance_annotations"].([]any)
		if !ok {
			t.Fatalf("data_governance_annotations = %T, want []map[string]any or []any", payload["data_governance_annotations"])
		}
		items = make([]map[string]any, 0, len(rawItems))
		for _, rawItem := range rawItems {
			items = append(items, rawItem.(map[string]any))
		}
	}

	for _, item := range items {
		if item["target_name"] == targetName && item["is_protected"] == true {
			return
		}
	}
	t.Fatalf("missing protected governance annotation for %q in %#v", targetName, items)
}
