package reducer

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// stubBackendRepositoryResolver returns a canned resolution per
// (backend_kind, locator_hash) and records every call so tests can assert the
// builder memoizes one resolve per distinct backend locator.
type stubBackendRepositoryResolver struct {
	byKey map[string]BackendRepositoryResolution
	err   error
	calls int
}

func (s *stubBackendRepositoryResolver) ResolveBackendRepository(
	_ context.Context,
	backendKind, locatorHash string,
) (BackendRepositoryResolution, error) {
	s.calls++
	if s.err != nil {
		return BackendRepositoryResolution{}, s.err
	}
	return s.byKey[backendKind+":"+locatorHash], nil
}

func decisionByProviderServiceID(
	decisions []IncidentRepositoryCorrelationDecision,
	id string,
) (IncidentRepositoryCorrelationDecision, bool) {
	for _, decision := range decisions {
		if decision.ProviderServiceID == id {
			return decision, true
		}
	}
	return IncidentRepositoryCorrelationDecision{}, false
}

// TestBuildIncidentRepositoryCorrelationsExactEdge proves the positive case: an
// incident provider service id that matched an applied Terraform service whose
// backend locator is owned by a single repository yields an exact, edge-bearing
// decision.
func TestBuildIncidentRepositoryCorrelationsExactEdge(t *testing.T) {
	resolver := &stubBackendRepositoryResolver{byKey: map[string]BackendRepositoryResolution{
		"s3:loc-checkout": {RepositoryID: "repo-checkout"},
	}}
	rows := []AppliedPagerDutyServiceRouting{{
		FactID:           "fact-1",
		StableFactKey:    "stable-1",
		ProviderObjectID: "PDSVC1",
		BackendKind:      "s3",
		LocatorHash:      "loc-checkout",
		ProviderIDExact:  true,
	}}

	decisions, err := BuildIncidentRepositoryCorrelations(context.Background(), "pagerduty", rows, resolver)
	if err != nil {
		t.Fatalf("build: unexpected error %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1", len(decisions))
	}
	got := decisions[0]
	if got.Outcome != IncidentRepositoryCorrelationExact {
		t.Fatalf("outcome = %q, want exact", got.Outcome)
	}
	if got.RepositoryID != "repo-checkout" {
		t.Fatalf("repository_id = %q, want repo-checkout", got.RepositoryID)
	}
	if got.ProvenanceOnly {
		t.Fatalf("exact decision must not be provenance-only")
	}
	if len(got.EvidenceFactIDs) != 1 || got.EvidenceFactIDs[0] != "stable-1" {
		t.Fatalf("evidence_fact_ids = %v, want [stable-1] (durable StableFactKey preferred)", got.EvidenceFactIDs)
	}
}

// TestBuildIncidentRepositoryCorrelationsDerivedEdge proves a non-raw provider
// id match (ProviderIDExact=false) over a single-owner backend yields a derived,
// still edge-bearing decision.
func TestBuildIncidentRepositoryCorrelationsDerivedEdge(t *testing.T) {
	resolver := &stubBackendRepositoryResolver{byKey: map[string]BackendRepositoryResolution{
		"gcs:loc-orders": {RepositoryID: "repo-orders"},
	}}
	rows := []AppliedPagerDutyServiceRouting{{
		FactID:           "fact-2",
		ProviderObjectID: "PDSVC2",
		BackendKind:      "gcs",
		LocatorHash:      "loc-orders",
		ProviderIDExact:  false,
	}}

	decisions, err := BuildIncidentRepositoryCorrelations(context.Background(), "pagerduty", rows, resolver)
	if err != nil {
		t.Fatalf("build: unexpected error %v", err)
	}
	got := decisions[0]
	if got.Outcome != IncidentRepositoryCorrelationDerived {
		t.Fatalf("outcome = %q, want derived", got.Outcome)
	}
	if got.RepositoryID != "repo-orders" || got.ProvenanceOnly {
		t.Fatalf("derived decision must carry repo and not be provenance-only, got %+v", got)
	}
}

