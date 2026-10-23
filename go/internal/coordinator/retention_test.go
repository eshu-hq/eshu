// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/aws/scheduled"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeWorkItemRetention struct {
	asOf     []time.Time
	policies []workflow.WorkItemRetentionPolicy
	result   workflow.WorkItemRetentionResult
	err      error
	size     workflow.WorkItemTableSize
	sizeErr  error
}

func (f *fakeWorkItemRetention) PruneWorkItems(
	_ context.Context,
	asOf time.Time,
	policy workflow.WorkItemRetentionPolicy,
) (workflow.WorkItemRetentionResult, error) {
	f.asOf = append(f.asOf, asOf)
	f.policies = append(f.policies, policy)
	return f.result, f.err
}

func (f *fakeWorkItemRetention) WorkItemTableSize(context.Context) (workflow.WorkItemTableSize, error) {
	return f.size, f.sizeErr
}

type fakeRetentionMetrics struct {
	fakeMetrics
	retention []WorkItemRetentionObservation
}

func (f *fakeRetentionMetrics) RecordWorkItemRetention(_ context.Context, observation WorkItemRetentionObservation) {
	f.retention = append(f.retention, observation)
}

func retentionEnv(overrides map[string]string) func(string) string {
	return func(key string) string {
		if value, ok := overrides[key]; ok {
			return value
		}
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "dark"
		default:
			return ""
		}
	}
}

func TestLoadConfigWorkItemRetentionIsOnByDefault(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig(retentionEnv(nil))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	want := WorkItemRetentionConfig{
		Enabled:  true,
		Interval: 5 * time.Minute,
		Policy:   workflow.DefaultWorkItemRetentionPolicy(),
	}
	if cfg.WorkItemRetention != want {
		t.Fatalf("WorkItemRetention = %+v, want %+v (on by default at 720h)", cfg.WorkItemRetention, want)
	}
	if got := cfg.WorkItemRetention.Policy.Age; got != 720*time.Hour {
		t.Fatalf("default retention age = %s, want 720h", got)
	}
}

func TestLoadConfigWorkItemRetentionExplicitZeroAgeTurnsItOff(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"0", "0s", "0h"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			cfg, err := LoadConfig(retentionEnv(map[string]string{
				"ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE": value,
			}))
			if err != nil {
				t.Fatalf("LoadConfig(age %s) error = %v, want nil", value, err)
			}
			if cfg.WorkItemRetention.Enabled {
				t.Fatalf("WorkItemRetention.Enabled = true for age %s, want off", value)
			}
		})
	}
}

func TestLoadConfigWorkItemRetentionParsesEnvironment(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig(retentionEnv(map[string]string{
		"ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE":        "168h",
		"ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_BATCH_SIZE": "250",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	want := WorkItemRetentionConfig{
		Enabled:  true,
		Interval: 5 * time.Minute,
		Policy: workflow.WorkItemRetentionPolicy{
			Age:         168 * time.Hour,
			BatchSize:   250,
			MaxBatches:  100,
			LockTimeout: 2 * time.Second,
		},
	}
	if cfg.WorkItemRetention != want {
		t.Fatalf("WorkItemRetention = %+v, want %+v", cfg.WorkItemRetention, want)
	}
}

func TestLoadConfigWorkItemRetentionRejectsUnsafeSettings(t *testing.T) {
	t.Parallel()

	for name, env := range map[string]map[string]string{
		"age under a day":           {"ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE": "1h"},
		"age of one day":            {"ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE": "24h"},
		"age just under two days":   {"ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE": "47h59m"},
		"negative age":              {"ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE": "-720h"},
		"unparsable age":            {"ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE": "a month"},
		"oversized batch":           {"ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_BATCH_SIZE": "50000"},
		"zero batch while disabled": {"ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_BATCH_SIZE": "0"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := LoadConfig(retentionEnv(env)); err == nil {
				t.Fatal("LoadConfig() error = nil, want a retention config error")
			}
		})
	}
}

func activeRetentionService(now time.Time, retention WorkItemRetentionConfig) (Service, *fakeStore) {
	store := &fakeStore{instances: []workflow.CollectorInstance{testServiceAWSScheduledInstance(now)}}
	return Service{
		Config: Config{
			DeploymentMode:           deploymentModeActive,
			ClaimsEnabled:            true,
			ReconcileInterval:        time.Hour,
			ReapInterval:             time.Hour,
			ClaimLeaseTTL:            time.Minute,
			HeartbeatInterval:        20 * time.Second,
			ExpiredClaimLimit:        10,
			ExpiredClaimRequeueDelay: 5 * time.Second,
			WorkItemRetention:        retention,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    "collector-aws",
				CollectorKind: scope.CollectorAWS,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: testServiceAWSScheduledRunConfiguration(),
			}},
		},
		Store:               store,
		AWSScheduledPlanner: scheduled.WorkPlanner{},
		Clock:               func() time.Time { return now },
	}, store
}

