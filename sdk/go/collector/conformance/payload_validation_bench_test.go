// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package conformance

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

var benchmarkValidationErrSink error

func BenchmarkPayloadSchemaValidation(b *testing.B) {
	cases := []struct {
		name    string
		schema  json.RawMessage
		payload map[string]any
	}{
		{
			name:    "small_dns_record",
			schema:  json.RawMessage(benchmarkSmallDNSRecordSchema),
			payload: benchmarkSmallDNSRecordPayload(),
		},
		{
			name:    "medium_aws_resource",
			schema:  json.RawMessage(benchmarkAWSResourceSchema),
			payload: benchmarkAWSResourcePayload(),
		},
		{
			name:    "large_parsed_file_data",
			schema:  json.RawMessage(benchmarkFileSchema),
			payload: benchmarkLargeFilePayload(),
		},
	}
	for _, tc := range cases {
		schema, err := compileSchema(tc.schema)
		if err != nil {
			b.Fatalf("compileSchema(%s) error = %v", tc.name, err)
		}
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkValidationErrSink = schema.validatePayload(tc.payload)
				if benchmarkValidationErrSink != nil {
					b.Fatalf("validatePayload() error = %v", benchmarkValidationErrSink)
				}
			}
		})
	}
}

func benchmarkSmallDNSRecordPayload() map[string]any {
	return map[string]any{
		"account_id":              "123456789012",
		"region":                  "us-east-1",
		"service_kind":            "route53",
		"collector_instance_id":   "collector-1",
		"hosted_zone_id":          "Z0123456789",
		"hosted_zone_name":        "example.com.",
		"hosted_zone_private":     false,
		"record_name":             "api.example.com.",
		"normalized_record_name":  "api.example.com",
		"record_type":             "A",
		"ttl":                     int64(300),
		"values":                  []string{"203.0.113.10"},
		"correlation_anchors":     []string{"api.example.com.", "api.example.com", "203.0.113.10"},
		"has_alias_target":        false,
		"source_hosted_zone_name": "example.com.",
	}
}

func benchmarkAWSResourcePayload() map[string]any {
	return map[string]any{
		"arn":           "arn:aws:ecs:us-east-1:123456789012:service/supply-chain-demo/supply-chain-demo",
		"resource_id":   "arn:aws:ecs:us-east-1:123456789012:service/supply-chain-demo/supply-chain-demo",
		"resource_type": "ecs.service",
		"name":          "supply-chain-demo",
		"state":         "active",
		"account_id":    "123456789012",
		"region":        "us-east-1",
		"service_kind":  "ecs",
		"tags":          map[string]string{"env": "prod", "service": "supply-chain-demo"},
		"correlation_anchors": []string{
			"arn:aws:ecs:us-east-1:123456789012:service/supply-chain-demo/supply-chain-demo",
		},
		"attributes": map[string]any{
			"cluster_arn":   "arn:aws:ecs:us-east-1:123456789012:cluster/supply-chain-demo",
			"desired_count": 2,
			"running_count": 2,
		},
	}
}

func benchmarkLargeFilePayload() map[string]any {
	return map[string]any{
		"repo_id":          "repo-example",
		"relative_path":    "src/service.go",
		"parsed_file_data": benchmarkLargeParsedFileData(),
		"graph_id":         "repo-example:src/service.go",
		"graph_kind":       "file",
		"is_dependency":    false,
		"language":         "go",
	}
}

func benchmarkLargeParsedFileData() map[string]any {
	imports := make([]any, 0, 80)
	functions := make([]any, 0, 120)
	calls := make([]any, 0, 240)
	for i := 0; i < 80; i++ {
		imports = append(imports, map[string]any{"source": fmt.Sprintf("pkg/%03d", i), "alias": fmt.Sprintf("p%d", i)})
	}
	for i := 0; i < 120; i++ {
		name := fmt.Sprintf("Function%03d", i)
		functions = append(functions, map[string]any{
			"name": name, "line": i + 10, "signature": "func " + name + "(context.Context) error",
			"annotations": []any{"handler", "reducer", "materializer"},
		})
	}
	for i := 0; i < 240; i++ {
		calls = append(calls, map[string]any{
			"caller": fmt.Sprintf("Function%03d", i%120),
			"callee": fmt.Sprintf("Dependency%03d", i),
			"line":   i + 20,
			"args":   []any{"ctx", "row", strings.Repeat("x", 24)},
		})
	}
	return map[string]any{
		"path":           "src/service.go",
		"language":       "go",
		"imports":        imports,
		"functions":      functions,
		"function_calls": calls,
		"classes":        []any{},
		"variables":      []any{},
	}
}

const benchmarkSmallDNSRecordSchema = `{
  "$id": "https://eshu.dev/schemas/bench/aws-dns-record.schema.json",
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "additionalProperties": true,
  "properties": {
    "account_id": {"type": "string"},
    "region": {"type": "string"},
    "service_kind": {"type": "string"},
    "collector_instance_id": {"type": "string"},
    "hosted_zone_id": {"type": "string"},
    "hosted_zone_name": {"type": ["string", "null"]},
    "hosted_zone_private": {"type": ["boolean", "null"]},
    "record_name": {"type": "string"},
    "normalized_record_name": {"type": "string"},
    "record_type": {"type": "string"},
    "set_identifier": {"type": ["string", "null"]},
    "ttl": {"type": ["integer", "null"]},
    "values": {"items": {"type": "string"}, "type": ["array", "null"]},
    "correlation_anchors": {"items": {"type": "string"}, "type": ["array", "null"]},
    "has_alias_target": {"type": "boolean"},
    "source_hosted_zone_name": {"type": ["string", "null"]}
  },
  "required": ["account_id", "region", "service_kind", "collector_instance_id", "hosted_zone_id", "record_name", "normalized_record_name", "record_type", "has_alias_target"],
  "title": "Eshu aws_dns_record-shaped Benchmark Payload",
  "type": "object"
}`

const benchmarkAWSResourceSchema = `{
  "$id": "https://eshu.dev/schemas/factschema/aws/v1/resource.schema.json",
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "additionalProperties": true,
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "arn": {"type": ["string", "null"]},
    "name": {"type": ["string", "null"]},
    "state": {"type": ["string", "null"]},
    "service_kind": {"type": ["string", "null"]},
    "correlation_anchors": {"items": {"type": "string"}, "type": ["array", "null"]},
    "tags": {"additionalProperties": {"type": "string"}, "type": ["object", "null"]},
    "attributes": {"type": ["object", "null"]}
  },
  "required": ["account_id", "resource_id", "region", "resource_type"],
  "title": "Eshu aws_resource Payload (schema version 1)",
  "type": "object"
}`

const benchmarkFileSchema = `{
  "$id": "https://eshu.dev/schemas/factschema/codegraph/v1/file.schema.json",
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "additionalProperties": true,
  "properties": {
    "graph_id": {"type": ["string", "null"]},
    "graph_kind": {"type": ["string", "null"]},
    "is_dependency": {"type": ["boolean", "null"]},
    "language": {"type": ["string", "null"]},
    "parsed_file_data": {"type": "object"},
    "relative_path": {"type": "string"},
    "repo_id": {"type": "string"}
  },
  "required": ["repo_id", "relative_path", "parsed_file_data"],
  "title": "Eshu file Payload (schema version 1)",
  "type": "object"
}`
