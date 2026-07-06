// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"fmt"
	"strings"
	"testing"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

var benchmarkPayloadSink map[string]any

const benchmarkFactKindAWSDNSRecord = "aws_dns_record"

// Benchmark Evidence: issue #4785 measured emit strategy cost with
// `go test -run '^$' -bench 'BenchmarkEmitStrategy|BenchmarkEmitRuntimeDecodeValidation' -benchmem -count=5`
// on Apple M5 Max. Median results showed existing encodeToPayload materially
// slower than inline maps and direct-map prototypes: ~4.7 us/op small DNS,
// ~13.0 us/op AWS resource, and ~554 us/op large parsed_file_data. Direct-map
// prototypes stayed within noise of inline maps. No-Observability-Change:
// benchmark-only proof; no production runtime behavior or telemetry changed.

func BenchmarkEmitStrategy(b *testing.B) {
	b.Run("small_dns_record", func(b *testing.B) {
		record := benchmarkDNSRecord()
		b.Run("inline_map", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkPayloadSink = benchmarkDNSRecordMap(record)
			}
		})
		b.Run("encode_existing", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				payload, err := encodeToPayload(record)
				if err != nil {
					b.Fatalf("encodeToPayload() error = %v", err)
				}
				benchmarkPayloadSink = payload
			}
		})
		b.Run("direct_map_prototype", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkPayloadSink = encodeBenchmarkDNSRecordDirect(record)
			}
		})
	})

	b.Run("medium_aws_resource", func(b *testing.B) {
		resource := benchmarkAWSResource()
		b.Run("inline_map", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkPayloadSink = benchmarkAWSResourceMap(resource)
			}
		})
		b.Run("encode_existing", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				payload, err := EncodeAWSResource(resource)
				if err != nil {
					b.Fatalf("EncodeAWSResource() error = %v", err)
				}
				benchmarkPayloadSink = payload
			}
		})
		b.Run("direct_map_prototype", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkPayloadSink = encodeAWSResourceDirect(resource)
			}
		})
	})

	b.Run("large_parsed_file_data", func(b *testing.B) {
		file := benchmarkCodegraphFile()
		b.Run("inline_map", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkPayloadSink = benchmarkCodegraphFileMap(file)
			}
		})
		b.Run("encode_existing", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				payload, err := EncodeCodegraphFile(file)
				if err != nil {
					b.Fatalf("EncodeCodegraphFile() error = %v", err)
				}
				benchmarkPayloadSink = payload
			}
		})
		b.Run("direct_map_prototype", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkPayloadSink = encodeCodegraphFileDirect(file)
			}
		})
	})
}

func BenchmarkEmitRuntimeDecodeValidation(b *testing.B) {
	dnsPayload := benchmarkDNSRecordMap(benchmarkDNSRecord())
	b.Run("small_dns_record", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := decodeAndValidate[benchmarkDNSRecordPayload](benchmarkFactKindAWSDNSRecord, dnsPayload); err != nil {
				b.Fatalf("decodeAndValidate() error = %v", err)
			}
		}
	})

	awsEnv := Envelope{FactKind: FactKindAWSResource, SchemaVersion: "1.0.0", Payload: benchmarkAWSResourceMap(benchmarkAWSResource())}
	b.Run("medium_aws_resource", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := DecodeAWSResource(awsEnv); err != nil {
				b.Fatalf("DecodeAWSResource() error = %v", err)
			}
		}
	})

	fileEnv := Envelope{FactKind: FactKindCodegraphFile, SchemaVersion: "1.0.0", Payload: benchmarkCodegraphFileMap(benchmarkCodegraphFile())}
	b.Run("large_parsed_file_data", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := DecodeCodegraphFile(fileEnv); err != nil {
				b.Fatalf("DecodeCodegraphFile() error = %v", err)
			}
		}
	})
}

