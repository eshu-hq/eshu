// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

// stubGraphQuery is a GraphQuery fake that optionally reports its
// configured state.
type stubGraphQuery struct {
	GraphQuery
	configured bool
}

func (s stubGraphQuery) GraphConfigured() bool { return s.configured }

// plainGraphQuery implements GraphQuery without the configured report.
type plainGraphQuery struct{ GraphQuery }

// TestGraphConfigured proves nil reads unconfigured, reporting readers
// decide, and silent readers count as configured.
func TestGraphConfigured(t *testing.T) {
	t.Parallel()

	if GraphConfigured(nil) {
		t.Fatal("nil reader reported configured, want false")
	}
	if !GraphConfigured(plainGraphQuery{}) {
		t.Fatal("silent reader reported unconfigured, want true")
	}
	if !GraphConfigured(stubGraphQuery{configured: true}) {
		t.Fatal("configured reader reported unconfigured, want true")
	}
	if GraphConfigured(stubGraphQuery{configured: false}) {
		t.Fatal("undriven reader reported configured, want false")
	}
}

// TestGraphQueryNameContext proves blank names skip context work while
// named reads round-trip, defaulting to unnamed.
func TestGraphQueryNameContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if got := WithGraphQueryName(ctx, ""); got != ctx {
		t.Fatal("blank name wrapped the context, want it unchanged")
	}
	named := WithGraphQueryName(ctx, "entity.context")
	if got := GraphQueryNameFromContext(named); got != "entity.context" {
		t.Fatalf("query name = %q, want entity.context", got)
	}
	if got := GraphQueryNameFromContext(ctx); got != DefaultGraphQueryName {
		t.Fatalf("default query name = %q, want unnamed", got)
	}
}

// TestBoundedGraphReadDeadline proves the policy budget marks its own
// deadline and ordinary deadlines stay unmarked.
func TestBoundedGraphReadDeadline(t *testing.T) {
	t.Parallel()

	if IsBoundedGraphReadDeadline(context.Background()) {
		t.Fatal("plain context reported a policy deadline, want false")
	}
	bounded, cancel := WithBoundedGraphReadDeadlineFor(context.Background(), time.Nanosecond)
	defer cancel()
	deadline, ok := bounded.Deadline()
	if !ok || time.Until(deadline) > time.Second {
		t.Fatalf("bounded deadline = (%v, %v), want an imminent deadline", deadline, ok)
	}
	time.Sleep(50 * time.Millisecond)
	if !IsBoundedGraphReadDeadline(bounded) {
		t.Fatal("expired policy budget not recognized, want true")
	}
	outer, outerCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer outerCancel()
	if IsBoundedGraphReadDeadline(outer) {
		t.Fatal("caller deadline reported as policy budget, want false")
	}
	shared, sharedCancel := WithBoundedGraphReadDeadline(context.Background())
	defer sharedCancel()
	if deadline, ok := shared.Deadline(); !ok || time.Until(deadline) > DefaultGraphReadTimeout {
		t.Fatalf("default bounded deadline = (%v, %v), want within %v", deadline, ok, DefaultGraphReadTimeout)
	}
}

// stubStatementRejecter implements statementRejecter with a fixed verdict.
type stubStatementRejecter struct {
	message string
	ok      bool
}

func (s stubStatementRejecter) Error() string { return "rejected" }

func (s stubStatementRejecter) StatementRejection() (string, bool) { return s.message, s.ok }

// TestGraphStatementRejection proves nil and foreign errors stay silent
// while backend rejections surface their redacted first line.
func TestGraphStatementRejection(t *testing.T) {
	t.Parallel()

	if _, ok := GraphStatementRejection(nil); ok {
		t.Fatal("nil error reported a rejection, want false")
	}
	if _, ok := GraphStatementRejection(errors.New("boom")); ok {
		t.Fatal("foreign error reported a rejection, want false")
	}
	if message, ok := GraphStatementRejection(stubStatementRejecter{message: "line one", ok: true}); !ok || message != "line one" {
		t.Fatalf("rejection = (%q, %v), want the redacted line", message, ok)
	}
	if _, ok := GraphStatementRejection(stubStatementRejecter{ok: false}); ok {
		t.Fatal("negative rejection reported ok, want false")
	}
}

