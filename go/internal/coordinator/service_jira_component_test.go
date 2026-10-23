// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/component/extension"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/jira"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// recordingJiraPlanner delegates to the production Jira planner and records
// which instances the in-tree Jira scheduler handed it.
type recordingJiraPlanner struct {
	instanceIDs []string
}

func (r *recordingJiraPlanner) PlanJiraWork(
	ctx context.Context,
	request jira.PlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	r.instanceIDs = append(r.instanceIDs, request.Instance.InstanceID)
	return jira.WorkPlanner{}.PlanJiraWork(ctx, request)
}

// recordingExtensionPlanner delegates to the production generic component
// planner and records which instances the component scheduler handed it.
type recordingExtensionPlanner struct {
	instanceIDs []string
}

func (r *recordingExtensionPlanner) PlanComponentExtensionWork(
	ctx context.Context,
	request extension.PlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	r.instanceIDs = append(r.instanceIDs, request.Instance.InstanceID)
	return extension.WorkPlanner{}.PlanComponentExtensionWork(ctx, request)
}

func testJiraComponentConfig() string {
	return `{
		"schema_version":"eshu.component.instance.v1",
		"component_id":"dev.eshu.collector.jira",
		"component_version":"0.1.0",
		"publisher":"eshu-hq",
		"manifest_digest":"sha256:1234",
		"config_handle":"component-config:abcd",
		"host":{
			"source_system":"jira",
			"scope":{"id":"jira:site:synthetic-reference","kind":"jira_site"}
		},
		"runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"process"}
	}`
}

func testJiraComponentInstance(observedAt time.Time) workflow.CollectorInstance {
	instance := testServiceJiraInstance(observedAt)
	instance.InstanceID = "jira-component"
	instance.Mode = workflow.CollectorModeScheduled
	instance.Configuration = testJiraComponentConfig()
	return instance
}

func testJiraInTreeInstance(observedAt time.Time) workflow.CollectorInstance {
	instance := testServiceJiraInstance(observedAt)
	instance.InstanceID = "jira-in-tree"
	instance.Mode = workflow.CollectorModeScheduled
	return instance
}

// TestRunReconcileJiraComponentInstanceBelongsToGenericPlannerOnly is the
// #8907 regression: a jira instance carrying eshu.component.instance.v1
// configuration must be planned by the generic component planner only, and
// must not make the in-tree Jira planner fail the whole reconcile with
// "requires targets".
func TestRunReconcileJiraComponentInstanceBelongsToGenericPlannerOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		mutate        func(component, inTree *workflow.CollectorInstance)
		wantJira      []string
		wantExtension []string
		wantEnqueued  int
	}{
		{
			name:          "mixed component and in-tree instances",
			mutate:        func(_, _ *workflow.CollectorInstance) {},
			wantJira:      []string{"jira-in-tree"},
			wantExtension: []string{"jira-component"},
			wantEnqueued:  2,
		},
		{
			name: "disabled component instance is planned by nobody",
			mutate: func(component, _ *workflow.CollectorInstance) {
				component.Enabled = false
			},
			wantJira:     []string{"jira-in-tree"},
			wantEnqueued: 1,
		},
		{
			name: "claims-disabled component instance is planned by nobody",
			mutate: func(component, _ *workflow.CollectorInstance) {
				component.ClaimsEnabled = false
			},
			wantJira:     []string{"jira-in-tree"},
			wantEnqueued: 1,
		},
		{
			name: "disabled in-tree instance leaves only the component instance",
			mutate: func(_, inTree *workflow.CollectorInstance) {
				inTree.Enabled = false
			},
			wantExtension: []string{"jira-component"},
			wantEnqueued:  1,
		},
		{
			name: "claims-disabled in-tree instance leaves only the component instance",
			mutate: func(_, inTree *workflow.CollectorInstance) {
				inTree.ClaimsEnabled = false
			},
			wantExtension: []string{"jira-component"},
			wantEnqueued:  1,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			component := testJiraComponentInstance(now)
			inTree := testJiraInTreeInstance(now)
			test.mutate(&component, &inTree)
			instances := []workflow.CollectorInstance{component, inTree}
			desired := make([]workflow.DesiredCollectorInstance, 0, len(instances))
			for _, instance := range instances {
				desired = append(desired, workflow.DesiredCollectorInstance{
					InstanceID:    instance.InstanceID,
					CollectorKind: instance.CollectorKind,
					Mode:          instance.Mode,
					Enabled:       instance.Enabled,
					ClaimsEnabled: instance.ClaimsEnabled,
					Configuration: instance.Configuration,
				})
			}
			jiraPlanner := &recordingJiraPlanner{}
			extensionPlanner := &recordingExtensionPlanner{}
			store := &fakeStore{instances: instances}
			service := Service{
				Config: Config{
					DeploymentMode:        deploymentModeActive,
					ClaimsEnabled:         true,
					ReconcileInterval:     time.Hour,
					ExtensionEgressPolicy: mustParseExtensionEgressPolicy(t, `{"mode":"broad"}`),
					CollectorInstances:    desired,
				},
				Store:                     store,
				JiraPlanner:               jiraPlanner,
				ComponentExtensionPlanner: extensionPlanner,
				Clock:                     func() time.Time { return now },
			}

			if err := service.runReconcile(context.Background()); err != nil {
				t.Fatalf("runReconcile() error = %v, want nil", err)
			}
			if !reflect.DeepEqual(jiraPlanner.instanceIDs, test.wantJira) {
				t.Fatalf("jira planner instances = %v, want %v", jiraPlanner.instanceIDs, test.wantJira)
			}
			if !reflect.DeepEqual(extensionPlanner.instanceIDs, test.wantExtension) {
				t.Fatalf(
					"component extension planner instances = %v, want %v",
					extensionPlanner.instanceIDs,
					test.wantExtension,
				)
			}
			if got := len(store.enqueuedItems); got != test.wantEnqueued {
				t.Fatalf("enqueued items = %d, want %d", got, test.wantEnqueued)
			}
			for _, item := range store.enqueuedItems {
				if item.CollectorKind != scope.CollectorJira {
					t.Fatalf("enqueued collector kind = %q, want %q", item.CollectorKind, scope.CollectorJira)
				}
			}
		})
	}
}

