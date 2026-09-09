// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package admin_test pins the admin routes in the assembled OpenAPI document
// from outside the admin packages. These tests assert on the query root's
// OpenAPISpec (which stays in the root: scripts/verify-openapi.sh scans the
// top-level openapi_paths_*.go fragments only), so they import the root
// instead of living in package admin like the handler tests. Go permits this
// external test package to import the root even though the root imports the
// admin leaves.
package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
	"github.com/eshu-hq/eshu/go/internal/recovery"
)

func TestOpenAPISpecAdminPathsMatchMountedContract(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(query.OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	expectedPaths := []string{
		"/api/v0/admin/refinalize",
		"/api/v0/admin/reindex",
		"/api/v0/admin/shared-projection/tuning-report",
		"/api/v0/admin/work-items/query",
		"/api/v0/admin/decisions/query",
		"/api/v0/admin/dead-letters/query",
		"/api/v0/admin/dead-letter",
		"/api/v0/admin/skip",
		"/api/v0/admin/replay",
		"/api/v0/admin/backfill",
		"/api/v0/admin/replay-events/query",
	}

	for _, path := range expectedPaths {
		if _, ok := paths[path]; !ok {
			t.Fatalf("OpenAPI paths missing %s", path)
		}
	}
}

// specAdminStore is the minimal admin.Store double the recover-generations
// spec test needs: it scripts the idempotency claim/complete pair and
// reports empty results for every other read. It mirrors the internal
// stubAdminStore's claim behavior without importing package admin's
// internals, which an external test package cannot reach.
type specAdminStore struct {
	claim     admin.ReplayIdempotencyClaim
	completed bool
}

func (s *specAdminStore) ListWorkItems(_ context.Context, _ admin.WorkItemFilter) ([]admin.WorkItem, error) {
	return nil, nil
}

func (s *specAdminStore) ListDeadLetterWorkItems(_ context.Context, _ admin.DeadLetterListFilter) ([]admin.DeadLetterWorkItem, error) {
	return nil, nil
}

func (s *specAdminStore) ListReducerInputInvalidFacts(_ context.Context, _ admin.InputInvalidFactListFilter) ([]admin.InputInvalidFact, error) {
	return nil, nil
}

func (s *specAdminStore) DeadLetterWorkItems(_ context.Context, _ admin.DeadLetterFilter) ([]admin.WorkItem, error) {
	return nil, nil
}

func (s *specAdminStore) SkipRepositoryWorkItems(_ context.Context, _ string, _ string) ([]admin.WorkItem, error) {
	return nil, nil
}

func (s *specAdminStore) ReplayFailedWorkItems(_ context.Context, _ admin.ReplayWorkItemFilter) ([]admin.WorkItem, error) {
	return nil, nil
}

func (s *specAdminStore) ClaimReplayIdempotency(_ context.Context, _, _ string, _ time.Time) (admin.ReplayIdempotencyClaim, error) {
	return s.claim, nil
}

func (s *specAdminStore) CompleteReplayIdempotency(_ context.Context, _ string, _ int, _ []string, _ time.Time) error {
	s.completed = true
	return nil
}

func (s *specAdminStore) RequestBackfill(_ context.Context, _ admin.BackfillInput) (*admin.BackfillRequest, error) {
	return nil, nil
}

func (s *specAdminStore) ListReplayEvents(_ context.Context, _ admin.ReplayEventFilter) ([]admin.ReplayEvent, error) {
	return nil, nil
}

func (s *specAdminStore) ListDecisions(_ context.Context, _ admin.DecisionQueryFilter) ([]admin.DecisionRow, error) {
	return nil, nil
}

func (s *specAdminStore) ListEvidence(_ context.Context, _ string) ([]admin.EvidenceRow, error) {
	return nil, nil
}

// specRecoveryHandler scripts the recovery surface the recover-generations
// spec test needs, mirroring the internal stubRecoveryHandler.
type specRecoveryHandler struct {
	refinalizeResult recovery.RefinalizeResult
}

func (s *specRecoveryHandler) Refinalize(_ context.Context, _ recovery.RefinalizeFilter) (recovery.RefinalizeResult, error) {
	return s.refinalizeResult, nil
}

func (s *specRecoveryHandler) ReplayFailed(_ context.Context, _ recovery.ReplayFilter) (recovery.ReplayResult, error) {
	return recovery.ReplayResult{}, nil
}

func specAdminMux(h *admin.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	h.Mount(mux)
	return mux
}

func specPostJSON(mux *http.ServeMux, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func specDecodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v\nbody: %s", err, w.Body.String())
	}
	return got
}

