package reducer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type stubPackageSourceFactLoader struct {
	scopeFacts           []facts.Envelope
	repositoryFacts      []facts.Envelope
	manifestDependencies []facts.Envelope
	kindCalls            [][]string
	repositoryCalls      int
	manifestCalls        int
	manifestEcosystems   []string
	manifestPackageNames []string
}

func (s *stubPackageSourceFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubPackageSourceFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	kinds []string,
) ([]facts.Envelope, error) {
	s.kindCalls = append(s.kindCalls, append([]string(nil), kinds...))
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubPackageSourceFactLoader) ListActiveRepositoryFacts(
	context.Context,
) ([]facts.Envelope, error) {
	s.repositoryCalls++
	return append([]facts.Envelope(nil), s.repositoryFacts...), nil
}

func (s *stubPackageSourceFactLoader) ListActivePackageManifestDependencyFacts(
	_ context.Context,
	ecosystems []string,
	packageNames []string,
) ([]facts.Envelope, error) {
	s.manifestCalls++
	s.manifestEcosystems = append([]string(nil), ecosystems...)
	s.manifestPackageNames = append([]string(nil), packageNames...)
	return append([]facts.Envelope(nil), s.manifestDependencies...), nil
}

type recordingPackageCorrelationWriter struct {
	write PackageCorrelationWrite
	calls int
}

func (w *recordingPackageCorrelationWriter) WritePackageCorrelations(
	_ context.Context,
	write PackageCorrelationWrite,
) (PackageCorrelationWriteResult, error) {
	w.calls++
	w.write = write
	return PackageCorrelationWriteResult{
		CanonicalWrites: packageCorrelationCanonicalWrites(write.ConsumptionDecisions),
		FactsWritten:    len(write.OwnershipDecisions) + len(write.ConsumptionDecisions),
	}, nil
}

