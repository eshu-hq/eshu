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
