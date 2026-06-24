// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestComponentSubcommandsRenderTextOutput(t *testing.T) {
	t.Parallel()

	manifestPath := writeComponentCommandManifest(t)
	home := t.TempDir()

	verifyOut := &bytes.Buffer{}
	verifyCmd := newComponentCommandWithTrustFlags(verifyOut, home, false, false)
	if err := runComponentVerify(verifyCmd, []string{manifestPath}); err != nil {
		t.Fatalf("runComponentVerify() error = %v, want nil", err)
	}
	assertContains(t, verifyOut.String(), "verified dev.eshu.collector.aws@0.1.0")

	installOut := &bytes.Buffer{}
	installCmd := newComponentCommandWithTrustFlags(installOut, home, false, false)
	if err := runComponentInstall(installCmd, []string{manifestPath}); err != nil {
		t.Fatalf("runComponentInstall() error = %v, want nil", err)
	}
	assertContains(t, installOut.String(), "installed dev.eshu.collector.aws@0.1.0")

	listOut := &bytes.Buffer{}
	listCmd := newComponentCommandWithHomeFlag(listOut, home, false)
	if err := runComponentList(listCmd, nil); err != nil {
		t.Fatalf("runComponentList() error = %v, want nil", err)
	}
	assertContains(t, listOut.String(), "dev.eshu.collector.aws")

	enableOut := &bytes.Buffer{}
	enableCmd := newComponentCommandWithActivationFlags(enableOut, home, false, false)
	if err := runComponentEnable(enableCmd, []string{"dev.eshu.collector.aws"}); err != nil {
		t.Fatalf("runComponentEnable() error = %v, want nil", err)
	}
	assertContains(t, enableOut.String(), "enabled dev.eshu.collector.aws instance prod-aws")

	disableOut := &bytes.Buffer{}
	disableCmd := newComponentCommandWithDisableFlags(disableOut, home, false)
	if err := runComponentDisable(disableCmd, []string{"dev.eshu.collector.aws"}); err != nil {
		t.Fatalf("runComponentDisable() error = %v, want nil", err)
	}
	assertContains(t, disableOut.String(), "disabled dev.eshu.collector.aws instance prod-aws")

	uninstallOut := &bytes.Buffer{}
	uninstallCmd := newComponentCommandWithUninstallFlags(uninstallOut, home, false)
	if err := runComponentUninstall(uninstallCmd, []string{"dev.eshu.collector.aws"}); err != nil {
		t.Fatalf("runComponentUninstall() error = %v, want nil", err)
	}
	assertContains(t, uninstallOut.String(), "uninstalled dev.eshu.collector.aws@0.1.0")
}

