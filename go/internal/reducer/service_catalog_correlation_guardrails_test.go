package reducer

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestServiceCatalogCorrelationSummaryExposesFanoutGuardrails(t *testing.T) {
	t.Parallel()

	loader := &stubServiceCatalogCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			serviceCatalogEntityFact("entity-shared", "component:default/shared", "Shared"),
			serviceCatalogRepositoryLinkFact("repo-link-shared", "component:default/shared", "https://github.com/acme/shared.git"),
			serviceCatalogEntityFact("entity-name-only", "component:default/name-only", "Name Only"),
			serviceCatalogRepositoryLinkWithNameOnlyFact("repo-link-name-only", "component:default/name-only", "shared"),
			serviceCatalogEntityFact("entity-missing-link", "component:default/missing-link", "Missing Link"),
		},
		activeRepos: []facts.Envelope{
			repositoryFact("repo-shared-1", "shared-a", "https://github.com/acme/shared.git", false),
			repositoryFact("repo-shared-2", "shared-b", "git@github.com:acme/shared.git", false),
		},
	}
	writer := &recordingServiceCatalogCorrelationWriter{}
	handler := ServiceCatalogCorrelationHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-service-catalog",
		ScopeID:      "service-catalog-manifest://repo-shared/catalog-info.yaml",
		GenerationID: "generation-service-catalog",
		Domain:       DomainServiceCatalogCorrelation,
		SourceSystem: "service_catalog",
		Cause:        "service catalog facts observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	wantParts := []string{
		"candidate_fanout_total=2",
		"max_candidate_fanout=2",
		"dropped_ambiguous_candidates=2",
		"missing_anchor_entities=2",
		"required_anchor_keys=repository_id,normalized_url|repository_url|raw_url|url,git-repository-scope:<repo_id>",
	}
	for _, want := range wantParts {
		if !strings.Contains(result.EvidenceSummary, want) {
			t.Fatalf("EvidenceSummary = %q, want part %q", result.EvidenceSummary, want)
		}
	}

	decisions := serviceCatalogDecisionsByEntity(writer.write.Decisions)
	rejected := decisions["component:default/name-only"]
	if got, want := rejected.RequiredAnchorKeys, serviceCatalogCorrelationRequiredAnchorKeys(); !slices.Equal(got, want) {
		t.Fatalf("rejected RequiredAnchorKeys = %v, want %v", got, want)
	}
}

func TestServiceCatalogCorrelationPayloadPersistsRequiredAnchorKeys(t *testing.T) {
	t.Parallel()

	payload := serviceCatalogCorrelationPayload(ServiceCatalogCorrelationWrite{
		IntentID:     "intent-service-catalog",
		ScopeID:      "service-catalog-manifest://repo-shared/catalog-info.yaml",
		GenerationID: "generation-service-catalog",
		SourceSystem: "service_catalog",
		Cause:        "service catalog facts observed",
	}, ServiceCatalogCorrelationDecision{
		Provider:           "backstage",
		EntityRef:          "component:default/name-only",
		EntityType:         "component",
		DisplayName:        "Name Only",
		Outcome:            ServiceCatalogCorrelationRejected,
		Reason:             "catalog repository link lacks URL or canonical repository id; name-only links cannot prove ownership",
		ProvenanceOnly:     true,
		DriftKind:          "repository",
		DriftStatus:        "rejected",
		RequiredAnchorKeys: serviceCatalogCorrelationRequiredAnchorKeys(),
	})

	got, ok := payload["required_anchor_keys"].([]string)
	if !ok {
		t.Fatalf("required_anchor_keys type = %T, want []string", payload["required_anchor_keys"])
	}
	if want := serviceCatalogCorrelationRequiredAnchorKeys(); !slices.Equal(got, want) {
		t.Fatalf("required_anchor_keys = %v, want %v", got, want)
	}
}

