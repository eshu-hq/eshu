// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9146 real coordinator Service and PostgreSQL inbox/admission proof for a 100-delivery claim group, late arrival, independent coordinators, and partial ACK recovery
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/ghactionsruntime"
	cicdrun "github.com/eshu-hq/eshu/go/internal/coordinator/cicd/run"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	actionsstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/webhook/actions"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func openCICDBurstProof(t *testing.T) (*sql.DB, Service, *actionsstore.Store, *time.Time) {
	t.Helper()
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN is not set")
	}
	database := postgresproof.OpenIsolatedSchema(t, dsn, "cicd_service_burst", func(ctx context.Context, db *sql.DB) error {
		return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: db})
	})
	store := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: database})
	now := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	instance := testServiceCICDRunInstance(now)
	desired := workflow.DesiredCollectorInstance{InstanceID: instance.InstanceID, CollectorKind: instance.CollectorKind, Mode: instance.Mode, Enabled: true, ClaimsEnabled: true, Configuration: instance.Configuration}
	if err := store.ReconcileCollectorInstances(t.Context(), now, []workflow.DesiredCollectorInstance{desired}); err != nil {
		t.Fatal(err)
	}
	inbox := actionsstore.NewStore(postgres.SQLDB{DB: database})
	service := Service{Config: Config{DeploymentMode: deploymentModeActive, ClaimsEnabled: true}, Store: store, CICDRunPlanner: cicdrun.WorkPlanner{}, CICDRunActionsTriggers: inbox, Clock: func() time.Time { return now }}
	return database, service, inbox, &now
}

func seedCICDBurst(t *testing.T, inbox *actionsstore.Store, now time.Time, start, count int) {
	t.Helper()
	for i := start; i < start+count; i++ {
		_, err := inbox.Store(t.Context(), webhook.ActionsTrigger{DeliveryID: fmt.Sprintf("burst-%03d", i), EventKind: "workflow_job", Action: "completed", RepositoryID: "42", Repository: "eshu-hq/eshu", RunID: "123", JobID: fmt.Sprint(i + 1)}, now)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func assertCICDBurstCounts(t *testing.T, db *sql.DB, work, handed, queued int) {
	t.Helper()
	var actualWork, actualHanded, actualQueued int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM workflow_work_items`).Scan(&actualWork); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FILTER(WHERE status='handed_off'),count(*) FILTER(WHERE status IN ('queued','claimed')) FROM cicd_actions_deliveries`).Scan(&actualHanded, &actualQueued); err != nil {
		t.Fatal(err)
	}
	if actualWork != work || actualHanded != handed || actualQueued != queued {
		t.Fatalf("work=%d handed_off=%d pending=%d; want %d/%d/%d", actualWork, actualHanded, actualQueued, work, handed, queued)
	}
	t.Logf("durable work=%d handed_off=%d pending=%d", actualWork, actualHanded, actualQueued)
}

func TestCICDServiceClaimGroupBurstAndLateArrivalLive(t *testing.T) {
	db, service, inbox, now := openCICDBurstProof(t)
	seedCICDBurst(t, inbox, *now, 0, 100)
	started := time.Now()
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Logf("100-delivery handoff wall=%s", time.Since(started))
	assertCICDBurstCounts(t, db, 1, 100, 0)
	proveCICDBurstAppFetchCount(t, db)
	// This delivery did not belong to the first claim. It must wait for another
	// snapshot rather than attach to work that could already be collecting.
	seedCICDBurst(t, inbox, *now, 100, 1)
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCICDBurstCounts(t, db, 1, 100, 1)
	if _, err := db.ExecContext(t.Context(), `UPDATE workflow_runs SET status='complete'`); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(31 * time.Second)
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCICDBurstCounts(t, db, 2, 101, 0)
}

func TestCICDServiceConcurrentCoordinatorsShareClaimGroupLive(t *testing.T) {
	db, service, inbox, now := openCICDBurstProof(t)
	seedCICDBurst(t, inbox, *now, 0, 100)
	var group sync.WaitGroup
	errors := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		group.Go(func() { <-start; errors <- service.runCICDRunWebhookHandoff(t.Context()) })
	}
	close(start)
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	// SKIP LOCKED may split a concurrent claim into distinct causal groups.
	// Drain any second group after completing the first generation.
	for range 3 {
		if _, err := db.ExecContext(t.Context(), `UPDATE workflow_runs SET status='complete'`); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(31 * time.Second)
		if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	var work, groups int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM workflow_work_items`).Scan(&work); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT count(DISTINCT handoff_key) FROM cicd_actions_deliveries`).Scan(&groups); err != nil {
		t.Fatal(err)
	}
	if work != groups || groups < 1 || groups > 2 {
		t.Fatalf("work=%d claim groups=%d, want one work per group (1..2)", work, groups)
	}
	assertCICDBurstCounts(t, db, groups, 100, 0)
}