// TestWriteIdentityStoreUnavailable proves the retryable 503 carries no
// authentication challenge.
func TestWriteIdentityStoreUnavailable(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	WriteIdentityStoreUnavailable(recorder, contractRequest(t, "", ""))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	if got := recorder.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("WWW-Authenticate = %q, want absent for an unevaluated credential", got)
	}
	if got := recorder.Header().Get("Retry-After"); got == "" {
		t.Fatal("retryable 503 misses Retry-After, want the hint")
	}
	if envelope := IdentityStoreUnavailableEnvelope(); envelope.Code != ErrorCodeBackendUnavailable || !envelope.retryable {
		t.Fatalf("envelope = %#v, want retryable backend_unavailable", envelope)
	}
}

// TestInfraLabelAllowed proves known labels pass while unknown and blank
// labels fail closed.
func TestInfraLabelAllowed(t *testing.T) {
	t.Parallel()

	if !InfraLabelAllowed("K8sResource") {
		t.Fatal("K8sResource rejected, want allowed")
	}
	if InfraLabelAllowed("Repository") || InfraLabelAllowed("") {
		t.Fatal("unknown label allowed, want fail-closed")
	}
}

// TestPermissionDeniedContract proves the 403 envelope shape and the
// grant/deny feature paths.
func TestPermissionDeniedContract(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	WritePermissionDenied(recorder, "repos.read")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("denied body is not JSON: %v", err)
	}
	if errEnv, _ := body["error"].(map[string]any); errEnv["capability"] != "repos.read" {
		t.Fatalf("denied error = %#v, want the capability", errEnv)
	}
	grantedRecorder := httptest.NewRecorder()
	if !RequirePermissionFeature(grantedRecorder, contractRequest(t, "", ""), "repos.read", "export") {
		t.Fatal("unenforced request denied, want granted")
	}
	if grantedRecorder.Body.Len() != 0 {
		t.Fatalf("granted request wrote %q, want silence", grantedRecorder.Body.String())
	}
	deniedCtx := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode: auth.AuthModeScoped, PermissionCatalogEnforced: true,
	})
	deniedRecorder := httptest.NewRecorder()
	deniedRequest := contractRequest(t, "", "").WithContext(deniedCtx)
	if RequirePermissionFeature(deniedRecorder, deniedRequest, "repos.read", "export") {
		t.Fatal("ungranted feature reported granted, want false")
	}
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("denied status = %d, want 403", deniedRecorder.Code)
	}
	allowedCtx := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode: auth.AuthModeScoped, PermissionCatalogEnforced: true,
		AllowedPermissionFeatures: []string{"export"},
	})
	allowedRecorder := httptest.NewRecorder()
	allowedRequest := contractRequest(t, "", "").WithContext(allowedCtx)
	if !RequirePermissionFeature(allowedRecorder, allowedRequest, "repos.read", "export") {
		t.Fatal("granted feature reported denied, want true")
	}
}

// TestRelationshipVerbs proves the verb set is sorted and freshly copied.
func TestRelationshipVerbs(t *testing.T) {
	t.Parallel()

	verbs := RelationshipVerbs()
	if len(verbs) == 0 || !sort.StringsAreSorted(verbs) {
		t.Fatalf("verbs = %#v, want a non-empty sorted set", verbs)
	}
	verbs[0] = "MUTATED"
	if again := RelationshipVerbs(); again[0] == "MUTATED" {
		t.Fatal("verb set aliases shared state, want a fresh copy")
	}
}

