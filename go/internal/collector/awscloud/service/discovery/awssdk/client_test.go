// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssd "github.com/aws/aws-sdk-go-v2/service/servicediscovery"
	sdtypes "github.com/aws/aws-sdk-go-v2/service/servicediscovery/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIClientInterfaceExcludesMutationAndInstanceReaders is the security gate
// for the Cloud Map SDK adapter. The scanner contract forbids every mutation
// API and every instance discovery/read API that exposes instance attribute
// maps. This test reflects over the adapter's internal apiClient interface and
// FAILS if a future SDK refactor adds any of them.
func TestAPIClientInterfaceExcludesMutationAndInstanceReaders(t *testing.T) {
	apiClientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{
		// Namespace mutations.
		"CreateHttpNamespace", "CreatePrivateDnsNamespace", "CreatePublicDnsNamespace",
		"DeleteNamespace", "UpdateHttpNamespace", "UpdatePrivateDnsNamespace", "UpdatePublicDnsNamespace",
		// Service mutations.
		"CreateService", "DeleteService", "UpdateService",
		"DeleteServiceAttributes", "UpdateServiceAttributes",
		// Instance mutations.
		"RegisterInstance", "DeregisterInstance", "UpdateInstanceCustomHealthStatus",
		// Tag mutations.
		"TagResource", "UntagResource",
		// Instance attribute readers (can carry caller-defined secrets).
		"GetInstance", "ListInstances", "GetInstancesHealthStatus",
		"DiscoverInstances", "DiscoverInstancesRevision",
	}
	for _, name := range forbidden {
		if _, ok := apiClientType.MethodByName(name); ok {
			t.Fatalf("apiClient interface exposes %q; Cloud Map scanner forbids that API", name)
		}
	}
}

// fakeAPI is a read-only Cloud Map API stub. It records the filters passed to
// ListServices so the test can prove the NAMESPACE_ID scope is applied.
type fakeAPI struct {
	namespacePages []*awssd.ListNamespacesOutput
	servicesByNS   map[string][]sdtypes.ServiceSummary
	tagsByARN      map[string][]sdtypes.Tag
	serviceFilters [][]sdtypes.ServiceFilter
}

func (f *fakeAPI) ListNamespaces(_ context.Context, _ *awssd.ListNamespacesInput, _ ...func(*awssd.Options)) (*awssd.ListNamespacesOutput, error) {
	if len(f.namespacePages) == 0 {
		return &awssd.ListNamespacesOutput{}, nil
	}
	page := f.namespacePages[0]
	f.namespacePages = f.namespacePages[1:]
	return page, nil
}

func (f *fakeAPI) ListServices(_ context.Context, input *awssd.ListServicesInput, _ ...func(*awssd.Options)) (*awssd.ListServicesOutput, error) {
	f.serviceFilters = append(f.serviceFilters, input.Filters)
	var namespaceID string
	for _, filter := range input.Filters {
		if filter.Name == sdtypes.ServiceFilterNameNamespaceId && len(filter.Values) > 0 {
			namespaceID = filter.Values[0]
		}
	}
	return &awssd.ListServicesOutput{Services: f.servicesByNS[namespaceID]}, nil
}

func (f *fakeAPI) ListTagsForResource(_ context.Context, input *awssd.ListTagsForResourceInput, _ ...func(*awssd.Options)) (*awssd.ListTagsForResourceOutput, error) {
	return &awssd.ListTagsForResourceOutput{Tags: f.tagsByARN[aws.ToString(input.ResourceARN)]}, nil
}

func newClientWithFake(api apiClient) *Client {
	return &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceServiceDiscovery},
	}
}