type cicdPartialACKInbox struct {
	*actionsstore.Store
	calls int
}

func (s *cicdPartialACKInbox) Ack(ctx context.Context, claim webhook.StoredActionsTrigger, at time.Time) (bool, error) {
	s.calls++
	if s.calls == 2 {
		return false, fmt.Errorf("simulated second ACK crash")
	}
	return s.Store.Ack(ctx, claim, at)
}

func TestCICDServiceClaimGroupPartialACKCrashRecoveryLive(t *testing.T) {
	db, service, inbox, now := openCICDBurstProof(t)
	seedCICDBurst(t, inbox, *now, 0, 100)
	service.CICDRunActionsTriggers = &cicdPartialACKInbox{Store: inbox}
	if err := service.runCICDRunWebhookHandoff(t.Context()); err == nil {
		t.Fatal("want simulated partial ACK failure")
	}
	assertCICDBurstCounts(t, db, 1, 99, 1)
	if _, err := db.ExecContext(t.Context(), `UPDATE workflow_runs SET status='complete'`); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Minute)
	service.CICDRunActionsTriggers = inbox
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCICDBurstCounts(t, db, 1, 100, 0)
}

func TestCICDServiceGroupRecoversPartiallyAdmittedScopesLive(t *testing.T) {
	db, service, inbox, now := openCICDBurstProof(t)
	instances, err := service.Store.ListCollectorInstances(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	instance := instances[0]
	var configuration struct {
		Targets []map[string]any `json:"targets"`
	}
	if err := json.Unmarshal([]byte(instance.Configuration), &configuration); err != nil {
		t.Fatal(err)
	}
	second := make(map[string]any, len(configuration.Targets[0]))
	for key, value := range configuration.Targets[0] {
		second[key] = value
	}
	second["scope_id"] = "github-actions://eshu-hq/eshu-secondary"
	configuration.Targets = append(configuration.Targets, second)
	encoded, err := json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	instance.Configuration = string(encoded)
	desired := workflow.DesiredCollectorInstance{InstanceID: instance.InstanceID, CollectorKind: instance.CollectorKind, Mode: instance.Mode, Enabled: true, ClaimsEnabled: true, Configuration: instance.Configuration}
	if err := service.Store.ReconcileCollectorInstances(t.Context(), *now, []workflow.DesiredCollectorInstance{desired}); err != nil {
		t.Fatal(err)
	}
	run, items, err := (cicdrun.WorkPlanner{}).PlanCICDRunWork(t.Context(), cicdrun.PlanRequest{Instance: instance, ObservedAt: *now, PlanKey: "unrelated-open-scope", TriggerKind: workflow.TriggerKindWebhook, ScopeIDs: []string{second["scope_id"].(string)}})
	if err != nil {
		t.Fatal(err)
	}
	if admission, err := service.Store.CreateRunWithWorkItemsIfNoOpenTargets(t.Context(), run, items); err != nil || admission.InsertedWorkItems != 1 {
		t.Fatalf("unrelated admission=%+v err=%v", admission, err)
	}
	seedCICDBurst(t, inbox, *now, 0, 100)
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCICDBurstCounts(t, db, 2, 0, 100)
	// The admitted scope and the unrelated blocker finish before retry. The
	// missing scope must use its independent run, despite the first being terminal.
	if _, err := db.ExecContext(t.Context(), `UPDATE workflow_runs SET status='complete'`); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(31 * time.Second)
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCICDBurstCounts(t, db, 3, 100, 0)
}

func TestCICDServiceGroupExactRecoveryRejectsOtherTenantLive(t *testing.T) {
	db, service, inbox, now := openCICDBurstProof(t)
	seedCICDBurst(t, inbox, *now, 0, 1)
	if err := service.runCICDRunWebhookHandoff(t.Context()); err != nil {
		t.Fatal(err)
	}
	var item workflow.WorkItem
	if err := db.QueryRowContext(t.Context(), `SELECT work_item_id,run_id,collector_instance_id,scope_id,generation_id,tenant_id,workspace_id,subject_class,policy_revision_hash,acceptance_unit_id FROM workflow_work_items`).Scan(&item.WorkItemID, &item.RunID, &item.CollectorInstanceID, &item.ScopeID, &item.GenerationID, &item.TenantID, &item.WorkspaceID, &item.SubjectClass, &item.PolicyRevisionHash, &item.AcceptanceUnitID); err != nil {
		t.Fatal(err)
	}
	reader := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: db})
	if found, err := reader.HasExactCICDWebhookWork(t.Context(), item); err != nil || !found {
		t.Fatalf("original exact=%v err=%v", found, err)
	}
	for _, field := range []string{"tenant", "workspace", "subject", "policy", "acceptance"} {
		t.Run(field, func(t *testing.T) {
			other := item
			switch field {
			case "tenant":
				other.TenantID = "other-tenant"
			case "workspace":
				other.WorkspaceID = "other-workspace"
			case "subject":
				other.SubjectClass = "other-subject"
			case "policy":
				other.PolicyRevisionHash = "other-policy"
			case "acceptance":
				other.AcceptanceUnitID = "other-unit"
			}
			if found, err := reader.HasExactCICDWebhookWork(t.Context(), other); err != nil || found {
				t.Fatalf("other authorization boundary exact=%v err=%v, want false", found, err)
			}
		})
	}
}

