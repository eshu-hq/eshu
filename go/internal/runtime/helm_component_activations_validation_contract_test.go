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
type activationMutation struct {
	name       string
	old, new   string
	wantOutput string
}

func TestHelmComponentActivationsValidation(t *testing.T) {
	t.Parallel()

	base := componentActivationsValues(pagerDutyActivationValues, jiraActivationValues)
	pagerDutyImage := "ghcr.io/example/collector-pagerduty@" + activationPagerDutyDigest
	tests := []activationMutation{
		{
			name:       "tag without digest",
			old:        pagerDutyImage,
			new:        "ghcr.io/example/collector-pagerduty:v0.1.0",
			wantOutput: "does not match pattern",
		},
		{
			name:       "tag next to a digest-shaped suffix",
			old:        pagerDutyImage,
			new:        "ghcr.io/example/collector-pagerduty:v0.1.0@sha256:1111",
			wantOutput: "does not match pattern",
		},
		{
			name:       "uppercase digest",
			old:        activationPagerDutyDigest,
			new:        strings.ToUpper(activationPagerDutyDigest),
			wantOutput: "does not match pattern",
		},
		{
			name:       "sha512 digest",
			old:        pagerDutyImage,
			new:        "ghcr.io/example/collector-pagerduty@sha512:" + strings.Repeat("a", 64),
			wantOutput: "does not match pattern",
		},
		{
			name:       "duplicate instance id",
			old:        "instanceId: jira-b",
			new:        "instanceId: pagerduty-a",
			wantOutput: `instanceId "pagerduty-a" is used more than once`,
		},
		{
			name:       "instance id with uppercase",
			old:        "instanceId: jira-b",
			new:        "instanceId: Jira_B",
			wantOutput: "does not match pattern",
		},
		{
			name:       "instance id too long",
			old:        "instanceId: jira-b",
			new:        "instanceId: " + strings.Repeat("a", 31),
			wantOutput: "maxLength: got 31, want 30",
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
			name:       "empty grant schema versions",
			old:        `schemaVersions: ["1.0.0", "1.1.0"]`,
			new:        `schemaVersions: []`,
			wantOutput: "minItems: got 0, want 1",
		},
		{
			name:       "grant kind with shell syntax",
			old:        "kind: incident_routing",
			new:        `kind: "x'; touch /tmp/pwn; echo '"`,
			wantOutput: "does not match pattern",
		},
		{
			name:       "grant scope with command substitution",
			old:        "scope: pagerduty\n          expiresIn",
			new:        "scope: \"$(touch /tmp/pwn)\"\n          expiresIn",
			wantOutput: "does not match pattern",
		},
		{
			name:       "grant lifetime not a duration",
			old:        "expiresIn: 360h",
			new:        "expiresIn: forever",
			wantOutput: "does not match pattern",
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
			name:       "credential-shaped config value",
			old:        "api_token_env: PAGERDUTY_API_TOKEN",
			new:        "note: ghp_abcdefghijklmnopqrstuvwxyz0123456789",
			wantOutput: "holds a credential-shaped value",
		},
		{
			name:       "unknown activation key",
			old:        "      replicas: 2\n",
			new:        "      replicas: 2\n      imageOverride: ghcr.io/example/x:latest\n",
			wantOutput: "additional properties 'imageOverride' not allowed",
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
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(base, tt.old) {
				t.Fatalf("fragment %q not found in base values", tt.old)
			}
			values := strings.Replace(base, tt.old, tt.new, 1)
			if values == base {
				t.Fatalf("mutation %q did not change the values", tt.name)
			}
			output := renderHelmChartFailure(t, "-f", writeComponentActivationValues(t, values))
			if !strings.Contains(output, tt.wantOutput) {
				t.Fatalf("helm template error = %q, want it to contain %q", output, tt.wantOutput)
			}
		})
	}
}
