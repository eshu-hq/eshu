// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"testing"
	"time"

	emrtypes "github.com/aws/aws-sdk-go-v2/service/emr/types"
	emrserverlesstypes "github.com/aws/aws-sdk-go-v2/service/emrserverless/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestEMRAPIClientInterfaceExcludesMutationAndBodyAPIs proves the AWS SDK EMR
// surface this adapter accepts never lists a cluster/step/fleet mutation, a
// step-body reader, or a security-configuration policy-body reader as a
// callable method. It is the reflective guard the issue requires first: a
// maintainer cannot widen the metadata-only contract to reach mutation or
// sensitive-body APIs without failing this test.
func TestEMRAPIClientInterfaceExcludesMutationAndBodyAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*emrAPIClient)(nil)).Elem()
	forbidden := []string{
		// Cluster / step / job-flow mutation (steps are job invocations).
		"RunJobFlow",
		"TerminateJobFlows",
		"AddJobFlowSteps",
		"CancelSteps",
		"AddInstanceFleet",
		"AddInstanceGroups",
		"ModifyInstanceFleet",
		"ModifyInstanceGroups",
		"ModifyCluster",
		"SetTerminationProtection",
		"SetKeepJobFlowAliveWhenNoSteps",
		"SetVisibleToAllUsers",
		"PutAutoScalingPolicy",
		"RemoveAutoScalingPolicy",
		"PutAutoTerminationPolicy",
		"PutBlockPublicAccessConfiguration",
		"PutManagedScalingPolicy",
		// Security configuration mutation.
		"CreateSecurityConfiguration",
		"DeleteSecurityConfiguration",
		// Studio mutation.
		"CreateStudio",
		"DeleteStudio",
		"UpdateStudio",
		"CreateStudioSessionMapping",
		"DeleteStudioSessionMapping",
		"UpdateStudioSessionMapping",
		// Tag mutation.
		"AddTags",
		"RemoveTags",
		// Step / bootstrap body and security-config policy-body readers.
		"DescribeStep",
		"ListSteps",
		"ListBootstrapActions",
		"DescribeSecurityConfiguration",
		// Notebook execution data plane.
		"DescribeNotebookExecution",
		"ListNotebookExecutions",
		"StartNotebookExecution",
		"StopNotebookExecution",
	}
	assertNoForbiddenMethods(t, clientType, "emrAPIClient", forbidden)
}

// TestEMRServerlessAPIClientInterfaceExcludesMutationAndJobAPIs proves the EMR
// Serverless surface this adapter accepts never lists an application/job
// mutation or a job-run reader (which carries SparkSubmit entry-point
// arguments) as a callable method.
func TestEMRServerlessAPIClientInterfaceExcludesMutationAndJobAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*emrServerlessAPIClient)(nil)).Elem()
	forbidden := []string{
		// Application / job mutation.
		"CreateApplication",
		"DeleteApplication",
		"UpdateApplication",
		"StartApplication",
		"StopApplication",
		"StartJobRun",
		"CancelJobRun",
		"TagResource",
		"UntagResource",
		// Job-run readers carry SparkSubmit entryPointArguments.
		"GetJobRun",
		"ListJobRuns",
		"ListJobRunAttempts",
		"GetDashboardForJobRun",
		"GetSession",
		"ListSessions",
	}
	assertNoForbiddenMethods(t, clientType, "emrServerlessAPIClient", forbidden)
}

func assertNoForbiddenMethods(t *testing.T, clientType reflect.Type, name string, forbidden []string) {
	t.Helper()
	for _, method := range forbidden {
		if _, ok := clientType.MethodByName(method); ok {
			t.Fatalf("%s exposes forbidden method %q; EMR SDK adapter must stay metadata-only", name, method)
		}
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceEMR,
	}
}

