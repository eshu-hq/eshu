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

type goldenCorpusWorkflow struct {
	Jobs map[string]goldenCorpusJob `yaml:"jobs"`
}

type goldenCorpusJob struct {
	Steps []goldenCorpusStep `yaml:"steps"`
}

type goldenCorpusStep struct {
	Name string `yaml:"name"`
	Uses string `yaml:"uses"`
}

// checkGoldenCorpusUploadRouting requires every artifact upload in
// golden-corpus-gate.yml to ride the retry-upload-artifact wrapper: a bare
// actions/upload-artifact step of any version is a real regression, not an
// alternative, and both the per-leg and the failure-bundle uploads must stay
// present under their reviewed names.
func checkGoldenCorpusUploadRouting(workflow []byte) string {
	var parsed goldenCorpusWorkflow
	if err := yaml.Unmarshal(workflow, &parsed); err != nil {
		return err.Error()
	}
	const wrapper = "./.github/actions/retry-upload-artifact"
	required := map[string]bool{
		"Upload capture recording":             false,
		"Upload capture recordings on failure": false,
	}
	for _, job := range parsed.Jobs {
		for _, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
				return "golden-corpus upload must route through retry-upload-artifact: " + step.Name
			}
			if step.Uses == wrapper {
				if _, ok := required[step.Name]; ok {
					required[step.Name] = true
				}
			}
		}
	}
	for name, seen := range required {
		if !seen {
			return "missing golden-corpus wrapper upload step: " + name
		}
	}
	return ""
}

// Seeded violations for the golden-corpus upload routing gate. Each entry
// must fail checkGoldenCorpusUploadRouting; a passing entry means the gate
// lost a pin.
var goldenCorpusUploadMutations = []struct {
	name, old, replacement string
}{
	{
		"per-leg upload reverts to bare v4",
		"uses: ./.github/actions/retry-upload-artifact\n        with:\n          name: differential-capture-",
		"uses: actions/upload-artifact@v4\n        with:\n          name: differential-capture-",
	},
	{
		"failure bundle reverts to bare v4",
		"uses: ./.github/actions/retry-upload-artifact\n        with:\n          name: differential-joined-capture-",
		"uses: actions/upload-artifact@v4\n        with:\n          name: differential-joined-capture-",
	},
	{
		"bare v5 upload slips past a version pin",
		"uses: ./.github/actions/retry-upload-artifact\n        with:\n          name: differential-capture-",
		"uses: actions/upload-artifact@v5\n        with:\n          name: differential-capture-",
	},
	{
		"failure bundle renamed away",
		"      - name: Upload capture recordings on failure\n",
		"      - name: Upload capture recordings eventually\n",
	},
	{
		"per-leg upload step removed",
		"      - name: Upload capture recording\n",
		"      # Upload capture recording removed\n",
	},
}

func TestGoldenCorpusUploadRouting(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "golden-corpus-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if problem := checkGoldenCorpusUploadRouting(workflow); problem != "" {
		t.Fatal(problem)
	}
	for _, mutation := range goldenCorpusUploadMutations {
		t.Run(mutation.name, func(t *testing.T) {
			if !strings.Contains(string(workflow), mutation.old) {
				t.Fatalf("mutation anchor missing: %s", mutation.old)
			}
			changed := strings.Replace(string(workflow), mutation.old, mutation.replacement, 1)
			if problem := checkGoldenCorpusUploadRouting([]byte(changed)); problem == "" {
				t.Fatal("seeded violation passed")
			}
		})
	}
}