// TestShouldScheduleJiraMirrorsPagerDutyComponentExclusion pins the shared
// component-configuration check: both in-tree schedulers skip exactly the
// instances the generic component scheduler claims, including a malformed
// component document (the generic planner owns reporting that error), and
// leave blank or in-tree target configuration to the in-tree planner.
func TestShouldScheduleJiraMirrorsPagerDutyComponentExclusion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		configuration string
		wantInTree    bool
	}{
		{name: "in-tree targets", configuration: testJiraConfig(), wantInTree: true},
		{name: "blank configuration", configuration: "", wantInTree: true},
		{name: "whitespace configuration", configuration: "  \n", wantInTree: true},
		{name: "unrelated json object", configuration: `{"foo":"bar"}`, wantInTree: true},
		{name: "unrelated schema version", configuration: `{"schema_version":"other.v1"}`, wantInTree: true},
		{name: "valid component document", configuration: testJiraComponentConfig(), wantInTree: false},
		{
			name: "component document missing component_id",
			configuration: `{"schema_version":"eshu.component.instance.v1",` +
				`"component_version":"0.1.0"}`,
			wantInTree: false,
		},
		{
			name:          "component id with wrong schema version",
			configuration: `{"schema_version":"other.v1","component_id":"dev.eshu.collector.jira"}`,
			wantInTree:    false,
		},
		{name: "malformed json", configuration: `{not json`, wantInTree: false},
	}
	now := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			jiraInstance := testServiceJiraInstance(now)
			jiraInstance.Configuration = test.configuration
			if got := shouldScheduleJira(jiraInstance); got != test.wantInTree {
				t.Fatalf("shouldScheduleJira() = %v, want %v", got, test.wantInTree)
			}
			pagerDutyInstance := testServicePagerDutyInstance(now)
			pagerDutyInstance.Configuration = test.configuration
			if got := shouldSchedulePagerDuty(pagerDutyInstance); got != test.wantInTree {
				t.Fatalf("shouldSchedulePagerDuty() = %v, want %v", got, test.wantInTree)
			}
			// The generic scheduler claims exactly the complement.
			if got := shouldScheduleComponentExtension(jiraInstance); got == test.wantInTree {
				t.Fatalf("shouldScheduleComponentExtension() = %v, want %v", got, !test.wantInTree)
			}
		})
	}
}