// TestOpenAPIRecoverGenerationsResponsesMatchTheHandler holds the published
// contract to what the endpoint actually sends. The two 200 bodies are not the
// same shape: a recovery this call performed reports the three dedup counters,
// and an idempotent replay cannot, because the admin_replay_requests ledger does
// not persist them.
//
// A single documented shape covering both is how an operator ends up reading
// "reducer_work_deleted" in the reference and finding it absent from the reply
// to their retry -- exactly when the original response was lost and the counters
// are what they need. This drives the handler for both cases and compares the
// keys it wrote against the schema's required lists.
func TestOpenAPIRecoverGenerationsResponsesMatchTheHandler(t *testing.T) {
	freshStore := &specAdminStore{claim: admin.ReplayIdempotencyClaim{Claimed: true}}
	freshHandler := &admin.Handler{
		Recovery: &specRecoveryHandler{refinalizeResult: recovery.RefinalizeResult{
			Enqueued:               1,
			ScopeIDs:               []string{"scope-1"},
			ReducerWorkDeleted:     4,
			SharedIntentsReopened:  5,
			ReadinessPhasesCleared: 6,
		}},
		Store: freshStore,
	}
	fresh := specPostJSON(specAdminMux(freshHandler), "/api/v0/admin/recover-generations", map[string]any{
		"scope_ids":       []string{"scope-1"},
		"reason":          "wedged",
		"idempotency_key": "fresh-key",
	})
	if fresh.Code != http.StatusOK {
		t.Fatalf("fresh recovery status = %d, want %d; body: %s", fresh.Code, http.StatusOK, fresh.Body.String())
	}

	duplicateHandler := &admin.Handler{
		Recovery: &specRecoveryHandler{},
		Store: &specAdminStore{claim: admin.ReplayIdempotencyClaim{
			Claimed:       false,
			Status:        admin.ReplayRequestStatusCompleted,
			ReplayedCount: 1,
			WorkItemIDs:   []string{"scope-1"},
		}},
	}
	duplicate := specPostJSON(specAdminMux(duplicateHandler), "/api/v0/admin/recover-generations", map[string]any{
		"scope_ids":       []string{"scope-1"},
		"reason":          "retry",
		"idempotency_key": "dup-key",
	})
	if duplicate.Code != http.StatusOK {
		t.Fatalf("duplicate replay status = %d, want %d; body: %s", duplicate.Code, http.StatusOK, duplicate.Body.String())
	}

	variants := recoverGenerationsResponseVariants(t)
	for name, tc := range map[string]struct {
		body      map[string]any
		duplicate bool
	}{
		"recovery performed by this call": {body: specDecodeBody(t, fresh), duplicate: false},
		"idempotent replay":               {body: specDecodeBody(t, duplicate), duplicate: true},
	} {
		t.Run(name, func(t *testing.T) {
			required, ok := variants[tc.duplicate]
			if !ok {
				t.Fatalf("the OpenAPI 200 response has no variant for duplicate=%v", tc.duplicate)
			}
			if got := sortedResponseKeys(tc.body); strings.Join(got, ",") != strings.Join(required, ",") {
				t.Fatalf("the %s response sends %v but the OpenAPI 200 schema requires %v; "+
					"a client generated from this spec would read a field the endpoint never sends, "+
					"or miss one it does", name, got, required)
			}
		})
	}
}

// recoverGenerationsResponseVariants returns the required-property list of each
// 200 response variant, keyed by the value of its `duplicate` discriminator.
func recoverGenerationsResponseVariants(t *testing.T) map[bool][]string {
	t.Helper()

	var spec struct {
		Paths map[string]struct {
			Post struct {
				Responses map[string]struct {
					Content map[string]struct {
						Schema struct {
							OneOf []struct {
								Required   []string `json:"required"`
								Properties map[string]struct {
									Enum []any `json:"enum"`
								} `json:"properties"`
							} `json:"oneOf"`
						} `json:"schema"`
					} `json:"content"`
				} `json:"responses"`
			} `json:"post"`
		} `json:"paths"`
	}
	if err := json.Unmarshal([]byte(query.OpenAPISpec()), &spec); err != nil {
		t.Fatalf("OpenAPI spec is not valid JSON: %v", err)
	}

	schema := spec.Paths["/api/v0/admin/recover-generations"].Post.
		Responses["200"].Content["application/json"].Schema
	if len(schema.OneOf) != 2 {
		t.Fatalf("recover-generations 200 has %d response variants, want 2 (performed, replayed)", len(schema.OneOf))
	}

	variants := make(map[bool][]string, 2)
	for _, variant := range schema.OneOf {
		enum := variant.Properties["duplicate"].Enum
		if len(enum) != 1 {
			t.Fatalf("a 200 variant does not pin `duplicate` to one value, so a client cannot tell "+
				"the two shapes apart: %v", enum)
		}
		flag, ok := enum[0].(bool)
		if !ok {
			t.Fatalf("`duplicate` enum value is %T, want bool", enum[0])
		}
		required := append([]string(nil), variant.Required...)
		sort.Strings(required)
		variants[flag] = required
	}
	return variants
}

