// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// TestFactSchemaKindsMatchWireFactKinds locks each contracts-module fact-kind
// constant (factschema.FactKind*) to the wire fact-kind constant the collector
// emits and the reducer loads (facts.*FactKind). The contracts module is a
// standalone module that cannot import go/internal/facts, so it duplicates the
// wire strings as its own constants; this reducer-side test — which CAN import
// both packages — is the drift lock that keeps the two byte-equal.
//
// Without this lock a typo or a namespaced value (for example the Wave-1
// scaffold's "aws.resource" against the real "aws_resource") would make a Decode
// dispatch silently never match a loaded envelope: no error, no dead letter,
// just a fact kind that is never decoded. Every new decoded kind MUST add a row
// here so the mismatch is a test failure at authoring time.
func TestFactSchemaKindsMatchWireFactKinds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		contract string
		wireKind string
	}{
		{"aws_resource", factschema.FactKindAWSResource, facts.AWSResourceFactKind},
		{"aws_relationship", factschema.FactKindAWSRelationship, facts.AWSRelationshipFactKind},
		{"aws_security_group_rule", factschema.FactKindAWSSecurityGroupRule, facts.AWSSecurityGroupRuleFactKind},
		{"ec2_instance_posture", factschema.FactKindEC2InstancePosture, facts.EC2InstancePostureFactKind},
		{"s3_bucket_posture", factschema.FactKindS3BucketPosture, facts.S3BucketPostureFactKind},
		{"aws_iam_permission", factschema.FactKindAWSIAMPermission, facts.AWSIAMPermissionFactKind},
		{"aws_resource_policy_permission", factschema.FactKindAWSResourcePolicyPermission, facts.AWSResourcePolicyPermissionFactKind},
		{"aws_iam_principal", factschema.FactKindAWSIAMPrincipal, facts.AWSIAMPrincipalFactKind},
		{"gcp_cloud_resource", factschema.FactKindGCPCloudResource, facts.GCPCloudResourceFactKind},
		{"gcp_cloud_relationship", factschema.FactKindGCPCloudRelationship, facts.GCPCloudRelationshipFactKind},
		{"gcp_collection_warning", factschema.FactKindGCPCollectionWarning, facts.GCPCollectionWarningFactKind},
		{"gcp_dns_record", factschema.FactKindGCPDNSRecord, facts.GCPDNSRecordFactKind},
		{"gcp_iam_policy_observation", factschema.FactKindGCPIAMPolicyObservation, facts.GCPIAMPolicyObservationFactKind},
		{"kubernetes_live.pod_template", factschema.FactKindKubernetesLivePodTemplate, facts.KubernetesPodTemplateFactKind},
		{"kubernetes_live.relationship", factschema.FactKindKubernetesLiveRelationship, facts.KubernetesRelationshipFactKind},
		{"kubernetes_live.warning", factschema.FactKindKubernetesLiveWarning, facts.KubernetesWarningFactKind},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.contract != tc.wireKind {
				t.Fatalf("factschema constant %q != wire fact kind facts constant %q; the contracts-module fact-kind string has drifted from the reducer's wire kind and Decode dispatch will silently never match", tc.contract, tc.wireKind)
			}
		})
	}
}