// TestServiceCatalogHasScope proves every anchor scopes the filter while
// the empty filter stays open.
func TestServiceCatalogHasScope(t *testing.T) {
	t.Parallel()

	if (ServiceCatalogCorrelationFilter{}).HasScope() {
		t.Fatal("empty filter reports scope, want false")
	}
	anchored := []ServiceCatalogCorrelationFilter{
		{ScopeID: "s"},
		{EntityRef: "e"},
		{RepositoryID: "r"},
		{ServiceID: "s"},
		{WorkloadID: "w"},
		{OwnerRef: "o"},
		{AllowedRepositoryIDs: []string{"r"}},
	}
	for i, filter := range anchored {
		if !filter.HasScope() {
			t.Fatalf("filter %d reports no scope, want true", i)
		}
	}
}

// TestMaxTruthLevelDefaultArm proves unknown profiles fall back to the
// production ceiling.
func TestMaxTruthLevelDefaultArm(t *testing.T) {
	t.Parallel()
	isolateCapabilityRegistry(t)

	ceiling := TruthLevelDerived
	RegisterCapabilities(CapabilityRegistration{
		Capability: "test.fallback",
		Support: CapabilitySupport{
			LocalLightweightMax:   &ceiling,
			LocalAuthoritativeMax: &ceiling,
			LocalFullStackMax:     &ceiling,
			ProductionMax:         &ceiling,
		},
	})
	if got := maxTruthLevel("test.fallback", QueryProfile("explorer")); got == nil || *got != TruthLevelDerived {
		t.Fatalf("unknown profile ceiling = %v, want the production fallback", got)
	}
}

// TestParseProfilesAndBackends proves every named profile and backend
// parses with case-insensitivity.
func TestParseProfilesAndBackends(t *testing.T) {
	t.Parallel()

	for _, profile := range []QueryProfile{ProfileLocalLightweight, ProfileLocalAuthoritative, ProfileLocalFullStack, ProfileProduction} {
		if got, err := ParseQueryProfile(string(profile)); err != nil || got != profile {
			t.Fatalf("profile %q = (%q, %v), want itself", profile, got, err)
		}
	}
	if backend, err := ParseGraphBackend("nornicdb"); err != nil || backend != GraphBackendNornicDB {
		t.Fatalf("nornicdb backend = (%q, %v), want nornicdb", backend, err)
	}
	if _, err := ParseGraphBackend("NORNICDB"); err == nil {
		t.Fatal("upper-case backend parsed ok, want the case-sensitive rejection")
	}
}

// TestWriteSuccessAndErrorEnvelopeNegotiation proves envelope and plain
// paths for success plus the retryable 503 header.
func TestWriteSuccessAndErrorEnvelopeNegotiation(t *testing.T) {
	t.Parallel()

	envelope := httptest.NewRecorder()
	WriteSuccess(envelope, contractRequest(t, "", EnvelopeMIMEType), http.StatusOK, map[string]any{"ok": true}, nil)
	var body map[string]any
	if err := json.Unmarshal(envelope.Body.Bytes(), &body); err != nil {
		t.Fatalf("envelope body is not JSON: %v", err)
	}
	if data, _ := body["data"].(map[string]any); data["ok"] != true {
		t.Fatalf("envelope data = %#v, want the payload", data)
	}
	plain := httptest.NewRecorder()
	WriteSuccess(plain, contractRequest(t, "", ""), http.StatusOK, map[string]any{"ok": true}, nil)
	var plainBody map[string]any
	if err := json.Unmarshal(plain.Body.Bytes(), &plainBody); err != nil || plainBody["ok"] != true {
		t.Fatalf("plain body = %q, want the payload", plain.Body.String())
	}
	retryable := httptest.NewRecorder()
	WriteErrorEnvelope(retryable, contractRequest(t, "", ""),
		http.StatusServiceUnavailable, IdentityStoreUnavailableEnvelope())
	if got := retryable.Header().Get("Retry-After"); got == "" {
		t.Fatal("retryable envelope misses Retry-After, want the hint")
	}
	nilEnvelope := httptest.NewRecorder()
	WriteErrorEnvelope(nilEnvelope, contractRequest(t, "", ""), http.StatusTeapot, nil)
	if nilEnvelope.Code != http.StatusTeapot {
		t.Fatalf("nil envelope status = %d, want 418", nilEnvelope.Code)
	}
	badOffset := httptest.NewRecorder()
	if _, ok := ParseOffset(badOffset, contractRequest(t, "offset=many", "")); ok {
		t.Fatal("non-numeric offset parsed ok, want a 400 rejection")
	} else if badOffset.Code != http.StatusBadRequest {
		t.Fatalf("non-numeric offset status = %d, want 400", badOffset.Code)
	}
}

