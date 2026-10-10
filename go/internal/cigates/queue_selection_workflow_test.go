// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// TestQueueWorkflowsShareSelection checks the scheduling boundary, rather than
// mirroring the registry's path lists in another gate.
func TestQueueWorkflowsShareSelection(t *testing.T) {
	root := repositoryRoot(t)
	files, err := filepath.Glob(filepath.Join(root, ".github/workflows/*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		triggers, err := workflowTriggerKeys(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := triggers["merge_group"]; !ok {
			continue
		}
		t.Run(filepath.Base(file), func(t *testing.T) {
			var workflow struct {
				Jobs map[string]struct {
					Uses  string    `yaml:"uses"`
					Needs yaml.Node `yaml:"needs"`
				} `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				t.Fatal(err)
			}
			if workflow.Jobs["queue-selection"].Uses != "./.github/workflows/queue-selection.yml" {
				t.Fatal("queue payload has no shared registry selection")
			}
			for key, job := range workflow.Jobs {
				if key == "queue-selection" {
					continue
				}
				if !strings.Contains(strings.Join(jobNeeds(job.Needs), ","), "queue-selection") {
					t.Errorf("job %s does not wait for selection", key)
				}
			}
		})
	}
}

func TestQueuePublisherBudgetCoversSelectionAndPayload(t *testing.T) {
	root := repositoryRoot(t)
	files, err := filepath.Glob(filepath.Join(root, ".github/workflows/*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	maximumPayload := 0
	selection := 0
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var workflow struct {
			Jobs map[string]struct {
				Timeout string `yaml:"timeout-minutes"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			t.Fatal(err)
		}
		triggers, err := workflowTriggerKeys(raw)
		if err != nil {
			t.Fatal(err)
		}
		_, queue := triggers["merge_group"]
		for _, job := range workflow.Jobs {
			minutes, err := time.ParseDuration(job.Timeout + "m")
			if err != nil {
				continue // Dynamic static-matrix budgets remain independently tested.
			}
			if queue && int(minutes.Minutes()) > maximumPayload {
				maximumPayload = int(minutes.Minutes())
			}
			if filepath.Base(file) == "queue-selection.yml" {
				selection = int(minutes.Minutes())
			}
		}
	}
	path := filepath.Join(root, ".github/workflows/required-gates.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`--timeout ([0-9]+m)`).FindSubmatch(raw)
	if len(match) != 2 {
		t.Fatal("publisher has no bounded await timeout")
	}
	budget, err := time.ParseDuration(string(match[1]))
	if err != nil {
		t.Fatal(err)
	}
	if budget.Minutes() < float64(selection+maximumPayload+5) {
		t.Fatalf("publisher await %s cannot cover %dm selection + %dm payload + 5m publication margin", budget, selection, maximumPayload)
	}
}

// Execute the production builder: unselected rows keep their check identities
// but run neither mirror nor gate commands.
func TestQueueStaticMatrixRetainsUnselectedChecks(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/static-contract-gates.yml"))
	if err != nil {
		t.Fatal(err)
	}
	builder := staticContractMatrixBuilder(t, raw)
	first := strings.Index(builder, "\nappend_gate ")
	if first < 0 {
		t.Fatal("no production matrix append")
	}
	probe := strings.ReplaceAll(builder[:first], "${{ github.event_name }}", "merge_group")
	probe += `
append_gate "true" "selected" "Selected" "echo mirror" "echo gate"
append_gate "true" "unselected" "Unselected" "echo mirror" "echo gate"
printf '{"include":[%s]}' "${matrix_items}"
`
	cmd := exec.Command("bash", "-c", probe)
	cmd.Env = append(os.Environ(), `QUEUE_JOBS=["Selected"]`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("production builder: %v\n%s", err, out)
	}
	var matrix struct {
		Include []struct {
			Key      string `json:"key"`
			Selected bool   `json:"queue_selected"`
			Run      bool   `json:"run_gate"`
		} `json:"include"`
	}
	if err := json.Unmarshal(out, &matrix); err != nil {
		t.Fatalf("decode: %v: %s", err, out)
	}
	if len(matrix.Include) != 2 {
		t.Fatalf("check identities disappeared: %+v", matrix)
	}
	if !matrix.Include[0].Selected || !matrix.Include[0].Run || matrix.Include[1].Selected || matrix.Include[1].Run {
		t.Fatalf("wrong queue payload selection: %+v", matrix)
	}
}

func TestQueueStaticBuilderDoesNotSkipIdleQueue(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/static-contract-gates.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				ID string `yaml:"id"`
				If string `yaml:"if"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, step := range workflow.Jobs["changes"].Steps {
		if step.ID == "gate-matrix" && step.If != "" {
			t.Fatal("matrix builder must run for an idle merge group: otherwise fromJSON receives an empty output")
		}
	}
}
