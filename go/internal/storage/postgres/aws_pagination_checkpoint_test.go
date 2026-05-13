package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/checkpoint"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestAWSPaginationCheckpointSchemaSQL(t *testing.T) {
	t.Parallel()

	sql := AWSPaginationCheckpointSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS aws_scan_pagination_checkpoints",
		"collector_instance_id TEXT NOT NULL",
		"account_id TEXT NOT NULL",
		"region TEXT NOT NULL",
		"service_kind TEXT NOT NULL",
		"resource_parent TEXT NOT NULL DEFAULT ''",
		"operation TEXT NOT NULL",
		"generation_id TEXT NOT NULL",
		"fencing_token BIGINT NOT NULL",
		"page_token TEXT NOT NULL DEFAULT ''",
		"page_number INTEGER NOT NULL DEFAULT 0",
		"payload JSONB NOT NULL DEFAULT '{}'::jsonb",
		"PRIMARY KEY (collector_instance_id, account_id, region, service_kind, resource_parent, operation)",
		"aws_scan_pagination_checkpoints_scope_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("AWSPaginationCheckpointSchemaSQL() missing %q:\n%s", want, sql)
		}
	}
}

func TestAWSPaginationCheckpointStoreSaveRejectsOlderFence(t *testing.T) {
	t.Parallel()

	db := &awsCheckpointStoreTestDB{execResults: []sql.Result{awsCheckpointRowsResult{rowsAffected: 0}}}
	store := NewAWSPaginationCheckpointStore(db)
	err := store.Save(context.Background(), checkpoint.Checkpoint{
		Key:        testAWSCheckpointKey(),
		PageToken:  "token-1",
		PageNumber: 2,
		UpdatedAt:  time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, checkpoint.ErrStaleFence) {
		t.Fatalf("Save() error = %v, want ErrStaleFence", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	query := db.execs[0].query
	for _, want := range []string{
		"ON CONFLICT (collector_instance_id, account_id, region, service_kind, resource_parent, operation) DO UPDATE",
		"WHERE aws_scan_pagination_checkpoints.fencing_token <= EXCLUDED.fencing_token",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("Save() query missing %q:\n%s", want, query)
		}
	}
}

func TestAWSPaginationCheckpointStoreExpireStaleScopesByGeneration(t *testing.T) {
	t.Parallel()

	db := &awsCheckpointStoreTestDB{execResults: []sql.Result{awsCheckpointRowsResult{rowsAffected: 3}}}
	store := NewAWSPaginationCheckpointStore(db)
	expired, err := store.ExpireStale(context.Background(), testAWSCheckpointScope())
	if err != nil {
		t.Fatalf("ExpireStale() error = %v, want nil", err)
	}
	if expired != 3 {
		t.Fatalf("ExpireStale() = %d, want 3", expired)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"DELETE FROM aws_scan_pagination_checkpoints",
		"collector_instance_id = $1",
		"account_id = $2",
		"region = $3",
		"service_kind = $4",
		"generation_id <> $5",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("ExpireStale() query missing %q:\n%s", want, query)
		}
	}
}

func TestAWSPaginationCheckpointStoreRecordsStableEventKinds(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	db := &awsCheckpointStoreTestDB{
		execResults: []sql.Result{
			awsCheckpointRowsResult{rowsAffected: 1},
			awsCheckpointRowsResult{rowsAffected: 1},
		},
	}
	store := NewAWSPaginationCheckpointStore(db)
	store.Instruments = instruments

	if err := store.Complete(context.Background(), testAWSCheckpointKey()); err != nil {
		t.Fatalf("Complete() error = %v, want nil", err)
	}
	if _, err := store.ExpireStale(context.Background(), testAWSCheckpointScope()); err != nil {
		t.Fatalf("ExpireStale() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	assertAWSCheckpointCounter(t, rm, map[string]string{
		telemetry.MetricDimensionService:   awscloud.ServiceECR,
		telemetry.MetricDimensionAccount:   "123456789012",
		telemetry.MetricDimensionRegion:    "us-east-1",
		telemetry.MetricDimensionOperation: "DescribeImages",
		telemetry.MetricDimensionEventKind: "complete",
		telemetry.MetricDimensionResult:    "success",
	})
	assertAWSCheckpointCounter(t, rm, map[string]string{
		telemetry.MetricDimensionService:   awscloud.ServiceECR,
		telemetry.MetricDimensionAccount:   "123456789012",
		telemetry.MetricDimensionRegion:    "us-east-1",
		telemetry.MetricDimensionOperation: "all",
		telemetry.MetricDimensionEventKind: "expiry",
		telemetry.MetricDimensionResult:    "success",
	})
}

func TestAWSPaginationCheckpointStoreRecordEventAllowsPartialInstruments(t *testing.T) {
	t.Parallel()

	store := AWSPaginationCheckpointStore{Instruments: &telemetry.Instruments{}}
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("recordEvent() panic = %v, want nil", recovered)
		}
	}()
	store.recordEvent(context.Background(), testAWSCheckpointScope(), "DescribeImages", "save", "success")
}

func testAWSCheckpointKey() checkpoint.Key {
	return checkpoint.Key{
		Scope: checkpoint.Scope{
			CollectorInstanceID: "aws-prod",
			AccountID:           "123456789012",
			Region:              "us-east-1",
			ServiceKind:         awscloud.ServiceECR,
			GenerationID:        "generation-1",
			FencingToken:        4,
		},
		ResourceParent: "arn:aws:ecr:us-east-1:123456789012:repository/team/api",
		Operation:      "DescribeImages",
	}
}

func testAWSCheckpointScope() checkpoint.Scope {
	return testAWSCheckpointKey().Scope
}

type awsCheckpointStoreTestDB struct {
	execs       []awsCheckpointExec
	execResults []sql.Result
}

func (db *awsCheckpointStoreTestDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	db.execs = append(db.execs, awsCheckpointExec{query: query, args: args})
	if len(db.execResults) == 0 {
		return awsCheckpointRowsResult{rowsAffected: 1}, nil
	}
	result := db.execResults[0]
	db.execResults = db.execResults[1:]
	return result, nil
}

func (db *awsCheckpointStoreTestDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, errors.New("unexpected QueryContext")
}

type awsCheckpointExec struct {
	query string
	args  []any
}

type awsCheckpointRowsResult struct {
	rowsAffected int64
}

func (r awsCheckpointRowsResult) LastInsertId() (int64, error) { return 0, nil }
func (r awsCheckpointRowsResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }

func assertAWSCheckpointCounter(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	wantAttrs map[string]string,
) {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != "eshu_dp_aws_pagination_checkpoint_events_total" {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want metricdata.Sum[int64]", metricRecord.Data)
			}
			for _, dp := range sum.DataPoints {
				if awsCheckpointAttrsMatch(dp.Attributes.ToSlice(), wantAttrs) && dp.Value == 1 {
					return
				}
			}
		}
	}
	t.Fatalf("checkpoint counter with attrs %v not found", wantAttrs)
}

func awsCheckpointAttrsMatch(actual []attribute.KeyValue, want map[string]string) bool {
	if len(actual) != len(want) {
		return false
	}
	for _, attr := range actual {
		if want[string(attr.Key)] != attr.Value.AsString() {
			return false
		}
	}
	return true
}
