package status_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestLoadReportBuildsProjectionFromReader(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		snapshot: status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			ScopeActivity: status.ScopeActivitySnapshot{
				Active:  2,
				Changed: 1,
			},
			GenerationCounts: []status.NamedCount{
				{Name: "active", Count: 2},
				{Name: "superseded", Count: 1},
			},
			Queue: status.QueueSnapshot{
				Outstanding:          2,
				InFlight:             1,
				OldestOutstandingAge: 30 * time.Second,
			},
		},
	}
	asOf := time.Date(2026, 4, 12, 12, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

	report, err := status.LoadReport(context.Background(), reader, asOf, status.DefaultOptions())
	if err != nil {
		t.Fatalf("LoadReport() error = %v, want nil", err)
	}

	if got := reader.asOf; !got.Equal(asOf.UTC()) {
		t.Fatalf("LoadReport() reader asOf = %v, want %v", got, asOf.UTC())
	}
	if report.Health.State != "progressing" {
		t.Fatalf("LoadReport().Health.State = %q, want %q", report.Health.State, "progressing")
	}
	if report.AsOf != reader.snapshot.AsOf {
		t.Fatalf("LoadReport().AsOf = %v, want %v", report.AsOf, reader.snapshot.AsOf)
	}
}

func TestLoadReportRequiresReader(t *testing.T) {
	t.Parallel()

	_, err := status.LoadReport(
		context.Background(),
		nil,
		time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
		status.DefaultOptions(),
	)
	if err == nil {
		t.Fatal("LoadReport() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "reader is required") {
		t.Fatalf("LoadReport() error = %q, want mention of missing reader", err)
	}
}

func TestLoadReportPropagatesReaderErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	reader := &fakeReader{err: wantErr}

	_, err := status.LoadReport(
		context.Background(),
		reader,
		time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
		status.DefaultOptions(),
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("LoadReport() error = %v, want wrapped %v", err, wantErr)
	}
}

type fakeReader struct {
	snapshot status.RawSnapshot
	err      error
	asOf     time.Time
}

func (r *fakeReader) ReadStatusSnapshot(_ context.Context, asOf time.Time) (status.RawSnapshot, error) {
	r.asOf = asOf
	if r.err != nil {
		return status.RawSnapshot{}, r.err
	}

	return r.snapshot, nil
}

func TestBuildReportIncludesGenerationHistorySummary(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			ScopeActivity: status.ScopeActivitySnapshot{
				Active:  5,
				Changed: 2,
			},
			ScopeCounts: []status.NamedCount{
				{Name: "active", Count: 5},
			},
			GenerationCounts: []status.NamedCount{
				{Name: "active", Count: 2},
				{Name: "pending", Count: 1},
				{Name: "completed", Count: 4},
				{Name: "superseded", Count: 3},
				{Name: "failed", Count: 1},
				{Name: "inactive", Count: 2},
			},
		},
		status.DefaultOptions(),
	)

	if got, want := report.ScopeActivity.Unchanged, 3; got != want {
		t.Fatalf("BuildReport().ScopeActivity.Unchanged = %d, want %d", got, want)
	}
	if got, want := report.GenerationHistory.Active, 2; got != want {
		t.Fatalf("BuildReport().GenerationHistory.Active = %d, want %d", got, want)
	}
	if got, want := report.GenerationHistory.Pending, 1; got != want {
		t.Fatalf("BuildReport().GenerationHistory.Pending = %d, want %d", got, want)
	}
	if got, want := report.GenerationHistory.Completed, 4; got != want {
		t.Fatalf("BuildReport().GenerationHistory.Completed = %d, want %d", got, want)
	}
	if got, want := report.GenerationHistory.Superseded, 3; got != want {
		t.Fatalf("BuildReport().GenerationHistory.Superseded = %d, want %d", got, want)
	}
	if got, want := report.GenerationHistory.Failed, 1; got != want {
		t.Fatalf("BuildReport().GenerationHistory.Failed = %d, want %d", got, want)
	}
	if got, want := report.GenerationHistory.Other, 2; got != want {
		t.Fatalf("BuildReport().GenerationHistory.Other = %d, want %d", got, want)
	}
}

