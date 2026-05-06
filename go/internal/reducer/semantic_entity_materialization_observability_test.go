package reducer

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSemanticEntityMaterializationHandlerLogsStageTiming(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	loader := &fakeSemanticEntityFactLoader{
		envelopes: []facts.Envelope{
			{
				FactKind: "content_entity",
				SourceRef: facts.Ref{
					SourceURI: "/repo/src/app.js",
				},
				Payload: map[string]any{
					"repo_id":       "repo-1",
					"entity_id":     "function-1",
					"relative_path": "src/app.js",
					"entity_type":   "Function",
					"entity_name":   "getTab",
					"language":      "javascript",
					"method_kind":   "getter",
				},
			},
		},
	}
	writer := &recordingSemanticEntityWriter{
		result: SemanticEntityWriteResult{CanonicalWrites: 1},
	}

	handler := SemanticEntityMaterializationHandler{
		FactLoader: loader,
		Writer:     writer,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		SourceSystem: "git",
		Domain:       DomainSemanticEntityMaterialization,
		Status:       IntentStatusClaimed,
		EnqueuedAt:   time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	logText := logs.String()
	for _, want := range []string{
		`"msg":"semantic entity materialization completed"`,
		`"fact_count":1`,
		`"row_count":1`,
		`"repo_count":1`,
		`"skip_retract":false`,
		`"load_facts_duration_seconds":`,
		`"extract_duration_seconds":`,
		`"retract_decision_duration_seconds":`,
		`"graph_write_duration_seconds":`,
		`"phase_publish_duration_seconds":`,
		`"total_duration_seconds":`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s:\n%s", want, logText)
		}
	}
}
