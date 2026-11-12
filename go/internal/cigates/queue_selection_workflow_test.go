// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestNoMergeGroupPlumbingInWorkflows is the merge-queue removal regression
// guard (#9367): main has had no merge queue since 2026-10-16 (#8288), so no
// workflow may carry a merge_group trigger, a queue-selection job, or a
// condition on the removed selector. A resurrected arm fails here, not in CI.
func TestNoMergeGroupPlumbingInWorkflows(t *testing.T) {
	root := repositoryRoot(t)
	files, err := filepath.Glob(filepath.Join(root, ".github/workflows/*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, token := range []string{"merge_group", "queue-selection", "queue_select"} {
				if strings.Contains(string(raw), token) {
					t.Errorf("dead merge-queue plumbing %q remains", token)
				}
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, ".github/workflows/queue-selection.yml")); !os.IsNotExist(err) {
		t.Error("queue-selection.yml must stay deleted")
	}
}

// TestManualMirrorJobsKeepGuards pins the release-mirror jobs' exact dispatch,
// repository, and main guards in docker-publish.yml: they never took part in
// queue selection and must not gain a needs edge.
func TestManualMirrorJobsKeepGuards(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/docker-publish.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			If    string    `yaml:"if"`
			Needs yaml.Node `yaml:"needs"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"publish-ci-service-mirrors", "verify-public-ci-service-mirrors"} {
		job, ok := workflow.Jobs[key]
		if !ok {
			t.Fatalf("manual mirror job %s missing", key)
		}
		if !manualMirrorConditionMatches(key, job.If) {
			t.Errorf("manual mirror job %s must keep its exact dispatch, repository, and main guards", key)
		}
		if len(jobNeeds(job.Needs)) != 0 {
			t.Errorf("manual mirror job %s must not wait for release queue selection", key)
		}
	}
}

func manualMirrorConditionMatches(job, condition string) bool {
	mode := map[string]string{
		"publish-ci-service-mirrors":       "ci-mirrors-publish",
		"verify-public-ci-service-mirrors": "ci-mirrors-verify-public",
	}[job]
	if mode == "" {
		return false
	}
	want := "github.event_name == 'workflow_dispatch' && inputs.mode == '" + mode + "' && " +
		"github.repository == 'eshu-hq/eshu' && github.ref == 'refs/heads/main'"
	return strings.Join(strings.Fields(condition), " ") == want
}

func TestManualMirrorConditionRejectsWeakenedGuards(t *testing.T) {
	tests := []struct {
		name      string
		job       string
		condition string
		want      bool
	}{
		{"publisher", "publish-ci-service-mirrors", "github.event_name == 'workflow_dispatch' && inputs.mode == 'ci-mirrors-publish' && github.repository == 'eshu-hq/eshu' && github.ref == 'refs/heads/main'", true},
		{"verifier", "verify-public-ci-service-mirrors", "github.event_name == 'workflow_dispatch' && inputs.mode == 'ci-mirrors-verify-public' && github.repository == 'eshu-hq/eshu' && github.ref == 'refs/heads/main'", true},
		{"release job", "build-and-push-image", "github.event_name == 'workflow_dispatch' && inputs.mode == 'ci-mirrors-publish' && github.repository == 'eshu-hq/eshu' && github.ref == 'refs/heads/main'", false},
		{"wrong mode", "publish-ci-service-mirrors", "github.event_name == 'workflow_dispatch' && inputs.mode == 'release' && github.repository == 'eshu-hq/eshu' && github.ref == 'refs/heads/main'", false},
		{"wrong repository", "publish-ci-service-mirrors", "github.event_name == 'workflow_dispatch' && inputs.mode == 'ci-mirrors-publish' && github.repository == 'other/eshu' && github.ref == 'refs/heads/main'", false},
		{"branch broadened", "publish-ci-service-mirrors", "github.event_name == 'workflow_dispatch' && inputs.mode == 'ci-mirrors-publish' && github.repository == 'eshu-hq/eshu' && github.ref != 'refs/heads/main'", false},
		{"boolean bypass", "publish-ci-service-mirrors", "github.event_name == 'workflow_dispatch' && inputs.mode == 'ci-mirrors-publish' && github.repository == 'eshu-hq/eshu' && github.ref == 'refs/heads/main' || true", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := manualMirrorConditionMatches(test.job, test.condition); got != test.want {
				t.Fatalf("manualMirrorConditionMatches(%q, %q) = %t, want %t", test.job, test.condition, got, test.want)
			}
		})
	}
}

// TestPublisherBudgetCoversPayload was removed with the publisher: #10584
// deleted the required-gates.yml workflow_run fan-in and the aggregate job,
// so there is no publisher await timeout to cover the payload budget.
