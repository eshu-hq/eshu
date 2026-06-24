// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	sdtypes "github.com/aws/aws-sdk-go-v2/service/servicediscovery/types"

	sdservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/servicediscovery"
)

// mapNamespace maps one Cloud Map NamespaceSummary into a scanner-owned
// metadata record. Services are attached separately by the client. The Route 53
// hosted-zone id is read from the DNS namespace properties when present; HTTP
// namespaces carry the HTTP discovery name instead.
func mapNamespace(summary sdtypes.NamespaceSummary) sdservice.Namespace {
	namespace := sdservice.Namespace{
		ID:           aws.ToString(summary.Id),
		ARN:          aws.ToString(summary.Arn),
		Name:         aws.ToString(summary.Name),
		Type:         string(summary.Type),
		Description:  aws.ToString(summary.Description),
		ServiceCount: aws.ToInt32(summary.ServiceCount),
		CreatedAt:    timeOrZero(summary.CreateDate),
	}
	if summary.Properties != nil {
		if dns := summary.Properties.DnsProperties; dns != nil {
			namespace.HostedZoneID = aws.ToString(dns.HostedZoneId)
		}
		if http := summary.Properties.HttpProperties; http != nil {
			namespace.HTTPName = aws.ToString(http.HttpName)
		}
	}
	return namespace
}

// mapService maps one Cloud Map ServiceSummary into a scanner-owned metadata
// record. The instance count is taken from the summary; instance attribute
// maps are never read. The parent namespace id and name are supplied by the
// caller because the service summary does not carry the namespace id.
func mapService(summary sdtypes.ServiceSummary, namespaceID, namespaceName string) sdservice.Service {
	service := sdservice.Service{
		ID:            aws.ToString(summary.Id),
		ARN:           aws.ToString(summary.Arn),
		Name:          aws.ToString(summary.Name),
		NamespaceID:   namespaceID,
		NamespaceName: namespaceName,
		Description:   aws.ToString(summary.Description),
		InstanceCount: aws.ToInt32(summary.InstanceCount),
		CreatedAt:     timeOrZero(summary.CreateDate),
	}
	if summary.DnsConfig != nil {
		service.DNSRoutingPolicy = string(summary.DnsConfig.RoutingPolicy)
		service.DNSRecords = mapDNSRecords(summary.DnsConfig.DnsRecords)
	}
	return service
}

func mapDNSRecords(records []sdtypes.DnsRecord) []sdservice.DNSRecord {
	if len(records) == 0 {
		return nil
	}
	mapped := make([]sdservice.DNSRecord, 0, len(records))
	for _, record := range records {
		mapped = append(mapped, sdservice.DNSRecord{
			Type: string(record.Type),
			TTL:  record.TTL,
		})
	}
	return mapped
}

func tagsToMap(tags []sdtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := aws.ToString(tag.Key)
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
