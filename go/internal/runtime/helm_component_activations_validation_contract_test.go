// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"strings"
	"testing"
)

// activationMutation rewrites one fragment of the two-activation values and
// names the error the chart must raise. Every rewrite is checked to have
// changed the values, so a typo in a fragment cannot turn a case into a pass.
//
// The schema duplicates many template guards, and it runs first, so a case that
// renders with the schema on can pass while the template guard is gone. Each
// case therefore renders twice: once as installed (wantOutput) and once with
// --skip-schema-validation, where only the template can reject it
// (wantTemplateOutput, or wantOutput when that is empty). schemaOnly marks a
// rule the template does not own.
type activationMutation struct {
	name               string
	old, new           string
	wantOutput         string
	wantTemplateOutput string
	schemaOnly         bool
}

func TestHelmComponentActivationsValidation(t *testing.T) {
	t.Parallel()

	base := componentActivationsValues(pagerDutyActivationValues, jiraActivationValues)
	pagerDutyImage := "ghcr.io/example/collector-pagerduty@" + activationPagerDutyDigest
	tests := []activationMutation{
		{
			name:               "tag without digest",
			old:                pagerDutyImage,
			new:                "ghcr.io/example/collector-pagerduty:v0.1.0",
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "must be pinned by digest",
		},
		{
			name:               "tag next to a digest-shaped suffix",
			old:                pagerDutyImage,
			new:                "ghcr.io/example/collector-pagerduty:v0.1.0@sha256:1111",
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "must be pinned by digest",
		},
		{
			name:               "uppercase digest",
			old:                activationPagerDutyDigest,
			new:                strings.ToUpper(activationPagerDutyDigest),
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "must be pinned by digest",
		},
		{
			name:               "sha512 digest",
			old:                pagerDutyImage,
			new:                "ghcr.io/example/collector-pagerduty@sha512:" + strings.Repeat("a", 64),
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "must be pinned by digest",
		},
		{
			name:       "duplicate instance id",
			old:        "instanceId: jira-b",
			new:        "instanceId: pagerduty-a",
			wantOutput: `instanceId "pagerduty-a" is used more than once`,
		},
		{
			name:               "instance id with uppercase",
			old:                "instanceId: jira-b",
			new:                "instanceId: Jira_B",
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "must be a lowercase DNS label",
		},
		{
			name:               "instance id too long",
			old:                "instanceId: jira-b",
			new:                "instanceId: " + strings.Repeat("a", 31),
			wantOutput:         "maxLength: got 31, want 30",
			wantTemplateOutput: "must be at most 30 characters",
		},
		{
			name:       "missing component id",
			old:        "      componentId: dev.example.collector.jira\n",
			new:        "",
			wantOutput: "componentId",
		},
		{
			name:       "component id not allowlisted",
			old:        "componentId: dev.example.collector.jira",
			new:        "componentId: dev.example.collector.other",
			wantOutput: `componentId "dev.example.collector.other" must be listed in componentExtensionCollector.allowIds`,
		},
		{
			name:       "missing component version",
			old:        "      componentVersion: 0.2.0\n",
			new:        "",
			wantOutput: "componentVersion",
		},
		{
			name:       "missing host scope",
			old:        "        scope:\n          id: jira:site:example.atlassian.net\n          kind: jira_site\n",
			new:        "",
			wantOutput: "scope",
		},
		{
			name:       "missing seed",
			old:        "      seed:\n        image: ghcr.io/example/collector-jira@" + activationJiraDigest + "\n",
			new:        "",
			wantOutput: "seed",
		},
		{
			name:       "same component at two versions",
			old:        "componentId: dev.example.collector.jira\n      componentVersion: 0.2.0",
			new:        "componentId: dev.example.collector.pagerduty\n      componentVersion: 0.3.0",
			wantOutput: "one component ID must use one version",
		},
		{
			name:               "empty grant schema versions",
			old:                `schemaVersions: ["1.0.0", "1.1.0"]`,
			new:                `schemaVersions: []`,
			wantOutput:         "minItems: got 0, want 1",
			wantTemplateOutput: "must list at least one schema version",
		},
		{
			name:               "grant kind with shell syntax",
			old:                "kind: incident.record",
			new:                `kind: "x'; touch /tmp/pwn; echo '"`,
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "kind is required",
		},
		{
			name:               "grant scope with command substitution",
			old:                "scope: pagerduty\n          expiresIn",
			new:                "scope: \"$(touch /tmp/pwn)\"\n          expiresIn",
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "scope is required",
		},
		{
			name:               "grant lifetime not a duration",
			old:                "expiresIn: 360h",
			new:                "expiresIn: forever",
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "expiresIn must be a positive Go duration",
		},
		{
			name:       "seed command with shell syntax",
			old:        "      seed:\n        image: ghcr.io/example/collector-jira@" + activationJiraDigest,
			new:        "      seed:\n        command: [\"/collector\", \"seed; rm -rf /\"]\n        image: ghcr.io/example/collector-jira@" + activationJiraDigest,
			wantOutput: "seed.command entries may only contain",
		},
		{
			name:       "credential key in config",
			old:        "api_token_env: PAGERDUTY_API_TOKEN",
			new:        "api_token: abc123",
			wantOutput: "looks like a credential",
		},
		{
			name:       "nested credential key in config",
			old:        "api_token_env: PAGERDUTY_API_TOKEN",
			new:        "auth:\n          password: hunter2",
			wantOutput: "looks like a credential",
		},
		{
			name:       "credential in process env",
			old:        `args: ["--sdk-stdio"]`,
			new:        `args: ["--sdk-stdio"]` + "\n        env: [\"PAGERDUTY_API_TOKEN=abc123\"]",
			wantOutput: "process.env entry",
		},
		{
			name:       "credential-shaped config value",
			old:        "api_token_env: PAGERDUTY_API_TOKEN",
			new:        "note: ghp_abcdefghijklmnopqrstuvwxyz0123456789",
			wantOutput: "holds a credential-shaped value",
		},
		{
			name:       "heartbeat equal to lease",
			old:        "      replicas: 2\n",
			new:        "      replicas: 2\n      claimLeaseTTL: 5m\n      heartbeatInterval: 5m\n",
			wantOutput: "heartbeatInterval 5m must be less than claimLeaseTTL 5m",
		},
		{
			name:       "heartbeat above the default lease",
			old:        "      replicas: 2\n",
			new:        "      replicas: 2\n      heartbeatInterval: 2m\n",
			wantOutput: "heartbeatInterval 2m must be less than claimLeaseTTL 60s",
		},
		{
			name:       "global heartbeat above the default lease",
			old:        "  pollInterval: 13s\n",
			new:        "  pollInterval: 13s\n  heartbeatInterval: 90s\n",
			wantOutput: "heartbeatInterval 90s must be less than claimLeaseTTL 60s",
		},
		{
			name:               "lease in milliseconds",
			old:                "      replicas: 2\n",
			new:                "      replicas: 2\n      claimLeaseTTL: 500ms\n",
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "must be a positive duration written with h, m and s",
		},
		{
			name:               "tag plus digest",
			old:                pagerDutyImage,
			new:                "ghcr.io/example/collector-pagerduty:1.0@" + activationPagerDutyDigest,
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "must be pinned by digest",
		},
		{
			name:               "registry port plus tag plus digest",
			old:                pagerDutyImage,
			new:                "registry.example:5000/collector-pagerduty:1.0@" + activationPagerDutyDigest,
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "must be pinned by digest",
		},
		{
			name:               "single-component name with tag plus digest",
			old:                pagerDutyImage,
			new:                "collector:10@" + activationPagerDutyDigest,
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "must be pinned by digest",
		},
		{
			name:               "instance id with underscore",
			old:                "instanceId: jira-b",
			new:                "instanceId: jira_b",
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "must be a lowercase DNS label",
		},
		{
			name:               "grant lifetime of zero",
			old:                "expiresIn: 360h",
			new:                "expiresIn: 0s",
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "expiresIn must be a positive Go duration",
		},
		{
			name:               "grant lifetime with a zero first component",
			old:                "expiresIn: 360h",
			new:                "expiresIn: 0h30m",
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "expiresIn must be a positive Go duration",
		},
		{
			name:       "resource name too long for the metrics service",
			old:        "contentStore:\n",
			new:        "fullnameOverride: " + strings.Repeat("r", 45) + "\ncontentStore:\n",
			wantOutput: "longer than 55 characters",
		},
		{
			name:       "hostile pull policy",
			old:        "      seed:\n        image: ghcr.io/example/collector-jira@" + activationJiraDigest,
			new:        "      seed:\n        pullPolicy: \"Always\\n  workingDir: /evil\"\n        image: ghcr.io/example/collector-jira@" + activationJiraDigest,
			wantOutput: "pullPolicy",
		},
		{
			name:       "hostile replicas",
			old:        "      replicas: 2\n",
			new:        "      replicas: \"2\\n  paused: true\"\n",
			wantOutput: "replicas",
		},
		{
			name:               "hostile host source system",
			old:                "sourceSystem: jira",
			new:                "sourceSystem: \"jira\\n  evil: true\"",
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "host.sourceSystem is required",
		},
		{
			name:               "hostile host scope id",
			old:                "id: jira:site:example.atlassian.net",
			new:                "id: \"x'; touch /tmp/pwn; echo '\"",
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "host.scope.id is required",
		},
		{
			name:               "hostile host scope kind",
			old:                "kind: jira_site",
			new:                "kind: \"Jira Site\"",
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "host.scope.kind is required",
		},
		{
			name:               "hostile mode",
			old:                "      replicas: 2\n",
			new:                "      replicas: 2\n      mode: \"scheduled\\n  evil: true\"\n",
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "mode must be a lowercase identifier",
		},
		{
			name:               "hostile component version",
			old:                "componentVersion: 0.2.0",
			new:                "componentVersion: \"0.2.0'; touch /tmp/pwn; echo '\"",
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "componentVersion is required",
		},
		{
			name:               "hostile grant schema version",
			old:                `schemaVersions: ["1.0.0", "1.1.0"]`,
			new:                `schemaVersions: ["1.0.0' --scope 'other"]`,
			wantOutput:         "does not match pattern",
			wantTemplateOutput: "must be major.minor.patch",
		},
		{
			name:       "credential-shaped value in process env",
			old:        `args: ["--sdk-stdio"]`,
			new:        `args: ["--sdk-stdio"]` + "\n        env: [\"FOO=ghp_abcdefghijklmnopqrstuvwxyz0123456789\"]",
			wantOutput: "process.env entry",
		},
		{
			name:       "github fine-grained token in process env",
			old:        `args: ["--sdk-stdio"]`,
			new:        `args: ["--sdk-stdio"]` + "\n        env: [\"FOO=github_pat_abcdefghijklmnopqrstuvwxyz\"]",
			wantOutput: "process.env entry",
		},
		{
			name:       "slack token in process env",
			old:        `args: ["--sdk-stdio"]`,
			new:        `args: ["--sdk-stdio"]` + "\n        env: [\"FOO=xoxb-1234567890-abcdef\"]",
			wantOutput: "process.env entry",
		},
		{
			name:       "aws access key in process env",
			old:        `args: ["--sdk-stdio"]`,
			new:        `args: ["--sdk-stdio"]` + "\n        env: [\"FOO=AKIAABCDEFGHIJKLMNOP\"]",
			wantOutput: "process.env entry",
		},
		{
			name:       "secret key prefix in process env",
			old:        `args: ["--sdk-stdio"]`,
			new:        `args: ["--sdk-stdio"]` + "\n        env: [\"FOO=sk-abcdefghijklmnopqrstuvwxyz\"]",
			wantOutput: "process.env entry",
		},
		{
			name:       "private key block in process env",
			old:        `args: ["--sdk-stdio"]`,
			new:        `args: ["--sdk-stdio"]` + "\n        env: [\"FOO=-----BEGIN PRIVATE KEY-----\"]",
			wantOutput: "process.env entry",
		},
		{
			name:       "unknown activation key",
			old:        "      replicas: 2\n",
			new:        "      replicas: 2\n      imageOverride: ghcr.io/example/x:latest\n",
			wantOutput: "additional properties 'imageOverride' not allowed",
			schemaOnly: true,
		},
	}
	runActivationMutations(t, base, tests)
}

