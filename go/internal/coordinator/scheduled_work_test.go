// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/aws/scheduled"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestScanIntervalFromConfiguration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		want    time.Duration
		wantSet bool
		wantErr string
	}{
		{name: "empty document", raw: "", want: 0, wantSet: false},
		{name: "object without field", raw: `{"scheduled_scan_enabled": true}`, want: 0, wantSet: false},
		{name: "blank string is unset", raw: `{"scan_interval": "  "}`, want: 0, wantSet: false},
		{name: "duration string", raw: `{"scan_interval": "12h"}`, want: 12 * time.Hour, wantSet: true},
		{name: "duration with surrounding space", raw: `{"scan_interval": " 90m "}`, want: 90 * time.Minute, wantSet: true},
		{name: "unparseable duration", raw: `{"scan_interval": "twelve hours"}`, wantErr: "scan_interval"},
		{name: "numeric value is rejected", raw: `{"scan_interval": 3600}`, wantErr: "scan_interval must be a duration string, got number"},
		// A present key with a null value is not "unset": a template that meant
		// 12h and emitted null would otherwise run on the global cadence with no
		// signal (Codex review on #6724).
		{name: "null value is rejected", raw: `{"scan_interval": null}`, wantErr: "scan_interval must be a duration string, got null"},
		{name: "boolean value is rejected", raw: `{"scan_interval": true}`, wantErr: "scan_interval must be a duration string, got boolean"},
		{name: "object value is rejected without echoing it", raw: `{"scan_interval": {"token": "do-not-echo"}}`, wantErr: "scan_interval must be a duration string, got object"},
		{name: "null document is unset", raw: `null`, want: 0, wantSet: false},
		// A valid non-object document carries no scan_interval field, so it is
		// unset: DesiredCollectorInstance.Validate accepts any valid JSON here and
		// a generic or disabled collector may legitimately carry [] (Codex review
		// on #6724).
		{name: "array document is unset", raw: `[]`, want: 0, wantSet: false},
		{name: "scalar document is unset", raw: `42`, want: 0, wantSet: false},
		{name: "string document is unset", raw: `"text"`, want: 0, wantSet: false},
		{name: "unclosed object", raw: `{"scan_interval": "1h"`, wantErr: "decode collector configuration"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, set, err := scanIntervalFromConfiguration(tc.raw)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("scanIntervalFromConfiguration(%q) error = %v, want containing %q", tc.raw, err, tc.wantErr)
				}
				if strings.Contains(err.Error(), "do-not-echo") {
					t.Fatalf("scanIntervalFromConfiguration(%q) echoed the rejected value: %v", tc.raw, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("scanIntervalFromConfiguration(%q) error = %v, want nil", tc.raw, err)
			}
			if set != tc.wantSet || got != tc.want {
				t.Fatalf("scanIntervalFromConfiguration(%q) = (%s, %v), want (%s, %v)", tc.raw, got, set, tc.want, tc.wantSet)
			}
		})
	}
}

func TestValidateScanIntervalAgainstReconcileInterval(t *testing.T) {
	t.Parallel()

	global := 30 * time.Second
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{name: "unset passes", raw: `{}`},
		{name: "array document passes", raw: `[]`},
		{name: "equal to global passes", raw: `{"scan_interval": "30s"}`},
		{name: "above global passes", raw: `{"scan_interval": "12h"}`},
		{name: "zero rejected", raw: `{"scan_interval": "0s"}`, wantErr: "must be at least 1s"},
		{name: "negative rejected", raw: `{"scan_interval": "-5m"}`, wantErr: "must be at least 1s"},
		{name: "sub-second rejected", raw: `{"scan_interval": "500ms"}`, wantErr: "must be at least 1s"},
		{name: "below global rejected", raw: `{"scan_interval": "10s"}`, wantErr: "must not be shorter than the reconcile interval 30s"},
		{name: "unparseable rejected", raw: `{"scan_interval": "soon"}`, wantErr: "scan_interval"},
		{name: "null value rejected", raw: `{"scan_interval": null}`, wantErr: "got null"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateScanInterval(tc.raw, global)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateScanInterval(%q) error = %v, want nil", tc.raw, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateScanInterval(%q) error = %v, want containing %q", tc.raw, err, tc.wantErr)
			}
		})
	}
}

