// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// stubEshu stands in for the eshu CLI inside the reconcile script. It appends
// each call to $STUB_LOG and answers from environment variables, so a test
// decides what grant, install, list and enable return.
const stubEshu = `#!/bin/sh
printf '%s\n' "$*" >> "${STUB_LOG}"
case "$2" in
  grant) exit "${STUB_GRANT_EXIT:-0}" ;;
  install) exit "${STUB_INSTALL_EXIT:-0}" ;;
  list)
    printf '%s\n' "${STUB_LIST_JSON}"
    exit "${STUB_LIST_EXIT:-0}"
    ;;
  enable)
    printf '%s\n' "${STUB_ENABLE_OUT}"
    exit "${STUB_ENABLE_EXIT:-0}"
    ;;
esac
`

const (
	scriptPagerDutyID = "dev.example.collector.pagerduty"
	scriptJiraID      = "dev.example.collector.jira"
	scriptHome        = "/data/.eshu/components"
	scriptConfigPath  = "/etc/eshu/component-activations/pagerduty-a.yaml"
)

// twoGrantPagerDuty adds a second grant so the script test sees grant order.
var twoGrantPagerDuty = strings.Replace(pagerDutyActivationValues,
	"          expiresIn: 360h\n",
	"          expiresIn: 360h\n"+
		"        - kind: incident.lifecycle_event\n"+
		"          schemaVersions: [\"1.0.0\"]\n"+
		"          scope: pagerduty\n", 1)

// listedComponent is one entry of `eshu component list --json`.
type listedComponent struct{ id, version string }

// componentListJSON mirrors the CLI's JSON shape (go/internal/cli/component
// CLIComponent): id, name, publisher and version come in that order, so the id
// and version of one component are NOT adjacent, and later fields hold nested
// objects. The script must match one component's own object.
func componentListJSON(entries ...listedComponent) string {
	var items []string
	for _, entry := range entries {
		items = append(items, fmt.Sprintf(`    {
      "id": %q,
      "name": "Example collector",
      "publisher": "example",
      "version": %q,
      "manifest_digest": "sha256:%s",
      "verified": true,
      "trust_mode": "allowlist",
      "installed_at": "2026-10-24T02:01:37Z",
      "states": ["installed", "enabled"],
      "activations": [
        {"instance_id": "other", "mode": "scheduled", "claims_enabled": true}
      ]
    }`, entry.id, entry.version, strings.Repeat("a", 64)))
	}
	return "{\n  \"schema_version\": \"eshu.component.cli.v1\",\n  \"command\": \"list\",\n  \"status\": \"listed\",\n  \"components\": [\n" +
		strings.Join(items, ",\n") + "\n  ]\n}"
}

// reconcileScript renders a one-activation chart and returns the reconcile init
// container script together with the literal env the chart gives it.
func reconcileScript(t *testing.T) (string, map[string]string) {
	t.Helper()
	return reconcileScriptFromManifests(t, renderComponentActivations(t, componentActivationsValues(twoGrantPagerDuty)))
}

// reconcileScriptFromManifests returns the pagerduty-a reconcile script and its
// literal env from an already rendered chart.
func reconcileScriptFromManifests(t *testing.T, manifests []helmManifest) (string, map[string]string) {
	t.Helper()

	deployment := requireHelmManifest(t, manifests, "Deployment", "eshu-cx-pagerduty-a")
	var reconcile map[string]any
	for _, init := range helmMapSlice(helmPodSpec(t, deployment)["initContainers"]) {
		if init["name"] == "reconcile-pagerduty-a" {
			reconcile = init
		}
	}
	if reconcile == nil {
		t.Fatal("reconcile-pagerduty-a init container not rendered")
	}
	command := helmStringSlice(reconcile["command"])
	if len(command) != 3 || command[0] != "sh" || command[1] != "-c" {
		t.Fatalf("reconcile command = %#v, want sh -c <script>", command)
	}
	env := map[string]string{}
	for name, entry := range helmEnvByName(reconcile) {
		if value, ok := entry["value"].(string); ok {
			env[name] = value
		}
	}
	return command[2], env
}

type scriptRun struct {
	exit   int
	output string
	calls  []string
}

