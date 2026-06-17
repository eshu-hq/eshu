package auditreport

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

func testCatalog() capabilitycatalog.Catalog {
	return capabilitycatalog.Catalog{Entries: []capabilitycatalog.Entry{
		{Capability: "code_search.symbol_lookup", Maturity: capabilitycatalog.MaturityGeneralAvailability},
		{Capability: "semantic_search.curated_retrieval", Maturity: capabilitycatalog.MaturityGated, LinkedIssues: []int{2676}},
	}}
}

func findingFor(t *testing.T, input AuditInput, catalog capabilitycatalog.Catalog, issues []OpenIssue, feature string) ReportEntry {
	t.Helper()
	report := Generate(input, catalog, issues)
	for _, entry := range report.Entries {
		if entry.Feature == feature {
			return entry
		}
	}
	t.Fatalf("feature %q not in report", feature)
	return ReportEntry{}
}

func TestGenerateRecommendations(t *testing.T) {
	t.Parallel()

	input := AuditInput{Competitors: []Competitor{{
		Name: "graphify",
		Findings: []AuditFinding{
			{Feature: "symbol graph", EshuCapability: "code_search.symbol_lookup", GapClass: "foundation exists", OwnerSurface: "api"},
			{Feature: "commit timeline", GapClass: "missing", OwnerSurface: "api"},
			{Feature: "semantic retrieval", EshuCapability: "semantic_search.curated_retrieval", GapClass: "already tracked", OwnerSurface: "search"},
			{Feature: "stale docs claim", EshuCapability: "code_search.symbol_lookup", GapClass: "docs stale", OwnerSurface: "docs"},
			{Feature: "broken classification", GapClass: "not a real class", OwnerSurface: "api"},
		},
	}}}
	catalog := testCatalog()

	if got := findingFor(t, input, catalog, nil, "symbol graph"); got.Recommendation != RecNoIssue {
		t.Fatalf("foundation exists rec = %q, want %q", got.Recommendation, RecNoIssue)
	}
	if got := findingFor(t, input, catalog, nil, "commit timeline"); got.Recommendation != RecCreateNew {
		t.Fatalf("missing rec = %q, want %q", got.Recommendation, RecCreateNew)
	}
	tracked := findingFor(t, input, catalog, nil, "semantic retrieval")
	if tracked.Recommendation != RecLinkExisting {
		t.Fatalf("already tracked rec = %q, want %q", tracked.Recommendation, RecLinkExisting)
	}
	if len(tracked.DuplicateIssues) == 0 || tracked.DuplicateIssues[0] != 2676 {
		t.Fatalf("already tracked should surface catalog linked issue 2676: %+v", tracked.DuplicateIssues)
	}
	if got := findingFor(t, input, catalog, nil, "stale docs claim"); got.Recommendation != RecUpdateExisting {
		t.Fatalf("docs stale rec = %q, want %q", got.Recommendation, RecUpdateExisting)
	}
	broken := findingFor(t, input, catalog, nil, "broken classification")
	if broken.Recommendation != RecReview {
		t.Fatalf("invalid gap class rec = %q, want %q", broken.Recommendation, RecReview)
	}
	if len(broken.Validation) == 0 {
		t.Fatalf("invalid gap class should carry validation findings")
	}
}

func TestGenerateMissingButCapabilityExistsIsReview(t *testing.T) {
	t.Parallel()

	input := AuditInput{Competitors: []Competitor{{
		Name: "x",
		Findings: []AuditFinding{
			{Feature: "f", EshuCapability: "code_search.symbol_lookup", GapClass: "missing", OwnerSurface: "api"},
		},
	}}}
	got := findingFor(t, input, testCatalog(), nil, "f")
	if got.Recommendation != RecReview {
		t.Fatalf("missing-but-exists rec = %q, want %q (conflict)", got.Recommendation, RecReview)
	}
}

func TestGenerateDuplicateDetectionDowngradesCreateNew(t *testing.T) {
	t.Parallel()

	input := AuditInput{Competitors: []Competitor{{
		Name: "x",
		Findings: []AuditFinding{
			{Feature: "commit timeline view", GapClass: "missing", OwnerSurface: "api"},
		},
	}}}
	issues := []OpenIssue{{Number: 999, Title: "Add commit timeline view to console"}}
	got := findingFor(t, input, testCatalog(), issues, "commit timeline view")
	if got.Recommendation != RecLinkExisting {
		t.Fatalf("duplicate rec = %q, want %q", got.Recommendation, RecLinkExisting)
	}
	if len(got.DuplicateIssues) == 0 || got.DuplicateIssues[0] != 999 {
		t.Fatalf("duplicate issue not detected: %+v", got.DuplicateIssues)
	}
}

func TestGenerateUnknownCapabilityIsReview(t *testing.T) {
	t.Parallel()

	input := AuditInput{Competitors: []Competitor{{
		Name: "x",
		Findings: []AuditFinding{
			{Feature: "f", EshuCapability: "code_search.symbo_lookup", GapClass: "foundation exists", OwnerSurface: "api"},
		},
	}}}
	got := findingFor(t, input, testCatalog(), nil, "f")
	if got.Recommendation != RecReview {
		t.Fatalf("unknown capability rec = %q, want %q", got.Recommendation, RecReview)
	}
	if got.CapabilityFound {
		t.Fatal("typo capability must not be marked found")
	}
}

func TestGenerateSingleTokenFeatureNotMatched(t *testing.T) {
	t.Parallel()

	input := AuditInput{Competitors: []Competitor{{
		Name: "x",
		Findings: []AuditFinding{
			{Feature: "search", GapClass: "missing", OwnerSurface: "api"},
		},
	}}}
	issues := []OpenIssue{{Number: 1, Title: "Improve search ranking"}}
	got := findingFor(t, input, testCatalog(), issues, "search")
	if len(got.DuplicateIssues) != 0 {
		t.Fatalf("single-token feature must not match: %+v", got.DuplicateIssues)
	}
	if got.Recommendation != RecCreateNew {
		t.Fatalf("rec = %q, want %q", got.Recommendation, RecCreateNew)
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	t.Parallel()

	input := AuditInput{Competitors: []Competitor{
		{Name: "b-comp", Findings: []AuditFinding{{Feature: "z", GapClass: "missing", OwnerSurface: "api"}, {Feature: "a", GapClass: "missing", OwnerSurface: "api"}}},
		{Name: "a-comp", Findings: []AuditFinding{{Feature: "m", GapClass: "missing", OwnerSurface: "api"}}},
	}}
	report := Generate(input, testCatalog(), nil)
	order := []string{}
	for _, e := range report.Entries {
		order = append(order, e.Competitor+"/"+e.Feature)
	}
	want := []string{"a-comp/m", "b-comp/a", "b-comp/z"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}
