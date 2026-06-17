package serviceintel

import (
	"reflect"
	"testing"
)

func adapterInputByKind(t *testing.T, in ReportInput, kind SectionKind) SectionInput {
	t.Helper()
	for _, section := range in.Sections {
		if section.Kind == kind {
			return section
		}
	}
	t.Fatalf("section %q not produced by adapter", kind)
	return SectionInput{}
}

func sampleDossier() map[string]any {
	return map[string]any{
		"service_identity": map[string]any{
			"service_id":   "svc:checkout",
			"service_name": "checkout",
			"kind":         "service",
			"repo_id":      "repo:checkout",
			"repo_name":    "checkout-api",
			"limitations":  []string{"materialization pending for one lane"},
		},
		"entrypoints":      []any{map[string]any{"x": 1}, map[string]any{"x": 2}},
		"network_paths":    []any{map[string]any{"y": 1}},
		"deployment_lanes": []map[string]any{{"lane_type": "kubernetes"}},
		"result_limits":    map[string]any{"truncated": true},
	}
}

func TestFromServiceStoryMapsSubjectAndSections(t *testing.T) {
	in := FromServiceStory(sampleDossier(), freshExactTruth("platform_impact.context_overview"))

	if in.Subject.ServiceName != "checkout" || in.Subject.ServiceID != "svc:checkout" || in.Subject.RepoID != "repo:checkout" {
		t.Fatalf("subject mapped wrong: %+v", in.Subject)
	}
	if len(in.Sections) != 3 {
		t.Fatalf("adapter should produce 3 service-story sections, got %d", len(in.Sections))
	}

	id := adapterInputByKind(t, in, SectionIdentity)
	if len(id.Evidence) != 1 || id.Evidence[0].EntityID != "svc:checkout" {
		t.Fatalf("identity should carry the service entity handle, got %+v", id.Evidence)
	}
	if len(id.Limitations) != 1 {
		t.Fatalf("identity should carry dossier limitations, got %v", id.Limitations)
	}
	if !id.Truncated {
		t.Fatalf("identity should reflect result_limits.truncated")
	}
}

func TestFromServiceStoryComposesToSupportedReport(t *testing.T) {
	in := FromServiceStory(sampleDossier(), freshExactTruth("platform_impact.context_overview"))
	report := Compose(in)

	if !report.Supported {
		t.Fatalf("a fresh dossier should compose to a supported report")
	}
	// All three service-story sections have content + evidence, so they are
	// supported (truncation alone keeps them partial — assert that honestly).
	id := sectionByKind(t, report, SectionIdentity)
	if id.Status == StatusUnsupported {
		t.Fatalf("identity should not be unsupported for a real dossier")
	}
	// Supply chain and incidents were not supplied by the service-story adapter,
	// so they remain unsupported with a fallback next call.
	sc := sectionByKind(t, report, SectionSupplyChain)
	if sc.Status != StatusUnsupported {
		t.Fatalf("supply chain should be unsupported when not supplied, got %q", sc.Status)
	}
	if len(sc.Answer.RecommendedNextCalls) == 0 {
		t.Fatalf("unsupplied supply-chain section must recommend a next call")
	}
}

func TestFromServiceStoryEmptyDossierIsUnsupported(t *testing.T) {
	in := FromServiceStory(map[string]any{"service_identity": map[string]any{}}, nil)
	report := Compose(in)
	if report.Supported {
		t.Fatalf("a dossier with no identity/truth should compose unsupported")
	}
	id := sectionByKind(t, report, SectionIdentity)
	if id.Answer.Summary != "" {
		t.Fatalf("unsupported identity must not carry a confident summary, got %q", id.Answer.Summary)
	}
}

func TestFromServiceStoryNilDossier(t *testing.T) {
	in := FromServiceStory(nil, freshExactTruth("x"))
	if in.Subject.ServiceName != "" || len(in.Sections) != 0 {
		t.Fatalf("nil dossier should yield an empty input, got %+v", in)
	}
}

func TestFromServiceStoryEmptySectionsMarkedNoEvidence(t *testing.T) {
	dossier := map[string]any{
		"service_identity": map[string]any{"service_id": "svc:x", "service_name": "x"},
		// no entrypoints/network_paths/deployment_lanes
	}
	in := FromServiceStory(dossier, freshExactTruth("x"))
	c2r := adapterInputByKind(t, in, SectionCodeToRuntime)
	if !c2r.NoEvidence || c2r.Summary != "" {
		t.Fatalf("empty code_to_runtime must be no-evidence with no summary, got %+v", c2r)
	}
	dep := adapterInputByKind(t, in, SectionDeploymentConfig)
	if !dep.NoEvidence {
		t.Fatalf("empty deployment_config must be no-evidence")
	}
}

