// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/awscloud"
	"github.com/eshu-hq/eshu/go/internal/reducer/ec2blockkms"
	"github.com/eshu-hq/eshu/go/internal/reducer/ec2instance"
	"github.com/eshu-hq/eshu/go/internal/reducer/ec2usesprofile"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamescalation"
	"github.com/eshu-hq/eshu/go/internal/reducer/iaminstprofile"
	"github.com/eshu-hq/eshu/go/internal/reducer/internetexposure"
	"github.com/eshu-hq/eshu/go/internal/reducer/kubernetescorrelation"
	"github.com/eshu-hq/eshu/go/internal/reducer/obscoverage"
	"github.com/eshu-hq/eshu/go/internal/reducer/rdsposture"
	"github.com/eshu-hq/eshu/go/internal/reducer/s3grant"
	"github.com/eshu-hq/eshu/go/internal/reducer/s3logsto"
	"github.com/eshu-hq/eshu/go/internal/reducer/secgroup"
	"github.com/eshu-hq/eshu/go/internal/reducer/secretsiam"
)

// waitingPhaseDeferral builds one phase-commit map row: the waiter's
// canonical nodes commit with the producer's phase publish.
func waitingPhaseDeferral(class, save string) WaitingDeferral {
	return WaitingDeferral{
		FailureClass:  class,
		WaitsForKind:  WaitingKindPhaseCommitted,
		WakeSite:      WaitingWakeSiteInTx,
		ProvidingSave: save,
	}
}

// waitingPhaseDeferrals lists the phase-commit family: materialization
// domains whose readiness gate waits on the producer's canonical-node phase
// commit (the largest deferral family in the #10876 census).
func waitingPhaseDeferrals() []WaitingDeferral {
	phase := waitingPhaseDeferral
	save := "producer phase-commit upsert (graph_projection_phase_state)"
	return []WaitingDeferral{
		phase(secretsiam.SecretsIAMEndpointNotReadyFailureClass, save),
		phase(kubernetescorrelation.KubernetesCorrelationNodesNotReadyFailureClass, save),
		phase(reducer.GCPRelationshipNodesNotReadyFailureClass, save),
		phase(ec2instance.EC2InstanceIdentityNodesNotReadyFailureClass, save),
		phase(reducer.AWSRelationshipEC2InstanceNodesNotReadyFailureClass, save),
		phase(awscloud.AWSCloudImageNodesNotReadyFailureClass, save),
		phase(reducer.AWSRelationshipNodesNotReadyFailureClass, save),
		phase(reducer.AzureRelationshipNodesNotReadyFailureClass, save),
		phase(iamcan.IAMCanAssumeNodesNotReadyFailureClass, save),
		phase(iamcan.IAMCanPerformNodesNotReadyFailureClass, save),
		phase(iamescalation.IAMEscalationNodesNotReadyFailureClass, save),
		phase(iaminstprofile.IAMInstanceProfileRoleNodesNotReadyFailureClass, save),
		phase(obscoverage.ObservabilityCoverageNodesNotReadyFailureClass, save),
		phase(rdsposture.RDSPostureNodesNotReadyFailureClass, save),
		phase(secgroup.SecurityGroupReachabilityNodesNotReadyFailureClass, save),
		phase(reducer.WorkloadCloudRelationshipNodesNotReadyFailureClass, save),
		phase(ec2blockkms.EC2BlockDeviceKMSPostureNodesNotReadyFailureClass, save),
		phase(internetexposure.EC2InternetExposureNodesNotReadyFailureClass, save),
		phase(ec2usesprofile.EC2UsesProfileNodesNotReadyFailureClass, save),
		phase(s3grant.S3ExternalPrincipalGrantNodesNotReadyFailureClass, save),
		phase(internetexposure.S3InternetExposureNodesNotReadyFailureClass, save),
		phase(s3logsto.S3LogsToNodesNotReadyFailureClass, save),
		{
			FailureClass:  reducer.DeployableUnitCorrelationCanonicalNodesNotReadyFailureClass,
			WaitsForKind:  WaitingKindPhaseCommitted,
			WakeSite:      WaitingWakeSiteInTx,
			ProvidingSave: "producer phase-commit upsert (graph_projection_phase_state)",
		},
	}
}
