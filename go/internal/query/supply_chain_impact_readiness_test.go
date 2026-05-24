package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type recordingSupplyChainImpactReadinessStore struct {
	snapshot   SupplyChainImpactReadinessSnapshot
	err        error
	lastQuery  SupplyChainImpactReadinessQuery
	calls      int
}

func (s *recordingSupplyChainImpactReadinessStore) ReadSupplyChainImpactReadiness(
	_ context.Context,
	query SupplyChainImpactReadinessQuery,
) (SupplyChainImpactReadinessSnapshot, error) {
	s.lastQuery = query
	s.calls++
	if s.err != nil {
		return SupplyChainImpactReadinessSnapshot{}, s.err
	}
	clone := SupplyChainImpactReadinessSnapshot{
		EvidenceSources:    append([]SupplyChainImpactEvidenceFamily(nil), s.snapshot.EvidenceSources...),
		UnsupportedTargets: append([]string(nil), s.snapshot.UnsupportedTargets...),
		TargetIncomplete:   s.snapshot.TargetIncomplete,
		IncompleteReasons:  append([]string(nil), s.snapshot.IncompleteReasons...),
	}
	return clone, nil
}

func TestBuildSupplyChainImpactReadinessClassifiesNotConfigured(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{},
	)
	if envelope.State != ReadinessStateNotConfigured {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateNotConfigured)
	}
	wantMissing := []string{MissingEvidenceAdvisorySources, MissingEvidenceOwnedPackages}
	sort.Strings(wantMissing)
	if !reflect.DeepEqual(envelope.MissingEvidence, wantMissing) {
		t.Fatalf("missing_evidence = %#v, want %#v", envelope.MissingEvidence, wantMissing)
	}
	if envelope.Freshness != FreshnessLabelUnknown {
		t.Fatalf("freshness = %q, want %q", envelope.Freshness, FreshnessLabelUnknown)
	}
	if envelope.Counts.FindingsReturned != 0 || envelope.Counts.FindingsTruncated {
		t.Fatalf("counts = %#v, want zero/false", envelope.Counts)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesEvidenceIncomplete(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 12, Freshness: FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != ReadinessStateEvidenceIncomplete {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateEvidenceIncomplete)
	}
	if !readinessMissingContains(envelope.MissingEvidence, MissingEvidenceOwnedPackages) {
		t.Fatalf("missing_evidence = %#v, want owned_packages", envelope.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesReadyZeroFindings(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 12, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageConsumption, FactCount: 3, Freshness: FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != ReadinessStateReadyZeroFindings {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateReadyZeroFindings)
	}
	if envelope.Freshness != FreshnessLabelFresh {
		t.Fatalf("freshness = %q, want %q", envelope.Freshness, FreshnessLabelFresh)
	}
	if len(envelope.MissingEvidence) != 0 {
		t.Fatalf("missing_evidence = %#v, want empty", envelope.MissingEvidence)
	}
	if envelope.Counts.EvidenceFactsTotal != 15 {
		t.Fatalf("evidence_facts_total = %d, want 15", envelope.Counts.EvidenceFactsTotal)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesReadyWithFindings(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{CVEID: "CVE-2026-0001"},
		[]SupplyChainImpactFindingResult{
			{FindingID: "finding-1", ImpactStatus: "affected_exact"},
			{FindingID: "finding-2", ImpactStatus: "possibly_affected"},
		},
		true,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: FreshnessLabelFresh},
			},
		},
	)
	if envelope.State != ReadinessStateReadyWithFindings {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateReadyWithFindings)
	}
	if !envelope.Counts.FindingsTruncated {
		t.Fatal("findings_truncated = false, want true")
	}
	if got, want := envelope.Counts.FindingsByStatus["affected_exact"], 1; got != want {
		t.Fatalf("findings_by_status[affected_exact] = %d, want %d", got, want)
	}
	if got, want := envelope.Counts.FindingsByStatus["possibly_affected"], 1; got != want {
		t.Fatalf("findings_by_status[possibly_affected] = %d, want %d", got, want)
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesTargetIncomplete(t *testing.T) {
	t.Parallel()

	// target_incomplete only fires when scope-relevant advisory evidence is
	// still missing; an in-flight snapshot for any source can flip the state
	// only when the scope has no advisory facts yet.
	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{RepositoryID: "repo://example/api"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			TargetIncomplete:  true,
			IncompleteReasons: []string{"nvd_paging_in_progress"},
		},
	)
	if envelope.State != ReadinessStateTargetIncomplete {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateTargetIncomplete)
	}
	if !readinessMissingContains(envelope.MissingEvidence, MissingEvidenceTargetCollection) {
		t.Fatalf("missing_evidence = %#v, want target_collection_incomplete", envelope.MissingEvidence)
	}
	if got, want := envelope.IncompleteReasons, []string{"nvd_paging_in_progress"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("incomplete_reasons = %#v, want %#v", got, want)
	}
}

