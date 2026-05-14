package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestComponentInspectPrintsManifestIdentity(t *testing.T) {
	t.Parallel()

	manifestPath := writeComponentCommandManifest(t)
	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)

	if err := runComponentInspect(cmd, []string{manifestPath}); err != nil {
		t.Fatalf("runComponentInspect() error = %v, want nil", err)
	}
	output := out.String()
	for _, want := range []string{"dev.eshu.collector.aws", "AWS cloud scanner", "0.1.0"} {
		if !strings.Contains(output, want) {
			t.Fatalf("inspect output missing %q: %s", want, output)
		}
	}
}

func TestComponentInstallAndList(t *testing.T) {
	t.Parallel()

	manifestPath := writeComponentCommandManifest(t)
	home := t.TempDir()
	installOut := &bytes.Buffer{}
	installCmd := newComponentTestCommand(installOut)
	installCmd.Flags().String(componentHomeFlag, home, "")
	installCmd.Flags().String(componentTrustModeFlag, "allowlist", "")
	installCmd.Flags().StringSlice(componentAllowIDFlag, []string{"dev.eshu.collector.aws"}, "")
	installCmd.Flags().StringSlice(componentAllowPublisherFlag, []string{"eshu-hq"}, "")

	if err := runComponentInstall(installCmd, []string{manifestPath}); err != nil {
		t.Fatalf("runComponentInstall() error = %v, want nil", err)
	}
	if !strings.Contains(installOut.String(), "installed dev.eshu.collector.aws@0.1.0") {
		t.Fatalf("install output = %q, want installed line", installOut.String())
	}

	listOut := &bytes.Buffer{}
	listCmd := newComponentTestCommand(listOut)
	listCmd.Flags().String(componentHomeFlag, home, "")
	if err := runComponentList(listCmd, nil); err != nil {
		t.Fatalf("runComponentList() error = %v, want nil", err)
	}
	if !strings.Contains(listOut.String(), "dev.eshu.collector.aws") {
		t.Fatalf("list output = %q, want installed component", listOut.String())
	}
}

func TestComponentEnableRejectsMissingInstall(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	cmd := newComponentTestCommand(out)
	cmd.Flags().String(componentHomeFlag, t.TempDir(), "")
	cmd.Flags().String(componentInstanceFlag, "prod-aws", "")
	cmd.Flags().String(componentModeFlag, "scheduled", "")
	cmd.Flags().Bool(componentClaimsFlag, false, "")
	cmd.Flags().String(componentConfigFlag, "", "")

	err := runComponentEnable(cmd, []string{"dev.eshu.collector.aws"})
	if err == nil {
		t.Fatal("runComponentEnable() error = nil, want missing install error")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("runComponentEnable() error = %v, want not installed error", err)
	}
}

func writeComponentCommandManifest(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "component.yaml")
	body := `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: dev.eshu.collector.aws
  name: AWS cloud scanner
  publisher: eshu-hq
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.5 <0.1.0"
  componentType: collector
  collectorKinds:
    - aws
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/components/aws-collector@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: dev.eshu.aws.cloud_resource
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - reported
  consumerContracts:
    reducer:
      phases:
        - cloud_resource_uid:canonical_nodes_committed
  telemetry:
    metricsPrefix: eshu_dp_aws_
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v, want nil", err)
	}
	return path
}

func newComponentTestCommand(out *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	return cmd
}
