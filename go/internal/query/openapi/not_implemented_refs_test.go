// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// serviceUnavailableResponseRef is the shared 503 response. Its description is
// the reader-fence and graph-outage contract, which never answers 501.
const serviceUnavailableResponseRef = "#/components/responses/ServiceUnavailable"

// notImplementedServiceUnavailableRefs returns "<path> <method>" for every
// operation in spec whose "501" response references the shared 503 response,
// sorted. A 501 is a profile or read-model verdict; documenting it as a
// retryable outage tells a client to retry a request that cannot succeed.
func notImplementedServiceUnavailableRefs(spec map[string]any) []string {
	var findings []string
	paths, _ := spec["paths"].(map[string]any)
	for path, item := range paths {
		operations, _ := item.(map[string]any)
		for method, operation := range operations {
			op, _ := operation.(map[string]any)
			responses, _ := op["responses"].(map[string]any)
			response, _ := responses["501"].(map[string]any)
			if ref, _ := response["$ref"].(string); ref == serviceUnavailableResponseRef {
				findings = append(findings, path+" "+method)
			}
		}
	}
	sort.Strings(findings)
	return findings
}

// TestOpenAPISpecNotImplementedResponsesAvoidServiceUnavailable is the #7674
// guard: no "501" response may reference the shared ServiceUnavailable
// response.
func TestOpenAPISpecNotImplementedResponsesAvoidServiceUnavailable(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(Spec()) error = %v, want nil", err)
	}
	if findings := notImplementedServiceUnavailableRefs(spec); len(findings) > 0 {
		t.Fatalf("501 responses reference %s; use #/components/responses/NotImplemented or an inline description:\n%v",
			serviceUnavailableResponseRef, findings)
	}
}

// TestNotImplementedServiceUnavailableRefsSeeded proves the guard's walker on
// a synthetic spec: it reports a planted 501 -> ServiceUnavailable ref and
// passes the allowed shapes (the NotImplemented ref, an inline description,
// and a 503 that references ServiceUnavailable).
func TestNotImplementedServiceUnavailableRefsSeeded(t *testing.T) {
	t.Parallel()

	const spec = `{"paths": {
		"/planted": {"post": {"responses": {"501": {"$ref": "#/components/responses/ServiceUnavailable"}}}},
		"/ref": {"post": {"responses": {"501": {"$ref": "#/components/responses/NotImplemented"}}}},
		"/inline": {"get": {"responses": {"501": {"description": "read model unavailable"}}}},
		"/outage": {"get": {"responses": {"503": {"$ref": "#/components/responses/ServiceUnavailable"}}}}
	}}`
	var parsed map[string]any
	if err := json.Unmarshal([]byte(spec), &parsed); err != nil {
		t.Fatalf("json.Unmarshal(seeded spec) error = %v, want nil", err)
	}
	if got, want := notImplementedServiceUnavailableRefs(parsed), []string{"/planted post"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("findings = %v, want %v", got, want)
	}
}