func TestBuildSupplyChainImpactReadinessScopeGuardsTargetIncomplete(t *testing.T) {
	t.Parallel()

	// An in-flight snapshot for an unrelated source must NOT downgrade a
	// scope whose advisory evidence is already collected. Otherwise normal
	// staggered ingestion makes ready_zero_findings unreachable.
	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{CVEID: "CVE-2026-0001"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: FreshnessLabelFresh},
			},
			TargetIncomplete:  true,
			IncompleteReasons: []string{"epss_refresh_in_progress"},
		},
	)
	if envelope.State != ReadinessStateReadyZeroFindings {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateReadyZeroFindings)
	}
	if len(envelope.MissingEvidence) != 0 {
		t.Fatalf("missing_evidence = %#v, want empty for ready scope", envelope.MissingEvidence)
	}
	if len(envelope.IncompleteReasons) != 0 {
		t.Fatalf("incomplete_reasons = %#v, want empty for ready scope", envelope.IncompleteReasons)
	}
}

func TestBuildSupplyChainImpactReadinessClearsMissingOnReadyWithFindings(t *testing.T) {
	t.Parallel()

	// findings + missing-evidence reasons must not coexist in the envelope:
	// once the reducer admitted a finding, missing_evidence becomes
	// internally contradictory and is dropped.
	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{RepositoryID: "repo://example/api"},
		[]SupplyChainImpactFindingResult{
			{FindingID: "finding-1", ImpactStatus: "affected_exact"},
		},
		false,
		SupplyChainImpactReadinessSnapshot{},
	)
	if envelope.State != ReadinessStateReadyWithFindings {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateReadyWithFindings)
	}
	if len(envelope.MissingEvidence) != 0 {
		t.Fatalf("missing_evidence = %#v, want empty for ready_with_findings", envelope.MissingEvidence)
	}
}

func TestBuildSupplyChainImpactReadinessUnavailable(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadinessUnavailable(
		SupplyChainImpactTargetScope{RepositoryID: "repo://example/api"},
		[]SupplyChainImpactFindingResult{{FindingID: "finding-1", ImpactStatus: "affected_exact"}},
		true,
	)
	if envelope.State != ReadinessStateReadinessUnavailable {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateReadinessUnavailable)
	}
	if !readinessMissingContains(envelope.MissingEvidence, MissingEvidenceReadinessUnavailable) {
		t.Fatalf("missing_evidence = %#v, want readiness_unavailable", envelope.MissingEvidence)
	}
	if envelope.Counts.FindingsReturned != 1 {
		t.Fatalf("counts.findings_returned = %d, want 1 (findings page must survive)", envelope.Counts.FindingsReturned)
	}
	if !envelope.Counts.FindingsTruncated {
		t.Fatal("counts.findings_truncated = false, want true (carries the original page truncation)")
	}
}

func TestBuildSupplyChainImpactReadinessClassifiesUnsupported(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{SubjectDigest: "sha256:abc"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyContainerImageIdentity, FactCount: 1, Freshness: FreshnessLabelFresh},
			},
			UnsupportedTargets: []string{"oci-runtime-unsupported"},
		},
	)
	if envelope.State != ReadinessStateUnsupported {
		t.Fatalf("state = %q, want %q", envelope.State, ReadinessStateUnsupported)
	}
	if !readinessMissingContains(envelope.MissingEvidence, MissingEvidenceUnsupportedTarget) {
		t.Fatalf("missing_evidence = %#v, want unsupported_target", envelope.MissingEvidence)
	}
	if got, want := envelope.UnsupportedTargets, []string{"oci-runtime-unsupported"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unsupported_targets = %#v, want %#v", got, want)
	}
}

