// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
)

// TestCov8DecodeIncidentCorrelations pins the reducer-derived row
// decoders: raw payload lookups map through verbatim.
func TestCov8DecodeIncidentCorrelations(t *testing.T) {
	t.Parallel()

	cicd := decodeIncidentCICDRunCorrelation(incidentContextFactRow{
		FactID: "fact-cicd",
		Payload: map[string]any{
			"provider": "github", "run_id": "99", "commit_sha": "abc123",
			"environment": "prod", "outcome": "exact",
			"provenance_only": true, "evidence_fact_ids": []any{"e1"},
		},
	})
	if cicd.Provider != "github" || cicd.RunID != "99" || cicd.CommitSHA != "abc123" ||
		!cicd.ProvenanceOnly || len(cicd.EvidenceFactIDs) != 1 {
		t.Errorf("cicd decode = %+v, want mapped fields", cicd)
	}

	k8s := decodeIncidentKubernetesCorrelation(incidentContextFactRow{
		FactID: "fact-k8s",
		Payload: map[string]any{
			"cluster_id": "c1", "namespace": "web", "image_ref": "img:1",
			"provenance_only": false, "candidate_source_digests": []any{"sha256:x"},
		},
	})
	if k8s.ClusterID != "c1" || k8s.Namespace != "web" || k8s.ImageRef != "img:1" ||
		k8s.ProvenanceOnly || len(k8s.CandidateSourceDigests) != 1 {
		t.Errorf("k8s decode = %+v, want mapped fields", k8s)
	}

	mapped := incidentCICDRunCorrelationsFromRows([]querycontract.CICDRunCorrelationRow{
		{CorrelationID: "c1", Provider: "github", RunID: "7"},
		{CorrelationID: "c2", Provider: "gitlab", CommitSHA: "abc"},
	})
	if len(mapped) != 2 || mapped[0].FactID != "c1" || mapped[1].CommitSHA != "abc" {
		t.Errorf("fromRows = %+v, want both rows mapped", mapped)
	}
	if mapped := incidentCICDRunCorrelationsFromRows(nil); len(mapped) != 0 {
		t.Errorf("fromRows(nil) = %v, want empty", mapped)
	}
}

// cov8CICDStore is a CICDRunCorrelationStore double.
type cov8CICDStore struct {
	rows []querycontract.CICDRunCorrelationRow
	err  error
}

func (s cov8CICDStore) ListCICDRunCorrelations(
	context.Context, querycontract.CICDRunCorrelationFilter,
) ([]querycontract.CICDRunCorrelationRow, error) {
	return s.rows, s.err
}

// cov8CatalogStore is a ServiceCatalogCorrelationStore double.
type cov8CatalogStore struct {
	rows []querycontract.ServiceCatalogCorrelationRow
	err  error
}

func (s cov8CatalogStore) ListServiceCatalogCorrelations(
	context.Context, querycontract.ServiceCatalogCorrelationFilter,
) ([]querycontract.ServiceCatalogCorrelationRow, error) {
	return s.rows, s.err
}

// cov8ImageStore is a ContainerImageIdentityStore double.
type cov8ImageStore struct {
	rows []supplychain.ContainerImageIdentityRow
	err  error
}

func (s cov8ImageStore) ListContainerImageIdentities(
	context.Context, supplychain.ContainerImageIdentityFilter,
) ([]supplychain.ContainerImageIdentityRow, error) {
	return s.rows, s.err
}

