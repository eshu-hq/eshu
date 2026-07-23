// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/graphowner"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

type canonicalGraphWriters struct {
	// cloudResourceNode / ec2InstanceNode / kubernetesWorkloadNode are wrapped
	// in the #5007 owner-ledger gate (graphowner) so a shared cross-scope node's
	// scope-derived properties resolve deterministically to the max-order-key
	// contributor. A nil-ledger gate writes through unchanged.
	cloudResourceNode               *graphowner.CloudResourceGatedWriter
	ec2InstanceNode                 *graphowner.EC2InstanceGatedWriter
	cloudResourceEdge               *sourcecypher.CloudResourceEdgeWriter
	cloudResourceContainerImageEdge *sourcecypher.CloudResourceContainerImageEdgeWriter
	gcpCloudResourceEdge            *sourcecypher.GCPCloudResourceEdgeWriter
	azureCloudResourceEdge          *sourcecypher.AzureCloudResourceEdgeWriter
	workloadCloudRelationshipEdge   *sourcecypher.WorkloadCloudRelationshipWriter
	kubernetesWorkloadNode          *graphowner.KubernetesWorkloadGatedWriter
	kubernetesNamespaceNode         *sourcecypher.KubernetesNamespaceNodeWriter
	securityGroupEndpointNode       *sourcecypher.SecurityGroupEndpointNodeWriter
	securityGroupReachability       *sourcecypher.SecurityGroupReachabilityWriter
	kubernetesCorrelationEdge       *sourcecypher.KubernetesCorrelationEdgeWriter
	crossplaneSatisfiedByEdge       *sourcecypher.CrossplaneSatisfiedByEdgeWriter
	iamEscalationEdge               *sourcecypher.IAMEscalationEdgeWriter
	iamCanPerformEdge               *sourcecypher.IAMCanPerformEdgeWriter
	observabilityCoverageEdge       *sourcecypher.ObservabilityCoverageEdgeWriter
	incidentRoutingEvidence         *sourcecypher.IncidentRoutingEvidenceWriter
	codeTaintEvidence               *sourcecypher.CodeTaintEvidenceWriter
	codeInterprocEvidence           *sourcecypher.CodeInterprocEvidenceWriter
	iamCanAssumeEdge                *sourcecypher.IAMCanAssumeEdgeWriter
	s3LogsToEdge                    *sourcecypher.S3LogsToEdgeWriter
	s3ExternalPrincipalGrant        *sourcecypher.S3ExternalPrincipalGrantWriter
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
	// provenanceEdge projects package-ownership/publication decisions to
	// PUBLISHES edges and container-image-identity decisions to BUILT_FROM
	// edges (issue #5457). One writer instance satisfies both the
	// reducer.PackageProvenanceEdgeWriter and
	// reducer.ContainerImageProvenanceEdgeWriter interfaces.
	provenanceEdge *sourcecypher.ProvenanceEdgeWriter
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
		cloudResourceNode:               graphowner.NewCloudResourceGatedWriter(ownerGate, rawCloudResourceNode.WriteCloudResourceNodes),
		ec2InstanceNode:                 graphowner.NewEC2InstanceGatedWriter(ownerGate, rawEC2InstanceNode.WriteEC2InstanceNodes),
		cloudResourceEdge:               sourcecypher.NewCloudResourceEdgeWriter(exec, batchSize),
		cloudResourceContainerImageEdge: sourcecypher.NewCloudResourceContainerImageEdgeWriter(exec, batchSize),
		gcpCloudResourceEdge:            sourcecypher.NewGCPCloudResourceEdgeWriter(exec, batchSize),
		azureCloudResourceEdge:          sourcecypher.NewAzureCloudResourceEdgeWriter(exec, batchSize),
		workloadCloudRelationshipEdge:   sourcecypher.NewWorkloadCloudRelationshipWriter(exec, batchSize),
		kubernetesWorkloadNode:          graphowner.NewKubernetesWorkloadGatedWriter(ownerGate, rawKubernetesWorkloadNode.WriteKubernetesWorkloadNodes),
		kubernetesNamespaceNode:         kubernetesNamespaceNode,
		securityGroupEndpointNode:       sourcecypher.NewSecurityGroupEndpointNodeWriter(exec, batchSize),
		securityGroupReachability:       sourcecypher.NewSecurityGroupReachabilityWriter(exec, batchSize),
		kubernetesCorrelationEdge:       sourcecypher.NewKubernetesCorrelationEdgeWriter(exec, batchSize),
		crossplaneSatisfiedByEdge:       sourcecypher.NewCrossplaneSatisfiedByEdgeWriter(exec, batchSize),
		iamEscalationEdge:               sourcecypher.NewIAMEscalationEdgeWriter(exec, batchSize),
		iamCanPerformEdge:               sourcecypher.NewIAMCanPerformEdgeWriter(exec, batchSize),
		observabilityCoverageEdge:       sourcecypher.NewObservabilityCoverageEdgeWriter(exec, batchSize),
		incidentRoutingEvidence:         sourcecypher.NewIncidentRoutingEvidenceWriter(exec, batchSize),
		codeTaintEvidence:               sourcecypher.NewCodeTaintEvidenceWriter(exec, batchSize),
		codeInterprocEvidence:           sourcecypher.NewCodeInterprocEvidenceWriter(exec, batchSize),
		iamCanAssumeEdge:                sourcecypher.NewIAMCanAssumeEdgeWriter(exec, batchSize),
		s3LogsToEdge:                    sourcecypher.NewS3LogsToEdgeWriter(exec, batchSize),
		s3ExternalPrincipalGrant:        sourcecypher.NewS3ExternalPrincipalGrantWriter(exec, batchSize),
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
		provenanceEdge: sourcecypher.NewProvenanceEdgeWriter(exec, batchSize),
	}
}

