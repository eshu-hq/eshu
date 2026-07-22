// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/graphowner"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

type canonicalGraphWriters struct {
	// cloudResourceNode / ec2InstanceNode / kubernetesWorkloadNode are wrapped
	// in the #5007 owner-ledger gate (graphowner) so a shared cross-scope node's
	// scope-derived properties resolve deterministically to the max-order-key
	// contributor. A nil-ledger gate writes through unchanged.
	cloudResourceNode             *graphowner.CloudResourceGatedWriter
	ec2InstanceNode               *graphowner.EC2InstanceGatedWriter
	cloudResourceEdge             *sourcecypher.CloudResourceEdgeWriter
	gcpCloudResourceEdge          *sourcecypher.GCPCloudResourceEdgeWriter
	azureCloudResourceEdge        *sourcecypher.AzureCloudResourceEdgeWriter
	workloadCloudRelationshipEdge *sourcecypher.WorkloadCloudRelationshipWriter
	kubernetesWorkloadNode        *graphowner.KubernetesWorkloadGatedWriter
	kubernetesNamespaceNode       *sourcecypher.KubernetesNamespaceNodeWriter
	securityGroupEndpointNode     *sourcecypher.SecurityGroupEndpointNodeWriter
	securityGroupReachability     *sourcecypher.SecurityGroupReachabilityWriter
	kubernetesCorrelationEdge     *sourcecypher.KubernetesCorrelationEdgeWriter
	crossplaneSatisfiedByEdge     *sourcecypher.CrossplaneSatisfiedByEdgeWriter
	iamEscalationEdge             *sourcecypher.IAMEscalationEdgeWriter
	iamCanPerformEdge             *sourcecypher.IAMCanPerformEdgeWriter
	observabilityCoverageEdge     *sourcecypher.ObservabilityCoverageEdgeWriter
	incidentRoutingEvidence       *sourcecypher.IncidentRoutingEvidenceWriter
	codeTaintEvidence             *sourcecypher.CodeTaintEvidenceWriter
	codeInterprocEvidence         *sourcecypher.CodeInterprocEvidenceWriter
	iamCanAssumeEdge              *sourcecypher.IAMCanAssumeEdgeWriter
	s3LogsToEdge                  *sourcecypher.S3LogsToEdgeWriter
	s3ExternalPrincipalGrant      *sourcecypher.S3ExternalPrincipalGrantWriter
	// rdsPostureNode / ec2InternetExposureNode / ec2BlockDeviceKMSPostureNode /
	// s3InternetExposureNode are wrapped in the #5062 lock-only gate
	// (graphowner.LockOnlyGate) so their SET/REMOVE writes to shared
	// CloudResource nodes serialize against the #5007/#5066 owner-ledger gate's
	// base-property writes on the SAME uid, under the identical Postgres
	// advisory-lock key. A nil-ledger gate writes through unchanged.
	rdsPostureNode               *graphowner.RDSPostureLockedWriter
	ec2UsesProfileEdge           *sourcecypher.EC2UsesProfileEdgeWriter
	iamInstanceProfileRoleEdge   *sourcecypher.IAMInstanceProfileRoleEdgeWriter
	ec2InternetExposureNode      *graphowner.EC2InternetExposureLockedWriter
	ec2BlockDeviceKMSPostureNode *graphowner.EC2BlockDeviceKMSPostureLockedWriter
	s3InternetExposureNode       *graphowner.S3InternetExposureLockedWriter
}

