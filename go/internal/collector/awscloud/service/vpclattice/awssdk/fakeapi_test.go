// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsvpclattice "github.com/aws/aws-sdk-go-v2/service/vpclattice"
	awsvpclatticetypes "github.com/aws/aws-sdk-go-v2/service/vpclattice/types"
)

// fakeVPCLatticeAPI is a single-page in-memory implementation of the adapter's
// read interface used to exercise Snapshot mapping without AWS.
type fakeVPCLatticeAPI struct {
	networks            []awsvpclatticetypes.ServiceNetworkSummary
	vpcAssociations     map[string][]awsvpclatticetypes.ServiceNetworkVpcAssociationSummary
	serviceAssociations map[string][]awsvpclatticetypes.ServiceNetworkServiceAssociationSummary
	services            []awsvpclatticetypes.ServiceSummary
	getService          map[string]*awsvpclattice.GetServiceOutput
	listeners           map[string][]awsvpclatticetypes.ListenerSummary
	targetGroups        []awsvpclatticetypes.TargetGroupSummary
	getTargetGroup      map[string]*awsvpclattice.GetTargetGroupOutput
	targets             map[string][]awsvpclatticetypes.TargetSummary
	tags                map[string]map[string]string
}

func (f *fakeVPCLatticeAPI) ListServiceNetworks(
	_ context.Context,
	_ *awsvpclattice.ListServiceNetworksInput,
	_ ...func(*awsvpclattice.Options),
) (*awsvpclattice.ListServiceNetworksOutput, error) {
	return &awsvpclattice.ListServiceNetworksOutput{Items: f.networks}, nil
}

func (f *fakeVPCLatticeAPI) ListServiceNetworkVpcAssociations(
	_ context.Context,
	input *awsvpclattice.ListServiceNetworkVpcAssociationsInput,
	_ ...func(*awsvpclattice.Options),
) (*awsvpclattice.ListServiceNetworkVpcAssociationsOutput, error) {
	return &awsvpclattice.ListServiceNetworkVpcAssociationsOutput{
		Items: f.vpcAssociations[aws.ToString(input.ServiceNetworkIdentifier)],
	}, nil
}

func (f *fakeVPCLatticeAPI) ListServiceNetworkServiceAssociations(
	_ context.Context,
	input *awsvpclattice.ListServiceNetworkServiceAssociationsInput,
	_ ...func(*awsvpclattice.Options),
) (*awsvpclattice.ListServiceNetworkServiceAssociationsOutput, error) {
	return &awsvpclattice.ListServiceNetworkServiceAssociationsOutput{
		Items: f.serviceAssociations[aws.ToString(input.ServiceNetworkIdentifier)],
	}, nil
}

func (f *fakeVPCLatticeAPI) ListServices(
	_ context.Context,
	_ *awsvpclattice.ListServicesInput,
	_ ...func(*awsvpclattice.Options),
) (*awsvpclattice.ListServicesOutput, error) {
	return &awsvpclattice.ListServicesOutput{Items: f.services}, nil
}

func (f *fakeVPCLatticeAPI) GetService(
	_ context.Context,
	input *awsvpclattice.GetServiceInput,
	_ ...func(*awsvpclattice.Options),
) (*awsvpclattice.GetServiceOutput, error) {
	if output := f.getService[aws.ToString(input.ServiceIdentifier)]; output != nil {
		return output, nil
	}
	return &awsvpclattice.GetServiceOutput{}, nil
}

func (f *fakeVPCLatticeAPI) ListListeners(
	_ context.Context,
	input *awsvpclattice.ListListenersInput,
	_ ...func(*awsvpclattice.Options),
) (*awsvpclattice.ListListenersOutput, error) {
	return &awsvpclattice.ListListenersOutput{
		Items: f.listeners[aws.ToString(input.ServiceIdentifier)],
	}, nil
}

func (f *fakeVPCLatticeAPI) ListTargetGroups(
	_ context.Context,
	_ *awsvpclattice.ListTargetGroupsInput,
	_ ...func(*awsvpclattice.Options),
) (*awsvpclattice.ListTargetGroupsOutput, error) {
	return &awsvpclattice.ListTargetGroupsOutput{Items: f.targetGroups}, nil
}

func (f *fakeVPCLatticeAPI) GetTargetGroup(
	_ context.Context,
	input *awsvpclattice.GetTargetGroupInput,
	_ ...func(*awsvpclattice.Options),
) (*awsvpclattice.GetTargetGroupOutput, error) {
	if output := f.getTargetGroup[aws.ToString(input.TargetGroupIdentifier)]; output != nil {
		return output, nil
	}
	return &awsvpclattice.GetTargetGroupOutput{}, nil
}

func (f *fakeVPCLatticeAPI) ListTargets(
	_ context.Context,
	input *awsvpclattice.ListTargetsInput,
	_ ...func(*awsvpclattice.Options),
) (*awsvpclattice.ListTargetsOutput, error) {
	return &awsvpclattice.ListTargetsOutput{
		Items: f.targets[aws.ToString(input.TargetGroupIdentifier)],
	}, nil
}

func (f *fakeVPCLatticeAPI) ListTagsForResource(
	_ context.Context,
	input *awsvpclattice.ListTagsForResourceInput,
	_ ...func(*awsvpclattice.Options),
) (*awsvpclattice.ListTagsForResourceOutput, error) {
	return &awsvpclattice.ListTagsForResourceOutput{
		Tags: f.tags[aws.ToString(input.ResourceArn)],
	}, nil
}