func TestRenderTextIncludesOperatorSummary(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			ScopeActivity: status.ScopeActivitySnapshot{
				Active:  2,
				Changed: 1,
			},
			ScopeCounts: []status.NamedCount{
				{Name: "active", Count: 3},
			},
			GenerationCounts: []status.NamedCount{
				{Name: "active", Count: 1},
				{Name: "completed", Count: 2},
				{Name: "superseded", Count: 1},
			},
			Queue: status.QueueSnapshot{
				Outstanding:          3,
				InFlight:             1,
				Retrying:             1,
				DeadLetter:           1,
				OldestOutstandingAge: 90 * time.Second,
			},
			LatestQueueFailure: &status.QueueFailureSnapshot{
				Stage:          "reducer",
				Domain:         "code_call_materialization",
				Status:         "retrying",
				FailureClass:   "graph_write_timeout",
				FailureMessage: "neo4j execute group timed out after 2s",
				FailureDetails: "phase=semantic label=Variable rows=500",
			},
			StageCounts: []status.StageStatusCount{
				{Stage: "projector", Status: "running", Count: 1},
				{Stage: "projector", Status: "retrying", Count: 1},
				{Stage: "reducer", Status: "pending", Count: 1},
			},
			DomainBacklogs: []status.DomainBacklog{
				{
					Domain:      "repository",
					Outstanding: 2,
					Retrying:    1,
					OldestAge:   90 * time.Second,
				},
			},
			QueueBlockages: []status.QueueBlockage{
				{
					Stage:          "reducer",
					Domain:         "semantic_entity_materialization",
					ConflictDomain: "code_graph",
					ConflictKey:    "scope-1:gen-1:code",
					Blocked:        2,
					OldestAge:      75 * time.Second,
				},
			},
		},
		status.DefaultOptions(),
	)

	rendered := status.RenderText(report)
	for _, want := range []string{
		"Health: degraded",
		"Queue: outstanding=3 in_flight=1 retrying=1 dead_letter=1 failed=0 oldest=1m30s",
		"Scope activity: active=2 changed=1 unchanged=1",
		"Scope statuses: active=3",
		"Generation history: active=1 pending=0 completed=2 superseded=1 failed=0 other=0",
		"Latest queue failure: stage=reducer domain=code_call_materialization status=retrying class=graph_write_timeout",
		"message=\"neo4j execute group timed out after 2s\" details=\"phase=semantic label=Variable rows=500\"",
		"Blocked queue work:",
		"reducer domain=semantic_entity_materialization conflict_domain=code_graph conflict_key=scope-1:gen-1:code blocked=2 oldest=1m15s",
		"projector pending=0 claimed=0 running=1 retrying=1 succeeded=0 dead_letter=0 failed=0",
		"repository outstanding=2 in_flight=0 retrying=1 dead_letter=0 failed=0 oldest=1m30s",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderText() missing %q in output:\n%s", want, rendered)
		}
	}
}

func TestRenderTextDoesNotRepeatTopLevelSummaries(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			ScopeActivity: status.ScopeActivitySnapshot{
				Active:  4,
				Changed: 2,
			},
			ScopeCounts: []status.NamedCount{
				{Name: "active", Count: 2},
			},
			GenerationCounts: []status.NamedCount{
				{Name: "completed", Count: 3},
				{Name: "superseded", Count: 1},
			},
			Queue: status.QueueSnapshot{
				Outstanding:          1,
				InFlight:             1,
				OldestOutstandingAge: 30 * time.Second,
			},
		},
		status.DefaultOptions(),
	)

	rendered := status.RenderText(report)
	for _, want := range []string{
		"Queue: outstanding=1 in_flight=1 retrying=0 dead_letter=0 failed=0 oldest=30s overdue_claims=0",
		"Scope activity: active=4 changed=2 unchanged=2",
		"Scope statuses: active=2",
		"Generation history: active=0 pending=0 completed=3 superseded=1 failed=0 other=0",
	} {
		if got := strings.Count(rendered, want); got != 1 {
			t.Fatalf("RenderText() occurrences of %q = %d, want 1\n%s", want, got, rendered)
		}
	}
}