type benchmarkDNSRecordPayload struct {
	AccountID            string         `json:"account_id"`
	Region               string         `json:"region"`
	ServiceKind          string         `json:"service_kind"`
	CollectorInstanceID  string         `json:"collector_instance_id"`
	HostedZoneID         string         `json:"hosted_zone_id"`
	HostedZoneName       string         `json:"hosted_zone_name,omitempty"`
	HostedZonePrivate    bool           `json:"hosted_zone_private,omitempty"`
	RecordName           string         `json:"record_name"`
	NormalizedRecordName string         `json:"normalized_record_name"`
	RecordType           string         `json:"record_type"`
	SetIdentifier        string         `json:"set_identifier,omitempty"`
	TTL                  *int64         `json:"ttl,omitempty"`
	Values               []string       `json:"values,omitempty"`
	AliasTarget          map[string]any `json:"alias_target,omitempty"`
	RoutingPolicy        map[string]any `json:"routing_policy,omitempty"`
	CorrelationAnchors   []string       `json:"correlation_anchors,omitempty"`
	HasAliasTarget       bool           `json:"has_alias_target"`
	SourceHostedZoneName string         `json:"source_hosted_zone_name,omitempty"`
}

func benchmarkDNSRecord() benchmarkDNSRecordPayload {
	ttl := int64(300)
	return benchmarkDNSRecordPayload{
		AccountID:            "123456789012",
		Region:               "us-east-1",
		ServiceKind:          "route53",
		CollectorInstanceID:  "collector-1",
		HostedZoneID:         "Z0123456789",
		HostedZoneName:       "example.com.",
		RecordName:           "api.example.com.",
		NormalizedRecordName: "api.example.com",
		RecordType:           "A",
		TTL:                  &ttl,
		Values:               []string{"203.0.113.10"},
		CorrelationAnchors:   []string{"api.example.com.", "api.example.com", "203.0.113.10"},
		SourceHostedZoneName: "example.com.",
	}
}

func benchmarkDNSRecordMap(record benchmarkDNSRecordPayload) map[string]any {
	return map[string]any{
		"account_id":              record.AccountID,
		"region":                  record.Region,
		"service_kind":            record.ServiceKind,
		"collector_instance_id":   record.CollectorInstanceID,
		"hosted_zone_id":          record.HostedZoneID,
		"hosted_zone_name":        record.HostedZoneName,
		"hosted_zone_private":     record.HostedZonePrivate,
		"record_name":             record.RecordName,
		"normalized_record_name":  record.NormalizedRecordName,
		"record_type":             record.RecordType,
		"ttl":                     *record.TTL,
		"values":                  record.Values,
		"correlation_anchors":     record.CorrelationAnchors,
		"has_alias_target":        record.HasAliasTarget,
		"source_hosted_zone_name": record.SourceHostedZoneName,
	}
}

func encodeBenchmarkDNSRecordDirect(record benchmarkDNSRecordPayload) map[string]any {
	payload := map[string]any{
		"account_id":             record.AccountID,
		"region":                 record.Region,
		"service_kind":           record.ServiceKind,
		"collector_instance_id":  record.CollectorInstanceID,
		"hosted_zone_id":         record.HostedZoneID,
		"record_name":            record.RecordName,
		"normalized_record_name": record.NormalizedRecordName,
		"record_type":            record.RecordType,
		"has_alias_target":       record.HasAliasTarget,
	}
	addString(payload, "hosted_zone_name", record.HostedZoneName)
	addBool(payload, "hosted_zone_private", record.HostedZonePrivate)
	addString(payload, "set_identifier", record.SetIdentifier)
	addInt64Ptr(payload, "ttl", record.TTL)
	addStringSlice(payload, "values", record.Values)
	addMap(payload, "alias_target", record.AliasTarget)
	addMap(payload, "routing_policy", record.RoutingPolicy)
	addStringSlice(payload, "correlation_anchors", record.CorrelationAnchors)
	addString(payload, "source_hosted_zone_name", record.SourceHostedZoneName)
	return payload
}