// seedReducerProjectedSourceLedgers runs buildReducerService's three one-time,
// idempotent startup backfills that seed the projected-edge/-node ledgers from
// existing graph state, so each ledger is a superset of the graph at deploy
// time. Extracted out of buildReducerService (go/cmd/reducer/main.go) to keep
// that file under the repo's file-size budget; it is grouped here alongside
// the canonical graph writer construction it feeds (the AWS/Azure/GCP
// relationship, observability-coverage, and security-group-reachability
// materialization handlers wired from canonicalGraphWriters all read this
// backfill's ProjectedSourceLedger before their first post-deploy retract).
//
//   - The TAINT_FLOWS_TO interproc-edge ledger and the CodeTaintEvidence
//     node ledger share one StateMarker and must run in that order (mirrors
//     the interproc edge backfill).
//   - The projected-source-edge ledger (AWS/Azure/GCP relationship,
//     observability-coverage, and security-group-reachability evidence) is
//     returned so the caller can reuse the SAME store instance as
//     reducer.DefaultHandlers.ProjectedSourceLedger — one store serves both
//     this backfill and the runtime handlers.
//
// Each backfiller runs against context.Background(), not a caller-supplied
// context, matching this startup sequence's pre-existing behavior: it must
// complete before the reducer service begins serving work, independent of any
// request-scoped deadline the caller may be operating under.
func seedReducerProjectedSourceLedgers(database postgres.ExecQueryer, graphReader query.GraphQuery) (postgres.ProjectedSourceEdgeStore, error) {
	backfillStateMarker := postgres.NewCodeValueFlowBackfillStateStore(database)
	backfiller := reducer.CodeInterprocProjectedEdgeBackfiller{
		Reader:      reducer.CodeInterprocProjectedEdgeBackfillReader{Graph: graphReader},
		Ledger:      postgres.NewCodeInterprocProjectedEdgeStore(database),
		StateMarker: backfillStateMarker,
		EvidenceSources: []string{
			reducer.CodeInterprocEvidenceSource(),
			reducer.CodeInterprocFixpointEvidenceSource(),
		},
	}
	if err := backfiller.Run(context.Background()); err != nil {
		return postgres.ProjectedSourceEdgeStore{}, fmt.Errorf("code interproc projected edge backfill: %w", err)
	}
	taintNodeBackfiller := reducer.CodeTaintEvidenceProjectedNodeBackfiller{
		Reader:      reducer.CodeTaintEvidenceProjectedNodeBackfillReader{Graph: graphReader},
		Ledger:      postgres.NewCodeTaintEvidenceProjectedNodeStore(database),
		StateMarker: backfillStateMarker,
		EvidenceSources: []string{
			reducer.CodeTaintEvidenceSource(),
		},
	}
	if err := taintNodeBackfiller.Run(context.Background()); err != nil {
		return postgres.ProjectedSourceEdgeStore{}, fmt.Errorf("code taint evidence projected node backfill: %w", err)
	}
	projectedSourceEdgeStore := postgres.NewProjectedSourceEdgeStore(database)
	projectedSourceEdgeBackfiller := reducer.ProjectedSourceEdgeBackfiller{
		Reader:      reducer.ProjectedSourceEdgeBackfillReader{Graph: graphReader},
		Ledger:      projectedSourceEdgeStore,
		StateMarker: backfillStateMarker,
		EvidenceSources: []string{
			reducer.AWSRelationshipEvidenceSource(),
			reducer.AzureRelationshipEvidenceSource(),
			reducer.GCPRelationshipEvidenceSource(),
			reducer.ObservabilityCoverageEvidenceSource(),
			reducer.SecurityGroupReachabilityEvidenceSource(),
		},
	}
	if err := projectedSourceEdgeBackfiller.Run(context.Background()); err != nil {
		return postgres.ProjectedSourceEdgeStore{}, fmt.Errorf("projected source edge backfill: %w", err)
	}
	return projectedSourceEdgeStore, nil
}
