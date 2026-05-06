package reducer

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestPlatformMaterializationHandlerLogsStageTiming(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	handler := PlatformMaterializationHandler{
		Writer: &recordingPlatformMaterializationWriter{
			result: PlatformMaterializationWriteResult{
				CanonicalWrites: 1,
				EvidenceSummary: "materialized platform binding",
			},
		},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-pm-observe",
		ScopeID:         "scope-1",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainDeploymentMapping,
		Cause:           "platform discovered",
		EntityKeys:      []string{"platform:kubernetes:aws:prod-cluster", "repo:service-edge-api"},
		RelatedScopeIDs: []string{"scope-1"},
		EnqueuedAt:      time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusClaimed,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	logText := logs.String()
	for _, want := range []string{
		`"msg":"deployment mapping materialization completed"`,
		`"entity_key_count":2`,
		`"related_scope_count":1`,
		`"canonical_write_count":1`,
		`"platform_write_duration_seconds":`,
		`"fact_load_duration_seconds":`,
		`"infrastructure_extract_duration_seconds":`,
		`"infrastructure_graph_write_duration_seconds":`,
		`"cross_repo_resolution_duration_seconds":`,
		`"workload_replay_duration_seconds":`,
		`"phase_publish_duration_seconds":`,
		`"total_duration_seconds":`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s:\n%s", want, logText)
		}
	}
}