func TestBuildSupplyChainImpactReadinessAggregatesFreshness(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{CVEID: "CVE-2026-0001"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 3, Freshness: FreshnessLabelStale},
				{Family: EvidenceFamilyVulnerabilityExploitability, FactCount: 1, Freshness: FreshnessLabelFresh},
			},
		},
	)
	if envelope.Freshness != FreshnessLabelStale {
		t.Fatalf("freshness = %q, want %q", envelope.Freshness, FreshnessLabelStale)
	}
}

func TestBuildSupplyChainImpactReadinessNormalizesEvidenceSources(t *testing.T) {
	t.Parallel()

	envelope := BuildSupplyChainImpactReadiness(
		SupplyChainImpactTargetScope{CVEID: "CVE-2026-0001"},
		nil,
		false,
		SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyPackageRegistry, FactCount: 1},
				{Family: " "},
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 7},
			},
		},
	)
	want := []string{EvidenceFamilyPackageRegistry, EvidenceFamilyVulnerabilityAdvisory}
	got := make([]string, 0, len(envelope.EvidenceSources))
	for _, source := range envelope.EvidenceSources {
		got = append(got, source.Family)
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("evidence_sources = %#v, want %#v", got, want)
	}
}

func TestSupplyChainListImpactFindingsAttachesReadinessForZeroFindings(t *testing.T) {
	t.Parallel()

	readiness := &recordingSupplyChainImpactReadinessStore{
		snapshot: SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 5, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageConsumption, FactCount: 2, Freshness: FreshnessLabelFresh},
			},
		},
	}
	findings := &recordingSupplyChainImpactFindingStore{}
	handler := &SupplyChainHandler{
		ImpactFindings: findings,
		Readiness:      readiness,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if readiness.calls != 1 {
		t.Fatalf("readiness.calls = %d, want 1", readiness.calls)
	}
	if got, want := readiness.lastQuery.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("readiness.lastQuery.RepositoryID = %q, want %q", got, want)
	}

	var resp struct {
		Findings  []SupplyChainImpactFindingResult   `json:"findings"`
		Count     int                                `json:"count"`
		Limit     int                                `json:"limit"`
		Truncated bool                               `json:"truncated"`
		Readiness SupplyChainImpactReadinessEnvelope `json:"readiness"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Readiness.State != ReadinessStateReadyZeroFindings {
		t.Fatalf("readiness.state = %q, want %q", resp.Readiness.State, ReadinessStateReadyZeroFindings)
	}
	if resp.Readiness.TargetScope.RepositoryID != "repo://example/api" {
		t.Fatalf("readiness.target_scope.repository_id = %q, want repo://example/api", resp.Readiness.TargetScope.RepositoryID)
	}
	if resp.Readiness.Freshness != FreshnessLabelFresh {
		t.Fatalf("readiness.freshness = %q, want %q", resp.Readiness.Freshness, FreshnessLabelFresh)
	}
	if resp.Count != 0 || resp.Truncated {
		t.Fatalf("count/truncated = %d/%v, want zero", resp.Count, resp.Truncated)
	}
}

func TestSupplyChainListImpactFindingsReadinessSurfacesNotConfigured(t *testing.T) {
	t.Parallel()

	readiness := &recordingSupplyChainImpactReadinessStore{}
	findings := &recordingSupplyChainImpactFindingStore{}
	handler := &SupplyChainHandler{
		ImpactFindings: findings,
		Readiness:      readiness,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=5",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Readiness SupplyChainImpactReadinessEnvelope `json:"readiness"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Readiness.State != ReadinessStateNotConfigured {
		t.Fatalf("readiness.state = %q, want %q", resp.Readiness.State, ReadinessStateNotConfigured)
	}
	if resp.Readiness.TargetScope.CVEID != "CVE-2026-0001" {
		t.Fatalf("readiness.target_scope.cve_id = %q, want CVE-2026-0001", resp.Readiness.TargetScope.CVEID)
	}
	if !readinessMissingContains(resp.Readiness.MissingEvidence, MissingEvidenceAdvisorySources) {
		t.Fatalf("missing_evidence = %#v, want advisory_sources", resp.Readiness.MissingEvidence)
	}
}