// TestRepositoryAccessFilterEdges proves the empty, unscoped, and
// pass-through edges of the access filter.
func TestRepositoryAccessFilterEdges(t *testing.T) {
	t.Parallel()

	if !(RepositoryAccessFilter{}).Empty() {
		t.Fatal("zero filter reports non-empty, want true")
	}
	if (RepositoryAccessFilter{AllScopes: true}).Empty() {
		t.Fatal("unscoped filter reports empty, want false")
	}
	if scopedTestFilter().Empty() {
		t.Fatal("scoped filter reports empty, want false")
	}
	open := RepositoryAccessFilter{AllScopes: true}
	if got := open.GraphWhereClause("n"); got != "" {
		t.Fatalf("unscoped GraphWhereClause = %q, want empty", got)
	}
	if got := open.GraphWhereClauseOnProperty("w", "repo_id"); got != "" {
		t.Fatalf("unscoped GraphWhereClauseOnProperty = %q, want empty", got)
	}
	rows := []map[string]any{{"id": "repo-a"}, {"other": 1}}
	if got := open.FilterRepositoryMaps(rows); !reflect.DeepEqual(got, rows) {
		t.Fatalf("unscoped FilterRepositoryMaps = %#v, want passthrough", got)
	}
	if got := scopedTestFilter().FilterRepositoryMaps(rows); len(got) != 1 {
		t.Fatalf("scoped FilterRepositoryMaps = %#v, want only the id row", got)
	}
	if got := (RepositoryAccessFilter{AllowedScopeIDs: []string{"s"}}).GrantedScopeIDs(); !reflect.DeepEqual(got, []string{"s"}) {
		t.Fatalf("scope-only GrantedScopeIDs = %#v, want [s]", got)
	}
	if got := (RepositoryAccessFilter{}).GrantedScopeIDs(); got != nil {
		t.Fatalf("empty GrantedScopeIDs = %#v, want nil", got)
	}
	if scalars, capped := ScopeGrantInlineScalars(nil, nil); len(scalars) != 0 || capped {
		t.Fatalf("empty scalars = (%#v, %v), want (empty, false)", scalars, capped)
	}
}

// TestDocumentationTargetRefsBranches proves direct-target and
// repository-fallback selection plus explicit-filter matching.
func TestDocumentationTargetRefsBranches(t *testing.T) {
	t.Parallel()

	direct := DocumentationTargetRefs(DocumentationTargetScope{TargetKind: "repository", TargetID: "shop"})
	if len(direct) != 1 || direct[0] != (DocumentationTargetRef{Kind: "repository", ID: "shop"}) {
		t.Fatalf("direct refs = %#v, want the repository ref", direct)
	}
	fallback := DocumentationTargetRefs(DocumentationTargetScope{TargetID: "shop"})
	if len(fallback) != 1 || fallback[0] != (DocumentationTargetRef{Kind: "", ID: "shop"}) {
		t.Fatalf("fallback refs = %#v, want the blank-kind wildcard ref", fallback)
	}
	mixed := []map[string]any{
		{"candidate_refs": []map[string]string{{"kind": "service", "id": "svc"}}},
		{"candidate_refs": []map[string]string{{"kind": "service"}}},
		{"candidate_refs": []map[string]string{{"id": "svc"}}},
	}
	filter := DocumentationFindingFilter{TargetKind: "service", TargetID: "svc"}
	got := storyTargetDocumentationFindings(filter, mixed)
	if len(got) != 1 {
		t.Fatalf("explicit findings = %#v, want only the exact match", got)
	}
	factsOnly := DocumentationFindingListReadModel{
		Coverage: DocumentationTargetCoverage{TargetFactCount: 2},
	}
	doc, err := LoadStoryTargetDocumentation(context.Background(),
		documentationStoryStubStore{readModel: factsOnly},
		DocumentationFindingFilter{Repository: "shop"})
	if err != nil {
		t.Fatalf("facts-only load err = %v, want nil", err)
	}
	if doc["finding_count"] != 0 || doc["findings"] == nil {
		t.Fatalf("facts-only doc = %#v, want normalized empty findings", doc)
	}
}

