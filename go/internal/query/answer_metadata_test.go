// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestAnswerMetadataAttachedToStoryAndInvestigationResponses(t *testing.T) {
	t.Parallel()

	serviceStory := buildServiceStoryResponse("workload:sample-service-api", sampleServiceDossierContext())
	assertAnswerMetadata(t, "service story", serviceStory)

	repositoryStory := buildRepositoryStoryResponseWithCoverage(
		RepoRef{ID: "repo-payments", Name: "payments", HasRemote: true},
		12,
		[]string{"go"},
		[]string{"payments-api"},
		[]string{"kubernetes"},
		3,
		map[string]any{"families": []string{"helm"}},
		nil,
		map[string]any{
			"state":     "partial",
			"truncated": true,
			"reason":    "semantic overview is still building",
		},
	)
	assertAnswerMetadata(t, "repository story", repositoryStory)

	codeTopic := codeTopicResponse(codeTopicInvestigationRequest{
		Topic:  "repo sync authentication",
		RepoID: "repo-payments",
		Limit:  1,
		Terms:  []string{"repo", "sync", "authentication"},
	}, []codeTopicEvidenceRow{{
		SourceKind:   "entity",
		RepoID:       "repo-payments",
		RelativePath: "go/internal/reposync/auth.go",
		EntityID:     "entity-auth",
		EntityName:   "resolveAuth",
		EntityType:   "Function",
		StartLine:    10,
		EndLine:      44,
	}}, true)
	assertAnswerMetadata(t, "code topic", codeTopic)

	changeSurface := (&ImpactHandler{}).changeSurfaceResponse(
		changeSurfaceInvestigationRequest{
			Topic:      "auth flow",
			RepoID:     "repo-payments",
			Limit:      1,
			MaxDepth:   2,
			Target:     "payments-api",
			TargetType: "service",
		},
		map[string]any{
			"status":    "resolved",
			"selected":  map[string]any{"id": "workload:payments-api"},
			"truncated": false,
		},
		map[string]any{
			"touched_symbols": []map[string]any{{
				"entity_id": "entity-auth",
				"source_handle": map[string]any{
					"entity_id": "entity-auth",
				},
			}},
			"coverage":  map[string]any{"query_shape": "content_topic_and_changed_path_surface"},
			"truncated": false,
		},
		[]map[string]any{{
			"id":     "repo-payments",
			"name":   "payments",
			"labels": []string{"Repository"},
			"depth":  1,
		}},
		false,
	)
	assertAnswerMetadata(t, "change surface", changeSurface)

	incident := BuildIncidentContextResponse(IncidentContextSnapshot{
		Query: IncidentContextQuery{ProviderIncidentID: "INC-1", Limit: 1},
		Incident: IncidentContextIncident{
			Provider:           "pagerduty",
			ProviderIncidentID: "INC-1",
			Title:              "payments degraded",
			EvidenceFactID:     "fact-incident",
		},
		Truncated: true,
	})
	if incident.AnswerMetadata.SchemaVersion != answerMetadataSchemaVersion {
		t.Fatalf("incident answer_metadata schema_version = %q, want %q", incident.AnswerMetadata.SchemaVersion, answerMetadataSchemaVersion)
	}
	if !incident.AnswerMetadata.Truncated {
		t.Fatal("incident answer_metadata.truncated = false, want true")
	}
	if len(incident.AnswerMetadata.MissingEvidence) == 0 {
		t.Fatal("incident answer_metadata.missing_evidence is empty, want missing incident path slots")
	}

	environment := environmentCompareResponse(
		compareEnvironmentsRequest{WorkloadID: "workload:payments-api", Left: "staging", Right: "prod", Limit: 1},
		map[string]any{"id": "workload:payments-api", "name": "payments-api"},
		map[string]any{"environment": "staging", "status": "present", "cloud_resources": []map[string]any{}},
		map[string]any{"environment": "prod", "status": "missing", "reason": "no prod evidence", "cloud_resources": []map[string]any{}},
		nil,
		0.4,
		"prod evidence missing",
		1,
		false,
		false,
	)
	assertAnswerMetadata(t, "environment comparison", environment)
}