func TestSupplyChainListImpactFindingsReadinessWithFindings(t *testing.T) {
	t.Parallel()

	readiness := &recordingSupplyChainImpactReadinessStore{
		snapshot: SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: FreshnessLabelFresh},
			},
		},
	}
	findings := &recordingSupplyChainImpactFindingStore{
		rows: []SupplyChainImpactFindingRow{
			{FindingID: "finding-1", CVEID: "CVE-2026-0001", ImpactStatus: "affected_exact"},
			{FindingID: "finding-2", CVEID: "CVE-2026-0001", ImpactStatus: "possibly_affected"},
		},
	}
	handler := &SupplyChainHandler{
		ImpactFindings: findings,
		Readiness:      readiness,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Findings  []SupplyChainImpactFindingResult   `json:"findings"`
		Truncated bool                               `json:"truncated"`
		Readiness SupplyChainImpactReadinessEnvelope `json:"readiness"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Readiness.State != ReadinessStateReadyWithFindings {
		t.Fatalf("readiness.state = %q, want %q", resp.Readiness.State, ReadinessStateReadyWithFindings)
	}
	if !resp.Readiness.Counts.FindingsTruncated {
		t.Fatal("readiness.counts.findings_truncated = false, want true (page limit was 1, store had 2)")
	}
	if got, want := resp.Readiness.Counts.FindingsReturned, len(resp.Findings); got != want {
		t.Fatalf("readiness.counts.findings_returned = %d, want %d", got, want)
	}
}

func TestPostgresSupplyChainImpactReadinessQueryShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		// Each fact_kind allowlist binding is referenced.
		"fact.fact_kind = ANY($1::text[])",
		"fact.fact_kind = ANY($2::text[])",
		"fact.fact_kind = ANY($3::text[])",
		"fact.fact_kind = ANY($4::text[])",
		"fact.fact_kind = ANY($5::text[])",
		"fact.fact_kind = ANY($6::text[])",
		"fact.fact_kind = ANY($7::text[])",
		"fact.fact_kind = ANY($8::text[])",
		// Active-fact gates are pushed into every per-family CTE.
		"generation.status = 'active'",
		"fact.is_tombstone = FALSE",
		// All 7 evidence families plus the source-snapshot rollup must
		// appear so a refactor that drops a CTE branch fails loudly.
		"'vulnerability.advisory' AS family",
		"'vulnerability.exploitability' AS family",
		"'package.consumption' AS family",
		"'package.registry' AS family",
		"'sbom.component' AS family",
		"'sbom.attestation' AS family",
		"'container_image.identity' AS family",
		"'vulnerability.source_snapshot' AS family",
		// Manifest consumption uses the real content_entity discriminator.
		"fact.fact_kind = 'content_entity'",
		"entity_metadata'->>'config_kind' = 'dependency'",
		"payload->>'repo_id'",
		// Source-snapshot completion check uses JSONB containment to
		// avoid boolean-cast errors on non-canonical payload values, and
		// surfaces all distinct warning messages.
		`payload @> '{"complete": false}'::jsonb`,
		"ARRAY_AGG(DISTINCT NULLIF(TRIM(payload->>'warning_message'), ''))",
	} {
		if !strings.Contains(listSupplyChainImpactReadinessQuery, want) {
			t.Fatalf("listSupplyChainImpactReadinessQuery missing %q:\n%s", want, listSupplyChainImpactReadinessQuery)
		}
	}
}

func TestSupplyChainListImpactFindingsReadinessWithoutStore(t *testing.T) {
	t.Parallel()

	findings := &recordingSupplyChainImpactFindingStore{}
	handler := &SupplyChainHandler{ImpactFindings: findings}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=5",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Readiness *SupplyChainImpactReadinessEnvelope `json:"readiness"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Readiness == nil {
		t.Fatal("readiness = nil, want envelope")
	}
	if resp.Readiness.State != ReadinessStateNotConfigured {
		t.Fatalf("readiness.state = %q, want %q (no store => no source evidence)", resp.Readiness.State, ReadinessStateNotConfigured)
	}
}
