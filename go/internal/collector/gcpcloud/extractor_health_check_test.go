// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const healthCheckFullName = "//compute.googleapis.com/projects/demo-project/global/healthChecks/http-hc"

func healthCheckContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: healthCheckFullName,
		AssetType:        assetTypeComputeHealthCheck,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestHealthCheckExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeHealthCheck); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeHealthCheck)
	}
}

func TestExtractHealthCheckHTTPFullAttributes(t *testing.T) {
	const data = `{
		"name": "http-hc",
		"type": "HTTP",
		"checkIntervalSec": 5,
		"timeoutSec": 5,
		"healthyThreshold": 2,
		"unhealthyThreshold": 2,
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00",
		"httpHealthCheck": {
			"port": 80,
			"portSpecification": "USE_FIXED_PORT",
			"requestPath": "/healthz",
			"host": "internal.example.com"
		}
	}`

	got, err := extractHealthCheck(healthCheckContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"type":                "HTTP",
		"check_interval_sec":  int64(5),
		"timeout_sec":         int64(5),
		"healthy_threshold":   int64(2),
		"unhealthy_threshold": int64(2),
		"creation_time":       "2024-06-01T07:00:00Z",
		"port":                int64(80),
		"port_specification":  "USE_FIXED_PORT",
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("health check derives no outbound edges (backend services are inbound), got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("health check derives no outbound anchors, got %#v", got.CorrelationAnchors)
	}
	if _, leaked := got.Attributes["request_path"]; leaked {
		t.Errorf("requestPath must never be persisted: %#v", got.Attributes)
	}
	if _, leaked := got.Attributes["host"]; leaked {
		t.Errorf("host must never be persisted: %#v", got.Attributes)
	}
}

func TestExtractHealthCheckHTTPSPort(t *testing.T) {
	const data = `{
		"type": "HTTPS",
		"httpsHealthCheck": {"port": 443}
	}`
	got, err := extractHealthCheck(healthCheckContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["port"] != int64(443) {
		t.Errorf("port = %v, want 443", got.Attributes["port"])
	}
	if got.Attributes["type"] != "HTTPS" {
		t.Errorf("type = %v, want HTTPS", got.Attributes["type"])
	}
}

func TestExtractHealthCheckTCPPort(t *testing.T) {
	const data = `{
		"type": "TCP",
		"tcpHealthCheck": {"port": 8080}
	}`
	got, err := extractHealthCheck(healthCheckContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["port"] != int64(8080) {
		t.Errorf("port = %v, want 8080", got.Attributes["port"])
	}
}

func TestExtractHealthCheckSSLPort(t *testing.T) {
	const data = `{
		"type": "SSL",
		"sslHealthCheck": {"port": 8443}
	}`
	got, err := extractHealthCheck(healthCheckContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["port"] != int64(8443) {
		t.Errorf("port = %v, want 8443", got.Attributes["port"])
	}
}

func TestExtractHealthCheckHTTP2Port(t *testing.T) {
	const data = `{
		"type": "HTTP2",
		"http2HealthCheck": {"port": 8444}
	}`
	got, err := extractHealthCheck(healthCheckContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["port"] != int64(8444) {
		t.Errorf("port = %v, want 8444", got.Attributes["port"])
	}
}

func TestExtractHealthCheckGRPCPort(t *testing.T) {
	const data = `{
		"type": "GRPC",
		"grpcHealthCheck": {"port": 50051, "grpcServiceName": "health.v1"}
	}`
	got, err := extractHealthCheck(healthCheckContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["port"] != int64(50051) {
		t.Errorf("port = %v, want 50051", got.Attributes["port"])
	}
	if _, leaked := got.Attributes["grpc_service_name"]; leaked {
		t.Errorf("grpcServiceName must never be persisted: %#v", got.Attributes)
	}
}

func TestExtractHealthCheckAbsentPortOmitted(t *testing.T) {
	const data = `{"type": "TCP", "tcpHealthCheck": {}}`
	got, err := extractHealthCheck(healthCheckContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["port"]; ok {
		t.Errorf("absent port must be omitted: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["port_specification"]; ok {
		t.Errorf("absent port_specification must be omitted: %#v", got.Attributes)
	}
}

func TestExtractHealthCheckEmptyDataYieldsNothing(t *testing.T) {
	got, err := extractHealthCheck(healthCheckContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for empty data, got %#v", got.Relationships)
	}
}

func TestExtractHealthCheckMalformedDataErrors(t *testing.T) {
	if _, err := extractHealthCheck(healthCheckContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}