// TestListNamespaceInventoryResolvesServicesWithCountOnly proves the adapter
// pages namespaces, scopes ListServices by NAMESPACE_ID, attaches services with
// the reported instance count, and never reads an instance attribute map.
func TestListNamespaceInventoryResolvesServicesWithCountOnly(t *testing.T) {
	nsARN := "arn:aws:servicediscovery:us-east-1:123456789012:namespace/ns-1"
	svcARN := "arn:aws:servicediscovery:us-east-1:123456789012:service/srv-1"
	ttl := int64(60)
	api := &fakeAPI{
		namespacePages: []*awssd.ListNamespacesOutput{{
			Namespaces: []sdtypes.NamespaceSummary{{
				Id:           aws.String("ns-1"),
				Arn:          aws.String(nsARN),
				Name:         aws.String("apps.local"),
				Type:         sdtypes.NamespaceTypeDnsPrivate,
				ServiceCount: aws.Int32(1),
				Properties: &sdtypes.NamespaceProperties{
					DnsProperties: &sdtypes.DnsProperties{HostedZoneId: aws.String("Z123")},
				},
			}},
		}},
		servicesByNS: map[string][]sdtypes.ServiceSummary{
			"ns-1": {{
				Id:            aws.String("srv-1"),
				Arn:           aws.String(svcARN),
				Name:          aws.String("checkout"),
				InstanceCount: aws.Int32(3),
				DnsConfig: &sdtypes.DnsConfig{
					RoutingPolicy: sdtypes.RoutingPolicyMultivalue,
					DnsRecords:    []sdtypes.DnsRecord{{Type: sdtypes.RecordTypeA, TTL: &ttl}},
				},
			}},
		},
		tagsByARN: map[string][]sdtypes.Tag{
			nsARN:  {{Key: aws.String("Environment"), Value: aws.String("prod")}},
			svcARN: {{Key: aws.String("team"), Value: aws.String("payments")}},
		},
	}

	namespaces, err := newClientWithFake(api).ListNamespaceInventory(context.Background())
	if err != nil {
		t.Fatalf("ListNamespaceInventory() error = %v", err)
	}
	if len(namespaces) != 1 {
		t.Fatalf("namespaces = %d, want 1", len(namespaces))
	}
	ns := namespaces[0]
	if ns.HostedZoneID != "Z123" || ns.Type != "DNS_PRIVATE" {
		t.Fatalf("namespace = %#v, want DNS_PRIVATE with hosted zone Z123", ns)
	}
	if ns.Tags["Environment"] != "prod" {
		t.Fatalf("namespace tags = %#v, want Environment=prod", ns.Tags)
	}
	if len(ns.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(ns.Services))
	}
	svc := ns.Services[0]
	if svc.InstanceCount != 3 {
		t.Fatalf("instance_count = %d, want 3", svc.InstanceCount)
	}
	if svc.NamespaceID != "ns-1" || svc.NamespaceName != "apps.local" {
		t.Fatalf("service namespace = %q/%q, want ns-1/apps.local", svc.NamespaceID, svc.NamespaceName)
	}
	if svc.DNSRoutingPolicy != "MULTIVALUE" || len(svc.DNSRecords) != 1 || svc.DNSRecords[0].Type != "A" {
		t.Fatalf("service dns config = %#v, want MULTIVALUE A record", svc)
	}

	if len(api.serviceFilters) != 1 {
		t.Fatalf("ListServices calls = %d, want 1", len(api.serviceFilters))
	}
	filters := api.serviceFilters[0]
	if len(filters) != 1 || filters[0].Name != sdtypes.ServiceFilterNameNamespaceId || filters[0].Values[0] != "ns-1" {
		t.Fatalf("ListServices filters = %#v, want NAMESPACE_ID=ns-1", filters)
	}
}

// TestListNamespaceInventoryTrimsNamespaceIDForServiceFilter proves the adapter
// scopes ListServices with the trimmed namespace id even when the SDK returns a
// namespace id with surrounding whitespace. The id is validated after trimming
// at the top of the loop; the ListServices filter and the attached services
// must use that same trimmed value, or services would be filtered with a value
// that never matches and would attach to a namespace keyed on a different id.
func TestListNamespaceInventoryTrimsNamespaceIDForServiceFilter(t *testing.T) {
	const trimmedID = "ns-1"
	nsARN := "arn:aws:servicediscovery:us-east-1:123456789012:namespace/ns-1"
	svcARN := "arn:aws:servicediscovery:us-east-1:123456789012:service/srv-1"
	api := &fakeAPI{
		namespacePages: []*awssd.ListNamespacesOutput{{
			Namespaces: []sdtypes.NamespaceSummary{{
				Id:   aws.String("  ns-1  "),
				Arn:  aws.String(nsARN),
				Name: aws.String("  apps.local  "),
				Type: sdtypes.NamespaceTypeDnsPrivate,
			}},
		}},
		servicesByNS: map[string][]sdtypes.ServiceSummary{
			trimmedID: {{
				Id:   aws.String("srv-1"),
				Arn:  aws.String(svcARN),
				Name: aws.String("checkout"),
			}},
		},
	}

	namespaces, err := newClientWithFake(api).ListNamespaceInventory(context.Background())
	if err != nil {
		t.Fatalf("ListNamespaceInventory() error = %v", err)
	}
	if len(api.serviceFilters) != 1 {
		t.Fatalf("ListServices calls = %d, want 1", len(api.serviceFilters))
	}
	filters := api.serviceFilters[0]
	if len(filters) != 1 || filters[0].Values[0] != trimmedID {
		t.Fatalf("ListServices filter value = %#v, want trimmed %q", filters, trimmedID)
	}
	if len(namespaces) != 1 || len(namespaces[0].Services) != 1 {
		t.Fatalf("namespaces/services = %#v, want one service attached via trimmed id", namespaces)
	}
	if got := namespaces[0].Services[0].NamespaceID; got != trimmedID {
		t.Fatalf("service NamespaceID = %q, want trimmed %q", got, trimmedID)
	}
	if got := namespaces[0].Services[0].NamespaceName; got != "apps.local" {
		t.Fatalf("service NamespaceName = %q, want trimmed %q", got, "apps.local")
	}
}
