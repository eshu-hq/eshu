// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entrypoints

import (
	"strings"
	"testing"
)

func TestGenerateClaimedCollectorEntrypoint(t *testing.T) {
	manifest := Manifest{
		SchemaVersion:  1,
		CommandDir:     "go/cmd/collector-pagerduty",
		RuntimeName:    "collector-pagerduty",
		BinaryName:     "eshu-collector-pagerduty",
		CollectorLabel: "pagerduty collector",
		GoName:         "PagerDuty",
		Env: EnvSpec{
			CollectorInstances: "ESHU_COLLECTOR_INSTANCES_JSON",
			InstanceID:         "ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID",
			PollInterval:       "ESHU_PAGERDUTY_POLL_INTERVAL",
			ClaimLeaseTTL:      "ESHU_PAGERDUTY_CLAIM_LEASE_TTL",
			HeartbeatInterval:  "ESHU_PAGERDUTY_HEARTBEAT_INTERVAL",
			OwnerID:            "ESHU_PAGERDUTY_COLLECTOR_OWNER_ID",
			OwnerIDConstName:   "envOwnerID",
		},
		StoreName:            "collector_pagerduty",
		ClaimIDPrefix:        "pagerduty-claim",
		CollectorKindExpr:    "scope.CollectorPagerDuty",
		MaxAttemptsExpr:      "workflow.DefaultClaimMaxAttempts()",
		ScopeKind:            "incident",
		AuthMode:             "token_env",
		TargetListField:      "targets",
		TargetIdentityFields: []string{"account_id", "service_ids"},
		TargetAuthFields:     []string{"token_env"},
		Source: SourceSpec{
			ImportPath:        "github.com/eshu-hq/eshu/go/internal/collector/pagerduty",
			PackageName:       "pagerduty",
			ConfigType:        "pagerduty.SourceConfig",
			Constructor:       "pagerduty.NewClaimedSource",
			ConfigLoader:      "loadPagerDutySourceConfig",
			ConfigAttacher:    "attachPagerDutyRuntimeSignals",
			RuntimeConfigType: "pagerDutyRuntimeConfiguration",
		},
	}

	files, err := Generate(manifest)
	if err != nil {
		t.Fatalf("Generate() error = %v, want nil", err)
	}
	if got, want := generatedFileNames(files), []string{"config.go", "main.go", "service.go"}; !sameStrings(got, want) {
		t.Fatalf("generated file names = %#v, want %#v", got, want)
	}

	mainFile := generatedFile(t, files, "main.go")
	for _, want := range []string{
		`const runtimeName = "collector-pagerduty"`,
		`buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-collector-pagerduty")`,
		`launchModeCassette    launchMode = "cassette"`,
		`opts, err := parseArgs(os.Args[1:])`,
		`if err := run(context.Background(), opts); err != nil {`,
		`runner, err = buildCassetteService(storeDB, opts.cassetteFile, tracer, instruments, logger)`,
		`app.NewHostedWithStatusServer(`,
		`StoreName:   "collector_pagerduty",`,
		`// Managed collector entrypoint. Update go/internal/collector/entrypoints/collector_entrypoints.yaml, then rerun scripts/generate-collector-entrypoints.sh.`,
	} {
		if !strings.Contains(mainFile, want) {
			t.Fatalf("generated main.go missing %q:\n%s", want, mainFile)
		}
	}

	configFile := generatedFile(t, files, "config.go")
	for _, want := range []string{
		`envCollectorInstanceID = "ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID"`,
		`sourceConfig, err := loadPagerDutySourceConfig(instance, getenv)`,
		`if heartbeatInterval >= claimLeaseTTL {`,
		`return "collector-pagerduty"`,
		`func selectPagerDutyInstance(`,
	} {
		if !strings.Contains(configFile, want) {
			t.Fatalf("generated config.go missing %q:\n%s", want, configFile)
		}
	}

	serviceFile := generatedFile(t, files, "service.go")
	for _, want := range []string{
		`"github.com/eshu-hq/eshu/go/internal/replay/cassette"`,
		`func buildCassetteService(`,
		`src, err := cassette.NewSource(cassettePath)`,
		`PollInterval: 24 * time.Hour,`,
		`attachPagerDutyRuntimeSignals(&config.Source, tracer, instruments)`,
		`func attachPagerDutyRuntimeSignals(config *pagerduty.SourceConfig, tracer trace.Tracer, instruments *telemetry.Instruments) {`,
		`source, err := pagerduty.NewClaimedSource(config.Source)`,
		`CollectorKind:       scope.CollectorPagerDuty,`,
		`MaxAttempts:         workflow.DefaultClaimMaxAttempts(),`,
		`return "pagerduty-claim-" + hex.EncodeToString(raw[:])`,
	} {
		if !strings.Contains(serviceFile, want) {
			t.Fatalf("generated service.go missing %q:\n%s", want, serviceFile)
		}
	}
}

func TestGenerateRejectsIncompleteManifest(t *testing.T) {
	_, err := Generate(Manifest{SchemaVersion: 1, RuntimeName: "collector-empty"})
	if err == nil {
		t.Fatal("Generate() error = nil, want manifest validation error")
	}
	if got := err.Error(); !strings.Contains(got, "binary_name") || !strings.Contains(got, "source.import_path") {
		t.Fatalf("Generate() error = %q, want missing field names", got)
	}
}

func generatedFile(t *testing.T, files []GeneratedFile, name string) string {
	t.Helper()
	for _, file := range files {
		if file.Name == name {
			return string(file.Contents)
		}
	}
	t.Fatalf("generated file %s not found in %#v", name, generatedFileNames(files))
	return ""
}

func generatedFileNames(files []GeneratedFile) []string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
	}
	return names
}

func sameStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