// TestBuildIncidentRepositoryCorrelationsNameOnlyRejected proves the negative
// case: an applied row with no provider service id (only a name fingerprint is
// available) is rejected as provenance-only and never resolves a repository.
func TestBuildIncidentRepositoryCorrelationsNameOnlyRejected(t *testing.T) {
	resolver := &stubBackendRepositoryResolver{byKey: map[string]BackendRepositoryResolution{
		"s3:loc-x": {RepositoryID: "repo-x"},
	}}
	rows := []AppliedPagerDutyServiceRouting{{
		FactID:          "fact-3",
		NameFingerprint: "deadbeefdeadbeef",
		BackendKind:     "s3",
		LocatorHash:     "loc-x",
	}}

	decisions, err := BuildIncidentRepositoryCorrelations(context.Background(), "pagerduty", rows, resolver)
	if err != nil {
		t.Fatalf("build: unexpected error %v", err)
	}
	got := decisions[0]
	if got.Outcome != IncidentRepositoryCorrelationRejected {
		t.Fatalf("outcome = %q, want rejected", got.Outcome)
	}
	if got.RepositoryID != "" || !got.ProvenanceOnly {
		t.Fatalf("name-only row must stay provenance-only with no repo, got %+v", got)
	}
	if resolver.calls != 0 {
		t.Fatalf("name-only row must not consult the backend resolver, calls = %d", resolver.calls)
	}
}

// TestBuildIncidentRepositoryCorrelationsUnresolved proves an applied service
// whose backend locator no Eshu repo owns stays unresolved and edge-free.
func TestBuildIncidentRepositoryCorrelationsUnresolved(t *testing.T) {
	resolver := &stubBackendRepositoryResolver{byKey: map[string]BackendRepositoryResolution{
		// No entry for s3:loc-orphan -> zero-value resolution (blank repo).
	}}
	rows := []AppliedPagerDutyServiceRouting{{
		ProviderObjectID: "PDSVC9",
		BackendKind:      "s3",
		LocatorHash:      "loc-orphan",
		ProviderIDExact:  true,
	}}

	decisions, err := BuildIncidentRepositoryCorrelations(context.Background(), "pagerduty", rows, resolver)
	if err != nil {
		t.Fatalf("build: unexpected error %v", err)
	}
	got := decisions[0]
	if got.Outcome != IncidentRepositoryCorrelationUnresolved {
		t.Fatalf("outcome = %q, want unresolved", got.Outcome)
	}
	if got.RepositoryID != "" || !got.ProvenanceOnly {
		t.Fatalf("unresolved decision must stay provenance-only, got %+v", got)
	}
}

// TestBuildIncidentRepositoryCorrelationsAmbiguousBackendOwner proves a backend
// locator claimed by more than one repository (resolver ambiguity) yields an
// ambiguous, edge-free decision: a tenant boundary cannot pick a winner.
func TestBuildIncidentRepositoryCorrelationsAmbiguousBackendOwner(t *testing.T) {
	resolver := &stubBackendRepositoryResolver{byKey: map[string]BackendRepositoryResolution{
		"s3:loc-shared": {Ambiguous: true},
	}}
	rows := []AppliedPagerDutyServiceRouting{{
		ProviderObjectID: "PDSVC4",
		BackendKind:      "s3",
		LocatorHash:      "loc-shared",
		ProviderIDExact:  true,
	}}

	decisions, err := BuildIncidentRepositoryCorrelations(context.Background(), "pagerduty", rows, resolver)
	if err != nil {
		t.Fatalf("build: unexpected error %v", err)
	}
	got := decisions[0]
	if got.Outcome != IncidentRepositoryCorrelationAmbiguous {
		t.Fatalf("outcome = %q, want ambiguous", got.Outcome)
	}
	if got.RepositoryID != "" || !got.ProvenanceOnly {
		t.Fatalf("ambiguous backend owner must not carry a repo edge, got %+v", got)
	}
}

