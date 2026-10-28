// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// contractRequest builds a GET request carrying the given query and Accept
// header.
func contractRequest(t *testing.T, query, accept string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/x?"+query, nil)
	if accept != "" {
		r.Header.Set("Accept", accept)
	}
	return r
}

// TestWriteContractError proves envelope negotiation for capability
// errors with the plain JSON fallback.
func TestWriteContractError(t *testing.T) {
	t.Parallel()

	envelope := httptest.NewRecorder()
	WriteContractError(envelope, contractRequest(t, "", EnvelopeMIMEType),
		http.StatusNotImplemented, "needs production", "unsupported_capability",
		"platform_impact.context_overview", ProfileLocalLightweight, ProfileProduction)
	if envelope.Code != http.StatusNotImplemented {
		t.Fatalf("envelope status = %d, want 501", envelope.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(envelope.Body.Bytes(), &body); err != nil {
		t.Fatalf("envelope body is not JSON: %v", err)
	}
	errEnv, _ := body["error"].(map[string]any)
	if errEnv["code"] != "unsupported_capability" || errEnv["capability"] != "platform_impact.context_overview" {
		t.Fatalf("envelope error = %#v, want the capability contract", errEnv)
	}
	plain := httptest.NewRecorder()
	WriteContractError(plain, contractRequest(t, "", ""), http.StatusNotImplemented,
		"needs production", "unsupported_capability", "c", ProfileLocalLightweight, ProfileProduction)
	var plainBody map[string]any
	if err := json.Unmarshal(plain.Body.Bytes(), &plainBody); err != nil {
		t.Fatalf("plain body is not JSON: %v", err)
	}
	if plainBody["detail"] != "needs production" {
		t.Fatalf("plain body = %#v, want the message as detail", plainBody)
	}
}

// TestReadJSON proves valid bodies decode while nil and malformed bodies
// fail closed.
func TestReadJSON(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"limit":5}`))
	var value struct {
		Limit int `json:"limit"`
	}
	if err := ReadJSON(r, &value); err != nil || value.Limit != 5 {
		t.Fatalf("ReadJSON = (%#v, %v), want limit 5", value, err)
	}
	bad := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{oops`))
	if err := ReadJSON(bad, &value); err == nil {
		t.Fatal("malformed JSON decoded without error, want a failure")
	}
	empty, _ := http.NewRequest(http.MethodPost, "/x", nil)
	if err := ReadJSON(empty, &value); err == nil {
		t.Fatal("nil body decoded without error, want a failure")
	}
}

// TestQueryParamHelpers proves trimming with the int default fallback.
func TestQueryParamHelpers(t *testing.T) {
	t.Parallel()

	r := contractRequest(t, "q=%20hi%20&n=7&bad=x", "")
	if got := QueryParam(r, "q"); got != "hi" {
		t.Fatalf("QueryParam = %q, want hi", got)
	}
	if got := QueryParam(r, "missing"); got != "" {
		t.Fatalf("missing QueryParam = %q, want empty", got)
	}
	if got := QueryParamInt(r, "n", 3); got != 7 {
		t.Fatalf("QueryParamInt = %d, want 7", got)
	}
	if got := QueryParamInt(r, "bad", 3); got != 3 {
		t.Fatalf("non-numeric QueryParamInt = %d, want the default", got)
	}
	if got := QueryParamInt(r, "missing", 3); got != 3 {
		t.Fatalf("missing QueryParamInt = %d, want the default", got)
	}
}

