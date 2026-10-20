// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicediscovery

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// serviceResourceID is the join key the Cloud Map service resource is keyed by:
// "namespaceName/serviceName". The App Mesh virtual-node service-discovery edge
// targets this exact identity with target_type aws_cloud_map_service, so the
// edge resolves only when the service resource uses the same id. It returns ""
// when either half is blank so a malformed service never emits a key that
// cannot join.
func serviceResourceID(service Service) string {
	namespace := strings.TrimSpace(service.NamespaceName)
	name := strings.TrimSpace(service.Name)
	if namespace == "" || name == "" {
		return ""
	}
	return namespace + "/" + name
}

// namespaceObservation maps one Cloud Map namespace into an aws_resource
// observation keyed by the Cloud Map namespace id.
func namespaceObservation(boundary awscloud.Boundary, namespace Namespace) awscloud.ResourceObservation {
	id := strings.TrimSpace(namespace.ID)
	arn := strings.TrimSpace(namespace.ARN)
	name := strings.TrimSpace(namespace.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeCloudMapNamespace,
		Name:         name,
		Tags:         cloneStringMap(namespace.Tags),
		Attributes: map[string]any{
			"namespace_id":   id,
			"namespace_name": name,
			"namespace_type": strings.TrimSpace(namespace.Type),
			"description":    strings.TrimSpace(namespace.Description),
			"service_count":  int64(namespace.ServiceCount),
			"hosted_zone_id": strings.TrimSpace(namespace.HostedZoneID),
			"http_name":      strings.TrimSpace(namespace.HTTPName),
			"created_at":     timeOrNil(namespace.CreatedAt),
		},
		CorrelationAnchors: correlationAnchors(id, name, arn),
		SourceRecordID:     id,
	}
}

// serviceObservation maps one Cloud Map service into an aws_resource
// observation keyed by "namespaceName/serviceName". It records the instance
// count only; instance attribute maps are never read or persisted.
func serviceObservation(boundary awscloud.Boundary, service Service) awscloud.ResourceObservation {
	resourceID := serviceResourceID(service)
	arn := strings.TrimSpace(service.ARN)
	name := strings.TrimSpace(service.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeCloudMapService,
		Name:         name,
		Tags:         cloneStringMap(service.Tags),
		Attributes: map[string]any{
			"service_id":         strings.TrimSpace(service.ID),
			"service_name":       name,
			"namespace_id":       strings.TrimSpace(service.NamespaceID),
			"namespace_name":     strings.TrimSpace(service.NamespaceName),
			"description":        strings.TrimSpace(service.Description),
			"instance_count":     int64(service.InstanceCount),
			"dns_routing_policy": strings.TrimSpace(service.DNSRoutingPolicy),
			"dns_records":        dnsRecordAttributes(service.DNSRecords),
			"created_at":         timeOrNil(service.CreatedAt),
		},
		CorrelationAnchors: correlationAnchors(resourceID, strings.TrimSpace(service.ID), arn),
		SourceRecordID:     resourceID,
	}
}

func dnsRecordAttributes(records []DNSRecord) []map[string]any {
	if len(records) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(records))
	for _, record := range records {
		entry := map[string]any{"type": strings.TrimSpace(record.Type)}
		if record.TTL != nil {
			entry["ttl"] = *record.TTL
		}
		result = append(result, entry)
	}
	return result
}

func correlationAnchors(values ...string) []string {
	anchors := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			anchors = append(anchors, trimmed)
		}
	}
	if len(anchors) == 0 {
		return nil
	}
	return anchors
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