// TestContextPartialReasons proves limitation promotion with dedup,
// sorting, and blank skipping.
func TestContextPartialReasons(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{"limitations": []string{"b", "", "a", "a"}}
	if got := ContextPartialReasons(ctx, "c", ""); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("partial reasons = %#v, want [a b c]", got)
	}
	if got := ContextPartialReasons(map[string]any{}); got == nil || len(got) != 0 {
		t.Fatalf("empty partial reasons = %#v, want empty non-nil", got)
	}
}

// TestWorkloadContextResultLimits proves fan-out capping with the
// surface-selected drilldown handle.
func TestWorkloadContextResultLimits(t *testing.T) {
	t.Parallel()

	many := make([]any, 0, ContextStoryItemLimit+1)
	for i := 0; i <= ContextStoryItemLimit; i++ {
		many = append(many, map[string]any{"id": i})
	}
	ctx := map[string]any{
		"instances":             many,
		"dependents":            []any{map[string]any{"id": "d"}},
		"consumer_repositories": []any{map[string]any{"id": "c"}},
	}
	limits := WorkloadContextResultLimits(ctx, "workload-1", "story")
	if limits["instance_count"] != ContextStoryItemLimit+1 || limits["dependent_count"] != 1 || limits["consumer_count"] != 1 {
		t.Fatalf("limits = %#v, want fan-out totals", limits)
	}
	if limits["truncated"] != true {
		t.Fatalf("limits = %#v, want truncated after the instance cut", limits)
	}
	if limits["drilldown_tool"] != "get_workload_context" {
		t.Fatalf("limits = %#v, want the story-surface drilldown", limits)
	}
	if limits["context_path"] != "/api/v0/workloads/workload-1/context" {
		t.Fatalf("limits = %#v, want the workload context path", limits)
	}
	bare := WorkloadContextResultLimits(map[string]any{}, "workload-1", "context")
	if bare["instance_count"] != 0 || bare["truncated"] != false {
		t.Fatalf("bare limits = %#v, want zero totals untruncated", bare)
	}
	if bare["drilldown_tool"] != "get_workload_story" {
		t.Fatalf("bare limits = %#v, want the context-surface drilldown", bare)
	}
}

// TestContextEvidenceRowCountProves shapes over lists, maps, and the
// relationship overview shortcut.
func TestContextEvidenceRowCountProves(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"a": 1}, {"b": 2}}
	if got := contextEvidenceRowCount("items", rows); got != 2 {
		t.Fatalf("list rows = %d, want 2", got)
	}
	if got := contextEvidenceRowCount("names", []string{"a"}); got != 1 {
		t.Fatalf("string rows = %d, want 1", got)
	}
	overview := map[string]any{"relationship_count": 7, "relationships": []any{1, 2}}
	if got := contextEvidenceRowCount("relationship_overview", overview); got != 7 {
		t.Fatalf("overview rows = %d, want the declared count", got)
	}
	fallback := map[string]any{"relationships": []any{1, 2, 3}}
	if got := contextEvidenceRowCount("relationship_overview", fallback); got != 3 {
		t.Fatalf("overview fallback rows = %d, want the list length", got)
	}
	if got := contextEvidenceRowCount("other", 42); got != 0 {
		t.Fatalf("scalar rows = %d, want 0", got)
	}
}