// TestParseBoundedLimitAndOffset proves defaults pass while out-of-range
// and malformed values fail with 400.
func TestParseBoundedLimitAndOffset(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	limit, ok := ParseBoundedLimit(w, contractRequest(t, "", ""), 10, 50)
	if !ok || limit != 10 {
		t.Fatalf("default limit = (%d, %v), want (10, true)", limit, ok)
	}
	w = httptest.NewRecorder()
	limit, ok = ParseBoundedLimit(w, contractRequest(t, "limit=5", ""), 10, 50)
	if !ok || limit != 5 {
		t.Fatalf("explicit limit = (%d, %v), want (5, true)", limit, ok)
	}
	for _, query := range []string{"limit=0", "limit=51", "limit=many"} {
		w := httptest.NewRecorder()
		if _, ok := ParseBoundedLimit(w, contractRequest(t, query, ""), 10, 50); ok {
			t.Fatalf("limit %q parsed ok, want a 400 rejection", query)
		} else if w.Code != http.StatusBadRequest {
			t.Fatalf("limit %q status = %d, want 400", query, w.Code)
		}
	}
	w = httptest.NewRecorder()
	offset, ok := ParseOffset(w, contractRequest(t, "offset=4", ""))
	if !ok || offset != 4 {
		t.Fatalf("offset = (%d, %v), want (4, true)", offset, ok)
	}
	w = httptest.NewRecorder()
	if _, ok := ParseOffset(w, contractRequest(t, "offset=-1", "")); ok {
		t.Fatal("negative offset parsed ok, want a 400 rejection")
	}
	if got := NextOffset(0, 10, false); got != nil {
		t.Fatalf("untruncated NextOffset = %#v, want nil", got)
	}
	if got := NextOffset(20, 10, true); got != 30 {
		t.Fatalf("truncated NextOffset = %#v, want 30", got)
	}
}

// TestParseCatalogView proves compact/full selection with rejection of
// unknown views.
func TestParseCatalogView(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	full, ok := ParseCatalogView(w, contractRequest(t, "view=full", ""))
	if !ok || !full {
		t.Fatalf("full view = (%v, %v), want (true, true)", full, ok)
	}
	w = httptest.NewRecorder()
	full, ok = ParseCatalogView(w, contractRequest(t, "", ""))
	if !ok || full {
		t.Fatalf("blank view = (%v, %v), want (false, true)", full, ok)
	}
	w = httptest.NewRecorder()
	if _, ok := ParseCatalogView(w, contractRequest(t, "view=wide", "")); ok {
		t.Fatal("unknown view parsed ok, want a 400 rejection")
	} else if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown view status = %d, want 400", w.Code)
	}
}

// TestParseGraphBackendAndProfile proves backend and profile validation
// with documented defaults.
func TestParseGraphBackendAndProfile(t *testing.T) {
	t.Parallel()

	if backend, err := ParseGraphBackend(""); err != nil || backend != GraphBackendNornicDB {
		t.Fatalf("blank backend = (%q, %v), want the nornicdb default", backend, err)
	}
	if backend, err := ParseGraphBackend("neo4j"); err != nil || backend != GraphBackendNeo4j {
		t.Fatalf("neo4j backend = (%q, %v), want neo4j", backend, err)
	}
	if _, err := ParseGraphBackend("titan"); err == nil {
		t.Fatal("unknown backend parsed ok, want an error")
	}
	if got := NormalizeQueryProfile("production"); got != ProfileProduction {
		t.Fatalf("NormalizeQueryProfile = %q, want production", got)
	}
	if got := NormalizeQueryProfile("nope"); got != "" {
		t.Fatalf("unknown NormalizeQueryProfile = %q, want empty", got)
	}
	if profile, err := ParseQueryProfile(""); err != nil || profile != "" {
		t.Fatalf("blank profile = (%q, %v), want empty without error", profile, err)
	}
	if _, err := ParseQueryProfile("nope"); err == nil {
		t.Fatal("unknown profile parsed ok, want an error")
	}
}

// TestCapabilityMatrixExtras proves the compatibility matrix, the
// hardcoded-secret ceilings, and the context-overview gate.
func TestCapabilityMatrixExtras(t *testing.T) {
	t.Parallel()
	isolateCapabilityRegistry(t)

	if matrix := CompatibilityCapabilityMatrix(); matrix == nil {
		t.Fatal("CompatibilityCapabilityMatrix is nil, want the live map")
	}
	support := HardcodedSecretSupport()
	for _, ceiling := range []*TruthLevel{
		support.LocalLightweightMax, support.LocalAuthoritativeMax,
		support.LocalFullStackMax, support.ProductionMax,
	} {
		if ceiling == nil || *ceiling != TruthLevelDerived {
			t.Fatalf("hardcoded-secret ceiling = %v, want derived", ceiling)
		}
	}
	pointers := map[*TruthLevel]bool{
		support.LocalLightweightMax: true, support.LocalAuthoritativeMax: true,
		support.LocalFullStackMax: true, support.ProductionMax: true,
	}
	if len(pointers) != 4 {
		t.Fatal("hardcoded-secret ceilings alias one variable, want four distinct pointers")
	}
	recorder := httptest.NewRecorder()
	if RequireContextOverview(recorder, contractRequest(t, "", ""), ProfileProduction, "needs production") {
		t.Fatal("unregistered capability reported servable, want false")
	}
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("gate status = %d, want 501", recorder.Code)
	}
	ceiling := TruthLevelDerived
	RegisterCapabilities(CapabilityRegistration{
		Capability: "platform_impact.context_overview",
		Support: CapabilitySupport{
			LocalLightweightMax:   &ceiling,
			LocalAuthoritativeMax: &ceiling,
			LocalFullStackMax:     &ceiling,
			ProductionMax:         &ceiling,
			RequiredProfile:       ProfileProduction,
		},
	})
	recorder = httptest.NewRecorder()
	if !RequireContextOverview(recorder, contractRequest(t, "", ""), ProfileProduction, "needs production") {
		t.Fatal("registered capability reported unservable, want true")
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("servable gate wrote body %q, want silence", recorder.Body.String())
	}
}

