// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
)

func TestComponentInitCollectorScaffoldsValidPackage(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "demo-component")
	output := executeRootCommand(
		t,
		"component", "init", "collector",
		"--id", "dev.example.collector.demo",
		"--publisher", "example",
		"--fact-kind", "dev.example.demo_observation",
		"--output", outDir,
	)
	if !strings.Contains(output, "scaffolded collector component") {
		t.Fatalf("component init output = %q, want scaffold summary", output)
	}

	for _, path := range []string{
		"manifest.yaml",
		"README.md",
		"config.example.yaml",
		"go.mod",
		"collector.go",
		"collector_test.go",
		"scripts/verify-local.sh",
	} {
		if _, err := os.Stat(filepath.Join(outDir, path)); err != nil {
			t.Fatalf("expected scaffold file %s: %v", path, err)
		}
	}

	manifestPath := filepath.Join(outDir, "manifest.yaml")
	if _, err := component.LoadManifest(manifestPath); err != nil {
		t.Fatalf("generated manifest did not validate: %v", err)
	}
	assertFileContains(
		t, manifestPath,
		"collector-sdk/v1alpha1",
		"dev.example.demo_observation",
		"sourceConfidence:",
		"observed",
		"metricsPrefix: eshu_dp_dev_example_demo_observation_",
	)
	assertFileContains(
		t, filepath.Join(outDir, "collector.go"),
		`collector.Fact{`,
		`Kind:             factKindDemoObservation`,
		`SourceConfidence: collector.SourceConfidenceObserved`,
	)
	assertFileContains(
		t, filepath.Join(outDir, "collector_test.go"),
		"ValidateResult",
		"manifest.yaml",
		"source confidence",
	)

	sdkPath, err := filepath.Abs("../../../sdk/go/collector")
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v, want nil", err)
	}
	runCommand(t, outDir, "go", "mod", "edit", "-replace", "github.com/eshu-hq/eshu/sdk/go/collector="+sdkPath)
	runCommand(t, outDir, "go", "mod", "tidy")
	runCommand(t, outDir, "go", "test", "./...")
}

func TestComponentInitCollectorJSONOutput(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "demo-component")
	output := executeRootCommand(
		t,
		"component", "init", "collector",
		"--id", "dev.example.collector.demo",
		"--publisher", "example",
		"--fact-kind", "dev.example.demo_observation",
		"--output", outDir,
		"--json",
	)

	var payload componentCLIOutput
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("component init --json output is not JSON: %v\n%s", err, output)
	}
	if got, want := payload.Command, "init"; got != want {
		t.Fatalf("Command = %q, want %q", got, want)
	}
	if got, want := payload.Status, "scaffolded"; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if payload.Component == nil {
		t.Fatal("Component = nil, want scaffolded component summary")
	}
	if got, want := payload.Component.ID, "dev.example.collector.demo"; got != want {
		t.Fatalf("Component.ID = %q, want %q", got, want)
	}
}

func TestComponentInitCollectorRequiresIDFlagName(t *testing.T) {
	_, err := executeRootCommandError(
		t,
		"component", "init", "collector",
		"--publisher", "example",
		"--fact-kind", "dev.example.demo_observation",
		"--output", filepath.Join(t.TempDir(), "missing-id"),
	)
	if err == nil {
		t.Fatal("component init collector error = nil, want missing --id error")
	}
	if !strings.Contains(err.Error(), "--id is required") {
		t.Fatalf("error = %q, want --id is required", err)
	}
}

func TestComponentInitCollectorRejectsNonNamespacedFactKind(t *testing.T) {
	_, err := executeRootCommandError(
		t,
		"component", "init", "collector",
		"--id", "dev.example.collector.demo",
		"--publisher", "example",
		"--fact-kind", "demo_observation",
		"--output", filepath.Join(t.TempDir(), "bad-fact-kind"),
	)
	if err == nil {
		t.Fatal("component init collector error = nil, want non-namespaced fact-kind rejection")
	}
	if !strings.Contains(err.Error(), "must be namespaced") {
		t.Fatalf("error = %q, want namespaced fact kind rejection", err)
	}
}

func TestComponentInitCollectorRejectsExistingOutputDirectory(t *testing.T) {
	outDir := t.TempDir()
	_, err := executeRootCommandError(
		t,
		"component", "init", "collector",
		"--id", "dev.example.collector.demo",
		"--publisher", "example",
		"--fact-kind", "dev.example.demo_observation",
		"--output", outDir,
	)
	if err == nil {
		t.Fatal("component init collector error = nil, want existing output directory rejection")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %q, want existing output directory rejection", err)
	}
}

func TestComponentInitCollectorRejectsUnsafeIdentifiers(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "bad-component")
	_, err := executeRootCommandError(
		t,
		"component", "init", "collector",
		"--id", "Dev.Example/collector",
		"--publisher", "example",
		"--fact-kind", "dev.example.demo_observation",
		"--output", outDir,
	)
	if err == nil {
		t.Fatal("component init collector error = nil, want unsafe identifier rejection")
	}
	if _, err := os.Stat(outDir); !os.IsNotExist(err) {
		t.Fatalf("output path exists after rejected init: %v", err)
	}
}

func executeRootCommand(t *testing.T, args ...string) string {
	t.Helper()

	output, err := executeRootCommandError(t, args...)
	if err != nil {
		t.Fatalf("rootCmd.Execute(%v) error = %v; output: %s", args, err, output)
	}
	return output
}

func executeRootCommandError(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var output bytes.Buffer
	resetComponentInitCollectorFlags(t)
	rootCmd.SetOut(&output)
	rootCmd.SetErr(&output)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		resetComponentInitCollectorFlags(t)
	})
	err := rootCmd.Execute()
	return output.String(), err
}

func resetComponentInitCollectorFlags(t *testing.T) {
	t.Helper()

	cmd, _, err := rootCmd.Find([]string{"component", "init", "collector"})
	if err != nil {
		t.Fatalf("find component init collector command: %v", err)
	}
	for name, value := range map[string]string{
		componentInitIDFlag:        "",
		componentInitPublisherFlag: "",
		componentInitFactKindFlag:  "",
		componentInitOutputFlag:    "",
	} {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("reset --%s: %v", name, err)
		}
	}
	if err := cmd.Flags().Set(componentJSONFlag, "false"); err != nil {
		t.Fatalf("reset --%s: %v", componentJSONFlag, err)
	}
}

func assertFileContains(t *testing.T, path string, wants ...string) {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v, want nil", path, err)
	}
	body := string(raw)
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing %q:\n%s", path, want, body)
		}
	}
}

func runCommand(t *testing.T, dir string, name string, args ...string) {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, string(output))
	}
}