// TestTruncateUTF8ByBytes proves byte-bounded prefixes never split a
// code point.
func TestTruncateUTF8ByBytes(t *testing.T) {
	t.Parallel()

	if got := truncateUTF8ByBytes("anything", 0); got != "" {
		t.Fatalf("zero limit = %q, want empty", got)
	}
	if got := truncateUTF8ByBytes("abc", 10); got != "abc" {
		t.Fatalf("short value = %q, want untouched", got)
	}
	if got := truncateUTF8ByBytes("abcdef", 4); got != "abcd" {
		t.Fatalf("ascii cut = %q, want abcd", got)
	}
	if got := truncateUTF8ByBytes("héllo", 3); got != "hé" {
		t.Fatalf("multibyte cut = %q, want hé without splitting é", got)
	}
	if got := truncateUTF8ByBytes("héllo", 2); got != "h" {
		t.Fatalf("tight multibyte cut = %q, want h", got)
	}
	if got := truncateUTF8ByBytes("héllo", 4); got != "hél" {
		t.Fatalf("multibyte fit = %q, want hél", got)
	}
}

// TestRequestStagesClock proves the clock round-trips through context
// and recording without one stays silent.
func TestRequestStagesClock(t *testing.T) {
	t.Parallel()

	if got := requestStagesOf(context.Background()); got != nil {
		t.Fatalf("clock-less stages = %#v, want nil", got)
	}
	ctx, stages := WithRequestStages(context.Background())
	if stages == nil || requestStagesOf(ctx) != stages {
		t.Fatal("request stages did not round-trip through context")
	}
	ObservePG(context.Background(), time.Second)
	ObservePG(ctx, time.Second)
}

// TestRelationshipConfidenceBasis proves the precedence ladder from
// assertion override down to empty.
func TestRelationshipConfidenceBasis(t *testing.T) {
	t.Parallel()

	if got := RelationshipConfidenceBasis(map[string]any{"confidence": 0}); got != "" {
		t.Fatalf("zero confidence basis = %q, want empty", got)
	}
	assertion := map[string]any{"confidence": 0.9, "resolution_source": " Assertion ", "evidence_count": 5}
	if got := RelationshipConfidenceBasis(assertion); got != "assertion_override" {
		t.Fatalf("assertion basis = %q, want assertion_override", got)
	}
	aggregate := map[string]any{"confidence": 0.9, "evidence_count": 3}
	if got := RelationshipConfidenceBasis(aggregate); got != "evidence_aggregate" {
		t.Fatalf("aggregate basis = %q, want evidence_aggregate", got)
	}
	single := map[string]any{"confidence": 0.9, "evidence_count": 1}
	if got := RelationshipConfidenceBasis(single); got != "evidence_constant" {
		t.Fatalf("single basis = %q, want evidence_constant", got)
	}
	typed := map[string]any{"confidence": 0.9, "evidence_type": "import"}
	if got := RelationshipConfidenceBasis(typed); got != "evidence_constant" {
		t.Fatalf("typed basis = %q, want evidence_constant", got)
	}
	if got := RelationshipConfidenceBasis(map[string]any{"confidence": 0.9}); got != "" {
		t.Fatalf("evidenceless basis = %q, want empty", got)
	}
}

// TestEntityContentSearchRowFull proves every optional field lands on the
// wire row when present.
func TestEntityContentSearchRowFull(t *testing.T) {
	t.Parallel()

	position := 3
	row := EntityContentSearchRow(EntityContent{
		EntityID: "e", RepoID: "r", RepoName: "shop", RelativePath: "a.go",
		EntityType: "Function", EntityName: "serve", StartLine: 1, EndLine: 5,
		Language: "go", SourceCache: "src", Metadata: map[string]any{"k": "v"},
		SearchBackend: "hybrid", PagePosition: &position,
	})
	for _, key := range []string{"repo_name", "language", "source_cache", "metadata", "search_backend", PagePositionKey} {
		if _, ok := row[key]; !ok {
			t.Fatalf("full row misses %q: %#v", key, row)
		}
	}
	if row[PagePositionKey] != 3 {
		t.Fatalf("page position = %#v, want 3", row[PagePositionKey])
	}
}