// TestRequestChallengeAttachment proves nil policies skip context work
// while real policies attach retrievably.
func TestRequestChallengeAttachment(t *testing.T) {
	t.Parallel()

	r := contractRequest(t, "", "")
	if got := RequestWithOAuthChallenge(r, nil); got != r {
		t.Fatal("nil policy wrapped the request, want it unchanged")
	}
	policy := &fakeOAuthChallengePolicy{ok: true}
	wrapped := RequestWithOAuthChallenge(r, policy)
	if wrapped == r {
		t.Fatal("policy left the request unwrapped, want a derived context")
	}
	if got := OAuthWWWAuthenticateChallengeForRequest(wrapped.Context()); got != "Bearer" {
		t.Fatalf("attached empty-policy challenge = %q, want bare Bearer", got)
	}
	full := &fakeOAuthChallengePolicy{
		metadataURL: "https://eshu.example.test/.well-known/oauth-protected-resource",
		scope:       "openid",
		ok:          true,
	}
	withFull := RequestWithOAuthChallenge(r, full)
	gotChallenge := OAuthWWWAuthenticateChallengeForRequest(withFull.Context())
	if !strings.HasPrefix(gotChallenge, "Bearer ") ||
		!strings.Contains(gotChallenge, `"https://eshu.example.test/.well-known/oauth-protected-resource"`) ||
		!strings.Contains(gotChallenge, `scope="openid"`) {
		t.Fatalf("attached challenge = %q, want bearer challenge with metadata URL and scope", gotChallenge)
	}
	if got := OAuthWWWAuthenticateChallengeForRequest(context.Background()); got != "Bearer" {
		t.Fatalf("context-less challenge = %q, want bare Bearer", got)
	}
}

// TestDocumentationCorrelationID proves header passthrough with a random
// hex fallback.
func TestDocumentationCorrelationID(t *testing.T) {
	t.Parallel()

	r := contractRequest(t, "", "")
	r.Header.Set("X-Correlation-ID", "corr-1")
	if got := DocumentationCorrelationID(r); got != "corr-1" {
		t.Fatalf("correlation id = %q, want the header value", got)
	}
	r2 := contractRequest(t, "", "")
	r2.Header.Set("X-Request-ID", "req-9")
	if got := DocumentationCorrelationID(r2); got != "req-9" {
		t.Fatalf("correlation id = %q, want the request-id fallback", got)
	}
	generated := DocumentationCorrelationID(contractRequest(t, "", ""))
	if len(generated) != 32 {
		t.Fatalf("generated correlation id = %q, want 32 hex chars", generated)
	}
	if _, err := hex.DecodeString(generated); err != nil {
		t.Fatalf("generated correlation id is not hex: %v", err)
	}
}