func TestServiceCatalogCorrelationCountersSeparateGuardrails(t *testing.T) {
	t.Parallel()

	inst, reader := newPackageSourceCorrelationInstruments(t)
	loader := &stubServiceCatalogCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			serviceCatalogEntityFact("entity-shared", "component:default/shared", "Shared"),
			serviceCatalogRepositoryLinkFact("repo-link-shared", "component:default/shared", "https://github.com/acme/shared.git"),
			serviceCatalogEntityFact("entity-name-only", "component:default/name-only", "Name Only"),
			serviceCatalogRepositoryLinkWithNameOnlyFact("repo-link-name-only", "component:default/name-only", "shared"),
			serviceCatalogEntityFact("entity-missing-link", "component:default/missing-link", "Missing Link"),
		},
		activeRepos: []facts.Envelope{
			repositoryFact("repo-shared-1", "shared-a", "https://github.com/acme/shared.git", false),
			repositoryFact("repo-shared-2", "shared-b", "git@github.com:acme/shared.git", false),
		},
	}
	handler := ServiceCatalogCorrelationHandler{
		FactLoader:  loader,
		Writer:      &recordingServiceCatalogCorrelationWriter{},
		Instruments: inst,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-service-catalog",
		ScopeID:      "service-catalog-manifest://repo-shared/catalog-info.yaml",
		GenerationID: "generation-service-catalog",
		Domain:       DomainServiceCatalogCorrelation,
		SourceSystem: "service_catalog",
		Cause:        "service catalog facts observed",
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	for _, forbiddenOutcome := range []string{"candidate_fanout", "dropped_ambiguous_candidate", "missing_anchor_entity"} {
		if reducerMetricHasAttributeValue(
			rm,
			"eshu_dp_service_catalog_correlations_total",
			telemetry.MetricDimensionOutcome,
			forbiddenOutcome,
		) {
			t.Fatalf("decision counter recorded guardrail outcome %q", forbiddenOutcome)
		}
	}
	if got := reducerCounterValue(t, rm, "eshu_dp_service_catalog_correlations_total", map[string]string{
		telemetry.MetricDimensionDomain:  string(DomainServiceCatalogCorrelation),
		telemetry.MetricDimensionOutcome: string(ServiceCatalogCorrelationAmbiguous),
	}); got != 1 {
		t.Fatalf("ambiguous service catalog correlations = %d, want 1", got)
	}
	if got := reducerCounterValue(t, rm, "eshu_dp_service_catalog_correlation_guardrails_total", map[string]string{
		telemetry.MetricDimensionDomain:    string(DomainServiceCatalogCorrelation),
		telemetry.MetricDimensionGuardrail: "candidate_fanout",
	}); got != 2 {
		t.Fatalf("candidate_fanout guardrails = %d, want 2", got)
	}
	if got := reducerCounterValue(t, rm, "eshu_dp_service_catalog_correlation_guardrails_total", map[string]string{
		telemetry.MetricDimensionDomain:    string(DomainServiceCatalogCorrelation),
		telemetry.MetricDimensionGuardrail: "dropped_ambiguous_candidate",
	}); got != 2 {
		t.Fatalf("dropped_ambiguous_candidate guardrails = %d, want 2", got)
	}
	if got := reducerCounterValue(t, rm, "eshu_dp_service_catalog_correlation_guardrails_total", map[string]string{
		telemetry.MetricDimensionDomain:    string(DomainServiceCatalogCorrelation),
		telemetry.MetricDimensionGuardrail: "missing_anchor_entity",
	}); got != 2 {
		t.Fatalf("missing_anchor_entity guardrails = %d, want 2", got)
	}
}

func reducerMetricHasAttributeValue(
	rm metricdata.ResourceMetrics,
	metricName string,
	attrKey string,
	attrValue string,
) bool {
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != metricName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				for _, attr := range dp.Attributes.ToSlice() {
					if string(attr.Key) == attrKey && attr.Value.AsString() == attrValue {
						return true
					}
				}
			}
		}
	}
	return false
}