func TestComponentSubcommandsRenderJSONOutput(t *testing.T) {
	t.Parallel()

	manifestPath := writeComponentCommandManifest(t)
	home := t.TempDir()

	inspectOut := &bytes.Buffer{}
	inspectCmd := newComponentCommandWithJSONFlag(inspectOut, true)
	if err := runComponentInspect(inspectCmd, []string{manifestPath}); err != nil {
		t.Fatalf("runComponentInspect() error = %v, want nil", err)
	}
	assertComponentJSON(t, inspectOut, "inspect", "inspected")

	verifyOut := &bytes.Buffer{}
	verifyCmd := newComponentCommandWithTrustFlags(verifyOut, home, true, false)
	if err := runComponentVerify(verifyCmd, []string{manifestPath}); err != nil {
		t.Fatalf("runComponentVerify() error = %v, want nil", err)
	}
	assertComponentJSON(t, verifyOut, "verify", "verified")

	dryInstallOut := &bytes.Buffer{}
	dryInstallCmd := newComponentCommandWithTrustFlags(dryInstallOut, home, true, true)
	if err := runComponentInstall(dryInstallCmd, []string{manifestPath}); err != nil {
		t.Fatalf("dry-run runComponentInstall() error = %v, want nil", err)
	}
	dryInstallPayload := assertComponentJSON(t, dryInstallOut, "install", "would_install")
	if got := dryInstallPayload["dry_run"]; got != true {
		t.Fatalf("install dry_run = %#v, want true", got)
	}
	assertRegistryComponentCount(t, home, 0)

	installOut := &bytes.Buffer{}
	installCmd := newComponentCommandWithTrustFlags(installOut, home, true, false)
	if err := runComponentInstall(installCmd, []string{manifestPath}); err != nil {
		t.Fatalf("runComponentInstall() error = %v, want nil", err)
	}
	assertComponentJSON(t, installOut, "install", "installed")

	listOut := &bytes.Buffer{}
	listCmd := newComponentCommandWithHomeFlag(listOut, home, true)
	if err := runComponentList(listCmd, nil); err != nil {
		t.Fatalf("runComponentList() error = %v, want nil", err)
	}
	listPayload := assertComponentJSON(t, listOut, "list", "listed")
	assertJSONComponentStates(t, listPayload, "installed")

	dryEnableOut := &bytes.Buffer{}
	dryEnableCmd := newComponentCommandWithActivationFlags(dryEnableOut, home, true, true)
	if err := runComponentEnable(dryEnableCmd, []string{"dev.eshu.collector.aws"}); err != nil {
		t.Fatalf("dry-run runComponentEnable() error = %v, want nil", err)
	}
	dryEnablePayload := assertComponentJSON(t, dryEnableOut, "enable", "would_enable")
	if got := dryEnablePayload["dry_run"]; got != true {
		t.Fatalf("enable dry_run = %#v, want true", got)
	}
	assertRegistryActivationCount(t, home, 0)

	enableOut := &bytes.Buffer{}
	enableCmd := newComponentCommandWithActivationFlags(enableOut, home, true, false)
	if err := runComponentEnable(enableCmd, []string{"dev.eshu.collector.aws"}); err != nil {
		t.Fatalf("runComponentEnable() error = %v, want nil", err)
	}
	assertComponentJSON(t, enableOut, "enable", "enabled")

	enabledListOut := &bytes.Buffer{}
	enabledListCmd := newComponentCommandWithHomeFlag(enabledListOut, home, true)
	if err := runComponentList(enabledListCmd, nil); err != nil {
		t.Fatalf("enabled runComponentList() error = %v, want nil", err)
	}
	enabledListPayload := assertComponentJSON(t, enabledListOut, "list", "listed")
	assertJSONComponentStates(t, enabledListPayload, "installed", "enabled", "claim_capable")

	revokedListOut := &bytes.Buffer{}
	revokedListCmd := newComponentCommandWithHomeFlag(revokedListOut, home, true)
	revokedListCmd.Flags().String(componentTrustModeFlag, "allowlist", "")
	revokedListCmd.Flags().StringSlice(componentAllowIDFlag, []string{"dev.eshu.collector.aws"}, "")
	revokedListCmd.Flags().StringSlice(componentAllowPublisherFlag, []string{"eshu-hq"}, "")
	revokedListCmd.Flags().StringSlice(componentRevokeIDFlag, []string{"dev.eshu.collector.aws"}, "")
	revokedListCmd.Flags().StringSlice(componentRevokePublisherFlag, nil, "")
	if err := runComponentList(revokedListCmd, nil); err != nil {
		t.Fatalf("revoked runComponentList() error = %v, want nil", err)
	}
	revokedListPayload := assertComponentJSON(t, revokedListOut, "list", "listed")
	assertJSONComponentStates(t, revokedListPayload, "installed", "enabled", "claim_capable", "revoked", "failed")

	disableOut := &bytes.Buffer{}
	disableCmd := newComponentCommandWithDisableFlags(disableOut, home, true)
	if err := runComponentDisable(disableCmd, []string{"dev.eshu.collector.aws"}); err != nil {
		t.Fatalf("runComponentDisable() error = %v, want nil", err)
	}
	assertComponentJSON(t, disableOut, "disable", "disabled")

	uninstallOut := &bytes.Buffer{}
	uninstallCmd := newComponentCommandWithUninstallFlags(uninstallOut, home, true)
	if err := runComponentUninstall(uninstallCmd, []string{"dev.eshu.collector.aws"}); err != nil {
		t.Fatalf("runComponentUninstall() error = %v, want nil", err)
	}
	assertComponentJSON(t, uninstallOut, "uninstall", "uninstalled")
}

func TestComponentJSONErrorClassDoesNotLeakUnselectedLocalPath(t *testing.T) {
	t.Parallel()

	missingPath := filepath.Join(t.TempDir(), "private", "component.yaml")
	out := &bytes.Buffer{}
	cmd := newComponentCommandWithJSONFlag(out, true)

	err := runComponentInspect(cmd, []string{missingPath})
	if err == nil {
		t.Fatal("runComponentInspect() error = nil, want invalid manifest error")
	}
	if strings.Contains(err.Error(), missingPath) {
		t.Fatalf("error leaks missing manifest path %q: %v", missingPath, err)
	}
	payload := decodeComponentOutput(t, out)
	errorPayload := payloadMap(t, payload, "error")
	if got, want := errorPayload["code"], "invalid_manifest"; got != want {
		t.Fatalf("error.code = %#v, want %q", got, want)
	}
	if strings.Contains(out.String(), missingPath) {
		t.Fatalf("json output leaks missing manifest path %q: %s", missingPath, out.String())
	}
}

func newComponentCommandWithTrustFlags(out *bytes.Buffer, home string, jsonOutput bool, dryRun bool) *cobra.Command {
	cmd := newComponentCommandWithHomeFlag(out, home, jsonOutput)
	cmd.Flags().String(componentTrustModeFlag, "allowlist", "")
	cmd.Flags().StringSlice(componentAllowIDFlag, []string{"dev.eshu.collector.aws"}, "")
	cmd.Flags().StringSlice(componentAllowPublisherFlag, []string{"eshu-hq"}, "")
	cmd.Flags().StringSlice(componentRevokeIDFlag, nil, "")
	cmd.Flags().StringSlice(componentRevokePublisherFlag, nil, "")
	cmd.Flags().Bool(componentDryRunFlag, dryRun, "")
	return cmd
}