// TestCov8IncidentPortReads pins the injected-port reads: missing ports
// fail loud, rows map through, port errors wrap.
func TestCov8IncidentPortReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bare := NewStore(nil)

	if _, err := bare.readIncidentServiceCatalogCorrelations(ctx, incidentServiceCatalogOperationalLink{}); err == nil {
		t.Error("catalog read without port = nil, want required-store error")
	}
	if _, err := bare.readIncidentContainerImageIdentities(ctx, "repo-1"); err == nil {
		t.Error("image read without port = nil, want required-store error")
	}
	if _, err := bare.readIncidentCICDRunCorrelations(ctx, incidentContainerImageIdentity{Digest: "sha256:x"}); err == nil {
		t.Error("cicd read without port = nil, want required-store error")
	}
	// No digest and no ref: nothing to correlate, no port needed.
	if got, err := bare.readIncidentCICDRunCorrelations(ctx, incidentContainerImageIdentity{}); err != nil || got != nil {
		t.Errorf("empty-image cicd read = %v, %v; want nil, nil", got, err)
	}
	if got, err := bare.readIncidentKubernetesCorrelations(ctx, incidentContainerImageIdentity{}); err != nil || got != nil {
		t.Errorf("empty-image k8s read = %v, %v; want nil, nil", got, err)
	}

	portErr := errors.New("port down")
	withPorts := NewStore(nil).
		WithCatalog(cov8CatalogStore{err: portErr}).
		WithCICD(cov8CICDStore{err: portErr}).
		WithImages(cov8ImageStore{err: portErr})
	if _, err := withPorts.readIncidentServiceCatalogCorrelations(ctx, incidentServiceCatalogOperationalLink{}); !errors.Is(err, portErr) {
		t.Errorf("catalog port error = %v, want wrap", err)
	}
	if _, err := withPorts.readIncidentContainerImageIdentities(ctx, "repo-1"); !errors.Is(err, portErr) {
		t.Errorf("image port error = %v, want wrap", err)
	}
	if _, err := withPorts.readIncidentCICDRunCorrelations(ctx, incidentContainerImageIdentity{Digest: "sha256:x"}); !errors.Is(err, portErr) {
		t.Errorf("cicd port error = %v, want wrap", err)
	}

	mapped := NewStore(nil).
		WithCatalog(cov8CatalogStore{rows: []querycontract.ServiceCatalogCorrelationRow{{
			CorrelationID: "corr-1", Provider: "svc", EntityRef: "svc/web",
			DisplayName: "web", RepositoryID: "repo-1", EvidenceFactIDs: []string{"e1"},
		}}}).
		WithCICD(cov8CICDStore{rows: []querycontract.CICDRunCorrelationRow{{
			CorrelationID: "run-1", Provider: "github", RunID: "7", CommitSHA: "abc",
		}}}).
		WithImages(cov8ImageStore{rows: []supplychain.ContainerImageIdentityRow{{
			IdentityID: "img-1", Digest: "sha256:x", ImageRef: "img:1", RepositoryID: "repo-1",
		}}})
	correlations, err := mapped.readIncidentServiceCatalogCorrelations(ctx, incidentServiceCatalogOperationalLink{Provider: "svc"})
	if err != nil || len(correlations) != 1 || correlations[0].RepositoryID != "repo-1" ||
		correlations[0].CandidateRepositoryIDs != nil {
		t.Errorf("catalog rows = %+v, %v; want one mapped row", correlations, err)
	}
	images, err := mapped.readIncidentContainerImageIdentities(ctx, "repo-1")
	if err != nil || len(images) != 1 || images[0].Digest != "sha256:x" {
		t.Errorf("image rows = %+v, %v; want one mapped row", images, err)
	}
	runs, err := mapped.readIncidentCICDRunCorrelations(ctx, incidentContainerImageIdentity{Digest: "sha256:x"})
	if err != nil || len(runs) != 1 || runs[0].RunID != "7" {
		t.Errorf("cicd rows = %+v, %v; want one mapped row", runs, err)
	}
}