func TestBuildReportAddsFlowSummaries(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			ScopeActivity: status.ScopeActivitySnapshot{
				Active:  2,
				Changed: 1,
			},
			ScopeCounts: []status.NamedCount{
				{Name: "active", Count: 3},
				{Name: "pending", Count: 1},
			},
			GenerationCounts: []status.NamedCount{
				{Name: "active", Count: 1},
				{Name: "completed", Count: 4},
				{Name: "superseded", Count: 1},
			},
			StageCounts: []status.StageStatusCount{
				{Stage: "projector", Status: "running", Count: 2},
				{Stage: "reducer", Status: "retrying", Count: 1},
			},
			DomainBacklogs: []status.DomainBacklog{
				{
					Domain:      "repository",
					Outstanding: 3,
					Retrying:    1,
					OldestAge:   2 * time.Minute,
				},
			},
		},
		status.DefaultOptions(),
	)

	if got := len(report.FlowSummaries); got != 3 {
		t.Fatalf("BuildReport().FlowSummaries len = %d, want 3", got)
	}
	if got := report.FlowSummaries[0]; got.Lane != "collector" || got.Source != "live" {
		t.Fatalf("BuildReport().FlowSummaries[0] = %+v, want collector/live", got)
	}
	if got := report.FlowSummaries[1]; got.Lane != "projector" || got.Source != "live" {
		t.Fatalf("BuildReport().FlowSummaries[1] = %+v, want projector/live", got)
	}
	if got := report.FlowSummaries[2]; got.Lane != "reducer" || got.Source != "live" {
		t.Fatalf("BuildReport().FlowSummaries[2] = %+v, want reducer/live", got)
	}
	if !strings.Contains(report.FlowSummaries[0].Progress, "scopes active=3 pending=1") {
		t.Fatalf("collector progress = %q, want scope totals", report.FlowSummaries[0].Progress)
	}
	if !strings.Contains(report.FlowSummaries[0].Backlog, "generations active=1 completed=4") {
		t.Fatalf("collector backlog = %q, want generation totals", report.FlowSummaries[0].Backlog)
	}
	if strings.Contains(report.FlowSummaries[0].Backlog, "not yet wired") {
		t.Fatalf("collector backlog = %q, want no placeholder wording", report.FlowSummaries[0].Backlog)
	}
	if !strings.Contains(report.FlowSummaries[1].Backlog, "queue") {
		t.Fatalf("projector backlog = %q, want queue pressure", report.FlowSummaries[1].Backlog)
	}
	if !strings.Contains(report.FlowSummaries[2].Backlog, "repository") {
		t.Fatalf("reducer backlog = %q, want top domain backlog", report.FlowSummaries[2].Backlog)
	}
}

