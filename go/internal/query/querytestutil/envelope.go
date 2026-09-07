// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ArgoCDControllerFixture builds a minimal argocd_application controller map
// as BuildDeploymentSourceControllerEntity would produce it. It moved here
// from the impact handler tests with lane B2 of #6060 for the same reason
// as K8sResourceFixture.
func ArgoCDControllerFixture(appName string) map[string]any {
	return map[string]any{"controller_kind": "argocd_application", "entity_name": appName}
}

// K8sResourceFixture builds the minimal K8s resource map the live-evidence
// identity tests feed anchor resolution: kind, entity name, namespace, and
// API version. It moved here from the impact handler tests with lane B2 of
// #6060 because root, impact/, and impacttrace/ tests all need it and test
// files cannot share helpers across packages.
func K8sResourceFixture(kind, name, namespace, apiVersion string) map[string]any {
	return map[string]any{
		"kind":        kind,
		"entity_name": name,
		"namespace":   namespace,
		"api_version": apiVersion,
	}
}

// AssertAnswerMetadata checks the normalized answer companion on an
// already-shaped response map: presence, AnswerMetadata type, schema
// version, and non-nil coverage/evidence/missing/limitation/partial/next
// slots. It moved here from the query root with lane B2 of #6060 because
// root and impact/ response tests both pin it and test files cannot share
// helpers across packages.
func AssertAnswerMetadata(t *testing.T, name string, data map[string]any) {
	t.Helper()

	raw, ok := data["answer_metadata"]
	if !ok {
		t.Fatalf("%s missing answer_metadata: %#v", name, data)
	}
	metadata, ok := raw.(querycontract.AnswerMetadata)
	if !ok {
		t.Fatalf("%s answer_metadata type = %T, want AnswerMetadata", name, raw)
	}
	if metadata.SchemaVersion != querycontract.AnswerMetadataSchemaVersion {
		t.Fatalf("%s schema_version = %q, want %q", name, metadata.SchemaVersion, querycontract.AnswerMetadataSchemaVersion)
	}
	if metadata.Coverage == nil {
		t.Fatalf("%s coverage is nil", name)
	}
	if metadata.EvidenceHandles == nil {
		t.Fatalf("%s evidence_handles is nil", name)
	}
	if metadata.MissingEvidence == nil {
		t.Fatalf("%s missing_evidence is nil", name)
	}
	if metadata.Limitations == nil {
		t.Fatalf("%s limitations is nil", name)
	}
	if metadata.PartialReasons == nil {
		t.Fatalf("%s partial_reasons is nil", name)
	}
	if metadata.RecommendedNextCalls == nil {
		t.Fatalf("%s recommended_next_calls is nil", name)
	}
}

// DecodeImpactEnvelopeData asserts an HTTP 200 envelope response and returns
// its data map. It is the shared home for the envelope-data decode the query
// root, impact/, and impacttrace/ handler tests all need; it moved here from
// the query root with lane B2 of #6060 so the subpackage tests can use it
// without importing the root package.
func DecodeImpactEnvelopeData(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", envelope.Data)
	}
	return data
}
