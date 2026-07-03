// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeComputeHealthCheck is declared in extractor_backend_service.go
// (the BackendService extractor's backend_service_uses_health_check edge
// resolves toward that same asset type as its target) and reused here; this
// file is the other side of that edge — the HealthCheck resource's own typed
// depth. RegisterAssetExtractor panics on a duplicate registration, so this
// file must never redeclare the constant.
func init() {
	RegisterAssetExtractor(assetTypeComputeHealthCheck, extractHealthCheck)
}

// healthCheckProtocolData is the bounded view of one protocol-specific
// sub-object (httpHealthCheck, httpsHealthCheck, tcpHealthCheck,
// sslHealthCheck, http2HealthCheck, grpcHealthCheck) inside a CAI
// compute.googleapis.com/HealthCheck resource.data blob. Only Port and
// PortSpecification are decoded. requestPath, host, response,
// proxyHeader, and grpcServiceName are data-plane routing/matching values,
// not typed depth for Terraform/edges/correlation/monitoring, and are
// dropped by omission per the GCP collector contract Payload Boundaries.
type healthCheckProtocolData struct {
	Port              *int64 `json:"port"`
	PortSpecification string `json:"portSpecification"`
}

// healthCheckData is the bounded view of a CAI
// compute.googleapis.com/HealthCheck resource.data blob. Type is a
// discriminator selecting exactly one of the six protocol-specific
// sub-objects below; GCP's REST API nests the port under the matching
// sub-object rather than at the top level, so
// healthCheckSelectedProtocol selects the right one after decode.
type healthCheckData struct {
	Type               string                   `json:"type"`
	CheckIntervalSec   *int64                   `json:"checkIntervalSec"`
	TimeoutSec         *int64                   `json:"timeoutSec"`
	HealthyThreshold   *int64                   `json:"healthyThreshold"`
	UnhealthyThreshold *int64                   `json:"unhealthyThreshold"`
	CreationTimestamp  string                   `json:"creationTimestamp"`
	HTTPHealthCheck    *healthCheckProtocolData `json:"httpHealthCheck"`
	HTTPSHealthCheck   *healthCheckProtocolData `json:"httpsHealthCheck"`
	TCPHealthCheck     *healthCheckProtocolData `json:"tcpHealthCheck"`
	SSLHealthCheck     *healthCheckProtocolData `json:"sslHealthCheck"`
	HTTP2HealthCheck   *healthCheckProtocolData `json:"http2HealthCheck"`
	GRPCHealthCheck    *healthCheckProtocolData `json:"grpcHealthCheck"`
}

// extractHealthCheck extracts bounded, redaction-safe typed depth for one
// Compute Engine HealthCheck CAI asset. It returns the Terraform/drift/
// monitoring attribute set (protocol type, check interval, timeout, healthy/
// unhealthy thresholds, creation time, and the protocol-specific port plus
// port specification). A HealthCheck derives no outbound relationships or
// correlation anchors from its own data: the graph value runs the other
// direction — a BackendService's backend_service_uses_health_check edge
// (extractor_backend_service.go) is inbound to this resource.
//
// requestPath, host, response, proxyHeader, and grpcServiceName are
// data-plane routing/matching values on the protocol sub-objects and are
// never decoded into Go memory; only Port and PortSpecification are read from
// whichever protocol sub-object matches Type.
func extractHealthCheck(ctx ExtractContext) (AttributeExtraction, error) {
	var data healthCheckData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode health check data: %w", err)
	}

	attrs := healthCheckAttributes(data)

	return AttributeExtraction{Attributes: attrs}, nil
}

// healthCheckAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a threshold or interval the provider never reported.
func healthCheckAttributes(data healthCheckData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Type); v != "" {
		attrs["type"] = v
	}
	if data.CheckIntervalSec != nil {
		attrs["check_interval_sec"] = *data.CheckIntervalSec
	}
	if data.TimeoutSec != nil {
		attrs["timeout_sec"] = *data.TimeoutSec
	}
	if data.HealthyThreshold != nil {
		attrs["healthy_threshold"] = *data.HealthyThreshold
	}
	if data.UnhealthyThreshold != nil {
		attrs["unhealthy_threshold"] = *data.UnhealthyThreshold
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}

	if proto := healthCheckSelectedProtocol(data); proto != nil {
		if proto.Port != nil {
			attrs["port"] = *proto.Port
		}
		if v := strings.TrimSpace(proto.PortSpecification); v != "" {
			attrs["port_specification"] = v
		}
	}

	return attrs
}

// healthCheckSelectedProtocol returns the one protocol-specific sub-object
// that carries this health check's port, selected by Type. GCP's REST API
// only ever populates the sub-object matching Type, but a partial or
// unexpected CAI page could carry more than one; Type is authoritative when
// present, and the function falls back to the first non-nil sub-object
// otherwise so a health check with a recognizable protocol payload but a
// missing/blank Type field is not silently dropped.
func healthCheckSelectedProtocol(data healthCheckData) *healthCheckProtocolData {
	switch strings.ToUpper(strings.TrimSpace(data.Type)) {
	case "HTTP":
		if data.HTTPHealthCheck != nil {
			return data.HTTPHealthCheck
		}
	case "HTTPS":
		if data.HTTPSHealthCheck != nil {
			return data.HTTPSHealthCheck
		}
	case "TCP":
		if data.TCPHealthCheck != nil {
			return data.TCPHealthCheck
		}
	case "SSL":
		if data.SSLHealthCheck != nil {
			return data.SSLHealthCheck
		}
	case "HTTP2":
		if data.HTTP2HealthCheck != nil {
			return data.HTTP2HealthCheck
		}
	case "GRPC":
		if data.GRPCHealthCheck != nil {
			return data.GRPCHealthCheck
		}
	}

	for _, candidate := range []*healthCheckProtocolData{
		data.HTTPHealthCheck,
		data.HTTPSHealthCheck,
		data.TCPHealthCheck,
		data.SSLHealthCheck,
		data.HTTP2HealthCheck,
		data.GRPCHealthCheck,
	} {
		if candidate != nil {
			return candidate
		}
	}
	return nil
}
