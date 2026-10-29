// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"strings"
	"testing"
)

const awsRedactionActivation = `    - instanceId: aws-live
      componentId: dev.eshu.collector.aws
      componentVersion: 0.1.0
      seed:
        image: ghcr.io/example/collector-aws@sha256:3333333333333333333333333333333333333333333333333333333333333333
      host:
        sourceSystem: aws
        scope:
          id: aws:123456789012
          kind: aws_account
      process:
        args: ["--sdk-stdio"]
      config:
        target_scopes:
          account_id: "123456789012"
        allowed_regions: [us-east-1]
        allowed_services: [lambda, ecs]
        source:
          redactionKeyEnv: ESHU_AWS_REDACTION_KEY
`

func awsActivationValues(activation, redaction string) string {
	header := strings.Replace(componentActivationsHeader,
		"  allowIds: dev.example.collector.pagerduty,dev.example.collector.jira\n",
		"  allowIds: dev.example.collector.pagerduty,dev.example.collector.jira,dev.eshu.collector.aws\n", 1)
	return redaction + header + "  activations:\n" + activation + pagerDutyActivationValues
}

// TestHelmAWSActivationRedactionKeyDelivery pins the Kubernetes Secret to the
// worker that launches the AWS satellite, without exposing it to other workers
// or the coordinator's activation ConfigMap.
func TestHelmAWSActivationRedactionKeyDelivery(t *testing.T) {
	values := awsActivationValues(awsRedactionActivation, "awsCloudCollector:\n  redaction:\n    secretName: aws-redaction\n    keyKey: hmac-key\n")
	manifests := renderComponentActivations(t, values)
	worker := requireHelmContainer(t, requireHelmManifest(t, manifests, "Deployment", "eshu-cx-aws-live"), "component-extension-collector")
	entry, ok := helmEnvByName(worker)["ESHU_AWS_REDACTION_KEY"]
	if !ok {
		t.Fatal("AWS worker has no ESHU_AWS_REDACTION_KEY")
	}
	ref := helmMap(helmMap(entry["valueFrom"])["secretKeyRef"])
	if ref["name"] != "aws-redaction" || ref["key"] != "hmac-key" || ref["optional"] == true {
		t.Fatalf("AWS worker redaction Secret reference = %#v", ref)
	}
	for _, name := range []string{"eshu-cx-pagerduty-a", "eshu-workflow-coordinator"} {
		deployment := requireHelmManifest(t, manifests, "Deployment", name)
		containerName := "component-extension-collector"
		if name == "eshu-workflow-coordinator" {
			containerName = "workflow-coordinator"
		}
		if _, leaked := helmEnvByName(requireHelmContainer(t, deployment, containerName))["ESHU_AWS_REDACTION_KEY"]; leaked {
			t.Fatalf("%s received the AWS redaction key", name)
		}
	}
	config := requireHelmManifest(t, manifests, "ConfigMap", "eshu-component-activations")
	file := helmString(helmMap(config["data"])["aws-live.yaml"])
	if !strings.Contains(file, "redactionKeyEnv: ESHU_AWS_REDACTION_KEY") || strings.Contains(file, "aws-redaction") || strings.Contains(file, "hmac-key") {
		t.Fatalf("AWS activation config contains Secret metadata or lacks the env reference: %s", file)
	}
}

// TestHelmAWSActivationSensitiveLiveServicesNeedKey refuses a worker that
// would reach Lambda or ECS without its configured redaction Secret.
func TestHelmAWSActivationSensitiveLiveServicesNeedKey(t *testing.T) {
	for _, tc := range []struct {
		name, activation, redaction, want string
	}{
		{"no Secret", awsRedactionActivation, "", "awsCloudCollector.redaction.secretName"},
		{"no key name", awsRedactionActivation, "awsCloudCollector:\n  redaction:\n    secretName: aws-redaction\n    keyKey: \"\"\n", "awsCloudCollector.redaction.keyKey"},
		{"no env reference", strings.Replace(awsRedactionActivation, "          redactionKeyEnv: ESHU_AWS_REDACTION_KEY\n", "", 1), "awsCloudCollector:\n  redaction:\n    secretName: aws-redaction\n", "source.redactionKeyEnv"},
		{"plaintext process env", strings.Replace(awsRedactionActivation, "        args: [\"--sdk-stdio\"]\n", "        args: [\"--sdk-stdio\"]\n        env: [\"ESHU_AWS_REDACTION_KEY=plaintext\"]\n", 1), "awsCloudCollector:\n  redaction:\n    secretName: aws-redaction\n", "process.env must not carry ESHU_AWS_REDACTION_KEY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := renderHelmChartFailure(t, "-f", writeComponentActivationValues(t, awsActivationValues(tc.activation, tc.redaction)))
			if !strings.Contains(output, tc.want) {
				t.Fatalf("helm error %q does not mention %q", output, tc.want)
			}
		})
	}
}

func TestHelmAWSActivationOtherModesDoNotRequireRedactionKey(t *testing.T) {
	for _, tc := range []struct {
		name, activation string
	}{
		{"non-sensitive live", strings.Replace(awsRedactionActivation, "allowed_services: [lambda, ecs]", "allowed_services: [ec2, s3]", 1)},
		{"fixture mode", strings.Replace(awsRedactionActivation, "          redactionKeyEnv: ESHU_AWS_REDACTION_KEY\n", "          input: /tmp/fixture.json\n", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifests := renderComponentActivations(t, awsActivationValues(tc.activation, ""))
			worker := requireHelmContainer(t, requireHelmManifest(t, manifests, "Deployment", "eshu-cx-aws-live"), "component-extension-collector")
			if _, ok := helmEnvByName(worker)["ESHU_AWS_REDACTION_KEY"]; ok {
				t.Fatal("AWS worker has a redaction key with no configured Secret")
			}
		})
	}
}