func TestServiceScanIntervalSelectsInstanceOverrideElseGlobal(t *testing.T) {
	t.Parallel()

	service := Service{Config: Config{ReconcileInterval: 5 * time.Minute}}
	observedAt := time.Date(2026, time.May, 20, 22, 7, 13, 0, time.UTC)

	global := testServiceAWSScheduledInstance(observedAt)
	globalInterval, err := service.scanInterval(global)
	if err != nil {
		t.Fatalf("scanInterval(global) error = %v, want nil", err)
	}
	if globalInterval != 5*time.Minute {
		t.Fatalf("scanInterval(global) = %s, want 5m0s", globalInterval)
	}

	override := testServiceAWSScheduledInstance(observedAt)
	override.InstanceID = "collector-aws-slow"
	override.Configuration = testServiceAWSScheduledConfigurationWithScanInterval("1h")
	overrideInterval, err := service.scanInterval(override)
	if err != nil {
		t.Fatalf("scanInterval(override) error = %v, want nil", err)
	}
	if overrideInterval != time.Hour {
		t.Fatalf("scanInterval(override) = %s, want 1h0m0s", overrideInterval)
	}

	zeroConfig := Service{}
	defaulted, err := zeroConfig.scanInterval(global)
	if err != nil {
		t.Fatalf("scanInterval(zero config) error = %v, want nil", err)
	}
	if defaulted != defaultReconcileInterval {
		t.Fatalf("scanInterval(zero config) = %s, want %s", defaulted, defaultReconcileInterval)
	}

	bootstrap := testServiceAWSScheduledInstance(observedAt)
	bootstrap.Bootstrap = true
	bootstrap.Configuration = testServiceAWSScheduledConfigurationWithScanInterval("1h")
	bootstrapInterval, err := service.scanInterval(bootstrap)
	if err != nil {
		t.Fatalf("scanInterval(bootstrap) error = %v, want nil", err)
	}
	if bootstrapInterval != 5*time.Minute {
		t.Fatalf("scanInterval(bootstrap) = %s, want the global 5m0s: bootstrap instances ignore scan_interval", bootstrapInterval)
	}

	broken := testServiceAWSScheduledInstance(observedAt)
	broken.Configuration = `{"scan_interval": "later"}`
	if _, err := service.scanInterval(broken); err == nil {
		t.Fatalf("scanInterval(broken) error = nil, want decode error")
	}
}

func TestScheduledPlanKeyBucketsByInterval(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 20, 22, 37, 13, 0, time.UTC)
	instance := workflow.CollectorInstance{Mode: workflow.CollectorModeContinuous}

	if got, want := scheduledPlanKey(instance, observedAt, 5*time.Minute), "continuous-20260520T223500Z"; got != want {
		t.Fatalf("5m plan key = %q, want %q", got, want)
	}
	if got, want := scheduledPlanKey(instance, observedAt, time.Hour), "continuous-20260520T220000Z"; got != want {
		t.Fatalf("1h plan key = %q, want %q", got, want)
	}
	if got, want := scheduledPlanKey(instance, observedAt, 12*time.Hour), "continuous-20260520T120000Z"; got != want {
		t.Fatalf("12h plan key = %q, want %q", got, want)
	}
	if got, want := scheduledPlanKey(workflow.CollectorInstance{}, observedAt, time.Hour), "schedule-20260520T220000Z"; got != want {
		t.Fatalf("blank-mode plan key = %q, want %q", got, want)
	}
	if got, want := scheduledPlanKey(workflow.CollectorInstance{Bootstrap: true}, observedAt, time.Hour), "bootstrap"; got != want {
		t.Fatalf("bootstrap plan key = %q, want %q", got, want)
	}
	if got, want := scheduledPlanKey(instance, observedAt, 0), scheduledPlanKey(instance, observedAt, defaultReconcileInterval); got != want {
		t.Fatalf("zero-interval plan key = %q, want default-interval key %q", got, want)
	}
}

