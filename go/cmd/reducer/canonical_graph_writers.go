// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/graphowner"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/taint"
	"github.com/eshu-hq/eshu/go/internal/reducer/obscoverage"
	"github.com/eshu-hq/eshu/go/internal/reducer/secgroup"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/ec2"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/iam"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/image"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/rds"
	awsrelationship "github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/relationship"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/s3"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/security/group"
	azurerelationship "github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/azure/relationship"
	gcprelationship "github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/gcp/relationship"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/resource"
	edgewriter "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/writer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/code/taint"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type canonicalGraphWriters struct {
	// cloudResourceNode / ec2InstanceNode / kubernetesWorkloadNode are wrapped
	// in the #5007 owner-ledger gate (graphowner) so a shared cross-scope node's
	// scope-derived properties resolve deterministically to the max-order-key
	// contributor. A nil-ledger gate writes through unchanged.
	cloudResourceNode *graphowner.CloudResourceGatedWriter
	ec2InstanceNode   *graphowner.EC2InstanceGatedWriter
	// cloudResourceRetracter / ec2InstanceRetracter delete predecessor-only
	// CloudResource uids under the globally-gated #6887 retract: per-uid
	// advisory lock, in-transaction global PG live-check, ledger release,
	// then the uid-anchored graph delete.
	cloudResourceRetracter          *graphowner.CloudResourceRetracter
	ec2InstanceRetracter            *graphowner.EC2InstanceRetracter
	cloudResourceEdge               *resource.CloudResourceEdgeWriter
	cloudResourceContainerImageEdge *image.CloudResourceContainerImageEdgeWriter
	gcpCloudResourceEdge            *gcprelationship.GCPCloudResourceEdgeWriter
	azureCloudResourceEdge          *azurerelationship.AzureCloudResourceEdgeWriter
	workloadCloudRelationshipEdge   *awsrelationship.WorkloadCloudRelationshipWriter
	kubernetesWorkloadNode          *graphowner.KubernetesWorkloadGatedWriter
	kubernetesNamespaceNode         *sourcecypher.KubernetesNamespaceNodeWriter
	securityGroupEndpointNode       *group.SecurityGroupEndpointNodeWriter
	securityGroupReachability       *group.SecurityGroupReachabilityWriter
	kubernetesCorrelationEdge       *sourcecypher.KubernetesCorrelationEdgeWriter
	crossplaneSatisfiedByEdge       *sourcecypher.CrossplaneSatisfiedByEdgeWriter
	iamEscalationEdge               *iam.IAMEscalationEdgeWriter
	iamCanPerformEdge               *iam.IAMCanPerformEdgeWriter
	observabilityCoverageEdge       *sourcecypher.ObservabilityCoverageEdgeWriter
	incidentRoutingEvidence         *sourcecypher.IncidentRoutingEvidenceWriter
	codeTaintEvidence               *sourcecypher.CodeTaintEvidenceWriter
	codeInterprocEvidence           *sourcecypher.CodeInterprocEvidenceWriter
	iamCanAssumeEdge                *iam.IAMCanAssumeEdgeWriter
	s3LogsToEdge                    *s3.S3LogsToEdgeWriter
	s3ExternalPrincipalGrant        *s3.S3ExternalPrincipalGrantWriter
	// rdsPostureNode / ec2InternetExposureNode / ec2BlockDeviceKMSPostureNode /
	// s3InternetExposureNode are wrapped in the #5062 lock-only gate
	// (graphowner.LockOnlyGate) so their SET/REMOVE writes to shared
	// CloudResource nodes serialize against the #5007/#5066 owner-ledger gate's
	// base-property writes on the SAME uid, under the identical Postgres
	// advisory-lock key. A nil-ledger gate writes through unchanged.
	rdsPostureNode               *graphowner.RDSPostureLockedWriter
	ec2UsesProfileEdge           *ec2.EC2UsesProfileEdgeWriter
	iamInstanceProfileRoleEdge   *iam.IAMInstanceProfileRoleEdgeWriter
	ec2InternetExposureNode      *graphowner.EC2InternetExposureLockedWriter
	ec2BlockDeviceKMSPostureNode *graphowner.EC2BlockDeviceKMSPostureLockedWriter
	s3InternetExposureNode       *graphowner.S3InternetExposureLockedWriter
	// provenanceEdge projects package-ownership/publication decisions to
	// PUBLISHES edges and container-image-identity decisions to BUILT_FROM
	// edges (issue #5457). One writer instance satisfies both the
	// reducer.PackageProvenanceEdgeWriter and
	// containerimage.ContainerImageProvenanceEdgeWriter interfaces.
	provenanceEdge *edgewriter.ProvenanceEdgeWriter
	// ec2InstanceIdentityNode is wrapped in the SAME #5062 lock-only gate as
	// the four writers above (#5448): it SETs the disjoint ami_id property on
	// the same CloudResource nodes ec2InstanceNode's owner-ledger gate writes
	// to, so it must serialize against that gate on the identical
	// advisory-lock key.
	ec2InstanceIdentityNode *graphowner.EC2InstanceIdentityLockedWriter
}

