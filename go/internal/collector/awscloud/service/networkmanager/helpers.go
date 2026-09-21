// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package networkmanager

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// globalNetworkResourceID returns the resource_id the global-network node
// publishes. It prefers the API-reported ARN (Network Manager always returns
// one) and synthesizes the partition-aware ARN from the boundary account when a
// fixture omits it, so child parent edges key the same value the node publishes.
func globalNetworkResourceID(boundary awscloud.Boundary, network GlobalNetwork) string {
	if arn := strings.TrimSpace(network.ARN); arn != "" {
		return arn
	}
	return globalNetworkARN(boundary, network.ID)
}

// globalNetworkARN synthesizes the partition-aware ARN for a global-network id,
// matching the region-less Network Manager ARN shape AWS publishes
// (arn:<partition>:networkmanager::<account>:global-network/<id>). It returns ""
// when the id is empty.
func globalNetworkARN(boundary awscloud.Boundary, globalNetworkID string) string {
	id := strings.TrimSpace(globalNetworkID)
	if id == "" {
		return ""
	}
	return networkManagerARN(boundary, "global-network/"+id)
}

// siteARN synthesizes the partition-aware ARN for a site id within a global
// network, matching the Network Manager ARN shape
// (arn:<partition>:networkmanager::<account>:site/<global-network-id>/<site-id>).
// It returns "" when either id is empty.
func siteARN(boundary awscloud.Boundary, globalNetworkID, siteID string) string {
	gnID := strings.TrimSpace(globalNetworkID)
	id := strings.TrimSpace(siteID)
	if gnID == "" || id == "" {
		return ""
	}
	return networkManagerARN(boundary, "site/"+gnID+"/"+id)
}

// deviceARN synthesizes the partition-aware ARN for a device id within a global
// network, matching the Network Manager ARN shape. It returns "" when either id
// is empty.
func deviceARN(boundary awscloud.Boundary, globalNetworkID, deviceID string) string {
	gnID := strings.TrimSpace(globalNetworkID)
	id := strings.TrimSpace(deviceID)
	if gnID == "" || id == "" {
		return ""
	}
	return networkManagerARN(boundary, "device/"+gnID+"/"+id)
}

// linkARN synthesizes the partition-aware ARN for a link id within a global
// network, matching the Network Manager ARN shape. It returns "" when either id
// is empty.
func linkARN(boundary awscloud.Boundary, globalNetworkID, linkID string) string {
	gnID := strings.TrimSpace(globalNetworkID)
	id := strings.TrimSpace(linkID)
	if gnID == "" || id == "" {
		return ""
	}
	return networkManagerARN(boundary, "link/"+gnID+"/"+id)
}

// networkManagerARN builds a region-less Network Manager ARN for resourcePath
// using the boundary partition and account. Network Manager is global, so the
// ARN region segment is empty; the partition is derived from the boundary so
// GovCloud and China edges resolve instead of dangling. It returns "" when the
// account id is missing, since a parent edge keyed to an account-less ARN would
// never join the real node.
func networkManagerARN(boundary awscloud.Boundary, resourcePath string) string {
	account := strings.TrimSpace(boundary.AccountID)
	if account == "" {
		return ""
	}
	partition := awscloud.PartitionForBoundary(boundary)
	return "arn:" + partition + ":networkmanager::" + account + ":" + resourcePath
}

// transitGatewayID extracts the bare transit gateway id (tgw-...) from a transit
// gateway ARN. The transit gateway scanner publishes its resource_id as the bare
// id, not an ARN, so registration edges must key the id rather than the ARN.
// Network Manager reports the ARN
// (arn:<partition>:ec2:<region>:<account>:transit-gateway/tgw-...), so the id is
// the final path segment. It returns "" when no id can be extracted.
func transitGatewayID(transitGatewayARN string) string {
	arn := strings.TrimSpace(transitGatewayARN)
	if arn == "" {
		return ""
	}
	if idx := strings.LastIndex(arn, "/"); idx >= 0 {
		arn = arn[idx+1:]
	}
	return strings.TrimSpace(arn)
}

// isARN reports whether value carries the canonical AWS ARN prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// timeOrNil returns the UTC time when value is set, or nil for the zero time so
// the attribute payload omits an unknown timestamp instead of emitting an epoch.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// cloneStringMap returns a trimmed-key copy of input, or nil when it is empty or
// every key trims to empty, keeping omitempty-style payload behavior consistent.
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

// cloneStrings returns a trimmed copy of input with empty entries dropped, or
// nil when nothing survives.
func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