// sortedResponseKeys returns a response body's top-level keys in sorted order.
func sortedResponseKeys(body map[string]any) []string {
	keys := make([]string, 0, len(body))
	for key := range body {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// TestProviderConfigKindOpenAPISpecMatchesAcceptedKinds is the OpenAPI-enum
// half of the provider-kind lockstep (issue #5166): the admin write path
// started accepting provider_kind "github" while the component enums still
// listed only oidc/saml, and scripts/verify-openapi.sh never inspects
// component-schema enum completeness, so the stale enum passed that gate
// silently.
//
// Every kind the write path accepts MUST appear in
// AdminProviderConfigWriteRequest.provider_kind's enum with nothing else, and
// its stored external_<kind> form MUST appear in AdminProviderConfig's enum
// with nothing else. The builder half lives in admin/provider/config
// (kinds_test.go, calling the unexported builder directly), and both halves
// read the one shared list in querytestutil — add a kind to the builder, the
// spec enums, and that list together.
func TestProviderConfigKindOpenAPISpecMatchesAcceptedKinds(t *testing.T) {
	t.Parallel()

	writeKindGroups := querytestutil.AcceptedProviderConfigKinds

	var spec map[string]any
	if err := json.Unmarshal([]byte(query.OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	schemas := querytestutil.MustMapField(t, querytestutil.MustMapField(t, spec, "components"), "schemas")

	// Write-request enum must equal the accepted-kind set exactly.
	writeReq := querytestutil.MustMapField(t, schemas, "AdminProviderConfigWriteRequest")
	writeProps := querytestutil.MustMapField(t, writeReq, "properties")
	writeEnum := enumStrings(t, querytestutil.MustMapField(t, writeProps, "provider_kind"), "AdminProviderConfigWriteRequest.provider_kind")
	assertSetEqual(t, "AdminProviderConfigWriteRequest.provider_kind enum", writeEnum, writeKindGroups)

	// The github field group's properties must be documented on the write
	// request (base_url/api_base_url are optional, allowed_orgs is the
	// required-non-empty github field — its presence as a documented property
	// is what this asserts; the required-ness is enforced server-side).
	for _, prop := range []string{"base_url", "api_base_url", "allowed_orgs"} {
		if _, ok := writeProps[prop]; !ok {
			t.Errorf("AdminProviderConfigWriteRequest.properties missing %q (github field group, issue #5166)", prop)
		}
	}

	// Read-view enum must equal the stored external_<kind> forms exactly.
	readView := querytestutil.MustMapField(t, schemas, "AdminProviderConfig")
	readEnum := enumStrings(t, querytestutil.MustMapField(t, querytestutil.MustMapField(t, readView, "properties"), "provider_kind"), "AdminProviderConfig.provider_kind")
	storedKinds := make([]string, 0, len(writeKindGroups))
	for _, kind := range writeKindGroups {
		storedKinds = append(storedKinds, "external_"+kind)
	}
	assertSetEqual(t, "AdminProviderConfig.provider_kind enum", readEnum, storedKinds)
}

// assertSetEqual reports a diff between an enum's sorted values and the
// wanted set. Moved with the lockstep spec half from the query root.
func assertSetEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	gotSorted := append([]string(nil), got...)
	wantSorted := append([]string(nil), want...)
	sort.Strings(gotSorted)
	sort.Strings(wantSorted)
	if strings.Join(gotSorted, ",") != strings.Join(wantSorted, ",") {
		t.Errorf("%s = %v, want exactly %v", label, gotSorted, wantSorted)
	}
}

// enumStrings extracts the sorted enum values of one schema property.
// Test-local mirror of the query root's openapi_repository_stats_test.go
// helper, which this external package cannot import.
func enumStrings(t *testing.T, prop map[string]any, field string) []string {
	t.Helper()
	raw, ok := prop["enum"]
	if !ok {
		t.Fatalf("%s schema missing enum array", field)
	}
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("%s enum is not an array, got %T", field, raw)
	}
	out := make([]string, 0, len(items))
	for _, v := range items {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("%s enum item is not a string: %v (%T)", field, v, v)
		}
		out = append(out, s)
	}
	return out
}

// TestOpenAPIRecoverGenerationsRequestRejectsBothModes documents the request
// rule the handler enforces: exactly one of a non-empty scope_ids or
// all_scopes: true. A generated client that sent both, or neither, would get a
// 400 with nothing in the schema to explain why.
func TestOpenAPIRecoverGenerationsRequestRejectsBothModes(t *testing.T) {
	var spec struct {
		Paths map[string]struct {
			Post struct {
				RequestBody struct {
					Content map[string]struct {
						Schema struct {
							OneOf []map[string]any `json:"oneOf"`
						} `json:"schema"`
					} `json:"content"`
				} `json:"requestBody"`
			} `json:"post"`
		} `json:"paths"`
	}
	if err := json.Unmarshal([]byte(query.OpenAPISpec()), &spec); err != nil {
		t.Fatalf("OpenAPI spec is not valid JSON: %v", err)
	}

	oneOf := spec.Paths["/api/v0/admin/recover-generations"].Post.
		RequestBody.Content["application/json"].Schema.OneOf
	if len(oneOf) != 2 {
		t.Fatalf("recover-generations request schema has %d oneOf branches, want 2 "+
			"(named scopes, or all_scopes); the handler rejects any other combination", len(oneOf))
	}
}