func TestNewAnswerPacketFromMetadataConsumesNormalizedShape(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"answer_metadata": AnswerMetadata{
			SchemaVersion: answerMetadataSchemaVersion,
			EvidenceHandles: []map[string]any{{
				"kind":          "entity",
				"entity_id":     "entity-auth",
				"relative_path": "go/internal/reposync/auth.go",
			}},
			MissingEvidence: []map[string]any{{
				"kind":    "file",
				"repo_id": "repo-payments",
				"reason":  "README evidence missing",
			}},
			Limitations: []map[string]any{{
				"kind":   "result_truncated",
				"reason": "result truncated; not all evidence is included",
			}},
			Truncated: true,
			Coverage: map[string]any{
				"query_shape": "content_topic_investigation",
				"empty":       false,
			},
			RecommendedNextCalls: []map[string]any{{
				"tool": "get_code_relationship_story",
			}},
		},
	}
	metadata, ok := AnswerMetadataFromData(data)
	if !ok {
		t.Fatal("AnswerMetadataFromData ok = false, want true")
	}

	packet := NewAnswerPacketFromMetadata(AnswerPacketInput{
		PromptFamily: "code.topic",
		PrimaryTool:  "investigate_code_topic",
		Summary:      "auth flow is concentrated in resolveAuth",
		Envelope: &ResponseEnvelope{
			Data: data,
			Truth: BuildTruthEnvelope(
				ProfileProduction,
				codeTopicCapability,
				TruthBasisContentIndex,
				"resolved from bounded content-index topic investigation",
			),
		},
	}, metadata)

	if !packet.Partial {
		t.Fatal("packet.Partial = false, want true from metadata truncation")
	}
	if !packet.Truncated {
		t.Fatal("packet.Truncated = false, want true from metadata")
	}
	if len(packet.EvidenceHandles) != 1 {
		t.Fatalf("len(packet.EvidenceHandles) = %d, want 1", len(packet.EvidenceHandles))
	}
	if len(packet.MissingEvidence) != 1 {
		t.Fatalf("len(packet.MissingEvidence) = %d, want 1", len(packet.MissingEvidence))
	}
	if len(packet.RecommendedNextCalls) != 1 {
		t.Fatalf("len(packet.RecommendedNextCalls) = %d, want 1", len(packet.RecommendedNextCalls))
	}
	if len(packet.Limitations) == 0 {
		t.Fatal("packet.Limitations is empty, want metadata limitation reason")
	}
}

func assertAnswerMetadata(t *testing.T, name string, data map[string]any) {
	t.Helper()

	raw, ok := data["answer_metadata"]
	if !ok {
		t.Fatalf("%s missing answer_metadata: %#v", name, data)
	}
	metadata, ok := raw.(AnswerMetadata)
	if !ok {
		t.Fatalf("%s answer_metadata type = %T, want AnswerMetadata", name, raw)
	}
	if metadata.SchemaVersion != answerMetadataSchemaVersion {
		t.Fatalf("%s schema_version = %q, want %q", name, metadata.SchemaVersion, answerMetadataSchemaVersion)
	}
	if metadata.Coverage == nil {
		t.Fatalf("%s coverage is nil", name)
	}
	if metadata.EvidenceHandles == nil {
		t.Fatalf("%s evidence_handles is nil", name)
	}
	if metadata.MissingEvidence == nil {
		t.Fatalf("%s missing_evidence is nil", name)
	}
	if metadata.Limitations == nil {
		t.Fatalf("%s limitations is nil", name)
	}
	if metadata.PartialReasons == nil {
		t.Fatalf("%s partial_reasons is nil", name)
	}
	if metadata.RecommendedNextCalls == nil {
		t.Fatalf("%s recommended_next_calls is nil", name)
	}
}