func newPackageSourceCorrelationInstruments(t *testing.T) (*telemetry.Instruments, sdkmetric.Reader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

func TestPackageSourceCorrelationHandlerRejectsWrongDomain(t *testing.T) {
	t.Parallel()

	_, err := PackageSourceCorrelationHandler{}.Handle(context.Background(), Intent{
		IntentID:     "intent-package-source",
		ScopeID:      "package-registry:npm:team-api",
		GenerationID: "generation-1",
		SourceSystem: "package_registry",
		Domain:       DomainWorkloadIdentity,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want wrong-domain error")
	}
}

func TestPackageSourceCorrelationHandlerRequiresFactLoader(t *testing.T) {
	t.Parallel()

	_, err := PackageSourceCorrelationHandler{}.Handle(context.Background(), Intent{
		IntentID:     "intent-package-source",
		ScopeID:      "package-registry:npm:team-api",
		GenerationID: "generation-1",
		SourceSystem: "package_registry",
		Domain:       DomainPackageSourceCorrelation,
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want missing FactLoader error")
	}
}

func TestPackageSourceCorrelationHandlerLoadsActiveRepositoriesAndEmitsCounters(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	inst, reader := newPackageSourceCorrelationInstruments(t)
	loader := &stubPackageSourceFactLoader{
		scopeFacts: []facts.Envelope{
			packageRegistryPackageFact(
				"pkg:npm://registry.example/team-api",
				"npm",
				"team-api",
				"",
				observedAt,
			),
			packageSourceHintFact(
				"pkg:npm://registry.example/team-api",
				"repository",
				"https://github.com/acme/team-api",
				observedAt,
			),
		},
		repositoryFacts: []facts.Envelope{
			packageSourceRepositoryFact(
				"repo-team-api",
				"team-api",
				"git@github.com:acme/team-api.git",
				false,
				observedAt,
			),
		},
		manifestDependencies: []facts.Envelope{
			packageManifestDependencyFact(
				"repo-consumer",
				"consumer",
				"package.json",
				"team-api",
				"npm",
				"^1.2.0",
				observedAt,
			),
		},
	}
	writer := &recordingPackageCorrelationWriter{}
	handler := PackageSourceCorrelationHandler{
		FactLoader:  loader,
		Writer:      writer,
		Instruments: inst,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-package-source",
		ScopeID:         "package-registry:npm:team-api",
		GenerationID:    "generation-1",
		SourceSystem:    "package_registry",
		Domain:          DomainPackageSourceCorrelation,
		Cause:           "package registry source hints observed",
		RelatedScopeIDs: []string{"package-registry:npm:team-api"},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("Handle().CanonicalWrites = %d, want 1 package consumption write", result.CanonicalWrites)
	}
	if !strings.Contains(result.EvidenceSummary, "evaluated=1") ||
		!strings.Contains(result.EvidenceSummary, "derived=1") ||
		!strings.Contains(result.EvidenceSummary, "consumption=1") ||
		!strings.Contains(result.EvidenceSummary, "canonical_writes=1") {
		t.Fatalf("Handle().EvidenceSummary = %q, want evaluated/derived/consumption/canonical_writes counts", result.EvidenceSummary)
	}
	if got, want := strings.Join(loader.kindCalls[0], ","), "package_registry.source_hint,package_registry.package,repository"; got != want {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	if loader.repositoryCalls != 1 {
		t.Fatalf("ListActiveRepositoryFacts() calls = %d, want 1", loader.repositoryCalls)
	}
	if loader.manifestCalls != 1 {
		t.Fatalf("ListActivePackageManifestDependencyFacts() calls = %d, want 1", loader.manifestCalls)
	}
	if got, want := loader.manifestPackageNames, []string{"team-api"}; !sameStrings(got, want) {
		t.Fatalf("PackageNames = %#v, want %#v", got, want)
	}
	if writer.calls != 1 {
		t.Fatalf("WritePackageCorrelations() calls = %d, want 1", writer.calls)
	}
	if got, want := len(writer.write.OwnershipDecisions), 1; got != want {
		t.Fatalf("OwnershipDecisions len = %d, want %d", got, want)
	}
	if got, want := len(writer.write.ConsumptionDecisions), 1; got != want {
		t.Fatalf("ConsumptionDecisions len = %d, want %d", got, want)
	}
	if got, want := writer.write.ConsumptionDecisions[0].RepositoryID, "repo-consumer"; got != want {
		t.Fatalf("consumption RepositoryID = %q, want %q", got, want)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := reducerCounterValue(t, rm, "eshu_dp_package_source_correlations_total", map[string]string{
		telemetry.MetricDimensionDomain:  string(DomainPackageSourceCorrelation),
		telemetry.MetricDimensionOutcome: "derived",
	}); got != 1 {
		t.Fatalf("derived package source correlations = %d, want 1", got)
	}
}

func TestBuildPackageSourceCorrelationDecisionsClassifiesExactRepositoryHint(t *testing.T) {
	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageSourceCorrelationDecisions([]facts.Envelope{
		packageSourceHintFact(
			"pkg:npm://registry.example/team-api",
			"repository",
			"https://github.com/acme/team-api",
			observedAt,
		),
		packageSourceRepositoryFact("repo-team-api", "team-api", "https://github.com/acme/team-api", false, observedAt),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if decision.Outcome != PackageSourceCorrelationExact {
		t.Fatalf("Outcome = %q, want %q", decision.Outcome, PackageSourceCorrelationExact)
	}
	if decision.RepositoryID != "repo-team-api" {
		t.Fatalf("RepositoryID = %q, want repo-team-api", decision.RepositoryID)
	}
	if decision.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 until package ownership admission is proven", decision.CanonicalWrites)
	}
	if !decision.ProvenanceOnly {
		t.Fatal("ProvenanceOnly = false, want true until corroborating build or release evidence exists")
	}
}

func TestBuildPackageSourceCorrelationDecisionsClassifiesDerivedRepositoryHint(t *testing.T) {
	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageSourceCorrelationDecisions([]facts.Envelope{
		packageSourceHintFact(
			"pkg:npm://registry.example/team-api",
			"repository",
			"https://github.com/acme/team-api",
			observedAt,
		),
		packageSourceRepositoryFact("repo-team-api", "team-api", "git@github.com:acme/team-api.git", false, observedAt),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	if got, want := decisions[0].Outcome, PackageSourceCorrelationDerived; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if got, want := decisions[0].Reason, "source hint matches repository remote after git URL canonicalization"; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
}

func TestBuildPackageSourceCorrelationDecisionsKeepsAmbiguousHintsOutOfOwnership(t *testing.T) {
	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageSourceCorrelationDecisions([]facts.Envelope{
		packageSourceHintFact(
			"pkg:npm://registry.example/team-api",
			"repository",
			"https://github.com/acme/team-api",
			observedAt,
		),
		packageSourceRepositoryFact("repo-team-api", "team-api", "https://github.com/acme/team-api", false, observedAt),
		packageSourceRepositoryFact("repo-team-api-fork", "team-api-fork", "git@github.com:acme/team-api.git", false, observedAt),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.Outcome, PackageSourceCorrelationAmbiguous; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if decision.RepositoryID != "" {
		t.Fatalf("RepositoryID = %q, want empty for ambiguous decision", decision.RepositoryID)
	}
	if got, want := decision.CandidateRepositoryIDs, []string{"repo-team-api", "repo-team-api-fork"}; !sameStrings(got, want) {
		t.Fatalf("CandidateRepositoryIDs = %#v, want %#v", got, want)
	}
}

func TestBuildPackageSourceCorrelationDecisionsClassifiesUnresolvedHints(t *testing.T) {
	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageSourceCorrelationDecisions([]facts.Envelope{
		packageSourceHintFact(
			"pkg:npm://registry.example/team-api",
			"repository",
			"https://github.com/acme/team-api",
			observedAt,
		),
		packageSourceRepositoryFact("repo-other", "other", "https://github.com/acme/other", false, observedAt),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	if got, want := decisions[0].Outcome, PackageSourceCorrelationUnresolved; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
}

func TestBuildPackageSourceCorrelationDecisionsClassifiesStaleRepositoryFacts(t *testing.T) {
	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageSourceCorrelationDecisions([]facts.Envelope{
		packageSourceHintFact(
			"pkg:npm://registry.example/team-api",
			"repository",
			"https://github.com/acme/team-api",
			observedAt,
		),
		packageSourceRepositoryFact("repo-team-api", "team-api", "https://github.com/acme/team-api", true, observedAt),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	if got, want := decisions[0].Outcome, PackageSourceCorrelationStale; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if decisions[0].RepositoryID != "" {
		t.Fatalf("RepositoryID = %q, want empty for stale source", decisions[0].RepositoryID)
	}
}

func TestBuildPackageSourceCorrelationDecisionsRejectsWeakHomepageHints(t *testing.T) {
	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageSourceCorrelationDecisions([]facts.Envelope{
		packageSourceHintFact(
			"pkg:npm://registry.example/team-api",
			"homepage",
			"https://github.com/acme/team-api",
			observedAt,
		),
		packageSourceRepositoryFact("repo-team-api", "team-api", "https://github.com/acme/team-api", false, observedAt),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	if got, want := decisions[0].Outcome, PackageSourceCorrelationRejected; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if got, want := decisions[0].Reason, "hint kind homepage is provenance-only and cannot prove repository ownership"; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
}

func packageSourceHintFact(packageID, hintKind, normalizedURL string, observedAt time.Time) facts.Envelope {
	return facts.Envelope{
		FactKind:   facts.PackageRegistrySourceHintFactKind,
		ObservedAt: observedAt,
		Payload: map[string]any{
			"package_id":        packageID,
			"hint_kind":         hintKind,
			"normalized_url":    normalizedURL,
			"raw_url":           normalizedURL,
			"confidence_reason": "test",
		},
	}
}

func packageSourceRepositoryFact(
	repositoryID string,
	repositoryName string,
	remoteURL string,
	tombstone bool,
	observedAt time.Time,
) facts.Envelope {
	return facts.Envelope{
		FactKind:      factKindRepository,
		ObservedAt:    observedAt,
		IsTombstone:   tombstone,
		StableFactKey: "repository:" + repositoryID,
		Payload: map[string]any{
			"graph_id":   repositoryID,
			"name":       repositoryName,
			"remote_url": remoteURL,
		},
	}
}

func packageRegistryPackageFact(
	packageID string,
	ecosystem string,
	normalizedName string,
	namespace string,
	observedAt time.Time,
) facts.Envelope {
	return facts.Envelope{
		FactID:        "package-fact:" + packageID,
		FactKind:      facts.PackageRegistryPackageFactKind,
		ObservedAt:    observedAt,
		IsTombstone:   false,
		SourceRef:     facts.Ref{SourceSystem: "package_registry"},
		StableFactKey: "package:" + packageID,
		Payload: map[string]any{
			"package_id":      packageID,
			"ecosystem":       ecosystem,
			"normalized_name": normalizedName,
			"namespace":       namespace,
		},
	}
}

func packageManifestDependencyFact(
	repositoryID string,
	repositoryName string,
	relativePath string,
	dependencyName string,
	packageManager string,
	dependencyRange string,
	observedAt time.Time,
) facts.Envelope {
	return facts.Envelope{
		FactID:        "manifest-dep:" + repositoryID + ":" + dependencyName,
		FactKind:      factKindContentEntity,
		ObservedAt:    observedAt,
		IsTombstone:   false,
		SourceRef:     facts.Ref{SourceSystem: "git"},
		StableFactKey: "content_entity:" + repositoryID + ":" + dependencyName,
		Payload: map[string]any{
			"repo_id":       repositoryID,
			"relative_path": relativePath,
			"entity_type":   "Variable",
			"entity_name":   dependencyName,
			"entity_metadata": map[string]any{
				"config_kind":     "dependency",
				"package_manager": packageManager,
				"section":         "dependencies",
				"value":           dependencyRange,
			},
			"repo_name": repositoryName,
		},
	}
}

func unmarshalPackageCorrelationPayload(t *testing.T, raw any) map[string]any {
	t.Helper()
	payloadBytes, ok := raw.([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}
