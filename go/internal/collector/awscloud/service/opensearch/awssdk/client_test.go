// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsopensearch "github.com/aws/aws-sdk-go-v2/service/opensearch"
	awsopensearchtypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	awsserverless "github.com/aws/aws-sdk-go-v2/service/opensearchserverless"
	awsserverlesstypes "github.com/aws/aws-sdk-go-v2/service/opensearchserverless/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListsOpenSearchDomainMetadataOnly(t *testing.T) {
	domainARN := "arn:aws:es:us-east-1:123456789012:domain/orders-search"
	roleARN := "arn:aws:iam::123456789012:role/orders-search-admin"
	accessPolicy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["` + roleARN + `","arn:aws:iam::123456789012:user/ignored"]},"Action":"es:*"}]}`

	domain := &fakeDomainAPI{
		domainNames: &awsopensearch.ListDomainNamesOutput{
			DomainNames: []awsopensearchtypes.DomainInfo{{DomainName: aws.String("orders-search")}},
		},
		domains: &awsopensearch.DescribeDomainsOutput{
			DomainStatusList: []awsopensearchtypes.DomainStatus{{
				ARN:           aws.String(domainARN),
				DomainId:      aws.String("123456789012/orders-search"),
				DomainName:    aws.String("orders-search"),
				EngineVersion: aws.String("OpenSearch_2.11"),
				Created:       aws.Bool(true),
				Processing:    aws.Bool(false),
				ClusterConfig: &awsopensearchtypes.ClusterConfig{
					InstanceType:           awsopensearchtypes.OpenSearchPartitionInstanceType("r6g.large.search"),
					InstanceCount:          aws.Int32(3),
					DedicatedMasterEnabled: aws.Bool(true),
					DedicatedMasterType:    awsopensearchtypes.OpenSearchPartitionInstanceType("r6g.large.search"),
					DedicatedMasterCount:   aws.Int32(3),
					ZoneAwarenessEnabled:   aws.Bool(true),
				},
				EncryptionAtRestOptions: &awsopensearchtypes.EncryptionAtRestOptions{
					Enabled:  aws.Bool(true),
					KmsKeyId: aws.String("arn:aws:kms:us-east-1:123456789012:key/orders"),
				},
				NodeToNodeEncryptionOptions: &awsopensearchtypes.NodeToNodeEncryptionOptions{Enabled: aws.Bool(true)},
				VPCOptions: &awsopensearchtypes.VPCDerivedInfo{
					VPCId:            aws.String("vpc-123"),
					SubnetIds:        []string{"subnet-a", "subnet-b"},
					SecurityGroupIds: []string{"sg-123"},
				},
				AdvancedSecurityOptions: &awsopensearchtypes.AdvancedSecurityOptions{
					Enabled:                     aws.Bool(true),
					InternalUserDatabaseEnabled: aws.Bool(false),
					SAMLOptions:                 &awsopensearchtypes.SAMLOptionsOutput{Enabled: aws.Bool(true)},
				},
				// AccessPolicies is an IAM resource policy body. The adapter must
				// extract role ARNs without persisting the policy body.
				AccessPolicies: aws.String(accessPolicy),
				// Endpoint is the domain HTTP endpoint; it must never be persisted.
				Endpoint: aws.String("vpc-orders-search.us-east-1.es.amazonaws.com"),
			}},
		},
		tags: map[string]*awsopensearch.ListTagsOutput{
			domainARN: {TagList: []awsopensearchtypes.Tag{{Key: aws.String("Environment"), Value: aws.String("prod")}}},
		},
	}
	client := newTestClient(domain, &fakeServerlessAPI{})

	domains, err := client.ListDomains(context.Background())
	if err != nil {
		t.Fatalf("ListDomains() error = %v", err)
	}
	if len(domains) != 1 {
		t.Fatalf("domains = %d, want 1", len(domains))
	}
	got := domains[0]
	if got.EngineVersion != "OpenSearch_2.11" {
		t.Fatalf("engine version = %q", got.EngineVersion)
	}
	if got.InstanceType != "r6g.large.search" || got.InstanceCount != 3 {
		t.Fatalf("instance type/count = %q/%d", got.InstanceType, got.InstanceCount)
	}
	if !got.EncryptionAtRestEnabled || !got.NodeToNodeEncryptionOn {
		t.Fatalf("encryption flags = %v/%v", got.EncryptionAtRestEnabled, got.NodeToNodeEncryptionOn)
	}
	if got.VPCID != "vpc-123" {
		t.Fatalf("vpc id = %q", got.VPCID)
	}
	if got.Tags["Environment"] != "prod" {
		t.Fatalf("tags = %#v", got.Tags)
	}
	if len(got.MasterUserRoleARNs) != 1 || got.MasterUserRoleARNs[0] != roleARN {
		t.Fatalf("master user role ARNs = %#v, want exactly [%q]", got.MasterUserRoleARNs, roleARN)
	}
}

func TestClientPaginatesPackagesAndAssociations(t *testing.T) {
	domain := &fakeDomainAPI{
		packagePages: []*awsopensearch.DescribePackagesOutput{
			{
				PackageDetailsList: []awsopensearchtypes.PackageDetails{{
					PackageID:     aws.String("F1"),
					PackageName:   aws.String("synonyms"),
					PackageType:   awsopensearchtypes.PackageTypeTxtDictionary,
					PackageStatus: awsopensearchtypes.PackageStatusAvailable,
				}},
				NextToken: aws.String("page2"),
			},
			{
				PackageDetailsList: []awsopensearchtypes.PackageDetails{{
					PackageID:   aws.String("F2"),
					PackageName: aws.String("stopwords"),
				}},
			},
		},
		associationPages: []*awsopensearch.ListDomainsForPackageOutput{{
			DomainPackageDetailsList: []awsopensearchtypes.DomainPackageDetails{{
				PackageID:           aws.String("F1"),
				DomainName:          aws.String("orders-search"),
				DomainPackageStatus: awsopensearchtypes.DomainPackageStatusActive,
			}},
		}},
	}
	client := newTestClient(domain, &fakeServerlessAPI{})

	packages, err := client.ListPackages(context.Background())
	if err != nil {
		t.Fatalf("ListPackages() error = %v", err)
	}
	if len(packages) != 2 {
		t.Fatalf("packages = %d, want 2 across two pages", len(packages))
	}
	if domain.describePackagesCalls != 2 {
		t.Fatalf("DescribePackages calls = %d, want 2", domain.describePackagesCalls)
	}

	associations, err := client.ListPackageAssociations(context.Background(), "F1")
	if err != nil {
		t.Fatalf("ListPackageAssociations() error = %v", err)
	}
	if len(associations) != 1 || associations[0].DomainName != "orders-search" {
		t.Fatalf("associations = %#v", associations)
	}
}

func TestClientListsServerlessCollectionsAndEndpoints(t *testing.T) {
	serverless := &fakeServerlessAPI{
		collectionList: &awsserverless.ListCollectionsOutput{
			CollectionSummaries: []awsserverlesstypes.CollectionSummary{{Id: aws.String("abc123")}},
		},
		collectionDetail: &awsserverless.BatchGetCollectionOutput{
			CollectionDetails: []awsserverlesstypes.CollectionDetail{{
				Arn:       aws.String("arn:aws:aoss:us-east-1:123456789012:collection/abc123"),
				Id:        aws.String("abc123"),
				Name:      aws.String("orders-vectors"),
				Type:      awsserverlesstypes.CollectionTypeVectorsearch,
				Status:    awsserverlesstypes.CollectionStatusActive,
				KmsKeyArn: aws.String("arn:aws:kms:us-east-1:123456789012:key/serverless"),
				// CollectionEndpoint and DashboardEndpoint must never be persisted.
				CollectionEndpoint: aws.String("https://abc123.us-east-1.aoss.amazonaws.com"),
				DashboardEndpoint:  aws.String("https://abc123.us-east-1.aoss.amazonaws.com/_dashboards"),
			}},
		},
		securityConfigList: &awsserverless.ListSecurityConfigsOutput{
			SecurityConfigSummaries: []awsserverlesstypes.SecurityConfigSummary{{
				Id:            aws.String("saml/orders/okta"),
				Type:          awsserverlesstypes.SecurityConfigTypeSaml,
				ConfigVersion: aws.String("MTcw"),
			}},
		},
		endpointList: &awsserverless.ListVpcEndpointsOutput{
			VpcEndpointSummaries: []awsserverlesstypes.VpcEndpointSummary{{Id: aws.String("vpce-aoss-123")}},
		},
		endpointDetail: &awsserverless.BatchGetVpcEndpointOutput{
			VpcEndpointDetails: []awsserverlesstypes.VpcEndpointDetail{{
				Id:               aws.String("vpce-aoss-123"),
				Name:             aws.String("orders-aoss-endpoint"),
				Status:           awsserverlesstypes.VpcEndpointStatusActive,
				VpcId:            aws.String("vpc-123"),
				SubnetIds:        []string{"subnet-a"},
				SecurityGroupIds: []string{"sg-456"},
			}},
		},
	}
	client := newTestClient(&fakeDomainAPI{}, serverless)

	collections, err := client.ListCollections(context.Background())
	if err != nil {
		t.Fatalf("ListCollections() error = %v", err)
	}
	if len(collections) != 1 || collections[0].Type != "VECTORSEARCH" {
		t.Fatalf("collections = %#v", collections)
	}
	if collections[0].KMSKeyARN != "arn:aws:kms:us-east-1:123456789012:key/serverless" {
		t.Fatalf("collection kms = %q", collections[0].KMSKeyARN)
	}

	configs, err := client.ListSecurityConfigs(context.Background())
	if err != nil {
		t.Fatalf("ListSecurityConfigs() error = %v", err)
	}
	if len(configs) == 0 || configs[0].Type != "saml" {
		t.Fatalf("security configs = %#v", configs)
	}

	endpoints, err := client.ListVPCEndpoints(context.Background())
	if err != nil {
		t.Fatalf("ListVPCEndpoints() error = %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].VPCID != "vpc-123" {
		t.Fatalf("endpoints = %#v", endpoints)
	}
}

func TestAccessPolicyRoleARNsHandlesShapes(t *testing.T) {
	roleARN := "arn:aws-cn:iam::123456789012:role/cn-admin"
	policy := `{"Statement":[{"Principal":{"AWS":"` + roleARN + `"}},{"Principal":"*"},{"Principal":{"Service":"es.amazonaws.com"}}]}`
	roles := accessPolicyRoleARNs(policy)
	if len(roles) != 1 || roles[0] != roleARN {
		t.Fatalf("roles = %#v, want partition-agnostic role ARN only", roles)
	}
	if accessPolicyRoleARNs("") != nil {
		t.Fatalf("empty policy must yield nil")
	}
	if accessPolicyRoleARNs("not json") != nil {
		t.Fatalf("invalid policy must yield nil, not panic")
	}
}

func newTestClient(domain domainAPI, serverless serverlessAPI) *Client {
	return &Client{
		domain:     domain,
		serverless: serverless,
		boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: awscloud.ServiceOpenSearch,
		},
	}
}

func emptyDomainStatus() awsopensearchtypes.DomainStatus {
	return awsopensearchtypes.DomainStatus{
		ARN:           aws.String("arn:aws:es:us-east-1:123456789012:domain/empty"),
		DomainName:    aws.String("empty"),
		ClusterConfig: &awsopensearchtypes.ClusterConfig{},
	}
}

type fakeDomainAPI struct {
	domainNames *awsopensearch.ListDomainNamesOutput
	domains     *awsopensearch.DescribeDomainsOutput
	tags        map[string]*awsopensearch.ListTagsOutput

	packagePages          []*awsopensearch.DescribePackagesOutput
	describePackagesCalls int

	associationPages      []*awsopensearch.ListDomainsForPackageOutput
	listDomainsForPkgCall int
}

func (f *fakeDomainAPI) ListDomainNames(context.Context, *awsopensearch.ListDomainNamesInput, ...func(*awsopensearch.Options)) (*awsopensearch.ListDomainNamesOutput, error) {
	return f.domainNames, nil
}

func (f *fakeDomainAPI) DescribeDomains(context.Context, *awsopensearch.DescribeDomainsInput, ...func(*awsopensearch.Options)) (*awsopensearch.DescribeDomainsOutput, error) {
	return f.domains, nil
}

func (f *fakeDomainAPI) DescribePackages(context.Context, *awsopensearch.DescribePackagesInput, ...func(*awsopensearch.Options)) (*awsopensearch.DescribePackagesOutput, error) {
	page := f.packagePages[f.describePackagesCalls]
	f.describePackagesCalls++
	return page, nil
}

func (f *fakeDomainAPI) ListDomainsForPackage(context.Context, *awsopensearch.ListDomainsForPackageInput, ...func(*awsopensearch.Options)) (*awsopensearch.ListDomainsForPackageOutput, error) {
	page := f.associationPages[f.listDomainsForPkgCall]
	f.listDomainsForPkgCall++
	return page, nil
}

func (f *fakeDomainAPI) ListTags(_ context.Context, in *awsopensearch.ListTagsInput, _ ...func(*awsopensearch.Options)) (*awsopensearch.ListTagsOutput, error) {
	if f.tags == nil {
		return &awsopensearch.ListTagsOutput{}, nil
	}
	if out, ok := f.tags[aws.ToString(in.ARN)]; ok {
		return out, nil
	}
	return &awsopensearch.ListTagsOutput{}, nil
}

type fakeServerlessAPI struct {
	collectionList     *awsserverless.ListCollectionsOutput
	collectionDetail   *awsserverless.BatchGetCollectionOutput
	securityConfigList *awsserverless.ListSecurityConfigsOutput
	endpointList       *awsserverless.ListVpcEndpointsOutput
	endpointDetail     *awsserverless.BatchGetVpcEndpointOutput

	securityConfigCalls int
}

func (f *fakeServerlessAPI) ListCollections(context.Context, *awsserverless.ListCollectionsInput, ...func(*awsserverless.Options)) (*awsserverless.ListCollectionsOutput, error) {
	return f.collectionList, nil
}

func (f *fakeServerlessAPI) BatchGetCollection(context.Context, *awsserverless.BatchGetCollectionInput, ...func(*awsserverless.Options)) (*awsserverless.BatchGetCollectionOutput, error) {
	return f.collectionDetail, nil
}

func (f *fakeServerlessAPI) ListSecurityConfigs(context.Context, *awsserverless.ListSecurityConfigsInput, ...func(*awsserverless.Options)) (*awsserverless.ListSecurityConfigsOutput, error) {
	// Only return the SAML config on the first type so the test asserts one
	// summary regardless of how many security config types AWS enumerates.
	f.securityConfigCalls++
	if f.securityConfigCalls == 1 {
		return f.securityConfigList, nil
	}
	return &awsserverless.ListSecurityConfigsOutput{}, nil
}

func (f *fakeServerlessAPI) ListVpcEndpoints(context.Context, *awsserverless.ListVpcEndpointsInput, ...func(*awsserverless.Options)) (*awsserverless.ListVpcEndpointsOutput, error) {
	return f.endpointList, nil
}

func (f *fakeServerlessAPI) BatchGetVpcEndpoint(context.Context, *awsserverless.BatchGetVpcEndpointInput, ...func(*awsserverless.Options)) (*awsserverless.BatchGetVpcEndpointOutput, error) {
	return f.endpointDetail, nil
}
