// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// impactAdmissionCannedStore is the pin-satisfying double behind the
// wire-shape proof: it implements exactly the fourteen pinned reads
// over fixed canned rows and is compile-asserted against the pin, so
// the proof below runs through the admission shape, not the wider
// staying ports.
type impactAdmissionCannedStore struct {
	entity querycontract.EntityContent
}

func impactAdmissionCannedEntity() querycontract.EntityContent {
	return querycontract.EntityContent{
		EntityID:     "entity-wire-1",
		RepoID:       "repo-wire",
		RelativePath: "src/wire/handler.go",
		EntityType:   "K8sResource",
		EntityName:   "wire-workload",
		Language:     "yaml",
		Metadata:     map[string]any{"wire": "entity"},
	}
}

func (s *impactAdmissionCannedStore) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return []map[string]any{{"wire": "graph-row", "n": 1}}, nil
}

func (s *impactAdmissionCannedStore) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return map[string]any{"wire": "graph-single", "n": 1}, nil
}

func (s *impactAdmissionCannedStore) GetEntityContent(context.Context, string) (*querycontract.EntityContent, error) {
	entity := s.entity
	return &entity, nil
}

func (s *impactAdmissionCannedStore) entityPage() ([]querycontract.EntityContent, error) {
	return []querycontract.EntityContent{s.entity}, nil
}

func (s *impactAdmissionCannedStore) SearchEntitiesByName(context.Context, string, string, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *impactAdmissionCannedStore) SearchEntitiesByExactNameAnyRepo(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *impactAdmissionCannedStore) ListRepoEntities(context.Context, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *impactAdmissionCannedStore) ListRepoEntitiesByType(context.Context, string, string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *impactAdmissionCannedStore) ListRepoEntitiesByPaths(context.Context, string, []string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *impactAdmissionCannedStore) ListRepoEntitiesByIDs(context.Context, string, []string, int) ([]querycontract.EntityContent, error) {
	return s.entityPage()
}

func (s *impactAdmissionCannedStore) ListRepoK8sSelectCandidates(context.Context, string, int) ([]querycontract.K8sSelectCandidate, error) {
	return []querycontract.K8sSelectCandidate{{EntityID: "entity-wire-1", EntityName: "wire-workload", Kind: "Deployment", Namespace: "wire-ns"}}, nil
}

func (s *impactAdmissionCannedStore) InvestigateCodeTopic(_ context.Context, req codequery.CodeTopicInvestigationRequest) ([]codequery.CodeTopicEvidenceRow, error) {
	if req.Topic != "wire" {
		return nil, nil
	}
	return []codequery.CodeTopicEvidenceRow{{
		SourceKind:   "content",
		RepoID:       "repo-wire",
		RelativePath: "src/wire/handler.go",
		EntityName:   "handleWire",
		MatchedTerms: []string{"wire"},
		Score:        7,
	}}, nil
}

func (s *impactAdmissionCannedStore) HasLiveIdentityMatch(_ context.Context, filter deployment.KubernetesPodTemplateFilter) (bool, error) {
	return filter.TrackingID == "wire-tracking", nil
}

func (s *impactAdmissionCannedStore) ListLiveIdentityMatches(_ context.Context, filter deployment.KubernetesPodTemplateFilter) ([]deployment.LiveIdentityMatch, error) {
	if filter.TrackingID != "wire-tracking" {
		return nil, nil
	}
	return []deployment.LiveIdentityMatch{{ClusterID: "cluster-wire", ObjectID: "object-wire"}}, nil
}

func (s *impactAdmissionCannedStore) ListRunningTasks(context.Context, string, querycontract.RepositoryAccessFilter) (*deployment.ECSServiceTaskEvidence, error) {
	running := 1
	return &deployment.ECSServiceTaskEvidence{
		RunningCount: &running,
		ImageDigests: []string{"wire-digest"},
	}, nil
}

var _ ImpactReadAdmission = (*impactAdmissionCannedStore)(nil)

// marshalImpactAdmissionWire projects one value through the same
// encoding the handlers use (no HTML escaping), so the proof compares
// wire bytes the way a satellite consumer would read them, not Go
// structs that could drift from the wire.
func marshalImpactAdmissionWire(t *testing.T, v any) string {
	t.Helper()
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("marshal wire shape: %v", err)
	}
	return strings.TrimSpace(sb.String())
}