// Two AWS instances under one coordinator: one on the global 5m bucket, one
// widened to 1h through scan_interval. Ticks at +0, +5m and +10m cross two 5m
// boundaries but no 1h boundary, so the global instance re-plans on every tick
// once its prior work has drained while the widened instance keeps the same
// plan key, and so the same run ID, and plans once. The fourth tick at +55m
// crosses into the next hour and the widened instance must plan again: the
// contract is a slower cadence, not a frozen one (#6720).
func TestServiceRunReconcileHonorsPerInstanceScanInterval(t *testing.T) {
	t.Parallel()

	first := time.Date(2026, time.May, 20, 22, 5, 0, 0, time.UTC)
	current := first
	globalInstance := testServiceAWSScheduledInstance(first)
	slowInstance := testServiceAWSScheduledInstance(first)
	slowInstance.InstanceID = "collector-aws-slow"
	slowInstance.Configuration = testServiceAWSScheduledConfigurationWithScanInterval("1h")
	store := &fakeStore{instances: []workflow.CollectorInstance{globalInstance, slowInstance}}
	service := Service{
		Config: Config{
			DeploymentMode:           deploymentModeActive,
			ClaimsEnabled:            true,
			ReconcileInterval:        5 * time.Minute,
			ReapInterval:             time.Hour,
			ClaimLeaseTTL:            time.Minute,
			HeartbeatInterval:        20 * time.Second,
			ExpiredClaimLimit:        10,
			ExpiredClaimRequeueDelay: 5 * time.Second,
			CollectorInstances: []workflow.DesiredCollectorInstance{
				{
					InstanceID:    globalInstance.InstanceID,
					CollectorKind: scope.CollectorAWS,
					Mode:          workflow.CollectorModeContinuous,
					Enabled:       true,
					ClaimsEnabled: true,
					Configuration: globalInstance.Configuration,
				},
				{
					InstanceID:    slowInstance.InstanceID,
					CollectorKind: scope.CollectorAWS,
					Mode:          workflow.CollectorModeContinuous,
					Enabled:       true,
					ClaimsEnabled: true,
					Configuration: slowInstance.Configuration,
				},
			},
		},
		Store:               store,
		AWSScheduledPlanner: scheduled.WorkPlanner{},
		Clock:               func() time.Time { return current },
	}

	for tick, offset := range []time.Duration{0, 5 * time.Minute, 10 * time.Minute, 55 * time.Minute} {
		current = first.Add(offset)
		if err := service.runReconcile(context.Background()); err != nil {
			t.Fatalf("tick %d runReconcile() error = %v, want nil", tick, err)
		}
		// The fake store dedupes on open targets only, so drain the queue to
		// model the prior run finishing before the next tick.
		store.enqueuedItems = nil
	}

	runsByInstance := map[string][]string{}
	for _, run := range store.createdRuns {
		parts := strings.Split(run.RunID, ":")
		if len(parts) < 4 {
			t.Fatalf("unexpected run ID shape %q", run.RunID)
		}
		runsByInstance[parts[1]] = append(runsByInstance[parts[1]], parts[3])
	}
	if got, want := len(runsByInstance[globalInstance.InstanceID]), 4; got != want {
		t.Fatalf("global instance runs = %d (%v), want %d", got, runsByInstance[globalInstance.InstanceID], want)
	}
	slowKeys := runsByInstance[slowInstance.InstanceID]
	wantSlowKeys := []string{"continuous-20260520T220000Z", "continuous-20260520T230000Z"}
	if len(slowKeys) != len(wantSlowKeys) {
		t.Fatalf("scan_interval instance runs = %d (%v), want %d %v", len(slowKeys), slowKeys, len(wantSlowKeys), wantSlowKeys)
	}
	for i := range wantSlowKeys {
		if slowKeys[i] != wantSlowKeys[i] {
			t.Fatalf("scan_interval instance plan key[%d] = %q, want %q", i, slowKeys[i], wantSlowKeys[i])
		}
	}
}