func TestFromSupplyChainInventory(t *testing.T) {
	subject := ReportSubject{ServiceName: "checkout", ServiceID: "svc:checkout"}
	truth := freshExactTruth("supply-chain.impact")

	withFindings := FromSupplyChainInventory(map[string]any{"count": 3, "truncated": true}, subject, truth)
	if withFindings.Kind != SectionSupplyChain {
		t.Fatalf("kind = %q, want supply_chain", withFindings.Kind)
	}
	if withFindings.NoEvidence {
		t.Fatalf("inventory with findings must not be no-evidence")
	}
	if !withFindings.Truncated {
		t.Fatalf("must reflect truncated")
	}
	if len(withFindings.Evidence) != 1 || withFindings.Evidence[0].EntityID != "svc:checkout" {
		t.Fatalf("supply-chain section should carry the service entity handle, got %+v", withFindings.Evidence)
	}

	empty := FromSupplyChainInventory(map[string]any{"count": 0}, subject, truth)
	if !empty.NoEvidence || empty.Summary != "" {
		t.Fatalf("empty inventory must be no-evidence with no summary, got %+v", empty)
	}

	if got := FromSupplyChainInventory(nil, subject, truth); got.Kind != "" {
		t.Fatalf("nil inventory should yield a zero SectionInput, got %+v", got)
	}
}

func TestFromSupplyChainInventoryComposesIntoReport(t *testing.T) {
	in := FromServiceStory(sampleDossier(), freshExactTruth("platform_impact.context_overview"))
	in.Sections = append(in.Sections, FromSupplyChainInventory(map[string]any{"count": 2}, in.Subject, freshExactTruth("supply-chain.impact")))

	report := Compose(in)
	sc := sectionByKind(t, report, SectionSupplyChain)
	if sc.Status == StatusUnsupported {
		t.Fatalf("supply-chain section with findings should not be unsupported, got %q", sc.Status)
	}
}

func TestFromServiceStoryCountsAPISurfaceForCodeToRuntime(t *testing.T) {
	// API-spec-only service: api_surface present, no raw entrypoints/network paths.
	// code_to_runtime must still be evidence-backed (the trace is built from
	// api_surface.endpoints).
	dossier := map[string]any{
		"service_identity": map[string]any{"service_id": "svc:api", "service_name": "api"},
		"api_surface":      map[string]any{"endpoint_count": 4, "endpoints": []any{map[string]any{"path": "/v1"}}},
	}
	in := FromServiceStory(dossier, freshExactTruth("x"))
	c2r := adapterInputByKind(t, in, SectionCodeToRuntime)
	if c2r.NoEvidence || c2r.Summary == "" {
		t.Fatalf("api-surface-only code_to_runtime must be evidence-backed, got %+v", c2r)
	}
}

func TestFromServiceStoryEmitsCitationCompatibleHandle(t *testing.T) {
	in := FromServiceStory(sampleDossier(), freshExactTruth("x"))
	id := adapterInputByKind(t, in, SectionIdentity)
	if len(id.Evidence) != 1 {
		t.Fatalf("identity should carry one handle, got %d", len(id.Evidence))
	}
	// The evidence-citation normalizer accepts only file/entity kinds; the
	// service handle must be an entity handle so callers can hydrate it.
	if id.Evidence[0].Kind != "entity" || id.Evidence[0].EntityID != "svc:checkout" {
		t.Fatalf("identity handle must be a citation-compatible entity handle, got %+v", id.Evidence[0])
	}
}

func TestFromServiceStoryNoHandleWithoutServiceID(t *testing.T) {
	// No service id means no citation-compatible entity handle; the section stays
	// explicit about missing citeable evidence rather than emitting a rejected handle.
	dossier := map[string]any{"service_identity": map[string]any{"service_name": "x", "repo_id": "repo:x"}}
	in := FromServiceStory(dossier, freshExactTruth("x"))
	id := adapterInputByKind(t, in, SectionIdentity)
	if len(id.Evidence) != 0 || !id.NoEvidence {
		t.Fatalf("no service id should yield no handle and NoEvidence, got %+v", id)
	}
}

func TestFromServiceStoryDeterministic(t *testing.T) {
	a := FromServiceStory(sampleDossier(), freshExactTruth("x"))
	b := FromServiceStory(sampleDossier(), freshExactTruth("x"))
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("adapter is not deterministic for identical input")
	}
}

// TestFromServiceStoryHandlesJSONDecodedSlices proves the adapter reads
// cardinality from both []any (JSON-decoded) and []map[string]any (in-process)
// section shapes.
func TestFromServiceStoryHandlesJSONDecodedSlices(t *testing.T) {
	jsonShape := map[string]any{
		"service_identity": map[string]any{"service_id": "svc:x", "service_name": "x"},
		"deployment_lanes": []any{map[string]any{"lane_type": "k8s"}, map[string]any{"lane_type": "vm"}},
	}
	in := FromServiceStory(jsonShape, freshExactTruth("x"))
	dep := adapterInputByKind(t, in, SectionDeploymentConfig)
	if dep.NoEvidence {
		t.Fatalf("deployment_config with two []any lanes must not be no-evidence")
	}
}