func TestHelmComponentActivationsRejectLegacyKeysAlongside(t *testing.T) {
	t.Parallel()

	base := componentActivationsValues(pagerDutyActivationValues, jiraActivationValues)
	tests := []activationMutation{
		{
			name:       "top-level instance id",
			old:        "  pollInterval: 13s\n",
			new:        "  pollInterval: 13s\n  instanceId: legacy\n",
			wantOutput: "componentExtensionCollector.instanceId cannot be set together with componentExtensionCollector.activations",
		},
		{
			name:       "top-level extra volumes",
			old:        "  pollInterval: 13s\n",
			new:        "  pollInterval: 13s\n  extraVolumes:\n    - name: legacy\n      emptyDir: {}\n",
			wantOutput: "extraVolumes and extraVolumeMounts cannot be set together",
		},
		{
			name: "top-level extra env",
			old:  "  pollInterval: 13s\n",
			new: "  pollInterval: 13s\n  extraEnv:\n    - name: SHARED\n      valueFrom:\n" +
				"        secretKeyRef:\n          name: shared\n          key: token\n",
			wantOutput: "extraEnv cannot be set together",
		},
	}
	runActivationMutations(t, base, tests)
}

func runActivationMutations(t *testing.T, base string, tests []activationMutation) {
	t.Helper()

	for _, tt := range tests {
		mutated := func(t *testing.T) string {
			t.Helper()
			if !strings.Contains(base, tt.old) {
				t.Fatalf("fragment %q not found in base values", tt.old)
			}
			values := strings.Replace(base, tt.old, tt.new, 1)
			if values == base {
				t.Fatalf("mutation %q did not change the values", tt.name)
			}
			return writeComponentActivationValues(t, values)
		}
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			output := renderHelmChartFailure(t, "-f", mutated(t))
			if !strings.Contains(output, tt.wantOutput) {
				t.Fatalf("helm template error = %q, want it to contain %q", output, tt.wantOutput)
			}
		})
		if tt.schemaOnly {
			continue
		}
		t.Run(tt.name+" (template only)", func(t *testing.T) {
			t.Parallel()

			want := tt.wantTemplateOutput
			if want == "" {
				want = tt.wantOutput
			}
			output := renderHelmChartFailure(t, "--skip-schema-validation", "-f", mutated(t))
			if !strings.Contains(output, want) {
				t.Fatalf("helm template --skip-schema-validation error = %q, want it to contain %q", output, want)
			}
		})
	}
}

// TestHelmComponentActivationsAcceptPlainProcessEnv is the GREEN half of the
// process.env token scan: an ordinary KEY=VALUE entry, and a value that only
// contains a token prefix mid-string, must render.
func TestHelmComponentActivationsAcceptPlainProcessEnv(t *testing.T) {
	t.Parallel()

	values := componentActivationsValues(
		strings.Replace(pagerDutyActivationValues, `args: ["--sdk-stdio"]`,
			`args: ["--sdk-stdio"]`+"\n        env: [\"FOO=bar\", \"MODE=sandbox\", \"NOTE=see ghp_ docs\"]", 1),
		jiraActivationValues,
	)
	manifests := renderComponentActivations(t, values)
	config := helmString(helmMap(requireHelmManifest(t, manifests, "ConfigMap", "eshu-component-activations")["data"])["pagerduty-a.yaml"])
	for _, want := range []string{"FOO=bar", "MODE=sandbox"} {
		if !strings.Contains(config, want) {
			t.Fatalf("pagerduty-a activation config missing %q:\n%s", want, config)
		}
	}
}