// TestWriteUnauthorized proves the bare challenge default with envelope
// negotiation and policy-driven challenges.
func TestWriteUnauthorized(t *testing.T) {
	t.Parallel()

	bare := httptest.NewRecorder()
	WriteUnauthorized(bare, contractRequest(t, "", ""))
	if bare.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", bare.Code)
	}
	if got := bare.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Fatalf("WWW-Authenticate = %q, want bare Bearer", got)
	}
	var plain map[string]string
	if err := json.Unmarshal(bare.Body.Bytes(), &plain); err != nil {
		t.Fatalf("plain body is not JSON: %v", err)
	}
	if plain["error_code"] != string(ErrorCodeUnauthenticated) || plain["correlation_id"] == "" {
		t.Fatalf("plain body = %#v, want code plus correlation", plain)
	}
	enveloped := httptest.NewRecorder()
	WriteUnauthorized(enveloped, contractRequest(t, "", EnvelopeMIMEType))
	var body map[string]any
	if err := json.Unmarshal(enveloped.Body.Bytes(), &body); err != nil {
		t.Fatalf("envelope body is not JSON: %v", err)
	}
	errEnv, _ := body["error"].(map[string]any)
	if errEnv["code"] != string(ErrorCodeUnauthenticated) || errEnv["correlation_id"] == nil {
		t.Fatalf("envelope error = %#v, want code plus correlation", errEnv)
	}
	policy := &fakeOAuthChallengePolicy{
		metadataURL: "https://eshu.example.test/.well-known/oauth-protected-resource",
		ok:          true,
	}
	withPolicy := RequestWithOAuthChallenge(contractRequest(t, "", ""), policy)
	challenged := httptest.NewRecorder()
	WriteUnauthorized(challenged, withPolicy)
	gotHeader := challenged.Header().Get("WWW-Authenticate")
	if !strings.HasPrefix(gotHeader, "Bearer ") ||
		!strings.Contains(gotHeader, `"https://eshu.example.test/.well-known/oauth-protected-resource"`) {
		t.Fatalf("WWW-Authenticate = %q, want bearer challenge with the metadata URL", gotHeader)
	}
}

// TestWithFreshnessCauseExtras proves nil, fresh, and invalid causes are
// no-ops while a proven cause stamps cause plus next check.
func TestWithFreshnessCauseExtras(t *testing.T) {
	t.Parallel()

	WithFreshnessCause(nil, FreshnessCauseReducerBacklog)
	fresh := &TruthEnvelope{Freshness: TruthFreshness{State: FreshnessFresh}}
	WithFreshnessCause(fresh, FreshnessCauseReducerBacklog)
	if fresh.Freshness.Cause != "" || fresh.Freshness.NextCheck != nil {
		t.Fatalf("fresh envelope gained a cause: %#v", fresh.Freshness)
	}
	stale := &TruthEnvelope{Freshness: TruthFreshness{State: FreshnessStale}}
	WithFreshnessCause(stale, FreshnessCause("bogus"))
	if stale.Freshness.Cause != "" {
		t.Fatalf("invalid cause stamped %#v, want untouched", stale.Freshness)
	}
	WithFreshnessCause(stale, FreshnessCauseReducerBacklog)
	if stale.Freshness.Cause != FreshnessCauseReducerBacklog {
		t.Fatalf("cause = %q, want reducer_backlog", stale.Freshness.Cause)
	}
	when := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if got := NullableRFC3339(when); got != when.Format(time.RFC3339) {
		t.Fatalf("NullableRFC3339 = %#v, want the RFC3339 instant", got)
	}
	if got := NullableRFC3339(time.Time{}); got != nil {
		t.Fatalf("zero NullableRFC3339 = %#v, want nil", got)
	}
	if got := FormatCoverageTimestamp(time.Time{}); got != "" {
		t.Fatalf("zero FormatCoverageTimestamp = %q, want empty", got)
	}
	if got := FormatCoverageTimestamp(when); got != when.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("FormatCoverageTimestamp = %q, want RFC3339Nano", got)
	}
}

// TestCICDRunFilterPredicates proves scope and disambiguator detection
// diverge exactly on the provider run id.
func TestCICDRunFilterPredicates(t *testing.T) {
	t.Parallel()

	if (CICDRunCorrelationFilter{}).HasScope() {
		t.Fatal("empty filter reports scope, want false")
	}
	scoped := []CICDRunCorrelationFilter{
		{ScopeID: "s"},
		{RepositoryID: "r"},
		{CommitSHA: "c"},
		{ArtifactDigest: "a"},
		{ImageRef: "i"},
		{Environment: "e"},
	}
	for i, filter := range scoped {
		if !filter.HasScope() {
			t.Fatalf("filter %d reports no scope, want true", i)
		}
		if !filter.HasProviderRunDisambiguator() {
			t.Fatalf("filter %d reports no disambiguator, want true", i)
		}
	}
	// ProviderRunID alone anchors the run it names, so it scopes the read
	// without disambiguating which provider run to correlate.
	runOnly := CICDRunCorrelationFilter{ProviderRunID: "p"}
	if !runOnly.HasScope() {
		t.Fatal("provider-run-only filter reports no scope, want true")
	}
	if runOnly.HasProviderRunDisambiguator() {
		t.Fatal("provider-run-only filter disambiguates, want false without a second anchor")
	}
	if (CICDRunCorrelationFilter{Provider: "github"}).HasScope() {
		t.Fatal("provider-only filter reports scope, want false")
	}
}