func newComponentCommandWithActivationFlags(out *bytes.Buffer, home string, jsonOutput bool, dryRun bool) *cobra.Command {
	cmd := newComponentCommandWithHomeFlag(out, home, jsonOutput)
	cmd.Flags().String(componentInstanceFlag, "prod-aws", "")
	cmd.Flags().String(componentModeFlag, "scheduled", "")
	cmd.Flags().Bool(componentClaimsFlag, true, "")
	cmd.Flags().String(componentConfigFlag, filepath.Join(home, "configs", "aws.yaml"), "")
	cmd.Flags().Bool(componentDryRunFlag, dryRun, "")
	return cmd
}

func newComponentCommandWithDisableFlags(out *bytes.Buffer, home string, jsonOutput bool) *cobra.Command {
	cmd := newComponentCommandWithHomeFlag(out, home, jsonOutput)
	cmd.Flags().String(componentInstanceFlag, "prod-aws", "")
	return cmd
}

func newComponentCommandWithUninstallFlags(out *bytes.Buffer, home string, jsonOutput bool) *cobra.Command {
	cmd := newComponentCommandWithHomeFlag(out, home, jsonOutput)
	cmd.Flags().String(componentVersionFlag, "0.1.0", "")
	return cmd
}

func newComponentCommandWithHomeFlag(out *bytes.Buffer, home string, jsonOutput bool) *cobra.Command {
	cmd := newComponentCommandWithJSONFlag(out, jsonOutput)
	cmd.Flags().String(componentHomeFlag, home, "")
	return cmd
}

func newComponentCommandWithJSONFlag(out *bytes.Buffer, jsonOutput bool) *cobra.Command {
	cmd := newComponentTestCommand(out)
	cmd.Flags().Bool(componentJSONFlag, jsonOutput, "")
	return cmd
}

func assertComponentJSON(t *testing.T, out *bytes.Buffer, command string, status string) map[string]any {
	t.Helper()

	payload := decodeComponentOutput(t, out)
	if got, want := payload["schema_version"], "eshu.component.cli.v1"; got != want {
		t.Fatalf("schema_version = %#v, want %q; output=%s", got, want, out.String())
	}
	if got := payload["command"]; got != command {
		t.Fatalf("command = %#v, want %q; output=%s", got, command, out.String())
	}
	if got := payload["status"]; got != status {
		t.Fatalf("status = %#v, want %q; output=%s", got, status, out.String())
	}
	return payload
}

func decodeComponentOutput(t *testing.T, out *bytes.Buffer) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	return payload
}

func assertJSONComponentStates(t *testing.T, payload map[string]any, wantStates ...string) {
	t.Helper()

	components, ok := payload["components"].([]any)
	if !ok || len(components) != 1 {
		t.Fatalf("components = %#v, want one component", payload["components"])
	}
	componentPayload, ok := components[0].(map[string]any)
	if !ok {
		t.Fatalf("components[0] = %#v, want object", components[0])
	}
	states, ok := componentPayload["states"].([]any)
	if !ok {
		t.Fatalf("states = %#v, want array", componentPayload["states"])
	}
	got := make([]string, 0, len(states))
	for _, state := range states {
		got = append(got, state.(string))
	}
	for _, want := range wantStates {
		if !slices.Contains(got, want) {
			t.Fatalf("states = %v, want %q", got, want)
		}
	}
}

func assertRegistryComponentCount(t *testing.T, home string, want int) {
	t.Helper()

	entries, err := os.ReadDir(home)
	if os.IsNotExist(err) && want == 0 {
		return
	}
	if err != nil {
		t.Fatalf("os.ReadDir(%q) error = %v, want nil", home, err)
	}
	if got := len(entries); got != want {
		t.Fatalf("component home entry count = %d, want %d", got, want)
	}
}

func assertRegistryActivationCount(t *testing.T, home string, want int) {
	t.Helper()

	listOut := &bytes.Buffer{}
	listCmd := newComponentCommandWithHomeFlag(listOut, home, true)
	if err := runComponentList(listCmd, nil); err != nil {
		t.Fatalf("runComponentList() error = %v, want nil", err)
	}
	payload := decodeComponentOutput(t, listOut)
	components := payload["components"].([]any)
	componentPayload := components[0].(map[string]any)
	activations, _ := componentPayload["activations"].([]any)
	if got := len(activations); got != want {
		t.Fatalf("activation count = %d, want %d", got, want)
	}
}

func payloadMap(t *testing.T, payload map[string]any, key string) map[string]any {
	t.Helper()

	value, ok := payload[key].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", key, payload[key])
	}
	return value
}

func assertContains(t *testing.T, got string, want string) {
	t.Helper()

	if !strings.Contains(got, want) {
		t.Fatalf("output = %q, want to contain %q", got, want)
	}
}