// TestCov8IncidentRuntimeEvidenceArms pins the runtime read: an incident
// without a service URL stays empty, a down store wraps, and two links
// yield the ambiguous edge without needing the catalog port.
func TestCov8IncidentRuntimeEvidenceArms(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	edges, err := NewStore(nil).readIncidentRuntimeEvidence(ctx, model.IncidentContextIncident{})
	if err != nil || edges != nil {
		t.Errorf("URL-less incident = %v, %v; want nil, nil", edges, err)
	}
	withURL := model.IncidentContextIncident{}
	withURL.Service.URL = "https://example.com/svc"
	want := errors.New("links down")
	if _, err := NewStoreWithReadStore(&cov8ScriptQueryer{steps: []cov8Step{{err: want}}}).readIncidentRuntimeEvidence(ctx, withURL); !errors.Is(err, want) {
		t.Errorf("links failure = %v, want wrap of %v", err, want)
	}

	twoLinks := NewStoreWithReadStore(&cov8ScriptQueryer{steps: []cov8Step{{rows: [][]any{
		cov8FactRowValues("fact-link-1", map[string]any{"entity_ref": "svc/a", "title": "A"}),
		cov8FactRowValues("fact-link-2", map[string]any{"entity_ref": "svc/b", "title": "B"}),
	}}}})
	edges, err = twoLinks.readIncidentRuntimeEvidence(ctx, withURL)
	if err != nil {
		t.Fatalf("two-link read = %v, want nil", err)
	}
	if len(edges) != 1 || edges[0].TruthLabel != model.IncidentTruthAmbiguous ||
		len(edges[0].Candidates) != 2 || edges[0].Slot != model.IncidentSlotDeployable {
		t.Errorf("two-link edges = %+v, want one ambiguous deployable edge with 2 candidates", edges)
	}

	direct := ambiguousIncidentServiceLinkEdge([]incidentServiceCatalogOperationalLink{
		{EntityRef: "svc/a", Title: "A", URL: "https://a"},
		{EntityRef: "svc/b"},
	})
	if direct.Candidates[0].Label != "A" || direct.Candidates[1].Label != "svc/b" ||
		direct.Explanation == "" {
		t.Errorf("ambiguous edge = %+v, want title-then-ref labels plus explanation", direct)
	}
}

// TestCov8IncidentCandidateValues pins the candidate projections and
// their FirstNonEmpty fallback chains.
func TestCov8IncidentCandidateValues(t *testing.T) {
	t.Parallel()

	applied := incidentAppliedRoutingCandidateValues([]incidentAppliedPagerDutyRouting{
		{FactID: "f1", TerraformStateAddress: "pd_service.web", Outcome: "exact"},
		{FactID: "f2", ProviderObjectID: "P123"},
	})
	if len(applied) != 2 || applied[0].ID != "pd_service.web" || applied[1].ID != "P123" ||
		applied[1].Reason != "applied PagerDuty routing candidate" {
		t.Errorf("applied candidates = %+v, want fallback chains", applied)
	}
	observed := incidentObservedRoutingCandidateValues([]incidentObservedPagerDutyRouting{
		{FactID: "f1", ServiceID: "S1", Status: "active", SourceURL: "https://x", Outcome: "derived"},
	})
	if len(observed) != 1 || observed[0].ID != "S1" || observed[0].Label != "active" ||
		observed[0].URL != "https://x" {
		t.Errorf("observed candidates = %+v, want mapped row", observed)
	}
	records := incidentWorkItemRecordCandidates([]incidentWorkItemRecord{
		{FactID: "f1", WorkItemKey: "OPS-1", Summary: "fix it", SourceURL: "https://jira/1"},
		{FactID: "f2", WorkItemID: "10002"},
	})
	if len(records) != 2 || records[0].ID != "OPS-1" || records[1].ID != "10002" ||
		records[1].Label != "" {
		t.Errorf("work-item candidates = %+v, want key-then-id fallbacks", records)
	}
	catalog := incidentCatalogCandidates([]incidentServiceCatalogCorrelation{
		{DisplayName: "web", Reason: "r", CandidateRepositoryIDs: []string{"repo-1", "repo-2"}},
		{DisplayName: "svc", ServiceID: "S9"},
	})
	if len(catalog) != 3 || catalog[0].ID != "repo-1" || catalog[0].Label != "web" ||
		catalog[2].ID != "S9" || catalog[2].Reason != "catalog correlation candidate" {
		t.Errorf("catalog candidates = %+v, want per-repo entries plus the id fallback", catalog)
	}
	kubernetes := incidentKubernetesCandidates([]incidentKubernetesCorrelation{
		{WorkloadObjectID: "w1", WorkloadName: "web", Reason: "r"},
		{ImageRef: "img:1"},
	})
	if len(kubernetes) != 2 || kubernetes[0].ID != "w1" || kubernetes[1].ID != "img:1" ||
		kubernetes[1].Label != "img:1" {
		t.Errorf("kubernetes candidates = %+v, want fallback chains", kubernetes)
	}
}