// newGraphOwnerGates builds the two Postgres-backed gates the canonical node
// writers share. ownerGate resolves cross-scope same-uid nodes to the
// max-(observed_at, source_fact_id) contributor through the owner ledger
// (#5007); a database without a transaction beginner yields a pass-through
// gate. lockGate serializes posture/exposure writers against ownerGate's
// same-uid base-property writes on the same advisory lock with no ledger row
// (#5062). Both carry the instruments for the locked-rows counter and
// lock-wait histogram (#5101).
func newGraphOwnerGates(database db.ExecQueryer, instruments *telemetry.Instruments) (*graphowner.Gate, *graphowner.LockOnlyGate) {
	ownerGate := graphowner.NewGate(reducerBeginner(database))
	ownerGate.Instruments = instruments
	lockGate := graphowner.NewLockOnlyGate(reducerBeginner(database))
	lockGate.Instruments = instruments
	return ownerGate, lockGate
}

// newCanonicalGraphWriters wires the canonical graph writers. reader backs
// the four posture node writers' pre-write CloudResource-existence check
// (issue #5652: a bare-MATCH-anchored UNWIND SET silently drops its write on
// the pinned production NornicDB image, so those writers now confirm a uid
// exists via a separate read before running a MERGE-anchored write) — it is
// typically the same query.GraphQuery graph-read port already wired for other
// reducer read paths.
//
// searchKey is the #7245 infra search key dialect resolved from the configured
// graph backend (search.DialectForBackend): the two writers that set
// searched CloudResource fields append the key clause on Neo4j only.
func newCanonicalGraphWriters(exec sourcecypher.Executor, reader sourcecypher.PostureExistenceReader, batchSize int, ownerGate *graphowner.Gate, lockGate *graphowner.LockOnlyGate, searchKey search.Dialect) canonicalGraphWriters {
	rawCloudResourceNode, rawEC2InstanceNode := newSearchKeyedCloudNodeWriters(exec, batchSize, searchKey)
	rawKubernetesWorkloadNode := sourcecypher.NewKubernetesWorkloadNodeWriter(exec, batchSize)
	kubernetesNamespaceNode := sourcecypher.NewKubernetesNamespaceNodeWriter(exec, batchSize)
	rawRDSPostureNode := rds.NewRDSPostureNodeWriter(exec, reader, batchSize)
	rawEC2InternetExposureNode := ec2.NewEC2InternetExposureNodeWriter(exec, reader, batchSize)
	rawEC2BlockDeviceKMSPostureNode := ec2.NewEC2BlockDeviceKMSPostureNodeWriter(exec, reader, batchSize)
	rawS3InternetExposureNode := s3.NewS3InternetExposureNodeWriter(exec, reader, batchSize)
	rawEC2InstanceIdentityNode := ec2.NewEC2InstanceIdentityNodeWriter(exec, reader, batchSize)
	return canonicalGraphWriters{
		cloudResourceNode:               graphowner.NewCloudResourceGatedWriter(ownerGate, rawCloudResourceNode.WriteCloudResourceNodes),
		ec2InstanceNode:                 graphowner.NewEC2InstanceGatedWriter(ownerGate, rawEC2InstanceNode.WriteEC2InstanceNodes),
		cloudResourceRetracter:          graphowner.NewCloudResourceRetracter(ownerGate, rawCloudResourceNode.RetractCloudResourceNodes),
		ec2InstanceRetracter:            graphowner.NewEC2InstanceRetracter(ownerGate, rawEC2InstanceNode.RetractEC2InstanceNodes),
		cloudResourceEdge:               resource.NewCloudResourceEdgeWriter(exec, batchSize),
		cloudResourceContainerImageEdge: image.NewCloudResourceContainerImageEdgeWriter(exec, batchSize),
		gcpCloudResourceEdge:            gcprelationship.NewGCPCloudResourceEdgeWriter(exec, batchSize),
		azureCloudResourceEdge:          azurerelationship.NewAzureCloudResourceEdgeWriter(exec, batchSize),
		workloadCloudRelationshipEdge:   awsrelationship.NewWorkloadCloudRelationshipWriter(exec, batchSize),
		kubernetesWorkloadNode:          graphowner.NewKubernetesWorkloadGatedWriter(ownerGate, rawKubernetesWorkloadNode.WriteKubernetesWorkloadNodes),
		kubernetesNamespaceNode:         kubernetesNamespaceNode,
		securityGroupEndpointNode:       group.NewSecurityGroupEndpointNodeWriter(exec, batchSize),
		securityGroupReachability:       group.NewSecurityGroupReachabilityWriter(exec, batchSize),
		kubernetesCorrelationEdge:       sourcecypher.NewKubernetesCorrelationEdgeWriter(exec, batchSize),
		crossplaneSatisfiedByEdge:       sourcecypher.NewCrossplaneSatisfiedByEdgeWriter(exec, batchSize),
		iamEscalationEdge:               iam.NewIAMEscalationEdgeWriter(exec, batchSize),
		iamCanPerformEdge:               iam.NewIAMCanPerformEdgeWriter(exec, batchSize),
		observabilityCoverageEdge:       sourcecypher.NewObservabilityCoverageEdgeWriter(exec, batchSize),
		incidentRoutingEvidence:         sourcecypher.NewIncidentRoutingEvidenceWriter(exec, batchSize),
		codeTaintEvidence:               sourcecypher.NewCodeTaintEvidenceWriter(exec, batchSize),
		codeInterprocEvidence:           sourcecypher.NewCodeInterprocEvidenceWriter(exec, batchSize),
		iamCanAssumeEdge:                iam.NewIAMCanAssumeEdgeWriter(exec, batchSize),
		s3LogsToEdge:                    s3.NewS3LogsToEdgeWriter(exec, batchSize),
		s3ExternalPrincipalGrant:        s3.NewS3ExternalPrincipalGrantWriter(exec, batchSize),
		rdsPostureNode: graphowner.NewRDSPostureLockedWriter(
			lockGate, rawRDSPostureNode.WriteRDSPostureNodes, rawRDSPostureNode.RetractRDSPostureNodes,
		),
		ec2UsesProfileEdge:         ec2.NewEC2UsesProfileEdgeWriter(exec, batchSize),
		iamInstanceProfileRoleEdge: iam.NewIAMInstanceProfileRoleEdgeWriter(exec, batchSize),
		ec2InternetExposureNode: graphowner.NewEC2InternetExposureLockedWriter(
			lockGate, rawEC2InternetExposureNode.WriteEC2InternetExposureNodes, rawEC2InternetExposureNode.RetractEC2InternetExposureNodes,
		),
		ec2BlockDeviceKMSPostureNode: graphowner.NewEC2BlockDeviceKMSPostureLockedWriter(
			lockGate, rawEC2BlockDeviceKMSPostureNode.WriteEC2BlockDeviceKMSPostureNodes, rawEC2BlockDeviceKMSPostureNode.RetractEC2BlockDeviceKMSPostureNodes,
		),
		s3InternetExposureNode: graphowner.NewS3InternetExposureLockedWriter(
			lockGate, rawS3InternetExposureNode.WriteS3InternetExposureNodes, rawS3InternetExposureNode.RetractS3InternetExposureNodes,
		),
		provenanceEdge: edgewriter.NewProvenanceEdgeWriter(exec, batchSize),
		ec2InstanceIdentityNode: graphowner.NewEC2InstanceIdentityLockedWriter(
			lockGate, rawEC2InstanceIdentityNode.WriteEC2InstanceIdentityNodes, rawEC2InstanceIdentityNode.RetractEC2InstanceIdentityNodes,
		),
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
func seedReducerProjectedSourceLedgers(database db.ExecQueryer, graphReader query.GraphQuery) (postgres.ProjectedSourceEdgeStore, error) {
	backfillStateMarker := postgres.NewCodeValueFlowBackfillStateStore(database)
	backfiller := taint.InterprocProjectedEdgeBackfiller{
		Reader:      taint.InterprocProjectedEdgeBackfillReader{Graph: graphReader},
		Ledger:      taintstore.NewCodeInterprocProjectedEdgeStore(database),
		StateMarker: backfillStateMarker,
		EvidenceSources: []string{
			taint.InterprocEvidenceSource(),
			taint.InterprocFixpointEvidenceSource(),
		},
	}
	if err := backfiller.Run(context.Background()); err != nil {
		return postgres.ProjectedSourceEdgeStore{}, fmt.Errorf("code interproc projected edge backfill: %w", err)
	}
	taintNodeBackfiller := taint.ProjectedNodeBackfiller{
		Reader:      taint.ProjectedNodeBackfillReader{Graph: graphReader},
		Ledger:      taintstore.NewCodeTaintEvidenceProjectedNodeStore(database),
		StateMarker: backfillStateMarker,
		EvidenceSources: []string{
			taint.EvidenceSource(),
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
			obscoverage.ObservabilityCoverageEvidenceSource(),
			secgroup.SecurityGroupReachabilityEvidenceSource(),
		},
	}
	if err := projectedSourceEdgeBackfiller.Run(context.Background()); err != nil {
		return postgres.ProjectedSourceEdgeStore{}, fmt.Errorf("projected source edge backfill: %w", err)
	}
	return projectedSourceEdgeStore, nil
}

// newSearchKeyedCloudNodeWriters builds the two reducer writers that set
// searched CloudResource fields, with the #7245 infra search key dialect.
func newSearchKeyedCloudNodeWriters(
	exec sourcecypher.Executor,
	batchSize int,
	searchKey search.Dialect,
) (*resource.CloudResourceNodeWriter, *ec2.EC2InstanceNodeWriter) {
	return resource.NewCloudResourceNodeWriterWithDialect(exec, batchSize, searchKey),
		ec2.NewEC2InstanceNodeWriterWithDialect(exec, batchSize, searchKey)
}

// loadReducerGraphBackend loads the configured graph backend and the #7245
// infra search key dialect it implies (search.DialectForBackend), so
// buildReducerService resolves both where it already loads the backend.
func loadReducerGraphBackend(getenv func(string) string) (runtimecfg.GraphBackend, search.Dialect, error) {
	backend, err := runtimecfg.LoadGraphBackend(getenv)
	if err != nil {
		return "", 0, err
	}
	searchKey, err := search.DialectForBackend(graph.SchemaBackend(backend))
	if err != nil {
		return "", 0, err
	}
	return backend, searchKey, nil
}

// newReducerCanonicalGraphWriters builds the #5007/#5062 owner-ledger and
// lock-only gates for the canonical node writers, then the writers, with the
// #7245 infra search key dialect.
func newReducerCanonicalGraphWriters(
	exec sourcecypher.Executor,
	reader sourcecypher.PostureExistenceReader,
	batchSize int,
	database db.ExecQueryer,
	instruments *telemetry.Instruments,
	searchKey search.Dialect,
) canonicalGraphWriters {
	ownerGate, lockGate := newGraphOwnerGates(database, instruments)
	return newCanonicalGraphWriters(exec, reader, batchSize, ownerGate, lockGate, searchKey)
}