func enabledRetention() WorkItemRetentionConfig {
	return WorkItemRetentionConfig{Enabled: true, Interval: time.Hour, Policy: workflow.DefaultWorkItemRetentionPolicy()}
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestServiceRunActiveModeRunsWorkItemRetentionWhenEnabled(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	service, _ := activeRetentionService(now, enabledRetention())
	retention := &fakeWorkItemRetention{
		result: workflow.WorkItemRetentionResult{
			Batches: 2, ScannedRows: 1500, DeletedCompleted: 40, DeletedFailedTerminal: 7, Wrapped: true,
			BatchDurations: []time.Duration{20 * time.Millisecond, 30 * time.Millisecond},
		},
		size: workflow.WorkItemTableSize{Bytes: 1 << 20, EstimatedRows: 9000},
	}
	metrics := &fakeRetentionMetrics{}
	var logs bytes.Buffer
	service.WorkItemRetention = retention
	service.Metrics = metrics
	service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	if err := service.Run(cancelledContext()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(retention.asOf) != 1 || !retention.asOf[0].Equal(now) {
		t.Fatalf("retention passes = %v, want one startup pass as of %s", retention.asOf, now)
	}
	if retention.policies[0] != workflow.DefaultWorkItemRetentionPolicy() {
		t.Fatalf("retention policy = %+v, want the configured policy", retention.policies[0])
	}
	if len(metrics.retention) != 1 {
		t.Fatalf("retention observations = %d, want 1", len(metrics.retention))
	}
	observation := metrics.retention[0]
	if observation.Outcome != workItemRetentionOutcomeSuccess || observation.Result.Deleted() != 47 ||
		!observation.TableSizeKnown || observation.TableSize.Bytes != 1<<20 {
		t.Fatalf("retention observation = %+v, want success with 47 deleted and the table size", observation)
	}
	for _, want := range []string{
		`"msg":"workflow work item retention pass"`,
		`"deleted_completed":40`,
		`"deleted_failed_terminal":7`,
		`"table_bytes":1048576`,
		`"wrapped":true`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("retention log missing %s:\n%s", want, logs.String())
		}
	}
}

// TestServiceRunActiveModeRunsDefaultRetentionWithNoEnvironment proves the
// shipped default: an active coordinator whose environment never mentions
// retention runs a startup pass at the 720h policy.
func TestServiceRunActiveModeRunsDefaultRetentionWithNoEnvironment(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig(retentionEnv(nil))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	service, _ := activeRetentionService(now, cfg.WorkItemRetention)
	retention := &fakeWorkItemRetention{}
	service.WorkItemRetention = retention

	if err := service.Run(cancelledContext()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(retention.policies) != 1 {
		t.Fatalf("retention passes = %d, want one startup pass with no environment set", len(retention.policies))
	}
	if got := retention.policies[0].Age; got != 720*time.Hour {
		t.Fatalf("startup pass age = %s, want 720h", got)
	}
}

func TestServiceRunSkipsWorkItemRetentionWhenDisabledOrDark(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	disabled, _ := activeRetentionService(now, WorkItemRetentionConfig{})
	disabledRetention := &fakeWorkItemRetention{}
	disabled.WorkItemRetention = disabledRetention
	if err := disabled.Run(cancelledContext()); err != nil {
		t.Fatalf("Run(disabled) error = %v, want nil", err)
	}
	if len(disabledRetention.asOf) != 0 {
		t.Fatalf("disabled retention ran %d passes, want 0", len(disabledRetention.asOf))
	}

	// An explicit ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE=0 is the
	// operator's off switch: active mode then schedules no pass, and Run does
	// not require a retention store.
	zeroAge, err := LoadConfig(retentionEnv(map[string]string{
		"ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE": "0",
	}))
	if err != nil {
		t.Fatalf("LoadConfig(age 0) error = %v, want nil", err)
	}
	zeroAgeService, _ := activeRetentionService(now, zeroAge.WorkItemRetention)
	if err := zeroAgeService.Run(cancelledContext()); err != nil {
		t.Fatalf("Run(age 0, no retention store) error = %v, want nil", err)
	}
	zeroAgeRetention := &fakeWorkItemRetention{}
	zeroAgeService.WorkItemRetention = zeroAgeRetention
	if err := zeroAgeService.Run(cancelledContext()); err != nil {
		t.Fatalf("Run(age 0) error = %v, want nil", err)
	}
	if len(zeroAgeRetention.asOf) != 0 {
		t.Fatalf("age 0 retention ran %d passes, want 0", len(zeroAgeRetention.asOf))
	}

	dark := Service{
		Config: Config{DeploymentMode: deploymentModeDark, WorkItemRetention: enabledRetention()},
		Store:  &fakeStore{},
		Clock:  func() time.Time { return now },
	}
	darkRetention := &fakeWorkItemRetention{}
	dark.WorkItemRetention = darkRetention
	if err := dark.Run(cancelledContext()); err != nil {
		t.Fatalf("Run(dark) error = %v, want nil", err)
	}
	if len(darkRetention.asOf) != 0 {
		t.Fatalf("dark-mode retention ran %d passes, want 0", len(darkRetention.asOf))
	}
}

func TestServiceRunRequiresRetentionStoreWhenEnabled(t *testing.T) {
	t.Parallel()

	service, _ := activeRetentionService(time.Now(), enabledRetention())
	err := service.Run(cancelledContext())
	if err == nil || !strings.Contains(err.Error(), "work item retention") {
		t.Fatalf("Run() error = %v, want a missing retention store error", err)
	}
}

func TestRunWorkItemRetentionReportsFailureWithoutStoppingTheCoordinator(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	service, _ := activeRetentionService(now, enabledRetention())
	retention := &fakeWorkItemRetention{
		result:  workflow.WorkItemRetentionResult{Batches: 1, DeletedCompleted: 5},
		err:     errors.New("canceling statement due to lock timeout"),
		sizeErr: errors.New("catalog read failed"),
	}
	metrics := &fakeRetentionMetrics{}
	var logs bytes.Buffer
	service.WorkItemRetention = retention
	service.Metrics = metrics
	service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	service.runWorkItemRetention(context.Background())

	if len(metrics.retention) != 1 {
		t.Fatalf("retention observations = %d, want 1", len(metrics.retention))
	}
	observation := metrics.retention[0]
	if observation.Outcome != workItemRetentionOutcomeError || observation.Result.DeletedCompleted != 5 || observation.TableSizeKnown {
		t.Fatalf("retention observation = %+v, want error outcome keeping the 5 committed deletes and no table size", observation)
	}
	for _, want := range []string{`"level":"ERROR"`, `lock timeout`, `"deleted_completed":5`} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("retention error log missing %s:\n%s", want, logs.String())
		}
	}
}