// TestCov8IncidentChangeWindow pins the time helpers: empty bounds stay
// open, malformed timestamps fail closed, and filter bounds win over the
// incident's own timestamps.
func TestCov8IncidentChangeWindow(t *testing.T) {
	t.Parallel()

	if got := parseIncidentContextBoundMust(""); !got.IsZero() {
		t.Errorf("empty bound = %v, want zero", got)
	}
	bound := parseIncidentContextBoundMust("2026-05-01T12:00:00Z")
	if bound.Hour() != 12 {
		t.Errorf("bound = %v, want noon UTC", bound)
	}
	if _, err := parseIncidentContextBound("not-a-time"); err == nil {
		t.Error("malformed bound = nil, want error")
	}
	if got := incidentContextSQLTime(time.Time{}); got != nil {
		t.Errorf("zero SQL time = %v, want nil", got)
	}
	now := time.Now().UTC()
	if got := incidentContextSQLTime(now); got != now {
		t.Errorf("SQL time = %v, want passthrough", got)
	}

	change := model.IncidentContextChangeCandidate{Timestamp: "2026-05-01T12:00:00Z"}
	if !incidentChangeInWindow(change, time.Time{}, time.Time{}) {
		t.Error("open window rejects, want accept")
	}
	since := bound.Add(-time.Hour)
	until := bound.Add(time.Hour)
	if !incidentChangeInWindow(change, since, until) {
		t.Error("inside window rejects, want accept")
	}
	if incidentChangeInWindow(change, bound.Add(time.Hour), time.Time{}) {
		t.Error("before-since accepts, want reject")
	}
	if incidentChangeInWindow(change, time.Time{}, bound.Add(-time.Hour)) {
		t.Error("after-until accepts, want reject")
	}
	bad := model.IncidentContextChangeCandidate{Timestamp: "not-a-time"}
	if incidentChangeInWindow(bad, since, until) {
		t.Error("malformed timestamp accepts, want fail-closed reject")
	}

	since, until = incidentChangeWindow(model.IncidentContextFilter{Since: "2026-05-01T12:00:00Z"}, model.IncidentContextIncident{})
	if since.Hour() != 12 || !until.IsZero() {
		t.Errorf("filter since wins = %v to %v, want noon to zero", since, until)
	}
	incident := model.IncidentContextIncident{CreatedAt: "2026-05-01T12:00:00Z", UpdatedAt: "2026-05-02T12:00:00Z"}
	derivedSince, derivedUntil := incidentChangeWindow(model.IncidentContextFilter{}, incident)
	if derivedSince.Hour() != 11 || derivedUntil.Day() != 2 || derivedUntil.Hour() != 13 {
		t.Errorf("incident-derived window = %v to %v, want created-1h to updated+1h", derivedSince, derivedUntil)
	}
}

// TestCov8IncidentAuthorizerRequiresStore pins the authorizer guard: a
// storeless authorizer fails loud instead of querying nothing.
func TestCov8IncidentAuthorizerRequiresStore(t *testing.T) {
	t.Parallel()

	authorizer := NewPostgresIncidentRepositoryAuthorizer(nil)
	if _, err := authorizer.ResolveDurableIncidentRepositories(
		context.Background(), "pagerduty", "INC-1", "scope-1"); err == nil {
		t.Error("storeless resolve = nil, want database-required error")
	}
	// The store guard runs before the blank short-circuit.
	if _, err := authorizer.ResolveDurableIncidentRepositories(
		context.Background(), "", "", ""); err == nil {
		t.Error("storeless blank resolve = nil, want database-required error")
	}
	withReader := NewPostgresIncidentRepositoryAuthorizerWithReadStore(&cov8ScriptQueryer{})
	if repos, err := withReader.ResolveDurableIncidentRepositories(
		context.Background(), "", "", ""); err != nil || repos != nil {
		t.Errorf("blank resolve = %v, %v; want nil, nil", repos, err)
	}
}

func parseIncidentContextBoundMust(value string) time.Time {
	parsed, err := parseIncidentContextBound(value)
	if err != nil {
		panic(err)
	}
	return parsed
}