func TestServiceRunReconcileRejectsUnparseableScanInterval(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 20, 22, 5, 0, 0, time.UTC)
	instance := testServiceAWSScheduledInstance(now)
	instance.Configuration = `{"scheduled_scan_enabled": true, "scan_interval": "soon", "target_scopes": []}`
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	service := Service{
		Config: Config{
			DeploymentMode:    deploymentModeActive,
			ClaimsEnabled:     true,
			ReconcileInterval: 5 * time.Minute,
			ReapInterval:      time.Hour,
			ClaimLeaseTTL:     time.Minute,
			HeartbeatInterval: 20 * time.Second,
			ExpiredClaimLimit: 10,
		},
		Store:               store,
		AWSScheduledPlanner: scheduled.WorkPlanner{},
		Clock:               func() time.Time { return now },
	}

	err := service.runReconcile(context.Background())
	if err == nil || !strings.Contains(err.Error(), "scan interval") {
		t.Fatalf("runReconcile() error = %v, want scan interval decode error", err)
	}
	if len(store.createdRuns) != 0 {
		t.Fatalf("created runs = %d, want 0", len(store.createdRuns))
	}
}

func testServiceAWSScheduledConfigurationWithScanInterval(scanInterval string) string {
	return `{
		"scheduled_scan_enabled": true,
		"scan_interval": "` + scanInterval + `",
		"target_scopes": [{
			"account_id": "123456789012",
			"allowed_regions": ["us-east-1"],
			"allowed_services": ["lambda"]
		}]
	}`
}

// The startup line means "this instance will be scheduled on its own cadence".
// A bootstrap, disabled, or claims-disabled instance never is, whatever its
// configuration says, so none of them may be logged as if it were (review
// findings on #6724).
func TestLogScanIntervalOverridesSkipsInstancesThatCannotUseIt(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	service := Service{
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
		Config: Config{
			ReconcileInterval: 30 * time.Second,
			CollectorInstances: []workflow.DesiredCollectorInstance{
				{InstanceID: "aws-slow", CollectorKind: scope.CollectorAWS, Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true, Configuration: `{"scan_interval": "12h"}`},
				{InstanceID: "git-bootstrap", CollectorKind: "git", Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true, Bootstrap: true, Configuration: `{"scan_interval": "12h"}`},
				{InstanceID: "git-plain", CollectorKind: "git", Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: true, Configuration: `{"provider": "github"}`},
				{InstanceID: "aws-disabled", CollectorKind: scope.CollectorAWS, Mode: workflow.CollectorModeContinuous, Enabled: false, ClaimsEnabled: true, Configuration: `{"scan_interval": "12h"}`},
				{InstanceID: "aws-no-claims", CollectorKind: scope.CollectorAWS, Mode: workflow.CollectorModeContinuous, Enabled: true, ClaimsEnabled: false, Configuration: `{"scan_interval": "12h"}`},
			},
		},
	}

	service.logScanIntervalOverrides()

	out := logs.String()
	if got := strings.Count(out, "sets scan interval"); got != 1 {
		t.Fatalf("scan interval log lines = %d, want 1; log:\n%s", got, out)
	}
	if !strings.Contains(out, "collector_instance_id=aws-slow") {
		t.Fatalf("log did not name the overriding instance; log:\n%s", out)
	}
	for _, skipped := range []string{"git-bootstrap", "aws-disabled", "aws-no-claims"} {
		if strings.Contains(out, skipped) {
			t.Fatalf("%s cannot be scheduled on the override yet was logged as carrying its own cadence; log:\n%s", skipped, out)
		}
	}
}
