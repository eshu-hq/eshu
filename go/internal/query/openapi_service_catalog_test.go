// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPISpecIncludesServiceCatalogCorrelations(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := testutil.MustMapField(t, spec, "paths")
	path := testutil.MustMapField(t, paths, "/api/v0/service-catalog/correlations")
	get := testutil.MustMapField(t, path, "get")
	if got, want := get["operationId"], "listServiceCatalogCorrelations"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	responses := testutil.MustMapField(t, get, "responses")
	okResponse := testutil.MustMapField(t, responses, "200")
	content := testutil.MustMapField(t, testutil.MustMapField(t, okResponse, "content"), "application/json")
	schema := testutil.MustMapField(t, content, "schema")
	properties := testutil.MustMapField(t, schema, "properties")
	correlations := testutil.MustMapField(t, properties, "correlations")
	items := testutil.MustMapField(t, correlations, "items")
	itemProperties := testutil.MustMapField(t, items, "properties")
	if got, want := testutil.MustMapField(t, itemProperties, "provenance_only")["type"], "boolean"; got != want {
		t.Fatalf("provenance_only type = %#v, want %#v", got, want)
	}
	candidates := testutil.MustMapField(t, itemProperties, "candidate_repository_ids")
	if got, want := candidates["type"], "array"; got != want {
		t.Fatalf("candidate_repository_ids type = %#v, want %#v", got, want)
	}
	requiredAnchors := testutil.MustMapField(t, itemProperties, "required_anchor_keys")
	if got, want := requiredAnchors["type"], "array"; got != want {
		t.Fatalf("required_anchor_keys type = %#v, want %#v", got, want)
	}
	missing := testutil.MustMapField(t, properties, "missing_evidence")
	if got, want := missing["type"], "array"; got != want {
		t.Fatalf("missing_evidence type = %#v, want %#v", got, want)
	}
	evidenceSummary := testutil.MustMapField(t, properties, "evidence_summary")
	evidenceProperties := testutil.MustMapField(t, evidenceSummary, "properties")
	localDescriptors := testutil.MustMapField(t, evidenceProperties, "local_descriptors")
	localProperties := testutil.MustMapField(t, localDescriptors, "properties")
	if got, want := testutil.MustMapField(t, localProperties, "source_uris")["type"], "array"; got != want {
		t.Fatalf("local_descriptors.source_uris type = %#v, want %#v", got, want)
	}
	external := testutil.MustMapField(t, evidenceProperties, "external_catalog_confirmation")
	externalProperties := testutil.MustMapField(t, external, "properties")
	if got, want := testutil.MustMapField(t, externalProperties, "reason")["type"], "string"; got != want {
		t.Fatalf("external_catalog_confirmation.reason type = %#v, want %#v", got, want)
	}
}