// TestBuildIncidentRepositoryCorrelationsForkMirrorNoFalseMerge proves the
// fork/mirror case: the same provider service id applied under two distinct
// backend locators owned by distinct repositories does NOT false-merge. The
// cross-locator disagreement forces ambiguity with no edge.
func TestBuildIncidentRepositoryCorrelationsForkMirrorNoFalseMerge(t *testing.T) {
	resolver := &stubBackendRepositoryResolver{byKey: map[string]BackendRepositoryResolution{
		"s3:loc-fork-a": {RepositoryID: "repo-a"},
		"s3:loc-fork-b": {RepositoryID: "repo-b"},
	}}
	rows := []AppliedPagerDutyServiceRouting{
		{ProviderObjectID: "PDSVC5", BackendKind: "s3", LocatorHash: "loc-fork-a", ProviderIDExact: true},
		{ProviderObjectID: "PDSVC5", BackendKind: "s3", LocatorHash: "loc-fork-b", ProviderIDExact: true},
	}

	decisions, err := BuildIncidentRepositoryCorrelations(context.Background(), "pagerduty", rows, resolver)
	if err != nil {
		t.Fatalf("build: unexpected error %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1 collapsed decision for the provider id", len(decisions))
	}
	got := decisions[0]
	if got.Outcome != IncidentRepositoryCorrelationAmbiguous {
		t.Fatalf("outcome = %q, want ambiguous (no false merge across distinct repos)", got.Outcome)
	}
	if got.RepositoryID != "" {
		t.Fatalf("fork/mirror disagreement must not pick a winning repo, got %q", got.RepositoryID)
	}
}

// TestBuildIncidentRepositoryCorrelationsMemoizesResolver proves the builder
// resolves each distinct backend locator at most once even across many provider
// services sharing it (concurrency/throughput contract: no per-incident fan-out
// re-query).
func TestBuildIncidentRepositoryCorrelationsMemoizesResolver(t *testing.T) {
	resolver := &stubBackendRepositoryResolver{byKey: map[string]BackendRepositoryResolution{
		"s3:loc-shared": {RepositoryID: "repo-platform"},
	}}
	rows := []AppliedPagerDutyServiceRouting{
		{ProviderObjectID: "PDSVC6", BackendKind: "s3", LocatorHash: "loc-shared", ProviderIDExact: true},
		{ProviderObjectID: "PDSVC7", BackendKind: "s3", LocatorHash: "loc-shared", ProviderIDExact: true},
		{ProviderObjectID: "PDSVC8", BackendKind: "s3", LocatorHash: "loc-shared", ProviderIDExact: true},
	}

	decisions, err := BuildIncidentRepositoryCorrelations(context.Background(), "pagerduty", rows, resolver)
	if err != nil {
		t.Fatalf("build: unexpected error %v", err)
	}
	if resolver.calls != 1 {
		t.Fatalf("resolver calls = %d, want 1 (memoized per distinct backend locator)", resolver.calls)
	}
	for _, id := range []string{"PDSVC6", "PDSVC7", "PDSVC8"} {
		d, ok := decisionByProviderServiceID(decisions, id)
		if !ok || d.Outcome != IncidentRepositoryCorrelationExact || d.RepositoryID != "repo-platform" {
			t.Fatalf("provider %s decision = %+v, want exact -> repo-platform", id, d)
		}
	}
}

// TestBuildIncidentRepositoryCorrelationsResolverError surfaces a resolver error
// rather than silently dropping the decision.
func TestBuildIncidentRepositoryCorrelationsResolverError(t *testing.T) {
	resolver := &stubBackendRepositoryResolver{err: errors.New("boom")}
	rows := []AppliedPagerDutyServiceRouting{{
		ProviderObjectID: "PDSVC1", BackendKind: "s3", LocatorHash: "loc", ProviderIDExact: true,
	}}
	if _, err := BuildIncidentRepositoryCorrelations(context.Background(), "pagerduty", rows, resolver); err == nil {
		t.Fatalf("expected resolver error to propagate")
	}
}