// TestImpactReadAdmissionWireShapesAreSatelliteConsumable proves a
// satellite can consume the pin using only the pin plus the
// querycontract, codequery, and deployment shapes: every one of the
// fourteen reads answers through an ImpactReadAdmission-typed value,
// the topic search carries its bounded request, the live and ECS reads
// answer through their filter shapes, and the entity and graph wire
// projections carry the keys a satellite record would depend on. A
// fail-closed empty-grant read (nil entity, no error) marshals to
// null, so the satellite path has no hidden non-nullable expectation.
func TestImpactReadAdmissionWireShapesAreSatelliteConsumable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var pin ImpactReadAdmission = &impactAdmissionCannedStore{
		entity: impactAdmissionCannedEntity(),
	}

	rows, err := pin.Run(ctx, "MATCH (n) RETURN n LIMIT 1", map[string]any{"repo_id": "repo-wire"})
	if err != nil || len(rows) != 1 || rows[0]["wire"] != "graph-row" {
		t.Fatalf("Run() = %#v, %v, want the one canned graph row", rows, err)
	}
	single, err := pin.RunSingle(ctx, "MATCH (n) RETURN n LIMIT 1", map[string]any{"repo_id": "repo-wire"})
	if err != nil || single["wire"] != "graph-single" {
		t.Fatalf("RunSingle() = %#v, %v, want the canned graph single", single, err)
	}

	gotEntity, err := pin.GetEntityContent(ctx, "entity-wire-1")
	if err != nil || gotEntity == nil || gotEntity.EntityID != "entity-wire-1" {
		t.Fatalf("GetEntityContent() = %#v, %v, want the canned entity", gotEntity, err)
	}
	if wire := marshalImpactAdmissionWire(t, gotEntity); !strings.Contains(wire, `"entity_id":"entity-wire-1"`) || !strings.Contains(wire, `"repo_id":"repo-wire"`) {
		t.Errorf("entity wire bytes = %s, want entity_id and repo_id keys", wire)
	}

	if page, err := pin.SearchEntitiesByName(ctx, "repo-wire", "K8sResource", "wire-workload", 10); err != nil || len(page) != 1 {
		t.Fatalf("SearchEntitiesByName() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.SearchEntitiesByExactNameAnyRepo(ctx, "Service", "wire-svc", 10); err != nil || len(page) != 1 {
		t.Fatalf("SearchEntitiesByExactNameAnyRepo() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.ListRepoEntities(ctx, "repo-wire", 10); err != nil || len(page) != 1 {
		t.Fatalf("ListRepoEntities() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.ListRepoEntitiesByType(ctx, "repo-wire", "TerraformResource", 10); err != nil || len(page) != 1 {
		t.Fatalf("ListRepoEntitiesByType() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.ListRepoEntitiesByPaths(ctx, "repo-wire", []string{"src/wire/handler.go"}, 10); err != nil || len(page) != 1 {
		t.Fatalf("ListRepoEntitiesByPaths() rows = %d, %v, want one", len(page), err)
	}
	if page, err := pin.ListRepoEntitiesByIDs(ctx, "repo-wire", []string{"entity-wire-1"}, 10); err != nil || len(page) != 1 {
		t.Fatalf("ListRepoEntitiesByIDs() rows = %d, %v, want one", len(page), err)
	}
	if candidates, err := pin.ListRepoK8sSelectCandidates(ctx, "repo-wire", 10); err != nil || len(candidates) != 1 || candidates[0].EntityID != "entity-wire-1" {
		t.Fatalf("ListRepoK8sSelectCandidates() = %#v, %v, want the canned candidate", candidates, err)
	}

	topicRows, err := pin.InvestigateCodeTopic(ctx, codequery.CodeTopicInvestigationRequest{Topic: "wire", Intent: "change_surface", Limit: 11})
	if err != nil || len(topicRows) != 1 || topicRows[0].EntityName != "handleWire" {
		t.Fatalf("InvestigateCodeTopic() = %#v, %v, want the canned topic row", topicRows, err)
	}
	if wire := marshalImpactAdmissionWire(t, topicRows[0]); !strings.Contains(wire, `"RepoID":"repo-wire"`) || !strings.Contains(wire, `"MatchedTerms":["wire"]`) {
		t.Errorf("topic wire bytes = %s, want RepoID and MatchedTerms", wire)
	}
	if empty, err := pin.InvestigateCodeTopic(ctx, codequery.CodeTopicInvestigationRequest{Topic: "nothing-matches"}); err != nil || len(empty) != 0 {
		t.Fatalf("InvestigateCodeTopic() unmatching = %#v, %v, want zero rows and no error", empty, err)
	}

	matched, err := pin.HasLiveIdentityMatch(ctx, deployment.KubernetesPodTemplateFilter{TrackingID: "wire-tracking"})
	if err != nil || !matched {
		t.Fatalf("HasLiveIdentityMatch() = %v, %v, want true for the canned tracking id", matched, err)
	}
	if unmatched, err := pin.HasLiveIdentityMatch(ctx, deployment.KubernetesPodTemplateFilter{TrackingID: "other"}); err != nil || unmatched {
		t.Fatalf("HasLiveIdentityMatch() unmatching = %v, %v, want false and no error", unmatched, err)
	}
	matches, err := pin.ListLiveIdentityMatches(ctx, deployment.KubernetesPodTemplateFilter{TrackingID: "wire-tracking"})
	if err != nil || len(matches) != 1 || matches[0].ClusterID != "cluster-wire" {
		t.Fatalf("ListLiveIdentityMatches() = %#v, %v, want the canned match", matches, err)
	}

	evidence, err := pin.ListRunningTasks(ctx, "arn:aws:ecs:wire", querycontract.RepositoryAccessFilter{})
	if err != nil || evidence == nil || evidence.RunningCount == nil || *evidence.RunningCount != 1 {
		t.Fatalf("ListRunningTasks() = %#v, %v, want the canned task evidence", evidence, err)
	}
	if wire := marshalImpactAdmissionWire(t, evidence); !strings.Contains(wire, `"ImageDigests":["wire-digest"]`) {
		t.Errorf("ECS wire bytes = %s, want the ImageDigests key", wire)
	}

	if wire := marshalImpactAdmissionWire(t, (*querycontract.EntityContent)(nil)); wire != "null" {
		t.Errorf("nil entity wire bytes = %s, want null (fail-closed emptiness has no hidden shape)", wire)
	}
	if wire := marshalImpactAdmissionWire(t, rows); !strings.Contains(wire, `"wire":"graph-row"`) {
		t.Errorf("graph wire bytes = %s, want the canned graph row", wire)
	}
}