// newCanonicalGraphWriters wires the canonical graph writers. reader backs
// the four posture node writers' pre-write CloudResource-existence check
// (issue #5652: a bare-MATCH-anchored UNWIND SET silently drops its write on
// the pinned production NornicDB image, so those writers now confirm a uid
// exists via a separate read before running a MERGE-anchored write) — it is
// typically the same query.GraphQuery graph-read port already wired for other
// reducer read paths.
func newCanonicalGraphWriters(exec sourcecypher.Executor, reader sourcecypher.PostureExistenceReader, batchSize int, ownerGate *graphowner.Gate, lockGate *graphowner.LockOnlyGate) canonicalGraphWriters {
	rawCloudResourceNode := sourcecypher.NewCloudResourceNodeWriter(exec, batchSize)
	rawEC2InstanceNode := sourcecypher.NewEC2InstanceNodeWriter(exec, batchSize)
	rawKubernetesWorkloadNode := sourcecypher.NewKubernetesWorkloadNodeWriter(exec, batchSize)
	kubernetesNamespaceNode := sourcecypher.NewKubernetesNamespaceNodeWriter(exec, batchSize)
	rawRDSPostureNode := sourcecypher.NewRDSPostureNodeWriter(exec, reader, batchSize)
	rawEC2InternetExposureNode := sourcecypher.NewEC2InternetExposureNodeWriter(exec, reader, batchSize)
	rawEC2BlockDeviceKMSPostureNode := sourcecypher.NewEC2BlockDeviceKMSPostureNodeWriter(exec, reader, batchSize)
	rawS3InternetExposureNode := sourcecypher.NewS3InternetExposureNodeWriter(exec, reader, batchSize)
	return canonicalGraphWriters{
		cloudResourceNode:             graphowner.NewCloudResourceGatedWriter(ownerGate, rawCloudResourceNode.WriteCloudResourceNodes),
		ec2InstanceNode:               graphowner.NewEC2InstanceGatedWriter(ownerGate, rawEC2InstanceNode.WriteEC2InstanceNodes),
		cloudResourceEdge:             sourcecypher.NewCloudResourceEdgeWriter(exec, batchSize),
		gcpCloudResourceEdge:          sourcecypher.NewGCPCloudResourceEdgeWriter(exec, batchSize),
		azureCloudResourceEdge:        sourcecypher.NewAzureCloudResourceEdgeWriter(exec, batchSize),
		workloadCloudRelationshipEdge: sourcecypher.NewWorkloadCloudRelationshipWriter(exec, batchSize),
		kubernetesWorkloadNode:        graphowner.NewKubernetesWorkloadGatedWriter(ownerGate, rawKubernetesWorkloadNode.WriteKubernetesWorkloadNodes),
		kubernetesNamespaceNode:       kubernetesNamespaceNode,
		securityGroupEndpointNode:     sourcecypher.NewSecurityGroupEndpointNodeWriter(exec, batchSize),
		securityGroupReachability:     sourcecypher.NewSecurityGroupReachabilityWriter(exec, batchSize),
		kubernetesCorrelationEdge:     sourcecypher.NewKubernetesCorrelationEdgeWriter(exec, batchSize),
		crossplaneSatisfiedByEdge:     sourcecypher.NewCrossplaneSatisfiedByEdgeWriter(exec, batchSize),
		iamEscalationEdge:             sourcecypher.NewIAMEscalationEdgeWriter(exec, batchSize),
		iamCanPerformEdge:             sourcecypher.NewIAMCanPerformEdgeWriter(exec, batchSize),
		observabilityCoverageEdge:     sourcecypher.NewObservabilityCoverageEdgeWriter(exec, batchSize),
		incidentRoutingEvidence:       sourcecypher.NewIncidentRoutingEvidenceWriter(exec, batchSize),
		codeTaintEvidence:             sourcecypher.NewCodeTaintEvidenceWriter(exec, batchSize),
		codeInterprocEvidence:         sourcecypher.NewCodeInterprocEvidenceWriter(exec, batchSize),
		iamCanAssumeEdge:              sourcecypher.NewIAMCanAssumeEdgeWriter(exec, batchSize),
		s3LogsToEdge:                  sourcecypher.NewS3LogsToEdgeWriter(exec, batchSize),
		s3ExternalPrincipalGrant:      sourcecypher.NewS3ExternalPrincipalGrantWriter(exec, batchSize),
		rdsPostureNode: graphowner.NewRDSPostureLockedWriter(
			lockGate, rawRDSPostureNode.WriteRDSPostureNodes, rawRDSPostureNode.RetractRDSPostureNodes,
		),
		ec2UsesProfileEdge:         sourcecypher.NewEC2UsesProfileEdgeWriter(exec, batchSize),
		iamInstanceProfileRoleEdge: sourcecypher.NewIAMInstanceProfileRoleEdgeWriter(exec, batchSize),
		ec2InternetExposureNode: graphowner.NewEC2InternetExposureLockedWriter(
			lockGate, rawEC2InternetExposureNode.WriteEC2InternetExposureNodes, rawEC2InternetExposureNode.RetractEC2InternetExposureNodes,
		),
		ec2BlockDeviceKMSPostureNode: graphowner.NewEC2BlockDeviceKMSPostureLockedWriter(
			lockGate, rawEC2BlockDeviceKMSPostureNode.WriteEC2BlockDeviceKMSPostureNodes, rawEC2BlockDeviceKMSPostureNode.RetractEC2BlockDeviceKMSPostureNodes,
		),
		s3InternetExposureNode: graphowner.NewS3InternetExposureLockedWriter(
			lockGate, rawS3InternetExposureNode.WriteS3InternetExposureNodes, rawS3InternetExposureNode.RetractS3InternetExposureNodes,
		),
	}
}