// BenchmarkBuildIncidentRepositoryCorrelations measures the per-intent
// classification cost over a realistic fan-out of applied service rows that
// share a small set of backend locators, confirming the resolver is memoized so
// throughput scales with distinct backends, not with incident count.
func BenchmarkBuildIncidentRepositoryCorrelations(b *testing.B) {
	const rows = 512
	resolver := &stubBackendRepositoryResolver{byKey: map[string]BackendRepositoryResolution{
		"s3:loc-a": {RepositoryID: "repo-a"},
		"s3:loc-b": {RepositoryID: "repo-b"},
		"s3:loc-c": {Ambiguous: true},
	}}
	locators := []string{"loc-a", "loc-b", "loc-c", "loc-orphan"}
	input := make([]AppliedPagerDutyServiceRouting, 0, rows)
	for i := 0; i < rows; i++ {
		loc := locators[i%len(locators)]
		input = append(input, AppliedPagerDutyServiceRouting{
			FactID:           fmt.Sprintf("fact-%d", i),
			ProviderObjectID: fmt.Sprintf("PDSVC%d", i),
			BackendKind:      "s3",
			LocatorHash:      loc,
			ProviderIDExact:  true,
		})
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver.calls = 0
		if _, err := BuildIncidentRepositoryCorrelations(ctx, "pagerduty", input, resolver); err != nil {
			b.Fatalf("build: %v", err)
		}
	}
}

// --- handler + writer wiring ---

type stubAppliedRoutingLoader struct {
	rows []AppliedPagerDutyServiceRouting
	err  error
}

func (s stubAppliedRoutingLoader) LoadAppliedPagerDutyServiceRouting(
	context.Context, string, string,
) ([]AppliedPagerDutyServiceRouting, error) {
	return s.rows, s.err
}

type recordingIncidentRepoCorrelationWriter struct {
	write IncidentRepositoryCorrelationWrite
	calls int
}

func (w *recordingIncidentRepoCorrelationWriter) WriteIncidentRepositoryCorrelations(
	_ context.Context,
	write IncidentRepositoryCorrelationWrite,
) (IncidentRepositoryCorrelationWriteResult, error) {
	w.calls++
	w.write = write
	return IncidentRepositoryCorrelationWriteResult{FactsWritten: len(write.Decisions)}, nil
}

func TestIncidentRepositoryCorrelationHandlerWritesDecisions(t *testing.T) {
	resolver := &stubBackendRepositoryResolver{byKey: map[string]BackendRepositoryResolution{
		"s3:loc-checkout": {RepositoryID: "repo-checkout"},
	}}
	writer := &recordingIncidentRepoCorrelationWriter{}
	handler := IncidentRepositoryCorrelationHandler{
		Loader: stubAppliedRoutingLoader{rows: []AppliedPagerDutyServiceRouting{{
			ProviderObjectID: "PDSVC1", BackendKind: "s3", LocatorHash: "loc-checkout", ProviderIDExact: true,
		}}},
		Resolver: resolver,
		Writer:   writer,
	}
	intent := Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "pagerduty",
		Domain:       DomainIncidentRepositoryCorrelation,
		Cause:        "test",
	}

	result, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("handle: unexpected error %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.calls != 1 || len(writer.write.Decisions) != 1 {
		t.Fatalf("writer calls=%d decisions=%d, want 1/1", writer.calls, len(writer.write.Decisions))
	}
	if writer.write.Decisions[0].Outcome != IncidentRepositoryCorrelationExact {
		t.Fatalf("decision outcome = %q, want exact", writer.write.Decisions[0].Outcome)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("canonical writes = %d, want 1", result.CanonicalWrites)
	}
}

func TestIncidentRepositoryCorrelationHandlerRejectsWrongDomain(t *testing.T) {
	handler := IncidentRepositoryCorrelationHandler{
		Loader: stubAppliedRoutingLoader{},
		Writer: &recordingIncidentRepoCorrelationWriter{},
	}
	if _, err := handler.Handle(context.Background(), Intent{Domain: DomainServiceCatalogCorrelation}); err == nil {
		t.Fatalf("expected wrong-domain rejection")
	}
}