// TestPartitionDecodeFailures locks the single fault-isolation boundary every
// batch extractor routes decode errors through. It is the linchpin of the
// per-fact isolation design: get this classifier wrong and either a malformed
// fact aborts the whole intent (regression) or a transient/graph/projection
// error is silently swallowed as a quarantine (the "swallow failures" sin the
// Life Motto forbids). The two branches must stay exactly as asserted here.
func TestPartitionDecodeFailures(t *testing.T) {
	t.Parallel()

	env := facts.Envelope{FactID: "fact-123", FactKind: facts.AWSResourceFactKind}

	t.Run("input_invalid decode error is quarantined", func(t *testing.T) {
		t.Parallel()

		// A *factDecodeError classified input_invalid (a missing required field)
		// is the ONLY quarantinable error: the extractor skips the one fact and
		// keeps projecting the rest.
		decodeErr := newFactDecodeError(factschema.FactKindAWSResource, &factschema.DecodeError{
			FactKind:       factschema.FactKindAWSResource,
			Classification: factschema.ClassificationInputInvalid,
			Field:          "account_id",
		})

		q, ok, fatal := partitionDecodeFailures(env, decodeErr)
		if !ok {
			t.Fatal("ok = false; an input_invalid *factDecodeError must be quarantinable")
		}
		if fatal != nil {
			t.Fatalf("fatal = %v, want nil for a quarantinable error", fatal)
		}
		if q.factID != "fact-123" || q.factKind != facts.AWSResourceFactKind {
			t.Fatalf("quarantined fact identity = {%q,%q}, want {fact-123, %q}", q.factID, q.factKind, facts.AWSResourceFactKind)
		}
		if q.field != "account_id" {
			t.Fatalf("quarantined field = %q, want account_id", q.field)
		}
		if q.classification != factschema.ClassificationInputInvalid {
			t.Fatalf("quarantined classification = %q, want %q", q.classification, factschema.ClassificationInputInvalid)
		}
	})

	t.Run("plain non-decode error stays fatal", func(t *testing.T) {
		t.Parallel()

		// A transient fact-load / graph-write / projection error is NOT a
		// *factDecodeError: it must be returned unchanged so the handler fails the
		// whole intent and the durable queue triages it (retry / dependency /
		// projection bug), never silently dropped as a quarantine.
		sentinel := errors.New("transient graph write failure")

		q, ok, fatal := partitionDecodeFailures(env, sentinel)
		if ok {
			t.Fatal("ok = true; a non-decode error must NOT be quarantined")
		}
		if !errors.Is(fatal, sentinel) {
			t.Fatalf("fatal = %v, want the original error returned unchanged", fatal)
		}
		if (q != quarantinedFact{}) {
			t.Fatalf("quarantinedFact = %+v, want zero value for a fatal error", q)
		}
	})

	t.Run("wrapped non-input_invalid decode error stays fatal", func(t *testing.T) {
		t.Parallel()

		// A *factDecodeError whose classification is NOT input_invalid (for
		// example a future unsupported-major or schema-mismatch class) is terminal
		// but not a per-fact quarantine: it must stay fatal so the operator sees
		// the real classification rather than a mislabeled input_invalid skip.
		decodeErr := newFactDecodeError(factschema.FactKindAWSResource, &factschema.DecodeError{
			FactKind:       factschema.FactKindAWSResource,
			Classification: "schema_mismatch",
			Err:            fmt.Errorf("unexpected shape"),
		})

		_, ok, fatal := partitionDecodeFailures(env, decodeErr)
		if ok {
			t.Fatal("ok = true; only ClassificationInputInvalid is quarantinable")
		}
		if fatal == nil {
			t.Fatal("fatal = nil; a non-input_invalid decode error must stay fatal")
		}
	})

	t.Run("unsupported schema major stays fatal even when labeled input_invalid", func(t *testing.T) {
		t.Parallel()

		// The contracts module currently labels an unsupported schema major
		// input_invalid, but it is version skew, not a malformed individual
		// payload. partitionDecodeFailures excludes the ErrUnsupportedSchemaMajor
		// sentinel from the quarantine path so a schema-rollout / version-skew fact
		// fails the whole work item for durable triage (it can succeed once the
		// reducer supports the major) rather than being silently skipped per-fact
		// as if the collector had dropped a required field.
		decodeErr := newFactDecodeError(factschema.FactKindAWSResource, &factschema.DecodeError{
			FactKind:       factschema.FactKindAWSResource,
			Classification: factschema.ClassificationInputInvalid,
			Err:            fmt.Errorf("%w: %q", factschema.ErrUnsupportedSchemaMajor, "2.0.0"),
		})

		q, ok, fatal := partitionDecodeFailures(env, decodeErr)
		if ok {
			t.Fatal("ok = true; an unsupported schema major must NOT be quarantined per-fact")
		}
		if fatal == nil {
			t.Fatal("fatal = nil; an unsupported schema major must stay fatal for durable triage")
		}
		if !errors.Is(fatal, factschema.ErrUnsupportedSchemaMajor) {
			t.Fatalf("fatal = %v, want it to wrap ErrUnsupportedSchemaMajor", fatal)
		}
		if (q != quarantinedFact{}) {
			t.Fatalf("quarantinedFact = %+v, want zero value for a fatal error", q)
		}
	})
}
