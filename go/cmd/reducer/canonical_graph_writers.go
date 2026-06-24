// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

type canonicalGraphWriters struct {
	cloudResourceNode             *sourcecypher.CloudResourceNodeWriter
	ec2InstanceNode               *sourcecypher.EC2InstanceNodeWriter
	cloudResourceEdge             *sourcecypher.CloudResourceEdgeWriter
	gcpCloudResourceEdge          *sourcecypher.GCPCloudResourceEdgeWriter
	azureCloudResourceEdge        *sourcecypher.AzureCloudResourceEdgeWriter
	workloadCloudRelationshipEdge *sourcecypher.WorkloadCloudRelationshipWriter
	kubernetesWorkloadNode        *sourcecypher.KubernetesWorkloadNodeWriter
	securityGroupEndpointNode     *sourcecypher.SecurityGroupEndpointNodeWriter
	securityGroupReachability     *sourcecypher.SecurityGroupReachabilityWriter
	kubernetesCorrelationEdge     *sourcecypher.KubernetesCorrelationEdgeWriter
	iamEscalationEdge             *sourcecypher.IAMEscalationEdgeWriter
	iamCanPerformEdge             *sourcecypher.IAMCanPerformEdgeWriter
	observabilityCoverageEdge     *sourcecypher.ObservabilityCoverageEdgeWriter
	incidentRoutingEvidence       *sourcecypher.IncidentRoutingEvidenceWriter
	codeTaintEvidence             *sourcecypher.CodeTaintEvidenceWriter
	codeInterprocEvidence         *sourcecypher.CodeInterprocEvidenceWriter
	iamCanAssumeEdge              *sourcecypher.IAMCanAssumeEdgeWriter
	s3LogsToEdge                  *sourcecypher.S3LogsToEdgeWriter
	s3ExternalPrincipalGrant      *sourcecypher.S3ExternalPrincipalGrantWriter
	rdsPostureNode                *sourcecypher.RDSPostureNodeWriter
	ec2UsesProfileEdge            *sourcecypher.EC2UsesProfileEdgeWriter
	iamInstanceProfileRoleEdge    *sourcecypher.IAMInstanceProfileRoleEdgeWriter
	ec2InternetExposureNode       *sourcecypher.EC2InternetExposureNodeWriter
	ec2BlockDeviceKMSPostureNode  *sourcecypher.EC2BlockDeviceKMSPostureNodeWriter
	s3InternetExposureNode        *sourcecypher.S3InternetExposureNodeWriter
}

func newCanonicalGraphWriters(exec sourcecypher.Executor, batchSize int) canonicalGraphWriters {
	return canonicalGraphWriters{
		cloudResourceNode:             sourcecypher.NewCloudResourceNodeWriter(exec, batchSize),
		ec2InstanceNode:               sourcecypher.NewEC2InstanceNodeWriter(exec, batchSize),
		cloudResourceEdge:             sourcecypher.NewCloudResourceEdgeWriter(exec, batchSize),
		gcpCloudResourceEdge:          sourcecypher.NewGCPCloudResourceEdgeWriter(exec, batchSize),
		azureCloudResourceEdge:        sourcecypher.NewAzureCloudResourceEdgeWriter(exec, batchSize),
		workloadCloudRelationshipEdge: sourcecypher.NewWorkloadCloudRelationshipWriter(exec, batchSize),
		kubernetesWorkloadNode:        sourcecypher.NewKubernetesWorkloadNodeWriter(exec, batchSize),
		securityGroupEndpointNode:     sourcecypher.NewSecurityGroupEndpointNodeWriter(exec, batchSize),
		securityGroupReachability:     sourcecypher.NewSecurityGroupReachabilityWriter(exec, batchSize),
		kubernetesCorrelationEdge:     sourcecypher.NewKubernetesCorrelationEdgeWriter(exec, batchSize),
		iamEscalationEdge:             sourcecypher.NewIAMEscalationEdgeWriter(exec, batchSize),
		iamCanPerformEdge:             sourcecypher.NewIAMCanPerformEdgeWriter(exec, batchSize),
		observabilityCoverageEdge:     sourcecypher.NewObservabilityCoverageEdgeWriter(exec, batchSize),
		incidentRoutingEvidence:       sourcecypher.NewIncidentRoutingEvidenceWriter(exec, batchSize),
		codeTaintEvidence:             sourcecypher.NewCodeTaintEvidenceWriter(exec, batchSize),
		codeInterprocEvidence:         sourcecypher.NewCodeInterprocEvidenceWriter(exec, batchSize),
		iamCanAssumeEdge:              sourcecypher.NewIAMCanAssumeEdgeWriter(exec, batchSize),
		s3LogsToEdge:                  sourcecypher.NewS3LogsToEdgeWriter(exec, batchSize),
		s3ExternalPrincipalGrant:      sourcecypher.NewS3ExternalPrincipalGrantWriter(exec, batchSize),
		rdsPostureNode:                sourcecypher.NewRDSPostureNodeWriter(exec, batchSize),
		ec2UsesProfileEdge:            sourcecypher.NewEC2UsesProfileEdgeWriter(exec, batchSize),
		iamInstanceProfileRoleEdge:    sourcecypher.NewIAMInstanceProfileRoleEdgeWriter(exec, batchSize),
		ec2InternetExposureNode:       sourcecypher.NewEC2InternetExposureNodeWriter(exec, batchSize),
		ec2BlockDeviceKMSPostureNode:  sourcecypher.NewEC2BlockDeviceKMSPostureNodeWriter(exec, batchSize),
		s3InternetExposureNode:        sourcecypher.NewS3InternetExposureNodeWriter(exec, batchSize),
	}
}