func TestClientListClustersMapsMetadataNetworkingAndInstanceGroups(t *testing.T) {
	clusterARN := "arn:aws:elasticmapreduce:us-east-1:123456789012:cluster/j-1"
	emr := &fakeEMRAPI{
		clusters: []emrtypes.ClusterSummary{{Id: ptr("j-1"), Name: ptr("analytics")}},
		clusterDetail: map[string]emrtypes.Cluster{
			"j-1": {
				ClusterArn:             ptr(clusterARN),
				Id:                     ptr("j-1"),
				Name:                   ptr("analytics"),
				ReleaseLabel:           ptr("emr-7.1.0"),
				ServiceRole:            ptr("EMR_DefaultRole"),
				AutoScalingRole:        ptr("EMR_AutoScaling_DefaultRole"),
				SecurityConfiguration:  ptr("prod-sec-config"),
				LogEncryptionKmsKeyId:  ptr("arn:aws:kms:us-east-1:123456789012:key/k1"),
				InstanceCollectionType: emrtypes.InstanceCollectionTypeInstanceGroup,
				Status: &emrtypes.ClusterStatus{
					State: emrtypes.ClusterStateRunning,
					Timeline: &emrtypes.ClusterTimeline{
						CreationDateTime: ptrTime(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
					},
				},
				Ec2InstanceAttributes: &emrtypes.Ec2InstanceAttributes{
					Ec2SubnetId:                    ptr("subnet-aaa"),
					IamInstanceProfile:             ptr("EMR_EC2_DefaultRole"),
					EmrManagedMasterSecurityGroup:  ptr("sg-master"),
					EmrManagedSlaveSecurityGroup:   ptr("sg-slave"),
					ServiceAccessSecurityGroup:     ptr("sg-service"),
					AdditionalMasterSecurityGroups: []string{"sg-extra"},
				},
				Tags: []emrtypes.Tag{{Key: ptr("team"), Value: ptr("data")}},
			},
		},
		instanceGroups: map[string][]emrtypes.InstanceGroup{
			"j-1": {{
				Id:                     ptr("ig-1"),
				Name:                   ptr("Master"),
				InstanceGroupType:      emrtypes.InstanceGroupTypeMaster,
				InstanceType:           ptr("m5.xlarge"),
				RequestedInstanceCount: ptr(int32(1)),
				RunningInstanceCount:   ptr(int32(1)),
				Status:                 &emrtypes.InstanceGroupStatus{State: emrtypes.InstanceGroupStateRunning},
			}},
		},
	}
	adapter := &Client{emr: emr, serverless: &fakeServerlessAPI{}, boundary: testBoundary()}

	clusters, err := adapter.ListClusters(context.Background())
	if err != nil {
		t.Fatalf("ListClusters() error = %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("len(clusters) = %d, want 1", len(clusters))
	}
	cluster := clusters[0]
	if cluster.ARN != clusterARN {
		t.Fatalf("ARN = %q, want %q", cluster.ARN, clusterARN)
	}
	if cluster.SubnetID != "subnet-aaa" {
		t.Fatalf("SubnetID = %q, want subnet-aaa", cluster.SubnetID)
	}
	if cluster.InstanceProfile != "EMR_EC2_DefaultRole" {
		t.Fatalf("InstanceProfile = %q", cluster.InstanceProfile)
	}
	wantGroups := map[string]struct{}{"sg-master": {}, "sg-slave": {}, "sg-service": {}, "sg-extra": {}}
	for _, sg := range cluster.SecurityGroupIDs {
		delete(wantGroups, sg)
	}
	if len(wantGroups) != 0 {
		t.Fatalf("missing security groups %v in %v", wantGroups, cluster.SecurityGroupIDs)
	}
	if len(cluster.InstanceGroups) != 1 || cluster.InstanceGroups[0].ID != "ig-1" {
		t.Fatalf("InstanceGroups = %#v, want one ig-1", cluster.InstanceGroups)
	}
	if cluster.Tags["team"] != "data" {
		t.Fatalf("Tags = %#v, want team=data", cluster.Tags)
	}
}

func TestClientListClustersRequestsBoundedStatesAndRecentWindow(t *testing.T) {
	emr := &fakeEMRAPI{}
	adapter := &Client{emr: emr, serverless: &fakeServerlessAPI{}, boundary: testBoundary()}
	if _, err := adapter.ListClusters(context.Background()); err != nil {
		t.Fatalf("ListClusters() error = %v", err)
	}
	if emr.listClustersInput == nil {
		t.Fatalf("ListClusters was not called")
	}
	if len(emr.listClustersInput.ClusterStates) == 0 {
		t.Fatalf("ListClusters did not bound ClusterStates; want running + recently terminated states")
	}
	if emr.listClustersInput.CreatedAfter == nil {
		t.Fatalf("ListClusters did not bound CreatedAfter; recently terminated window is required")
	}
}

func TestClientListServerlessApplicationsMapsNetworkAndEncryption(t *testing.T) {
	appARN := "arn:aws:emr-serverless:us-east-1:123456789012:/applications/00a"
	serverless := &fakeServerlessAPI{
		applications: []emrserverlesstypes.ApplicationSummary{{Id: ptr("00a"), Arn: ptr(appARN), Name: ptr("spark")}},
		applicationDetail: map[string]emrserverlesstypes.Application{
			"00a": {
				ApplicationId: ptr("00a"),
				Arn:           ptr(appARN),
				Name:          ptr("spark"),
				State:         emrserverlesstypes.ApplicationStateStarted,
				Type:          ptr("SPARK"),
				NetworkConfiguration: &emrserverlesstypes.NetworkConfiguration{
					SubnetIds:        []string{"subnet-ccc"},
					SecurityGroupIds: []string{"sg-app"},
				},
				DiskEncryptionConfiguration: &emrserverlesstypes.DiskEncryptionConfiguration{
					EncryptionKeyArn: ptr("arn:aws:kms:us-east-1:123456789012:key/s1"),
				},
			},
		},
	}
	adapter := &Client{emr: &fakeEMRAPI{}, serverless: serverless, boundary: testBoundary()}

	apps, err := adapter.ListServerlessApplications(context.Background())
	if err != nil {
		t.Fatalf("ListServerlessApplications() error = %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("len(apps) = %d, want 1", len(apps))
	}
	app := apps[0]
	if app.ARN != appARN || app.DiskEncryptKMS != "arn:aws:kms:us-east-1:123456789012:key/s1" {
		t.Fatalf("application mapping = %#v", app)
	}
	if len(app.SubnetIDs) != 1 || app.SubnetIDs[0] != "subnet-ccc" {
		t.Fatalf("SubnetIDs = %v, want [subnet-ccc]", app.SubnetIDs)
	}
}

func TestClientListStudiosMapsNetworkRolesAndSessionMappings(t *testing.T) {
	studioARN := "arn:aws:elasticmapreduce:us-east-1:123456789012:studio/es-1"
	emr := &fakeEMRAPI{
		studios: []emrtypes.StudioSummary{{StudioId: ptr("es-1"), Name: ptr("studio"), VpcId: ptr("vpc-1")}},
		studioDetail: map[string]emrtypes.Studio{
			"es-1": {
				StudioArn:                ptr(studioARN),
				StudioId:                 ptr("es-1"),
				Name:                     ptr("studio"),
				AuthMode:                 emrtypes.AuthModeSso,
				VpcId:                    ptr("vpc-1"),
				SubnetIds:                []string{"subnet-ddd"},
				EngineSecurityGroupId:    ptr("sg-engine"),
				WorkspaceSecurityGroupId: ptr("sg-workspace"),
				ServiceRole:              ptr("arn:aws:iam::123456789012:role/Svc"),
				UserRole:                 ptr("arn:aws:iam::123456789012:role/User"),
				EncryptionKeyArn:         ptr("arn:aws:kms:us-east-1:123456789012:key/st"),
			},
		},
		sessionMappings: map[string][]emrtypes.SessionMappingSummary{
			"es-1": {{
				StudioId:         ptr("es-1"),
				IdentityId:       ptr("id-1"),
				IdentityName:     ptr("analysts"),
				IdentityType:     emrtypes.IdentityTypeGroup,
				SessionPolicyArn: ptr("arn:aws:iam::aws:policy/AmazonEMRFullAccessPolicy_v2"),
			}},
		},
	}
	adapter := &Client{emr: emr, serverless: &fakeServerlessAPI{}, boundary: testBoundary()}

	studios, err := adapter.ListStudios(context.Background())
	if err != nil {
		t.Fatalf("ListStudios() error = %v", err)
	}
	if len(studios) != 1 {
		t.Fatalf("len(studios) = %d, want 1", len(studios))
	}
	studio := studios[0]
	if studio.VPCID != "vpc-1" || studio.EncryptionKeyARN != "arn:aws:kms:us-east-1:123456789012:key/st" {
		t.Fatalf("studio mapping = %#v", studio)
	}
	if len(studio.SessionMappings) != 1 || studio.SessionMappings[0].IdentityID != "id-1" {
		t.Fatalf("SessionMappings = %#v, want one id-1", studio.SessionMappings)
	}
	if studio.SessionMappings[0].IdentityType != "GROUP" {
		t.Fatalf("session mapping identity type = %q, want GROUP", studio.SessionMappings[0].IdentityType)
	}
}

func TestClientListSecurityConfigurationsMapsNameOnly(t *testing.T) {
	emr := &fakeEMRAPI{
		securityConfigs: []emrtypes.SecurityConfigurationSummary{{
			Name:             ptr("kerberos"),
			CreationDateTime: ptrTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		}},
	}
	adapter := &Client{emr: emr, serverless: &fakeServerlessAPI{}, boundary: testBoundary()}
	configs, err := adapter.ListSecurityConfigurations(context.Background())
	if err != nil {
		t.Fatalf("ListSecurityConfigurations() error = %v", err)
	}
	if len(configs) != 1 || configs[0].Name != "kerberos" {
		t.Fatalf("configs = %#v, want one kerberos", configs)
	}
}

func ptr[T any](v T) *T { return &v }

func ptrTime(t time.Time) *time.Time { return &t }
