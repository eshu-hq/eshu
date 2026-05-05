package runtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestNewStatusMetricsHandlerRequiresInputs(t *testing.T) {
	t.Parallel()

	if _, err := NewStatusMetricsHandler("   ", &fakeStatusReader{}); err == nil {
		t.Fatal("NewStatusMetricsHandler() error = nil, want non-nil for blank service")
	}

	if _, err := NewStatusMetricsHandler("collector-git", nil); err == nil {
		t.Fatal("NewStatusMetricsHandler() error = nil, want non-nil for nil reader")
	}
}

func TestStatusMetricsHandlerServesRuntimeMetrics(t *testing.T) {
	handler, err := NewStatusMetricsHandler("collector-git", &fakeStatusReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			ScopeActivity: statuspkg.ScopeActivitySnapshot{
				Active:    7,
				Changed:   3,
				Unchanged: 4,
			},
			Queue: statuspkg.QueueSnapshot{
				Total:                4,
				Outstanding:          2,
				Pending:              1,
				InFlight:             1,
				Retrying:             1,
				Succeeded:            2,
				Failed:               0,
				DeadLetter:           1,
				OldestOutstandingAge: 45 * time.Second,
				OverdueClaims:        0,
			},
			GenerationCounts: []statuspkg.NamedCount{{Name: "completed", Count: 3}},
			RetryPolicies: []statuspkg.RetryPolicySummary{
				{Stage: "projector", MaxAttempts: 4, RetryDelay: 42 * time.Second},
			},
			StageCounts:    []statuspkg.StageStatusCount{{Stage: "projector", Status: "running", Count: 1}},
			DomainBacklogs: []statuspkg.DomainBacklog{{Domain: "repository", Outstanding: 2, OldestAge: 30 * time.Second}},
		},
	})
	if err != nil {
		t.Fatalf("NewStatusMetricsHandler() error = %v, want nil", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("GET /metrics status = %d, want %d", got, want)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`eshu_runtime_info{service_name="collector-git",service_namespace="eshu",version="dev"} 1`,
		`eshu_runtime_scope_active{service_name="collector-git"} 7`,
		`eshu_runtime_scope_changed{service_name="collector-git"} 3`,
		`eshu_runtime_scope_unchanged{service_name="collector-git"} 4`,
		`eshu_runtime_refresh_skipped_total{service_name="collector-git"} 0`,
		`eshu_runtime_queue_outstanding{service_name="collector-git"} 2`,
		`eshu_runtime_queue_dead_letter{service_name="collector-git"} 1`,
		`eshu_runtime_queue_oldest_outstanding_age_seconds{service_name="collector-git"} 45`,
		`eshu_runtime_health_state{service_name="collector-git",state="degraded"} 1`,
		`eshu_runtime_retry_policy_max_attempts{service_name="collector-git",stage="projector"} 4`,
		`eshu_runtime_retry_policy_retry_delay_seconds{service_name="collector-git",stage="projector"} 42`,
		`eshu_runtime_stage_items{service_name="collector-git",stage="projector",status="running"} 1`,
		`eshu_runtime_domain_outstanding{domain="repository",service_name="collector-git"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET /metrics body missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestStatusMetricsHandlerReportsQueueFailureAndOverdueClaims(t *testing.T) {
	handler, err := NewStatusMetricsHandler("reducer", &fakeStatusReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			Queue: statuspkg.QueueSnapshot{
				Outstanding:          3,
				InFlight:             0,
				DeadLetter:           2,
				OverdueClaims:        1,
				OldestOutstandingAge: 45 * time.Second,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewStatusMetricsHandler() error = %v, want nil", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("GET /metrics status = %d, want %d", got, want)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`eshu_runtime_queue_dead_letter{service_name="reducer"} 2`,
		`eshu_runtime_queue_overdue_claims{service_name="reducer"} 1`,
		`eshu_runtime_health_state{service_name="reducer",state="stalled"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET /metrics body missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestStatusMetricsHandlerReportsSkippedRefreshes(t *testing.T) {
	telemetry.ResetSkippedRefreshCountForTesting()
	t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

	telemetry.RecordSkippedRefresh()
	telemetry.RecordSkippedRefresh()

	handler, err := NewStatusMetricsHandler("collector-git", &fakeStatusReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("NewStatusMetricsHandler() error = %v, want nil", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("GET /metrics status = %d, want %d", got, want)
	}
	if body := recorder.Body.String(); !strings.Contains(body, `eshu_runtime_refresh_skipped_total{service_name="collector-git"} 2`) {
		t.Fatalf("GET /metrics body missing skipped refresh metric\nbody:\n%s", body)
	}
}

func TestStatusMetricsHandlerSupportsHeadWithoutBody(t *testing.T) {
	t.Parallel()

	handler, err := NewStatusMetricsHandler("collector-git", &fakeStatusReader{
		snapshot: statuspkg.RawSnapshot{AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("NewStatusMetricsHandler() error = %v, want nil", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "/metrics", nil)
	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("HEAD /metrics status = %d, want %d", got, want)
	}
	if got := recorder.Body.String(); got != "" {
		t.Fatalf("HEAD /metrics body = %q, want empty", got)
	}
}

func TestStatusMetricsHandlerIncludesCoordinatorSnapshotMetrics(t *testing.T) {
	handler, err := NewStatusMetricsHandler("workflow-coordinator", &fakeStatusReader{
		snapshot: statuspkg.RawSnapshot{
			AsOf: time.Date(2026, 4, 20, 16, 0, 0, 0, time.UTC),
			Coordinator: &statuspkg.CoordinatorSnapshot{
				CollectorInstances: []statuspkg.CollectorInstanceSummary{
					{
						InstanceID:    "collector-git-default",
						CollectorKind: "git",
						Mode:          "continuous",
						Enabled:       true,
						Bootstrap:     true,
						ClaimsEnabled: false,
					},
					{
						InstanceID:     "collector-git-legacy",
						CollectorKind:  "git",
						Mode:           "continuous",
						Enabled:        true,
						Bootstrap:      false,
						ClaimsEnabled:  false,
						DeactivatedAt:  time.Date(2026, 4, 20, 15, 45, 0, 0, time.UTC),
						LastObservedAt: time.Date(2026, 4, 20, 15, 30, 0, 0, time.UTC),
					},
					{
						InstanceID:    "aws-prod",
						CollectorKind: "aws-cloud",
						Mode:          "scheduled",
						Enabled:       true,
						Bootstrap:     false,
						ClaimsEnabled: true,
					},
				},
				RunStatusCounts: []statuspkg.NamedCount{
					{Name: "collection_pending", Count: 2},
					{Name: "complete", Count: 1},
				},
				WorkItemStatusCounts: []statuspkg.NamedCount{
					{Name: "pending", Count: 3},
					{Name: "claimed", Count: 1},
				},
				CompletenessCounts: []statuspkg.NamedCount{
					{Name: "collection_active", Count: 1},
					{Name: "reducer_converging", Count: 2},
				},
				ActiveClaims:     4,
				OverdueClaims:    1,
				OldestPendingAge: 95 * time.Second,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewStatusMetricsHandler() error = %v, want nil", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("GET /metrics status = %d, want %d", got, want)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`eshu_runtime_coordinator_active_claims{service_name="workflow-coordinator"} 4`,
		`eshu_runtime_coordinator_overdue_claims{service_name="workflow-coordinator"} 1`,
		`eshu_runtime_coordinator_oldest_pending_age_seconds{service_name="workflow-coordinator"} 95`,
		`eshu_runtime_coordinator_collector_instances_total{service_name="workflow-coordinator"} 3`,
		`eshu_runtime_coordinator_collector_instances{bootstrap="true",claims_enabled="false",collector_kind="git",enabled="true",lifecycle="active",mode="continuous",service_name="workflow-coordinator"} 1`,
		`eshu_runtime_coordinator_collector_instances{bootstrap="false",claims_enabled="false",collector_kind="git",enabled="true",lifecycle="deactivated",mode="continuous",service_name="workflow-coordinator"} 1`,
		`eshu_runtime_coordinator_collector_instances{bootstrap="false",claims_enabled="true",collector_kind="aws-cloud",enabled="true",lifecycle="active",mode="scheduled",service_name="workflow-coordinator"} 1`,
		`eshu_runtime_coordinator_run_status{service_name="workflow-coordinator",status="collection_pending"} 2`,
		`eshu_runtime_coordinator_run_status{service_name="workflow-coordinator",status="complete"} 1`,
		`eshu_runtime_coordinator_work_item_status{service_name="workflow-coordinator",status="pending"} 3`,
		`eshu_runtime_coordinator_work_item_status{service_name="workflow-coordinator",status="claimed"} 1`,
		`eshu_runtime_coordinator_completeness{service_name="workflow-coordinator",state="collection_active"} 1`,
		`eshu_runtime_coordinator_completeness{service_name="workflow-coordinator",state="reducer_converging"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("GET /metrics body missing %q\nbody:\n%s", want, body)
		}
	}
}