// TestBuildCollectorListReadiness proves rows win, configuration
// disambiguates emptiness, and probe failure stays explicit.
func TestBuildCollectorListReadiness(t *testing.T) {
	t.Parallel()

	rows := BuildCollectorListReadiness(scope.CollectorSBOMAttestation, 3, false, false)
	if rows.State != CollectorListReadinessStateReadyWithResults || rows.CollectorKind != "sbom_attestation" {
		t.Fatalf("rows envelope = %#v, want ready_with_results", rows)
	}
	zero := BuildCollectorListReadiness(scope.CollectorSBOMAttestation, 0, true, true)
	if zero.State != CollectorListReadinessStateReadyZeroResults || !zero.Counts.ResultsTruncated {
		t.Fatalf("zero envelope = %#v, want ready_zero_results truncated", zero)
	}
	off := BuildCollectorListReadiness(scope.CollectorSBOMAttestation, 0, false, false)
	if off.State != CollectorListReadinessStateNotConfigured {
		t.Fatalf("off envelope = %#v, want not_configured", off)
	}
	unknown := BuildCollectorListReadinessUnavailable(scope.CollectorSBOMAttestation, 0, false)
	if unknown.State != CollectorListReadinessStateReadinessUnavailable {
		t.Fatalf("unavailable envelope = %#v, want readiness_unavailable", unknown)
	}
}

// TestContentPagePositions proves stamping, settling, and the row-map
// counterparts round-trip.
func TestContentPagePositions(t *testing.T) {
	t.Parallel()

	rows := []EntityContent{{EntityID: "a"}, {EntityID: "b"}}
	stamped := StampEntityPagePositions(rows)
	if rows[0].PagePosition != nil {
		t.Fatal("stamping mutated the input slice, want a copy")
	}
	if *stamped[0].PagePosition != 0 || *stamped[1].PagePosition != 1 {
		t.Fatalf("stamped positions = %#v, want 0 and 1", stamped)
	}
	settled := SettleEntityPagePositions(stamped)
	if settled[0].PagePosition != nil || settled[1].PagePosition != nil {
		t.Fatalf("settled rows = %#v, want cleared positions", settled)
	}
	reordered := []EntityContent{{EntityID: "b", PagePosition: intPtr(1)}, {EntityID: "a", PagePosition: intPtr(1)}}
	kept := SettleEntityPagePositions(reordered)
	if kept[0].PagePosition == nil {
		t.Fatal("reordered rows lost their positions, want them kept")
	}
	maps := []map[string]any{{"id": "a"}, {"id": "b"}}
	StampRowPagePositions(maps)
	if maps[0][PagePositionKey] != 0 || maps[1][PagePositionKey] != 1 {
		t.Fatalf("stamped maps = %#v, want positions", maps)
	}
	SettleRowPagePositions(maps)
	if _, ok := maps[0][PagePositionKey]; ok {
		t.Fatalf("settled maps = %#v, want cleared positions", maps)
	}
	maps[0][PagePositionKey] = 1
	SettleRowPagePositions(maps)
	if _, ok := maps[0][PagePositionKey]; !ok {
		t.Fatal("reordered maps lost their positions, want them kept")
	}
	shaped := EntityContentSearchRows([]EntityContent{{EntityID: "a", RepoID: "r"}})
	if len(shaped) != 1 || shaped[0]["entity_id"] != "a" {
		t.Fatalf("search rows = %#v, want the shaped row", shaped)
	}
	if got := EntityContentSearchRows(nil); len(got) != 0 {
		t.Fatalf("nil search rows = %#v, want empty", got)
	}
}

func intPtr(value int) *int {
	return &value
}