// runReconcileScript runs script under sh with a stub eshu first on PATH. The
// chart's own env is the base, so a renamed variable breaks the run.
func runReconcileScript(t *testing.T, script string, chartEnv map[string]string, extra map[string]string, seed func(runtimeDir string)) scriptRun {
	t.Helper()
	return runReconcileScriptWithStub(t, stubEshu, script, chartEnv, extra, seed)
}

// runReconcileScriptWithStub is runReconcileScript with a chosen stub eshu.
func runReconcileScriptWithStub(t *testing.T, stub, script string, chartEnv map[string]string, extra map[string]string, seed func(runtimeDir string)) scriptRun {
	t.Helper()

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	runtimeDir := filepath.Join(dir, "runtime")
	for _, path := range []string{binDir, runtimeDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(binDir, "eshu"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	seed(runtimeDir)

	logPath := filepath.Join(dir, "calls.log")
	env := []string{"PATH=" + binDir + ":/usr/bin:/bin", "STUB_LOG=" + logPath}
	merged := map[string]string{}
	for name, value := range chartEnv {
		merged[name] = value
	}
	merged["RUNTIME_DIR"] = runtimeDir
	for name, value := range extra {
		merged[name] = value
	}
	for name, value := range merged {
		env = append(env, name+"="+value)
	}

	cmd := exec.Command("sh", "-c", script)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	result := scriptRun{output: string(out)}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		result.exit = exitErr.ExitCode()
	default:
		t.Fatalf("run reconcile script: %v", err)
	}
	if body, err := os.ReadFile(logPath); err == nil {
		result.calls = strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	}
	return result
}

func seedRuntime(runtimeDir string) {
	_ = os.WriteFile(filepath.Join(runtimeDir, "collector"), []byte("#!/bin/sh\n"), 0o755)
	_ = os.WriteFile(filepath.Join(runtimeDir, "manifest.yaml"), []byte("kind: ComponentPackage\n"), 0o644)
}

// TestHelmComponentActivationReconcileScript runs the rendered reconcile
// script, the one piece of logic that decides whether a pod ever starts. The
// chart cannot be trusted on a read of the text: idempotent restart, the exact
// "already enabled" match, the version check and the fail-closed paths are all
// behavior.
func TestHelmComponentActivationReconcileScript(t *testing.T) {
	t.Parallel()

	script, chartEnv := reconcileScript(t)
	pagerDutyAt := func(version string) string {
		return componentListJSON(listedComponent{scriptPagerDutyID, version})
	}
	alreadyEnabled := `activation "pagerduty-a" is already enabled for component "` + scriptPagerDutyID + `" version "0.1.0"`

	grantCalls := func(runtimeDir string) []string {
		return []string{
			"component grant " + scriptPagerDutyID + " --component-home " + scriptHome + " --version 0.1.0 --kind incident.record --schema-version 1.0.0 --schema-version 1.1.0 --scope pagerduty --expires-in 360h",
			"component grant " + scriptPagerDutyID + " --component-home " + scriptHome + " --version 0.1.0 --kind incident.lifecycle_event --schema-version 1.0.0 --scope pagerduty --expires-in 720h",
		}
	}
	installCall := func(runtimeDir, revoke string) string {
		return "component install " + runtimeDir + "/manifest.yaml --component-home " + scriptHome + " --trust-mode allowlist --allow-id " + scriptPagerDutyID + "," + scriptJiraID + " --allow-publisher example" + revoke
	}
	listCall := "component list --component-home " + scriptHome + " --json"
	enableCall := "component enable " + scriptPagerDutyID + " --component-home " + scriptHome + " --instance pagerduty-a --mode scheduled --claims --config " + scriptConfigPath

	tests := []struct {
		name      string
		extra     map[string]string
		seed      func(runtimeDir string)
		wantExit  int
		wantCalls func(runtimeDir string) []string
		wantOut   string
	}{
		{
			name:     "first run grants installs verifies and enables in order",
			extra:    map[string]string{"STUB_LIST_JSON": pagerDutyAt("0.1.0"), "STUB_ENABLE_OUT": "enabled x"},
			wantExit: 0,
			wantCalls: func(r string) []string {
				return append(grantCalls(r), installCall(r, ""), listCall, enableCall)
			},
		},
		{
			name: "revocations reach install",
			extra: map[string]string{
				"STUB_LIST_JSON": pagerDutyAt("0.1.0"), "REVOKE_IDS": "bad.id", "REVOKE_PUBLISHERS": "badpub",
			},
			wantExit: 0,
			wantCalls: func(r string) []string {
				return append(grantCalls(r), installCall(r, " --revoke-id bad.id --revoke-publisher badpub"), listCall, enableCall)
			},
		},
		{
			name: "wanted component listed last among several",
			extra: map[string]string{"STUB_LIST_JSON": componentListJSON(
				listedComponent{scriptJiraID, "0.2.0"}, listedComponent{scriptPagerDutyID, "0.1.0"})},
			wantExit: 0,
			wantCalls: func(r string) []string {
				return append(grantCalls(r), installCall(r, ""), listCall, enableCall)
			},
		},
		{
			name: "restart finds the activation already enabled",
			extra: map[string]string{
				"STUB_LIST_JSON": pagerDutyAt("0.1.0"), "STUB_ENABLE_EXIT": "1", "STUB_ENABLE_OUT": alreadyEnabled,
			},
			wantExit: 0,
			wantOut:  "already enabled; continuing",
			wantCalls: func(r string) []string {
				return append(grantCalls(r), installCall(r, ""), listCall, enableCall)
			},
		},
		{
			name: "another enable failure keeps its exit code",
			extra: map[string]string{
				"STUB_LIST_JSON": pagerDutyAt("0.1.0"), "STUB_ENABLE_EXIT": "7", "STUB_ENABLE_OUT": "registry write failed",
			},
			wantExit: 7,
			wantCalls: func(r string) []string {
				return append(grantCalls(r), installCall(r, ""), listCall, enableCall)
			},
		},
		{
			name: "text that only resembles already enabled still fails",
			extra: map[string]string{
				"STUB_LIST_JSON": pagerDutyAt("0.1.0"), "STUB_ENABLE_EXIT": "1", "STUB_ENABLE_OUT": "claims already enabled elsewhere",
			},
			wantExit: 1,
			wantCalls: func(r string) []string {
				return append(grantCalls(r), installCall(r, ""), listCall, enableCall)
			},
		},
		{
			name: "already enabled message for another instance still fails",
			extra: map[string]string{
				"STUB_LIST_JSON": pagerDutyAt("0.1.0"), "STUB_ENABLE_EXIT": "1",
				"STUB_ENABLE_OUT": `activation "other" is already enabled for component "` + scriptPagerDutyID + `" version "0.1.0"`,
			},
			wantExit: 1,
			wantCalls: func(r string) []string {
				return append(grantCalls(r), installCall(r, ""), listCall, enableCall)
			},
		},
		{
			name:     "listing failure stops before enable",
			extra:    map[string]string{"STUB_LIST_JSON": pagerDutyAt("0.1.0"), "STUB_LIST_EXIT": "5"},
			wantExit: 5,
			wantCalls: func(r string) []string {
				return append(grantCalls(r), installCall(r, ""), listCall)
			},
		},
		{
			name:     "installed version differs from componentVersion",
			extra:    map[string]string{"STUB_LIST_JSON": pagerDutyAt("9.9.9")},
			wantExit: 1,
			wantOut:  "seeded manifest is not " + scriptPagerDutyID + "@0.1.0",
			wantCalls: func(r string) []string {
				return append(grantCalls(r), installCall(r, ""), listCall)
			},
		},
		{
			name: "wanted component at a wrong version while another holds the wanted version",
			extra: map[string]string{"STUB_LIST_JSON": componentListJSON(
				listedComponent{scriptPagerDutyID, "9.9.9"}, listedComponent{scriptJiraID, "0.1.0"})},
			wantExit: 1,
			wantCalls: func(r string) []string {
				return append(grantCalls(r), installCall(r, ""), listCall)
			},
		},
		{
			name: "wanted component at a wrong version listed after another at the wanted version",
			extra: map[string]string{"STUB_LIST_JSON": componentListJSON(
				listedComponent{scriptJiraID, "0.1.0"}, listedComponent{scriptPagerDutyID, "9.9.9"})},
			wantExit: 1,
			wantCalls: func(r string) []string {
				return append(grantCalls(r), installCall(r, ""), listCall)
			},
		},
		{
			name: "id that only starts with the wanted id",
			extra: map[string]string{"STUB_LIST_JSON": componentListJSON(
				listedComponent{scriptPagerDutyID + "-extra", "0.1.0"})},
			wantExit: 1,
			wantCalls: func(r string) []string {
				return append(grantCalls(r), installCall(r, ""), listCall)
			},
		},
		{
			name:     "wanted component absent from the listing",
			extra:    map[string]string{"STUB_LIST_JSON": componentListJSON()},
			wantExit: 1,
			wantCalls: func(r string) []string {
				return append(grantCalls(r), installCall(r, ""), listCall)
			},
		},
		{
			name:     "grant failure stops before install",
			extra:    map[string]string{"STUB_LIST_JSON": pagerDutyAt("0.1.0"), "STUB_GRANT_EXIT": "1"},
			wantExit: 1,
			wantCalls: func(r string) []string {
				return grantCalls(r)[:1]
			},
		},
		{
			name:     "install failure stops before enable",
			extra:    map[string]string{"STUB_LIST_JSON": pagerDutyAt("0.1.0"), "STUB_INSTALL_EXIT": "4"},
			wantExit: 4,
			wantCalls: func(r string) []string {
				return append(grantCalls(r), installCall(r, ""))
			},
		},
		{
			name:  "seed wrote no collector binary",
			extra: map[string]string{"STUB_LIST_JSON": pagerDutyAt("0.1.0")},
			seed: func(r string) {
				_ = os.WriteFile(filepath.Join(r, "manifest.yaml"), []byte("kind: ComponentPackage\n"), 0o644)
			},
			wantExit: 1,
			wantOut:  "seed image did not write",
		},
		{
			name:  "collector binary is not executable",
			extra: map[string]string{"STUB_LIST_JSON": pagerDutyAt("0.1.0")},
			seed: func(r string) {
				_ = os.WriteFile(filepath.Join(r, "collector"), []byte("x"), 0o644)
				_ = os.WriteFile(filepath.Join(r, "manifest.yaml"), []byte("kind: ComponentPackage\n"), 0o644)
			},
			wantExit: 1,
			wantOut:  "seed image did not write",
		},
		{
			name:  "seed wrote no manifest",
			extra: map[string]string{"STUB_LIST_JSON": pagerDutyAt("0.1.0")},
			seed: func(r string) {
				_ = os.WriteFile(filepath.Join(r, "collector"), []byte("#!/bin/sh\n"), 0o755)
			},
			wantExit: 1,
			wantOut:  "seed image did not write",
		},
		{
			name:  "seed wrote an empty manifest",
			extra: map[string]string{"STUB_LIST_JSON": pagerDutyAt("0.1.0")},
			seed: func(r string) {
				_ = os.WriteFile(filepath.Join(r, "collector"), []byte("#!/bin/sh\n"), 0o755)
				_ = os.WriteFile(filepath.Join(r, "manifest.yaml"), nil, 0o644)
			},
			wantExit: 1,
			wantOut:  "seed image did not write",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			seed := tt.seed
			if seed == nil {
				seed = seedRuntime
			}
			var runtimeDir string
			got := runReconcileScript(t, script, chartEnv, tt.extra, func(dir string) {
				runtimeDir = dir
				seed(dir)
			})
			if got.exit != tt.wantExit {
				t.Fatalf("exit = %d, want %d\noutput:\n%s\ncalls:\n%s", got.exit, tt.wantExit, got.output, strings.Join(got.calls, "\n"))
			}
			if tt.wantOut != "" && !strings.Contains(got.output, tt.wantOut) {
				t.Fatalf("output missing %q:\n%s", tt.wantOut, got.output)
			}
			var wantCalls []string
			if tt.wantCalls != nil {
				wantCalls = tt.wantCalls(runtimeDir)
			}
			if len(got.calls)+len(wantCalls) > 0 && !slices.Equal(got.calls, wantCalls) {
				t.Fatalf("eshu calls =\n%s\nwant\n%s", strings.Join(got.calls, "\n"), strings.Join(wantCalls, "\n"))
			}
		})
	}
}
