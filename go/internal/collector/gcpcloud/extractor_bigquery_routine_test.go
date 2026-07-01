// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const bigQueryRoutineAssetName = "//bigquery.googleapis.com/projects/demo-project/datasets/analytics/routines/enrich"

func bigQueryRoutineContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: bigQueryRoutineAssetName,
		AssetType:        assetTypeBigQueryRoutine,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestBigQueryRoutineExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeBigQueryRoutine); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeBigQueryRoutine)
	}
}

func TestExtractBigQueryRoutineRemoteFunction(t *testing.T) {
	const data = `{
		"routineReference": {"projectId": "demo-project", "datasetId": "analytics", "routineId": "enrich"},
		"routineType": "SCALAR_FUNCTION",
		"language": "JAVASCRIPT",
		"arguments": [{"name": "x"}, {"name": "y"}],
		"returnType": {"typeKind": "STRING"},
		"definitionBody": "return secretLogic(x, y);",
		"importedLibraries": ["gs://demo-libs/enrich.js", "gs://demo-libs/util.js"],
		"remoteFunctionOptions": {"endpoint": "https://region-demo.cloudfunctions.net/enrich", "connection": "projects/demo-project/locations/us/connections/remote"},
		"creationTime": "1717200000000"
	}`

	got, err := extractBigQueryRoutine(bigQueryRoutineContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"routine_type":           "SCALAR_FUNCTION",
		"language":               "JAVASCRIPT",
		"argument_count":         2,
		"return_type_kind":       "STRING",
		"has_definition_body":    true,
		"imported_library_count": 2,
		"creation_time":          "1717200000000",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	assertRelationship(t, got.Relationships, relationshipTypeRoutineInDataset,
		"//bigquery.googleapis.com/projects/demo-project/datasets/analytics", assetTypeBigQueryDataset)
	assertRelationship(t, got.Relationships, relationshipTypeRoutineUsesConnection,
		"//bigqueryconnection.googleapis.com/projects/demo-project/locations/us/connections/remote", assetTypeBigQueryConnection)
	if len(got.Relationships) != 2 {
		t.Fatalf("expected dataset + connection edges, got %d: %#v", len(got.Relationships), got.Relationships)
	}

	// The routine definition body (user code) must never leak.
	blob, _ := json.Marshal(got)
	for _, token := range []string{"secretLogic", "definitionBody", "return "} {
		if containsString(string(blob), token) {
			t.Fatalf("routine extraction leaked definition body token %q: %s", token, blob)
		}
	}
}

func TestExtractBigQueryRoutineSQLProcedureDerivesDatasetFromFullName(t *testing.T) {
	// No routineReference: the parent dataset must be derived by trimming the
	// routine segment from the routine's own full resource name.
	const data = `{"routineType": "PROCEDURE", "language": "SQL", "definitionBody": "BEGIN END"}`
	got, err := extractBigQueryRoutine(bigQueryRoutineContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["routine_type"] != "PROCEDURE" {
		t.Errorf("routine_type = %v, want PROCEDURE", got.Attributes["routine_type"])
	}
	if _, ok := got.Attributes["return_type_kind"]; ok {
		t.Errorf("a procedure has no return type: %#v", got.Attributes)
	}
	assertRelationship(t, got.Relationships, relationshipTypeRoutineInDataset,
		"//bigquery.googleapis.com/projects/demo-project/datasets/analytics", assetTypeBigQueryDataset)
	if len(got.Relationships) != 1 {
		t.Fatalf("expected only the dataset edge, got %#v", got.Relationships)
	}
}

func TestExtractBigQueryRoutineEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractBigQueryRoutine(ExtractContext{AssetType: assetTypeBigQueryRoutine, Data: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships (no dataset derivable), got %#v", got.Relationships)
	}
}

func TestExtractBigQueryRoutineMalformedDataErrors(t *testing.T) {
	if _, err := extractBigQueryRoutine(bigQueryRoutineContext(`{bad`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
	if _, err := extractBigQueryRoutine(bigQueryRoutineContext(``)); err == nil {
		t.Fatalf("expected an error for empty resource data")
	}
}

func TestBigQueryConnectionFullName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"relative", "projects/p/locations/l/connections/c", "//bigqueryconnection.googleapis.com/projects/p/locations/l/connections/c"},
		{"leading slash", "/projects/p/locations/l/connections/c", "//bigqueryconnection.googleapis.com/projects/p/locations/l/connections/c"},
		{"already full name", "//bigqueryconnection.googleapis.com/projects/p/locations/l/connections/c", "//bigqueryconnection.googleapis.com/projects/p/locations/l/connections/c"},
		{"not a connection", "projects/p/datasets/d", ""},
		{"blank", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bigQueryConnectionFullName(tc.in); got != tc.want {
				t.Errorf("bigQueryConnectionFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