// proveCICDBurstAppFetchCount drives the real App source for the group's
// admitted work against local TLS; source facts remain in memory.
func proveCICDBurstAppFetchCount(t *testing.T, db *sql.DB) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	var mints, reads atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/installations/123/access_tokens" {
			mints.Add(1)
			_, _ = w.Write([]byte(`{"token":"fixture-installation-token"}`))
			return
		}
		reads.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-installation-token" {
			t.Error("provider used wrong fixture credential")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/repos/eshu-hq/eshu/actions/runs":
			runs := make([]map[string]any, 10)
			for i := range runs {
				runs[i] = map[string]any{"id": 1000 + i, "workflow_id": 42, "run_attempt": 1, "run_number": i + 1, "name": "CI", "event": "push", "status": "completed", "conclusion": "success", "head_branch": "main", "head_sha": strings.Repeat("a", 40), "run_started_at": "2026-10-26T03:59:00Z", "updated_at": "2026-10-26T04:00:00Z", "html_url": fmt.Sprintf("https://github.com/eshu-hq/eshu/actions/runs/%d", 1000+i), "repository": map[string]any{"full_name": "eshu-hq/eshu", "html_url": "https://github.com/eshu-hq/eshu"}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total_count": 10, "workflow_runs": runs})
		case strings.HasSuffix(r.URL.Path, "/jobs"):
			_, _ = w.Write([]byte(`{"total_count":0,"jobs":[]}`))
		case strings.HasSuffix(r.URL.Path, "/artifacts"):
			_, _ = w.Write([]byte(`{"total_count":0,"artifacts":[]}`))
		case r.URL.Path == "/repos/eshu-hq/eshu/deployments":
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Errorf("unexpected fixture provider path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	var item workflow.WorkItem
	if err := db.QueryRowContext(t.Context(), `SELECT work_item_id,run_id,collector_kind,collector_instance_id,scope_id,generation_id FROM workflow_work_items`).Scan(&item.WorkItemID, &item.RunID, &item.CollectorKind, &item.CollectorInstanceID, &item.ScopeID, &item.GenerationID); err != nil {
		t.Fatal(err)
	}
	item.CurrentFencingToken = 1
	source, err := ghactionsruntime.NewClaimedSource(ghactionsruntime.SourceConfig{CollectorInstanceID: item.CollectorInstanceID, Client: ghactionsruntime.GitHubClient{HTTPClient: server.Client()}, Targets: []ghactionsruntime.TargetConfig{{ScopeID: item.ScopeID, Repository: "eshu-hq/eshu", AuthMode: ghactionsruntime.AuthModeGitHubApp, GitHubAppID: "456", GitHubAppInstallationID: "123", GitHubAppPrivateKey: string(keyPEM), AppHTTPClient: server.Client(), AllowedRepositories: []string{"eshu-hq/eshu"}, APIBaseURL: server.URL, MaxJobs: 20, MaxArtifacts: 10}}})
	if err != nil {
		t.Fatal(err)
	}
	generation, ok, err := source.NextClaimed(t.Context(), item)
	if err != nil || !ok {
		t.Fatalf("App source ok=%v err=%v", ok, err)
	}
	runFacts, totalFacts := 0, 0
	for fact := range generation.Facts {
		totalFacts++
		if fact.FactKind == "ci.run" {
			runFacts++
		}
	}
	if generation.FactStreamErr != nil {
		if err := generation.FactStreamErr(); err != nil {
			t.Fatal(err)
		}
	}
	if mints.Load() != 1 || reads.Load() != 22 || runFacts != 10 {
		t.Fatalf("mints=%d reads=%d run facts=%d, want 1/22/10", mints.Load(), reads.Load(), runFacts)
	}
	t.Logf("100 grouped deliveries -> 1 admitted generation, App mints=%d provider GETs=%d (21 Actions +1 deployments), ci.run=%d total facts=%d; in-memory source only", mints.Load(), reads.Load(), runFacts, totalFacts)
}
