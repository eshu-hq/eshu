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
      serviceAccount:
        create: true
        annotations:
          eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-aws-live
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

// TestHelmAWSActivationUsesDedicatedServiceAccount keeps the AWS workload
// identity on its activation pod instead of the coordinator's shared account.
func TestHelmAWSActivationUsesDedicatedServiceAccount(t *testing.T) {
	values := awsActivationValues(awsRedactionActivation, "awsCloudCollector:\n  redaction:\n    secretName: aws-redaction\n")
	manifests := renderComponentActivations(t, values)
	worker := requireHelmManifest(t, manifests, "Deployment", "eshu-cx-aws-live")
	if got := helmPodSpec(t, worker)["serviceAccountName"]; got != "eshu-cx-aws-live" {
		t.Fatalf("AWS worker serviceAccountName = %v, want eshu-cx-aws-live", got)
	}
	account := requireHelmManifest(t, manifests, "ServiceAccount", "eshu-cx-aws-live")
	annotations := helmMap(helmMap(account["metadata"])["annotations"])
	if got := annotations["eks.amazonaws.com/role-arn"]; got != "arn:aws:iam::123456789012:role/eshu-aws-live" {
		t.Fatalf("AWS activation IRSA annotation = %v", got)
	}
	for _, name := range []string{"eshu-cx-pagerduty-a", "eshu-workflow-coordinator"} {
		if got := helmPodSpec(t, requireHelmManifest(t, manifests, "Deployment", name))["serviceAccountName"]; got == "eshu-cx-aws-live" {
			t.Fatalf("%s received AWS service account", name)
		}
	}
}

func TestHelmAWSActivationUsesExistingServiceAccount(t *testing.T) {
	activation := strings.Replace(awsRedactionActivation,
		"      serviceAccount:\n        create: true\n        annotations:\n          eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-aws-live\n",
		"      serviceAccount:\n        name: aws-existing\n", 1)
	manifests := renderComponentActivations(t, awsActivationValues(activation, "awsCloudCollector:\n  redaction:\n    secretName: aws-redaction\n"))
	worker := requireHelmManifest(t, manifests, "Deployment", "eshu-cx-aws-live")
	if got := helmPodSpec(t, worker)["serviceAccountName"]; got != "aws-existing" {
		t.Fatalf("AWS worker serviceAccountName = %v, want aws-existing", got)
	}
	if helmManifestExists(manifests, "ServiceAccount", "aws-existing") {
		t.Fatal("chart created the operator-managed existing ServiceAccount")
	}
}

func TestHelmAWSActivationRejectsSharedServiceAccount(t *testing.T) {
	for _, name := range []string{"default", "eshu"} {
		t.Run(name, func(t *testing.T) {
			activation := strings.Replace(awsRedactionActivation,
				"      serviceAccount:\n        create: true\n        annotations:\n          eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-aws-live\n",
				"      serviceAccount:\n        name: "+name+"\n", 1)
			output := renderHelmChartFailure(t, "-f", writeComponentActivationValues(t, awsActivationValues(activation, "awsCloudCollector:\n  redaction:\n    secretName: aws-redaction\n")))
			if !strings.Contains(output, "shared or default ServiceAccount") {
				t.Fatalf("helm error %q does not reject shared service account", output)
			}
		})
	}
}

// TestHelmAWSActivationSensitiveLiveServicesNeedKey refuses a worker that
// would reach Lambda or ECS without its configured redaction Secret.
func TestHelmAWSActivationSensitiveLiveServicesNeedKey(t *testing.T) {
	nestedScope := strings.Replace(awsRedactionActivation,
		"        target_scopes:\n          account_id: \"123456789012\"\n        allowed_regions: [us-east-1]\n        allowed_services: [lambda, ecs]\n",
		"        target_scopes:\n          - account_id: \"123456789012\"\n            allowed_regions: [us-east-1]\n            allowed_services: [\" Lambda \", ECS]\n", 1)
	sourceServices := strings.Replace(awsRedactionActivation,
		"        allowed_services: [lambda, ecs]\n        source:\n",
		"        source:\n          services: [\" ECS \"]\n", 1)
	spaceInput := strings.Replace(awsRedactionActivation,
		"        source:\n          redactionKeyEnv:",
		"        source:\n          input: \"   \"\n          redactionKeyEnv:", 1)
	for _, tc := range []struct {
		name, activation, redaction, want string
	}{
		{"no Secret", awsRedactionActivation, "", "awsCloudCollector.redaction.secretName"},
		{"nested target scope and normalized services", nestedScope, "", "awsCloudCollector.redaction.secretName"},
		{"source service fallback", sourceServices, "", "awsCloudCollector.redaction.secretName"},
		{"whitespace input is live", spaceInput, "", "awsCloudCollector.redaction.secretName"},
		{"no key name", awsRedactionActivation, "awsCloudCollector:\n  redaction:\n    secretName: aws-redaction\n    keyKey: \"\"\n", "awsCloudCollector.redaction.keyKey"},
		{"no env reference", strings.Replace(awsRedactionActivation, "          redactionKeyEnv: ESHU_AWS_REDACTION_KEY\n", "", 1), "awsCloudCollector:\n  redaction:\n    secretName: aws-redaction\n", "source.redactionKeyEnv"},
		{"no dedicated service account", strings.Replace(awsRedactionActivation, "      serviceAccount:\n        create: true\n        annotations:\n          eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-aws-live\n", "", 1), "awsCloudCollector:\n  redaction:\n    secretName: aws-redaction\n", "serviceAccount"},
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