func benchmarkAWSResource() awsv1.Resource {
	tags := map[string]string{"env": "prod", "service": "supply-chain-demo"}
	return awsv1.Resource{
		AccountID:          "123456789012",
		ResourceID:         "arn:aws:ecs:us-east-1:123456789012:service/supply-chain-demo/supply-chain-demo",
		Region:             "us-east-1",
		ResourceType:       "ecs.service",
		ARN:                stringPtr("arn:aws:ecs:us-east-1:123456789012:service/supply-chain-demo/supply-chain-demo"),
		Name:               stringPtr("supply-chain-demo"),
		State:              stringPtr("active"),
		ServiceKind:        stringPtr("ecs"),
		CorrelationAnchors: []string{"arn:aws:ecs:us-east-1:123456789012:service/supply-chain-demo/supply-chain-demo"},
		Tags:               &tags,
		Attributes: map[string]any{
			"attributes": map[string]any{
				"cluster_arn":   "arn:aws:ecs:us-east-1:123456789012:cluster/supply-chain-demo",
				"desired_count": 2,
				"running_count": 2,
			},
		},
	}
}

func benchmarkAWSResourceMap(resource awsv1.Resource) map[string]any {
	payload := encodeAWSResourceDirect(resource)
	payload["collector_instance_id"] = "collector-1"
	return payload
}

func encodeAWSResourceDirect(resource awsv1.Resource) map[string]any {
	payload := map[string]any{
		"account_id":    resource.AccountID,
		"resource_id":   resource.ResourceID,
		"region":        resource.Region,
		"resource_type": resource.ResourceType,
	}
	addStringPtr(payload, "arn", resource.ARN)
	addStringPtr(payload, "name", resource.Name)
	addStringPtr(payload, "state", resource.State)
	addStringPtr(payload, "service_kind", resource.ServiceKind)
	addStringSlice(payload, "correlation_anchors", resource.CorrelationAnchors)
	if resource.Tags != nil {
		payload["tags"] = *resource.Tags
	}
	for key, value := range resource.Attributes {
		if _, known := resourceKnownPayloadKeys[key]; known {
			continue
		}
		if _, exists := payload[key]; !exists {
			payload[key] = value
		}
	}
	return payload
}

func benchmarkCodegraphFile() codegraphv1.File {
	return codegraphv1.File{
		RepoID:         "repo-example",
		RelativePath:   "src/service.go",
		ParsedFileData: largeParsedFileData(),
		GraphID:        stringPtr("repo-example:src/service.go"),
		GraphKind:      stringPtr("file"),
		IsDependency:   boolPtr(false),
		Language:       stringPtr("go"),
	}
}

func benchmarkCodegraphFileMap(file codegraphv1.File) map[string]any {
	return map[string]any{
		"repo_id":          file.RepoID,
		"relative_path":    file.RelativePath,
		"parsed_file_data": file.ParsedFileData,
		"graph_id":         *file.GraphID,
		"graph_kind":       *file.GraphKind,
		"is_dependency":    *file.IsDependency,
		"language":         *file.Language,
	}
}

func encodeCodegraphFileDirect(file codegraphv1.File) map[string]any {
	payload := map[string]any{
		"repo_id":          file.RepoID,
		"relative_path":    file.RelativePath,
		"parsed_file_data": file.ParsedFileData,
	}
	addStringPtr(payload, "graph_id", file.GraphID)
	addStringPtr(payload, "graph_kind", file.GraphKind)
	if file.IsDependency != nil {
		payload["is_dependency"] = *file.IsDependency
	}
	addStringPtr(payload, "language", file.Language)
	return payload
}

func largeParsedFileData() map[string]any {
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

var resourceKnownPayloadKeys = map[string]struct{}{
	"account_id": {}, "resource_id": {}, "region": {}, "resource_type": {},
	"arn": {}, "name": {}, "state": {}, "service_kind": {},
	"correlation_anchors": {}, "tags": {},
}

func addString(payload map[string]any, key string, value string) {
	if value != "" {
		payload[key] = value
	}
}

func addBool(payload map[string]any, key string, value bool) {
	if value {
		payload[key] = value
	}
}

func addStringPtr(payload map[string]any, key string, value *string) {
	if value != nil {
		payload[key] = *value
	}
}

func addInt64Ptr(payload map[string]any, key string, value *int64) {
	if value != nil {
		payload[key] = *value
	}
}

func addStringSlice(payload map[string]any, key string, value []string) {
	if len(value) > 0 {
		payload[key] = value
	}
}

func addMap(payload map[string]any, key string, value map[string]any) {
	if len(value) > 0 {
		payload[key] = value
	}
}

func stringPtr(value string) *string { return &value }

func boolPtr(value bool) *bool { return &value }
