// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
	"gopkg.in/yaml.v3"
)

const queueSnapshotRoot = "version: v1\ngate_fragments: [ci-gates.d/gates-01.yaml]\n"

func queueSnapshotFragment(id, trigger string) string {
	return fmt.Sprintf(`version: v1
gates:
  - id: %s
    name: Queue snapshot fixture
    category: exactness
    tier: pre-pr
    blocking: true
    triggers: ["%s"]
    local:
      command: "bash scripts/fixture.sh"
    ci:
      workflow: test.yml
      job: go-core
    requirements: [go]
`, id, trigger)
}

func writeQueueSnapshotFile(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runQueueSnapshotGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func queueSnapshotWorkflowPrefix(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), ".github/workflows/queue-selection.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, step := range workflow.Jobs["select"].Steps {
		if step.Name != "Select queue payload from both registries" {
			continue
		}
		before, _, ok := strings.Cut(step.Run, "bash scripts/ci/go-mod-download-retry.sh")
		if !ok {
			t.Fatal("queue workflow no longer has a snapshot prefix before Go prewarm")
		}
		return before
	}
	t.Fatal("queue workflow has no selection step")
	return ""
}

func runQueueSnapshotPrefix(t *testing.T, checkout, runnerTemp, fixtureRoot string) string {
	t.Helper()
	if err := os.MkdirAll(runnerTemp, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(runnerTemp, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// Old root-only workflow must not call the network in the RED phase.
	writeQueueSnapshotFile(t, bin, "gh", "#!/bin/sh\ncat \"$QUEUE_TEST_ROOT\"\n")
	if err := os.Chmod(filepath.Join(bin, "gh"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", queueSnapshotWorkflowPrefix(t))
	cmd.Dir = checkout
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"QUEUE_TEST_ROOT="+fixtureRoot,
		"RUN_EVENT=merge_group",
		"REPO=eshu-hq/eshu",
		"BASE_REF=main",
		"RUNNER_TEMP="+runnerTemp,
		"GITHUB_OUTPUT="+filepath.Join(runnerTemp, "output"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("production snapshot prefix: %v\n%s", err, out)
	}
	return filepath.Join(runnerTemp, "trusted/specs/ci-gates.v1.yaml")
}

func TestQueueWorkflowSnapshotsCompleteImmutableTrustedRegistry(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	main := filepath.Join(tmp, "main")
	if err := os.Mkdir(main, 0o755); err != nil {
		t.Fatal(err)
	}
	runQueueSnapshotGit(t, main, "init", "-q", "-b", "main")
	runQueueSnapshotGit(t, main, "config", "user.name", "Queue Fixture")
	runQueueSnapshotGit(t, main, "config", "user.email", "queue@example.invalid")
	writeQueueSnapshotFile(t, main, "specs/ci-gates.v1.yaml", queueSnapshotRoot)
	writeQueueSnapshotFile(t, main, "specs/ci-gates.d/gates-01.yaml", queueSnapshotFragment("trusted-gate", "go/**"))
	runQueueSnapshotGit(t, main, "add", "specs")
	runQueueSnapshotGit(t, main, "commit", "-qm", "trusted registry")

	head := filepath.Join(tmp, "head")
	cmd := exec.Command("git", "clone", "-q", main, head)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone local fixture: %v\n%s", err, out)
	}
	runQueueSnapshotGit(t, head, "checkout", "-qb", "queue")
	writeQueueSnapshotFile(t, head, "specs/ci-gates.d/gates-01.yaml", queueSnapshotFragment("head-gate", "docs/**"))
	runQueueSnapshotGit(t, head, "add", "specs")
	runQueueSnapshotGit(t, head, "-c", "user.name=Queue Fixture", "-c", "user.email=queue@example.invalid", "commit", "-qm", "weaken head registry")

	trustedPath := runQueueSnapshotPrefix(t, head, filepath.Join(tmp, "runner"), filepath.Join(main, "specs/ci-gates.v1.yaml"))
	trusted, err := cigates.Load(trustedPath)
	if err != nil {
		t.Fatalf("trusted snapshot did not load: %v", err)
	}
	headRegistry, err := cigates.Load(filepath.Join(head, "specs/ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := cigates.SelectQueueWorkflow([]*cigates.Registry{headRegistry, trusted}, []string{"go/changed.go"}, false, "test.yml", "")
	if err != nil || !selection.Selected || len(selection.Gates) != 1 || selection.Gates[0] != "trusted-gate" {
		t.Fatalf("head removed a trusted gate: selection=%+v err=%v", selection, err)
	}

	writeQueueSnapshotFile(t, main, "specs/ci-gates.d/gates-01.yaml", queueSnapshotFragment("later-gate", "go/**"))
	runQueueSnapshotGit(t, main, "add", "specs")
	runQueueSnapshotGit(t, main, "commit", "-qm", "advance main after snapshot")
	stillTrusted, err := cigates.Load(trustedPath)
	if err != nil || len(stillTrusted.Gates) != 1 || stillTrusted.Gates[0].ID != "trusted-gate" {
		t.Fatalf("snapshot followed moving main: registry=%+v err=%v", stillTrusted, err)
	}

	writeQueueSnapshotFile(t, main, "specs/ci-gates.v1.yaml", "version: v1\ngate_fragments: [ci-gates.d/missing.yaml]\n")
	runQueueSnapshotGit(t, main, "add", "specs")
	runQueueSnapshotGit(t, main, "commit", "-qm", "broken main fragment reference")
	broken := runQueueSnapshotPrefix(t, head, filepath.Join(tmp, "broken-runner"), filepath.Join(main, "specs/ci-gates.v1.yaml"))
	if _, err := cigates.Load(broken); err == nil {
		t.Fatal("missing trusted fragment did not fail closed")
	}

	writeQueueSnapshotFile(t, main, "specs/ci-gates.v1.yaml", queueSnapshotRoot)
	writeQueueSnapshotFile(t, main, "specs/ci-gates.d/gates-01.yaml", "version: [\n")
	runQueueSnapshotGit(t, main, "add", "specs")
	runQueueSnapshotGit(t, main, "commit", "-qm", "malformed main fragment")
	malformed := runQueueSnapshotPrefix(t, head, filepath.Join(tmp, "malformed-runner"), filepath.Join(main, "specs/ci-gates.v1.yaml"))
	if _, err := cigates.Load(malformed); err == nil {
		t.Fatal("malformed trusted fragment did not fail closed")
	}
}