func TestRenderJSONIncludesFlowSummaries(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			ScopeActivity: status.ScopeActivitySnapshot{
				Active:  3,
				Changed: 1,
			},
			ScopeCounts: []status.NamedCount{
				{Name: "active", Count: 3},
			},
			GenerationCounts: []status.NamedCount{
				{Name: "active", Count: 1},
				{Name: "completed", Count: 2},
				{Name: "superseded", Count: 1},
			},
			Queue: status.QueueSnapshot{
				Outstanding: 1,
			},
			LatestQueueFailure: &status.QueueFailureSnapshot{
				Stage:          "reducer",
				Domain:         "workload_materialization",
				Status:         "dead_letter",
				FailureClass:   "graph_write_timeout",
				FailureMessage: "neo4j execute timed out after 2s",
				FailureDetails: "phase=files rows=100 chunk=21/24",
			},
			StageCounts: []status.StageStatusCount{
				{Stage: "projector", Status: "pending", Count: 1},
			},
		},
		status.DefaultOptions(),
	)

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	if !strings.Contains(string(payload), "\"flow\"") {
		t.Fatalf("RenderJSON() = %s, want flow summaries", payload)
	}
	if !strings.Contains(string(payload), "\"generation_history\"") {
		t.Fatalf("RenderJSON() = %s, want generation history", payload)
	}
	if !strings.Contains(string(payload), "\"latest_failure\"") {
		t.Fatalf("RenderJSON() = %s, want latest queue failure", payload)
	}
	if !strings.Contains(string(payload), "\"queue_blockages\"") {
		t.Fatalf("RenderJSON() = %s, want queue blockage diagnostics", payload)
	}
	if !strings.Contains(string(payload), "\"failure_class\": \"graph_write_timeout\"") {
		t.Fatalf("RenderJSON() = %s, want latest failure class", payload)
	}
	if !strings.Contains(string(payload), "\"unchanged\": 2") {
		t.Fatalf("RenderJSON() = %s, want unchanged scope activity", payload)
	}
	if !strings.Contains(string(payload), "\"state\": \"progressing\"") {
		t.Fatalf("RenderJSON() = %s, want lower-case health state", payload)
	}
	if !strings.Contains(string(payload), "\"stage\": \"projector\"") {
		t.Fatalf("RenderJSON() = %s, want lower-case stage summary keys", payload)
	}
	if strings.Contains(string(payload), "\"State\"") {
		t.Fatalf("RenderJSON() = %s, want no exported-case health keys", payload)
	}
	if strings.Contains(string(payload), "\"Stage\"") {
		t.Fatalf("RenderJSON() = %s, want no exported-case stage keys", payload)
	}
}

func TestRenderJSONNormalizesCoordinatorSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 20, 15, 45, 0, 0, time.UTC)
	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: now,
			Coordinator: &status.CoordinatorSnapshot{
				CollectorInstances: []status.CollectorInstanceSummary{{
					InstanceID:     "collector-git-default",
					CollectorKind:  "git",
					Mode:           "continuous",
					Enabled:        true,
					Bootstrap:      true,
					ClaimsEnabled:  false,
					LastObservedAt: now,
					UpdatedAt:      now,
				}},
				RunStatusCounts:      []status.NamedCount{{Name: "collection_pending", Count: 1}},
				WorkItemStatusCounts: []status.NamedCount{{Name: "claimed", Count: 1}},
				ActiveClaims:         1,
			},
		},
		status.DefaultOptions(),
	)

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}

	body := string(payload)
	if !strings.Contains(body, "\"collector_instances\"") {
		t.Fatalf("RenderJSON() = %s, want coordinator collector instances", payload)
	}
	if !strings.Contains(body, "\"name\": \"collection_pending\"") {
		t.Fatalf("RenderJSON() = %s, want lower-case named count keys", payload)
	}
	if strings.Contains(body, "\"Name\"") {
		t.Fatalf("RenderJSON() = %s, want no exported-case named count keys", payload)
	}
	if strings.Contains(body, "0001-01-01T00:00:00Z") {
		t.Fatalf("RenderJSON() = %s, want zero deactivated_at omitted", payload)
	}
}

func TestRenderStatusIncludesRegistryCollectors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 13, 14, 30, 0, 0, time.UTC)
	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: now,
			RegistryCollectors: []status.RegistryCollectorSnapshot{{
				CollectorKind:        "oci_registry",
				ConfiguredInstances:  1,
				ActiveScopes:         2,
				CompletedGenerations: 4,
				LastCompletedAt:      now.Add(-2 * time.Minute),
				RetryableFailures:    1,
				FailureClassCounts: []status.NamedCount{{
					Name:  "registry_rate_limited",
					Count: 1,
				}},
			}},
		},
		status.DefaultOptions(),
	)

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	body := string(payload)
	for _, want := range []string{
		"\"registry_collectors\"",
		"\"collector_kind\": \"oci_registry\"",
		"\"active_scopes\": 2",
		"\"name\": \"registry_rate_limited\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("RenderJSON() = %s, want %q", payload, want)
		}
	}
	text := status.RenderText(report)
	if !strings.Contains(text, "Registry collectors:") ||
		!strings.Contains(text, "failure_classes=registry_rate_limited=1") {
		t.Fatalf("RenderText() = %s, want registry collector summary", text)
	}
}