// TestImplementedDefaultDomainDefinitionsOmitsIncidentRepositoryCorrelationWithoutWriter
// proves the additive domain stays unregistered when only the loader is wired,
// so a half-wired deployment never silently drops correlation intents.
func TestImplementedDefaultDomainDefinitionsOmitsIncidentRepositoryCorrelationWithoutWriter(t *testing.T) {
	t.Parallel()
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		IncidentRoutingHandlers: IncidentRoutingHandlers{
			AppliedPagerDutyServiceRoutingLoader: stubAppliedRoutingLoader{},
		},
	})
	for _, def := range definitions {
		if def.Domain == DomainIncidentRepositoryCorrelation {
			t.Fatalf("incident_repository_correlation registered without writer; want omitted")
		}
	}
}

// TestImplementedDefaultDomainDefinitionsIncludesIncidentRepositoryCorrelationWhenWired
// proves the domain registers with a fully-wired handler and canonical-write
// ownership once loader and writer are present.
func TestImplementedDefaultDomainDefinitionsIncludesIncidentRepositoryCorrelationWhenWired(t *testing.T) {
	t.Parallel()
	loader := stubAppliedRoutingLoader{}
	resolver := &stubBackendRepositoryResolver{}
	writer := &recordingIncidentRepoCorrelationWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		IncidentRoutingHandlers: IncidentRoutingHandlers{
			AppliedPagerDutyServiceRoutingLoader: loader,
			BackendRepositoryResolver:            resolver,
			IncidentRepositoryCorrelationWriter:  writer,
		},
	})
	found := false
	for _, def := range definitions {
		if def.Domain != DomainIncidentRepositoryCorrelation {
			continue
		}
		found = true
		handler, ok := def.Handler.(IncidentRepositoryCorrelationHandler)
		if !ok {
			t.Fatalf("handler type = %T, want IncidentRepositoryCorrelationHandler", def.Handler)
		}
		if handler.Loader == nil || handler.Resolver == nil || handler.Writer == nil {
			t.Fatal("incident_repository_correlation handler not fully wired")
		}
		if !def.Ownership.CanonicalWrite {
			t.Fatal("incident_repository_correlation must declare CanonicalWrite ownership")
		}
	}
	if !found {
		t.Fatal("incident_repository_correlation not registered after wiring loader+writer")
	}
}

// TestIncidentRepositoryCorrelationFactIDIsDeterministic proves the writer fact
// id is stable across re-runs for the same (scope, generation, provider,
// provider service id), so retries converge via ON CONFLICT DO UPDATE rather
// than appending duplicates. The resolved repository id is intentionally NOT in
// the identity so an exact -> ambiguous flip updates the same row.
func TestIncidentRepositoryCorrelationFactIDIsDeterministic(t *testing.T) {
	write := IncidentRepositoryCorrelationWrite{ScopeID: "s", GenerationID: "g"}
	exact := IncidentRepositoryCorrelationDecision{
		Provider: "pagerduty", ProviderServiceID: "PDSVC1", RepositoryID: "repo-a",
		Outcome: IncidentRepositoryCorrelationExact,
	}
	flipped := IncidentRepositoryCorrelationDecision{
		Provider: "pagerduty", ProviderServiceID: "PDSVC1", RepositoryID: "",
		Outcome: IncidentRepositoryCorrelationAmbiguous,
	}
	if incidentRepositoryCorrelationFactID(write, exact) != incidentRepositoryCorrelationFactID(write, flipped) {
		t.Fatalf("fact id must be stable across outcome flips for the same provider service id")
	}
	other := IncidentRepositoryCorrelationDecision{
		Provider: "pagerduty", ProviderServiceID: "PDSVC2", Outcome: IncidentRepositoryCorrelationExact,
	}
	if incidentRepositoryCorrelationFactID(write, exact) == incidentRepositoryCorrelationFactID(write, other) {
		t.Fatalf("distinct provider service ids must produce distinct fact ids")
	}
}