// TestIsServiceEvidenceCandidate proves name hits, keyword extensions,
// and the non-evidence rejection.
func TestIsServiceEvidenceCandidate(t *testing.T) {
	t.Parallel()

	api := FileContent{RelativePath: "docs/openapi.yaml"}
	if !IsServiceEvidenceCandidate(api, "") {
		t.Fatal("openapi spec should be service evidence")
	}
	named := FileContent{RelativePath: "deploy/shop-values.yaml"}
	if !IsServiceEvidenceCandidate(named, "_shop_") {
		t.Fatal("service-named values file should be service evidence")
	}
	if IsServiceEvidenceCandidate(FileContent{RelativePath: "src/main.go"}, "") {
		t.Fatal("plain Go source must not be service evidence")
	}
	if IsServiceEvidenceCandidate(FileContent{}, "shop") {
		t.Fatal("blank path must not be service evidence")
	}
}

// TestEdgeMaterializationCoverage proves known writers report
// materialized with reasons while unknown types stay explicit gaps.
func TestEdgeMaterializationCoverage(t *testing.T) {
	t.Parallel()

	contains := EdgeMaterializationCoverage("CONTAINS")
	if !contains.Materialized || contains.Reason == "" || contains.Reason == "no_writer" {
		t.Fatalf("CONTAINS coverage = %#v, want materialized with a reason", contains)
	}
	depends := EdgeMaterializationCoverage("DEPENDS_ON")
	if !depends.Materialized {
		t.Fatalf("DEPENDS_ON coverage = %#v, want materialized", depends)
	}
	gap := EdgeMaterializationCoverage("NOT_A_REAL_EDGE_TYPE")
	if gap.Materialized || gap.Reason != "no_writer" || gap.EdgeType != "NOT_A_REAL_EDGE_TYPE" {
		t.Fatalf("unknown coverage = %#v, want an explicit gap", gap)
	}
	set := MaterializedEdgeTypeSet()
	if _, ok := set["CONTAINS"]; !ok {
		t.Fatal("materialized set misses CONTAINS, want it present")
	}
	if _, ok := set["NOT_A_REAL_EDGE_TYPE"]; ok {
		t.Fatal("materialized set carries the unknown type, want it absent")
	}
}

// TestNormalizeEvidenceTokenDirect proves separator folding with whole
// token wrapping for alias detection.
func TestNormalizeEvidenceTokenDirect(t *testing.T) {
	t.Parallel()

	if got := NormalizeEvidenceToken("Prod-API.v2:x"); got != "_prod_api_v2_x_" {
		t.Fatalf("token = %q, want folded underscores", got)
	}
	if got := DetectEnvironmentAliases("deploys to production nightly"); len(got) != 1 || got[0] != "prod" {
		t.Fatalf("aliases = %#v, want [prod]", got)
	}
	if got := InferHostnameEnvironment("api-production-01"); got != "prod" {
		t.Fatalf("hostname env = %q, want prod", got)
	}
	if got := InferHostnameEnvironment("api-01"); got != "" {
		t.Fatalf("plain hostname env = %q, want empty", got)
	}
}

// TestAppendReasonAndFilterNullRelationships proves reason dedup and the
// nil-type relationship filter over both shapes.
func TestAppendReasonAndFilterNullRelationships(t *testing.T) {
	t.Parallel()

	reasons := AppendReason(nil, "  first  ")
	reasons = AppendReason(reasons, "first")
	reasons = AppendReason(reasons, "")
	reasons = AppendReason(reasons, "second")
	if len(reasons) != 2 || reasons[0] != "first" || reasons[1] != "second" {
		t.Fatalf("reasons = %#v, want [first second]", reasons)
	}
	typed := []map[string]any{{"type": "USES"}, {"other": 1}}
	if got := FilterNullRelationships(typed); len(got) != 1 {
		t.Fatalf("typed filter = %#v, want only the typed row", got)
	}
	loose := []any{map[string]any{"type": "USES"}, map[string]any{"type": nil}, "nope", map[string]any{}}
	if got := FilterNullRelationships(loose); len(got) != 1 {
		t.Fatalf("loose filter = %#v, want only the typed row", got)
	}
	if got := FilterNullRelationships("nope"); got != nil {
		t.Fatalf("scalar filter = %#v, want nil", got)
	}
}
