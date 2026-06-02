package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// ec2InternetExposureReadinessQueueDB proves the durable reducer queue gate keeps
// EC2 internet-exposure node-property updates waiting until the EC2 instance
// CloudResource nodes for the same scope generation have committed.
type ec2InternetExposureReadinessQueueDB struct {
	now          time.Time
	phaseReady   bool
	status       string
	attemptCount int
}

func (db *ec2InternetExposureReadinessQueueDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return fakeResult{}, nil
}

func (db *ec2InternetExposureReadinessQueueDB) QueryContext(_ context.Context, query string, _ ...any) (Rows, error) {
	if !strings.Contains(query, "FROM fact_work_items") || !strings.Contains(query, "FROM claimed") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	if !strings.Contains(query, "ec2_internet_exposure_materialization") {
		return nil, fmt.Errorf("claim query missing ec2 internet-exposure readiness gate:\n%s", query)
	}
	hasReadinessGate := strings.Contains(query, "graph_projection_phase_state AS aws_nodes") &&
		strings.Contains(query, "aws_nodes.keyspace = 'cloud_resource_uid'") &&
		strings.Contains(query, "aws_nodes.phase = 'canonical_nodes_committed'")
	if hasReadinessGate && !db.phaseReady {
		return &queueFakeRows{}, nil
	}

	status := strings.TrimSpace(db.status)
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "retrying" {
		return &queueFakeRows{}, nil
	}

	return &queueFakeRows{rows: [][]any{{
		"reducer-ec2-internet-exposure-1",
		"aws:111122223333:us-east-1:ec2",
		"gen-aws-1",
		string(reducer.DomainEC2InternetExposureMaterialization),
		db.attemptCount + 1,
		db.now.Add(-time.Minute),
		db.now.Add(-time.Minute),
		[]byte(`{"entity_key":"ec2_instance_node_materialization:aws:111122223333:us-east-1:ec2","reason":"ec2 instance posture observed","fact_id":"fact-ec2-posture-1","source_system":"aws"}`),
	}}}, nil
}

func TestReducerQueueClaimQueryGatesEC2InternetExposureOnInstanceNodeReadiness(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"ec2_internet_exposure_materialization",
		"aws_nodes.acceptance_unit_id = COALESCE(NULLIF(fact_work_items.payload->>'entity_key', ''), fact_work_items.scope_id)",
		"aws_nodes.keyspace = 'cloud_resource_uid'",
		"aws_nodes.phase = 'canonical_nodes_committed'",
	} {
		if !strings.Contains(claimReducerWorkQuery, want) {
			t.Fatalf("claim query missing EC2 internet-exposure readiness token %q:\n%s", want, claimReducerWorkQuery)
		}
	}
}

func TestReducerQueueBatchClaimQueryGatesEC2InternetExposureOnInstanceNodeReadiness(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"ec2_internet_exposure_materialization",
		"same_nodes.acceptance_unit_id = COALESCE(NULLIF(same.payload->>'entity_key', ''), same.scope_id)",
		"same_nodes.keyspace = 'cloud_resource_uid'",
		"same_nodes.phase = 'canonical_nodes_committed'",
	} {
		if !strings.Contains(claimReducerWorkBatchQuery, want) {
			t.Fatalf("batch claim query missing EC2 internet-exposure readiness token %q:\n%s", want, claimReducerWorkBatchQuery)
		}
	}
}

func TestReducerQueueClaimWaitsForEC2InternetExposureReadinessBehavior(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 2, 14, 0, 0, 0, time.UTC)
	db := &ec2InternetExposureReadinessQueueDB{
		now:        now,
		phaseReady: false,
		status:     "pending",
	}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	intent, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatalf("Claim() claimed %q before canonical readiness, want unclaimed waiting work", intent.IntentID)
	}

	db.phaseReady = true
	intent, claimed, err = queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() after readiness error = %v", err)
	}
	if !claimed {
		t.Fatal("Claim() after readiness claimed = false, want true")
	}
	if got, want := intent.Domain, reducer.DomainEC2InternetExposureMaterialization; got != want {
		t.Fatalf("claimed domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKeys, []string{"ec2_instance_node_materialization:aws:111122223333:us-east-1:ec2"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("claimed entity keys = %v, want %v", got, want)
	}
}

func TestReducerConflictBlockageReportsEC2InternetExposureReadiness(t *testing.T) {
	t.Parallel()

	if !strings.Contains(reducerConflictBlockageQuery, "ec2_internet_exposure_materialization") {
		t.Fatalf("blockage query missing ec2 internet-exposure readiness domain:\n%s", reducerConflictBlockageQuery)
	}
}
